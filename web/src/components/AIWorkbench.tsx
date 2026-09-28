import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Brain,
  ChatCircle,
  CircleNotch,
  ListBullets,
  Stop,
  Tag,
  TextAlignLeft,
  Translate,
  X,
} from "@phosphor-icons/react"
import { type FormEvent, type PointerEvent, useEffect, useState } from "react"

import {
  cancelJob,
  getAIChat,
  getJob,
  listAIResults,
  runAIOperation,
  startAIChat,
  startAILibraryChat,
} from "../api/client"
import type { AIChatSession, AIOperation, AIProfile, AIResult, ListResponse } from "../api/types"
import { formatAIResult } from "../lib/ai"
import { useTranslation } from "../lib/i18n"
import { AIIcon } from "./AIIcon"

interface AIWorkbenchProps {
  entryID?: string
  entryIDs?: string[]
  profiles: AIProfile[]
  width: number
  contextLabel: string
  initialMode?: AIOperation | "chat"
  onWidthChange: (width: number) => void
  onClose: () => void
  onConfigure: () => void
}

const operations: Array<{ id: AIOperation; labelKey: string; icon: typeof Brain }> = [
  { id: "summary", labelKey: "summary", icon: TextAlignLeft },
  { id: "translation", labelKey: "translate", icon: Translate },
  { id: "key_points", labelKey: "keyPoints", icon: ListBullets },
  { id: "academic_tags", labelKey: "automaticTags", icon: Tag },
]

