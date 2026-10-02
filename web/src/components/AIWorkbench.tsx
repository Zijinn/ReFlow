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
import { type FormEvent, type PointerEvent, useEffect, useRef, useState } from "react"

// 对话区的样式单独一份，走本模块引入：模块图里 main.tsx 那串 styles.css /
// phase*.css 都先于本模块求值，所以它仍排在最后，同特异度靠文件顺序取胜。
import "./ai-chat.css"

import {
  cancelJob,
  getAIChat,
  getJob,
  listAIResults,
  runAIOperation,
  startAIChat,
  startAILibraryChat,
  startAIPaperChat,
} from "../api/client"
import type {
  AIChatMessage,
  AIChatSession,
  AIOperation,
  AIProfile,
  AIResult,
  ListResponse,
} from "../api/types"
import { formatAIResult } from "../lib/ai"
import { useTranslation } from "../lib/i18n"
import { AIIcon } from "./AIIcon"

interface AIWorkbenchProps {
  entryID?: string
  entryIDs?: string[]
  /**
   * Research paper ids for the workbench context. When present the panel chats
   * through `/ai/paper-chat` instead of the library endpoint, which validates
   * against RSS content and structurally cannot take paper ids.
   */
  paperIDs?: string[]
  profiles: AIProfile[]
  width: number
  contextLabel: string
  /**
   * How many items the caller actually has in view, when that is more than the
   * ids it passed. `contextCount` is capped by the endpoint, so without this the
   * panel can print "20 条在上下文" over a 200-entry list and read as if nothing
   * was left behind.
   */
  contextTotal?: number
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

/**
 * Openers the paper endpoint can actually answer: `/ai/paper-chat` receives one
 * block per paper with title, authors, keywords, status, journals, tags, stage
 * completion, deadline + proximity, next action, notes, submission count and
 * idle days — never the PDF text and no change log. Every prompt below is
 * answerable from that envelope alone and promises no per-paper action.
 */
const paperOpeners = [
  { labelKey: "aiChatAskPush", promptKey: "aiChatAskPushPrompt" },
  { labelKey: "aiChatAskDeadlines", promptKey: "aiChatAskDeadlinesPrompt" },
  { labelKey: "aiChatAskIdle", promptKey: "aiChatAskIdlePrompt" },
]

const libraryOpeners = [
  { labelKey: "summarizeLatest", promptKey: "summarizeLatestPrompt" },
  { labelKey: "politicalBrief", promptKey: "politicalBriefPrompt" },
]

// The provider answers in one shot, so a run is polled rather than streamed;
// 1.2s is fast enough to feel live without a second request per second.
const CHAT_POLL_MS = 1200

/**
 * The backend writes no pending or failed assistant row — every assistant
 * message it stores is already `completed`
 * (internal/storage/ai.go:SaveAIChatAssistantAndUsage), so the answer's cost
 * comes from `usage` and its model from the message metadata.
 */
function answerModel(item: AIChatMessage): string {
  return item.metadata?.model ?? ""
}

/**
 * Seconds between the question this answers and the answer itself. Both
 * timestamps are the server's, so the number survives a reload instead of
 * being guessed from the client clock; a session with no question before the
 * answer (the daily digest) and any nonsense gap return nothing at all.
 */
function answerDurationSeconds(messages: AIChatMessage[], index: number): number | null {
  const answer = messages[index]
  if (!answer || answer.role !== "assistant") return null
  for (let prior = index - 1; prior >= 0; prior -= 1) {
    const question = messages[prior]
    if (!question || question.role !== "user") continue
    const started = Date.parse(question.created_at)
    const finished = Date.parse(answer.created_at)
    if (Number.isNaN(started) || Number.isNaN(finished)) return null
    const seconds = Math.round((finished - started) / 1000)
    return seconds >= 0 && seconds < 3_600 ? seconds : null
  }
  return null
}

export function AIWorkbench(props: AIWorkbenchProps) {
  const { locale, t } = useTranslation()
  const queryClient = useQueryClient()
  const articleMode = Boolean(props.entryID)
  // Workbench papers mode: chat only. The paper endpoint has no per-entry
  // operations (summary/translation/…) and the digest endpoint deliberately
  // opens a session with no user turn, so the two never share a session id.
  const paperMode = Boolean(props.paperIDs)
  const contextCount = paperMode
    ? (props.paperIDs?.length ?? 0)
    : articleMode
      ? 1
      : (props.entryIDs?.length ?? 0)
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
  // Wall clock of the chat turn in flight. The backend never stores a pending
  // assistant row, so the in-flight line is measured here, from the click — the
  // only elapsed value that exists while the answer is still coming.
  const [chatRunStart, setChatRunStart] = useState<number | null>(null)
  const [elapsedSeconds, setElapsedSeconds] = useState(0)
  const threadRef = useRef<HTMLDivElement>(null)
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
    // The answer lands in the session, not in the job response, so the thread
    // is only worth polling while that job is still running: the panel polls the
    // session it already has and stops the moment the job settles.
    refetchInterval: () => (jobActive ? CHAT_POLL_MS : false),
  })
  const messages = chat.data?.messages ?? []
  const chatVisible = mode === "chat" || !articleMode
  const noConversation = messages.length === 0
  // A failed answer has no assistant row to carry it, so the job's own error is
  // the only copy of what went wrong: it belongs in the thread, next to the
  // question it failed to answer, not in a footnote under the composer.
  const answerFailure = chatVisible && !jobActive ? jobFailure : null
  // Docked = the thread owns the vertical space, so the composer sits at the
  // bottom of the panel and never moves when an answer arrives. Only the chat
  // panel docks; the article-mode operation results keep the scrolling body.
  const docked = chatVisible && Boolean(activeProfile)

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
      setChatRunStart(null)
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

  // The one liveness signal while a turn is in flight: a seconds counter. It
  // ticks from the click, not from the job's own timestamps, because the queue
  // stamp and this clock are not the same machine's guarantee.
  useEffect(() => {
    if (chatRunStart === null) return
    const tick = () => setElapsedSeconds(Math.max(0, Math.round((Date.now() - chatRunStart) / 1000)))
    tick()
    const timer = window.setInterval(tick, 1000)
    return () => window.clearInterval(timer)
  }, [chatRunStart])

  // Text arrives by the poll, so the thread follows it downward — but only while
  // a run is in flight, so scrolling back to read an older answer is never
  // yanked forward by the next tick.
  const runInFlight = chatRunStart !== null
  useEffect(() => {
    if (!runInFlight) return
    const thread = threadRef.current
    if (thread) thread.scrollTop = thread.scrollHeight
  }, [runInFlight, messages.length, elapsedSeconds])

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
      props.paperIDs
        ? startAIPaperChat({
            paperIDs: props.paperIDs,
            profileID: input.profile,
            sessionID: sessionID || undefined,
            message: input.text,
          })
        : props.entryID
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
    // A turn that never reached the queue has no job to settle, so the in-flight
    // line has to be taken down here or it would keep counting forever.
    onError: () => setChatRunStart(null),
  })
  const cancelMutation = useMutation({ mutationFn: cancelJob, onSuccess: () => void job.refetch() })
  const latestResult = results.data?.items.find((item) => item.operation === mode)
  const error =
    operationMutation.error ??
    chatMutation.error ??
    cancelMutation.error ??
    // In the thread the failure is an answer-shaped row of its own; the note
    // below the composer only speaks for the modes that have no thread.
    (!chatVisible && jobFailure ? new Error(jobFailure) : null)

  const askLabel = paperMode
    ? t("askAboutPapers")
    : articleMode
      ? t("askAboutArticle")
      : t("askAboutLatest")
  const openers = paperMode ? paperOpeners : articleMode ? [] : libraryOpeners
  const emptyLeadKey = paperMode
    ? "aiChatEmptyPapers"
    : articleMode
      ? "aiChatEmptyArticle"
      : "aiChatEmptyLibrary"
  const emptyScopeKey = paperMode
    ? "aiChatEmptyPapersCount"
    : articleMode
      ? ""
      : "aiChatEmptyLibraryCount"
  // The receipt of one answer: what it cost, how long it took, which model
  // wrote it. Anything the backend did not record is left out rather than
  // estimated, so a session that returns no usage prints nothing at all.
  const answerReceipt = (index: number): string => {
    const item = messages[index]
    if (!item || item.role !== "assistant") return ""
    const segments: string[] = []
    const tokens = item.usage.total_tokens ?? 0
    if (tokens > 0)
      segments.push(`${new Intl.NumberFormat(locale).format(tokens)} ${t("tokens")}`)
    const seconds = answerDurationSeconds(messages, index)
    if (seconds !== null) segments.push(`${seconds} ${t("aiElapsedSeconds")}`)
    const model = answerModel(item)
    if (model) segments.push(model)
    return segments.join(" · ")
  }

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
    if (!message.trim() || jobActive || contextCount === 0) return
    setJobFailure(null)
    setChatRunStart(Date.now())
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
      className={
        docked
          ? "ai-workbench ai-workbench--open ai-workbench--dock"
          : "ai-workbench ai-workbench--open"
      }
      style={{ width: props.width }}
      aria-label={t("aiAssistant")}
      aria-busy={jobActive || runInFlight}
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
            {chatVisible ? (
              <div
                className={noConversation ? "ai-chat ai-chat--empty" : "ai-chat"}
                id="ai-tool-panel"
                role="tabpanel"
              >
                {noConversation && !runInFlight && (
                  <div className="ai-chat__empty">
                    {contextCount > 1 && emptyScopeKey && (
                      <p className="ai-chat__empty-scope">
                        <strong>{contextCount}</strong>
                        <span>{t(emptyScopeKey)}</span>
                      </p>
                    )}
                    {/* 端点只收 20 个 id，调用方在递进来之前就截断了。上一行说"20 条在上下文里"
                        时，列表里其实有 200 条——那句真话读起来像"全都带上了"。差值只有这一行
                        能说清，所以只在真被截断时出现。 */}
                    {props.contextTotal !== undefined && props.contextTotal > contextCount && (
                      <p className="ai-chat__empty-scope">
                        <strong>{props.contextTotal}</strong>
                        <span>{t("aiChatContextCap")}</span>
                      </p>
                    )}
                    <p className="ai-chat__empty-lead">{t(emptyLeadKey)}</p>
                    {openers.length > 0 && (
                      <>
                        <p className="ai-chat__openers-label">{t("aiChatSuggestionLead")}</p>
                        <div className="ai-chat__suggestions">
                          {openers.map((opener) => (
                            <button
                              type="button"
                              key={opener.labelKey}
                              onClick={() => setMessage(t(opener.promptKey))}
                            >
                              {t(opener.labelKey)}
                            </button>
                          ))}
                        </div>
                      </>
                    )}
                  </div>
                )}
                <div className="ai-chat__messages" aria-live="polite" ref={threadRef}>
                  {messages.map((item, index) => {
                    const receipt = answerReceipt(index)
                    return (
                      <div
                        className={`ai-chat__message ai-chat__message--${item.role}`}
                        key={item.id}
                      >
                        <strong>{item.role === "user" ? t("you") : activeProfile.name}</strong>
                        <p>{item.content}</p>
                        {receipt && <p className="ai-chat__receipt">{receipt}</p>}
                      </div>
                    )
                  })}
                  {runInFlight && (
                    <div className="ai-chat__message ai-chat__message--assistant ai-chat__message--pending">
                      <strong>{activeProfile.name}</strong>
                      {/* One liveness signal: the dot and the clock. The count is
                          aria-hidden so a polite region does not read a number out
                          every second; the line under it says it once. */}
                      <p className="ai-chat__live" aria-hidden="true">
                        <i className="ai-chat__pulse" />
                        <span className="ai-chat__elapsed">
                          {elapsedSeconds} {t("aiElapsedSeconds")}
                        </span>
                      </p>
                      <span className="sr-only">{t("aiChatGenerating")}</span>
                    </div>
                  )}
                  {answerFailure && (
                    <div
                      className="ai-chat__message ai-chat__message--assistant ai-chat__message--failed"
                      role="alert"
                    >
                      <strong>{t("aiTaskFailed")}</strong>
                      <p>{answerFailure}</p>
                    </div>
                  )}
                </div>
                <form className="ai-chat__form ai-chat__composer" onSubmit={submitChat}>
                  <textarea
                    className="text-input ai-chat__input"
                    aria-label={askLabel}
                    placeholder={askLabel}
                    maxLength={4000}
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                  />
                  {/* row-reverse on purpose: the send button is the first stop for
                      the keyboard and the last one on screen, so `stop` never moves
                      out of the slot to its left. */}
                  <div className="ai-chat__actions">
                    <button
                      className="button button--primary ai-chat__send"
                      type="submit"
                      disabled={
                        !message.trim() ||
                        jobActive ||
                        chatMutation.isPending ||
                        contextCount === 0
                      }
                    >
                      <ChatCircle />
                      {t("ask")}
                    </button>
                    {/* 空闲时这颗不存在。它原来常驻、并且永远 disabled：一个"可点但永远不可点"
                        的东西挂在输入区下方，读起来像界面坏了。而且它画的是 <Stop />（方块）
                        却贴"取消"——图标说的是"掐断正在跑的东西"，文案说的是"撤销一个还没做的
                        决定"，两回事。现在只在真有作业时出现，文案跟图标对齐。
                        row-reverse 让发送键始终在它右边那一格，增删这一颗不会推动发送键。 */}
                    {(jobActive || cancelMutation.isPending) && (
                      <button
                        className="button button--quiet ai-stop-control"
                        type="button"
                        disabled={cancelMutation.isPending}
                        onClick={() => cancelMutation.mutate(pendingJobID)}
                      >
                        <Stop />
                        {t("aiStopGeneration")}
                      </button>
                    )}
                  </div>
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
                {/* 与对话那一颗同一规矩：只在真有作业时出现（见上面那段注释）。 */}
                {(jobActive || cancelMutation.isPending) && (
                  <button
                    className="button button--quiet ai-stop-control"
                    type="button"
                    disabled={cancelMutation.isPending}
                    onClick={() => cancelMutation.mutate(pendingJobID)}
                  >
                    <Stop />
                    {t("aiStopGeneration")}
                  </button>
                )}
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