export function AIWorkbench(props: AIWorkbenchProps) {
  const { locale, t } = useTranslation()
  const queryClient = useQueryClient()
  const articleMode = Boolean(props.entryID)
  const [mode, setMode] = useState<AIOperation | "chat">(
    props.initialMode ?? (articleMode ? "summary" : "chat"),
  )
  const [profileID, setProfileID] = useState("")
  const [language, setLanguage] = useState(() => (locale === "zh-CN" ? "Chinese" : "English"))
  const [pendingJobID, setPendingJobID] = useState("")
  const [pendingOperation, setPendingOperation] = useState<AIOperation | null>(null)
  const [jobFailure, setJobFailure] = useState<string | null>(null)
  const [sessionID, setSessionID] = useState("")
  const [message, setMessage] = useState("")
  const activeProfile =
    props.profiles.find((profile) => profile.id === profileID && profile.enabled) ??
    props.profiles.find((profile) => profile.is_default && profile.enabled) ??
    props.profiles.find((profile) => profile.enabled)
  const activeProfileID = activeProfile?.id ?? ""

  const job = useQuery({
    queryKey: ["job", pendingJobID],
    queryFn: ({ signal }) => getJob(pendingJobID, signal),
    enabled: pendingJobID !== "",
    refetchInterval: (query) =>
      query.state.data?.state === "queued" || query.state.data?.state === "running" ? 700 : false,
  })
  const jobActive = job.data?.state === "queued" || job.data?.state === "running"
  const results = useQuery({
    queryKey: ["ai-results", props.entryID],
    queryFn: ({ signal }) => listAIResults(props.entryID!, signal),
    enabled: articleMode && props.profiles.length > 0,
  })
  const chat = useQuery({
    queryKey: ["ai-chat", sessionID],
    queryFn: ({ signal }) => getAIChat(sessionID, signal),
    enabled: sessionID !== "",
  })

  useEffect(() => {
    if (!pendingJobID || !job.data || jobActive) return
    const state = job.data
    const operation = pendingOperation
    void (async () => {
      if (state.state === "succeeded") {
        if (operation && props.entryID) {
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["ai-results", props.entryID] }),
            queryClient.invalidateQueries({ queryKey: ["entries"] }),
            queryClient.invalidateQueries({ queryKey: ["entry", props.entryID] }),
            queryClient.invalidateQueries({ queryKey: ["ai-usage"] }),
            ...(operation === "academic_tags"
              ? [queryClient.invalidateQueries({ queryKey: ["tags"] })]
              : []),
          ])
        } else {
          await Promise.all([
            ...(sessionID ? [queryClient.invalidateQueries({ queryKey: ["ai-chat", sessionID] })] : []),
            queryClient.invalidateQueries({ queryKey: ["ai-usage"] }),
          ])
        }
      } else if (state.state === "failed") {
        setJobFailure(state.error_message ?? t("aiTaskFailed"))
      }
      // The job reached a terminal state; stop tracking it so the polling
      // query unmounts and a later run starts from a clean slate.
      setPendingJobID("")
      setPendingOperation(null)
    })()
  }, [
    job.data,
    jobActive,
    pendingJobID,
    pendingOperation,
    props.entryID,
    queryClient,
    sessionID,
    t,
  ])

  const operationMutation = useMutation({
    mutationFn: ({
      operation,
      profile,
      targetLanguage,
    }: {
      operation: AIOperation
      profile: string
      targetLanguage: string
    }) => runAIOperation(props.entryID!, operation, profile, targetLanguage),
    onSuccess: (response, variables) => {
      setPendingOperation(response.job ? variables.operation : null)
      if (response.result && props.entryID) {
        queryClient.setQueryData<ListResponse<AIResult>>(
          ["ai-results", props.entryID],
          (current) => ({
            items: [
              response.result!,
              ...(current?.items.filter((item) => item.id !== response.result!.id) ?? []),
            ],
          }),
        )
      }
      setPendingJobID(response.job?.id ?? "")
      if (props.entryID)
        void queryClient.invalidateQueries({ queryKey: ["ai-results", props.entryID] })
    },
  })
  const chatMutation = useMutation({
    mutationFn: (input: { profile: string; text: string }) =>
      props.entryID
        ? startAIChat(props.entryID, input.profile, sessionID || undefined, input.text)
        : startAILibraryChat(
            props.entryIDs ?? [],
            input.profile,
            sessionID || undefined,
            input.text,
          ),
    onSuccess: (response) => {
      setSessionID(response.session.id)
      setPendingJobID(response.job.id)
      setPendingOperation(null)
      setMessage("")
      queryClient.setQueryData<AIChatSession>(["ai-chat", response.session.id], response.session)
    },
  })
  const cancelMutation = useMutation({ mutationFn: cancelJob, onSuccess: () => void job.refetch() })
  const latestResult = results.data?.items.find((item) => item.operation === mode)
  const error =
    operationMutation.error ??
    chatMutation.error ??
    cancelMutation.error ??
    (jobFailure ? new Error(jobFailure) : null)

  const startOperation = (operation: AIOperation) => {
    if (!activeProfileID) {
      props.onConfigure()
      return
    }
    setJobFailure(null)
    operationMutation.mutate({ operation, profile: activeProfileID, targetLanguage: language })
  }
  const submitChat = (event: FormEvent) => {
    event.preventDefault()
    if (!activeProfileID) {
      props.onConfigure()
      return
    }
    if (!message.trim() || jobActive || (!props.entryID && (props.entryIDs?.length ?? 0) === 0))
      return
    setJobFailure(null)
    chatMutation.mutate({ profile: activeProfileID, text: message.trim() })
  }
  const startResize = (event: PointerEvent<HTMLButtonElement>) => {
    if (window.innerWidth <= 900) return
    event.preventDefault()
    const startX = event.clientX
    const startWidth = props.width
    const move = (moveEvent: globalThis.PointerEvent) =>
      props.onWidthChange(
        Math.round(Math.min(600, Math.max(300, startWidth + startX - moveEvent.clientX))),
      )
    const stop = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", stop)
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", stop, { once: true })
  }

  return (
    <aside
      className="ai-workbench ai-workbench--open"
      style={{ width: props.width }}
      aria-label={t("aiAssistant")}
      aria-busy={jobActive}
    >
      <button
        className="ai-workbench__resize"
        type="button"
        role="separator"
        aria-orientation="vertical"
        aria-label={t("resizeAIPanel")}
        aria-valuemin={300}
        aria-valuemax={600}
        aria-valuenow={props.width}
        onPointerDown={startResize}
        onKeyDown={(event) => {
          if (event.key === "ArrowLeft") props.onWidthChange(Math.min(600, props.width + 16))
          if (event.key === "ArrowRight") props.onWidthChange(Math.max(300, props.width - 16))
        }}
      />
      <div className="ai-workbench__body" id="ai-workbench-panel">
        <div className="ai-workbench__header">
          <span className="ai-workbench__identity">
            <i>
              <AIIcon />
            </i>
            <span>
              <strong>ReFlow Insight</strong>
              <small>{props.contextLabel}</small>
            </span>
          </span>
          <button
            className="icon-button"
            type="button"
            aria-label={t("close")}
            title={t("close")}
            onClick={props.onClose}
          >
            <X />
          </button>
        </div>
        {!activeProfile ? (
          <button className="button button--secondary" type="button" onClick={props.onConfigure}>
            <Brain />
            {t("configureAI")}
          </button>
        ) : (
          <>
            <div className="ai-workbench__controls">
              <select
                className="select-input ai-profile-select"
                aria-label={t("aiProvider")}
                value={activeProfileID}
                onChange={(event) => setProfileID(event.target.value)}
              >
                {props.profiles
                  .filter((profile) => profile.enabled)
                  .map((profile) => (
                    <option value={profile.id} key={profile.id}>
                      {profile.name}
                    </option>
                  ))}
              </select>
              <select
                className="select-input ai-language-select"
                aria-label={t("aiResponseLanguage")}
                value={language}
                onChange={(event) => setLanguage(event.target.value)}
              >
                <option value="English">{t("english")}</option>
                <option value="Chinese">{t("chinese")}</option>
                <option value="Japanese">{t("japanese")}</option>
                <option value="Spanish">{t("spanish")}</option>
                <option value="French">{t("french")}</option>
                <option value="German">{t("german")}</option>
              </select>
            </div>
            {articleMode && (
              <div className="ai-mode-tabs" role="tablist" aria-label={t("aiAssistant")}>
                {operations.map((operation) => {
                  const Icon = operation.icon
                  return (
                    <button
                      className={
                        mode === operation.id ? "ai-mode-tab ai-mode-tab--active" : "ai-mode-tab"
                      }
                      type="button"
                      role="tab"
                      aria-selected={mode === operation.id}
                      key={operation.id}
                      onClick={() => setMode(operation.id)}
                    >
                      <Icon />
                      {t(operation.labelKey)}
                    </button>
                  )
                })}
                <button
                  className={mode === "chat" ? "ai-mode-tab ai-mode-tab--active" : "ai-mode-tab"}
                  type="button"
                  role="tab"
                  aria-selected={mode === "chat"}
                  onClick={() => setMode("chat")}
                >
                  <ChatCircle />
                  {t("chat")}
                </button>
              </div>
            )}
            {mode === "chat" || !articleMode ? (
              <div className="ai-chat" id="ai-tool-panel" role="tabpanel">
                {!articleMode && !chat.data && (
                  <div className="ai-chat__suggestions">
                    <button type="button" onClick={() => setMessage(t("summarizeLatestPrompt"))}>
                      {t("summarizeLatest")}
                    </button>
                    <button type="button" onClick={() => setMessage(t("politicalBriefPrompt"))}>
                      {t("politicalBrief")}
                    </button>
                  </div>
                )}
                <div className="ai-chat__messages" aria-live="polite">
                  {chat.data?.messages.map((item) => (
                    <div
                      className={`ai-chat__message ai-chat__message--${item.role}`}
                      key={item.id}
                    >
                      <strong>{item.role === "user" ? t("you") : activeProfile.name}</strong>
                      <p>{item.content}</p>
                    </div>
                  ))}
                </div>
                <form className="ai-chat__form" onSubmit={submitChat}>
                  <textarea
                    className="text-input"
                    aria-label={articleMode ? t("askAboutArticle") : t("askAboutLatest")}
                    placeholder={articleMode ? t("askAboutArticle") : t("askAboutLatest")}
                    maxLength={4000}
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                  />
                  <button
                    className="button button--primary"
                    type="submit"
                    disabled={
                      !message.trim() ||
                      jobActive ||
                      chatMutation.isPending ||
                      (!articleMode && (props.entryIDs?.length ?? 0) === 0)
                    }
                  >
                    {chatMutation.isPending || jobActive ? (
                      <CircleNotch className="spin" />
                    ) : (
                      <ChatCircle />
                    )}
                    {t("ask")}
                  </button>
                </form>
              </div>
            ) : (
              <div className="ai-operation" id="ai-tool-panel" role="tabpanel">
                <button
                  className="button button--primary"
                  type="button"
                  disabled={!activeProfileID || jobActive || operationMutation.isPending}
                  onClick={() => startOperation(mode)}
                >
                  {operationMutation.isPending || (jobActive && pendingOperation === mode) ? (
                    <CircleNotch className="spin" />
                  ) : (
                    <Brain />
                  )}
                  {t("run")} {t(operations.find((item) => item.id === mode)?.labelKey ?? "summary")}
                </button>
                {latestResult && (
                  <div className="ai-result" aria-live="polite">
                    <p>{formatAIResult(latestResult)}</p>
                    <small>
                      {new Intl.NumberFormat(locale).format(latestResult.usage.total_tokens ?? 0)}{" "}
                      {t("tokens")}
                    </small>
                  </div>
                )}
              </div>
            )}
            {jobActive && (
              <button
                className="button button--quiet ai-cancel"
                type="button"
                disabled={cancelMutation.isPending}
                onClick={() => cancelMutation.mutate(pendingJobID)}
              >
                <Stop />
                {t("cancel")}
              </button>
            )}
            {error && (
              <p className="form-error" role="alert">
                {error.message}
              </p>
            )}
          </>
        )}
      </div>
    </aside>
  )
}

