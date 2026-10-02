import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ArrowLeft,
  ArrowSquareOut,
  BookmarkSimple,
  Books,
  Brain,
  ChatCircle,
  CheckCircle,
  CircleNotch,
  ClockCountdown,
  HighlighterCircle,
  ListBullets,
  Minus,
  NotePencil,
  Plus,
  Star,
  Tag as TagIcon,
  TextAa,
  TextAlignLeft,
  TextUnderline,
  Trash,
  Sparkle,
  Translate,
  WarningCircle,
  WaveSine,
  X,
} from "@phosphor-icons/react"
import DOMPurify from "dompurify"
import {
  type CSSProperties,
  type FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"

import type {
  AIOperation,
  AIProfile,
  AIResult,
  Entry,
  EntryDetail,
  EntryState,
  ListResponse,
  Tag,
} from "../api/types"
import {
  createEntryAnnotation,
  deleteEntryAnnotation,
  getEntryZoteroStatus,
  getJob,
  getZoteroStatus,
  listAIResults,
  listEntryAnnotations,
  runAIOperation,
  saveEntryToZotero,
} from "../api/client"
import {
  applyAnnotations,
  serializeSelection,
  type AnnotationStyle,
  type ReaderAnnotation,
  type SerializedSelection,
} from "../lib/annotations"
import { formatBibTeX, formatGBT7714 } from "../lib/citation"
import { formatAIResult } from "../lib/ai"
import { useTranslation } from "../lib/i18n"
import { formatAuthors, summaryDuplicatesContent } from "../lib/metadata"
import { useReaderStore } from "../store/reader"
import { toast } from "../store/toast"
import { AIIcon } from "./AIIcon"

interface PendingSelection extends SerializedSelection {
  left: number
  top: number
}

// Quick AI actions run straight from the toolbar menu; only chat opens the panel.
const aiQuickOperations: Array<{ id: AIOperation; labelKey: string; icon: typeof Brain }> = [
  { id: "summary", labelKey: "summary", icon: TextAlignLeft },
  { id: "translation", labelKey: "translate", icon: Translate },
  { id: "key_points", labelKey: "keyPoints", icon: ListBullets },
  { id: "academic_tags", labelKey: "automaticTags", icon: TagIcon },
]

interface ReaderPaneProps {
  summary: Entry | null
  detail?: EntryDetail
  isLoading: boolean
  error: Error | null
  mutationPending: boolean
  readabilityPending: boolean
  aiProfiles: AIProfile[]
  tags: Tag[]
  feedIconURL?: string | null
  onBack: () => void
  onRetry: () => void
  onStateChange: (entry: Entry, patch: Partial<EntryState>) => void
  onAutoMarkRead?: (entry: Entry) => void
  onTagsChange: (entryID: string, tagIDs: string[]) => void
  onFetchReadability: (entryID: string) => void
  onConfigureAI: () => void
  aiOpen?: boolean
  onToggleAI?: () => void
}

export function ReaderPane(props: ReaderPaneProps) {
  const { locale, t } = useTranslation()
  const queryClient = useQueryClient()
  const { detail, onStateChange, onAutoMarkRead } = props
  // Feed content wins by default. Readability has no paywall detection, so on a
  // journal landing page it stores nav and citation furniture and would show
  // that instead of the abstract. Fetching opts in explicitly.
  const [preferReadability, setPreferReadability] = useState(false)
  const [lastEntryID, setLastEntryID] = useState<string | undefined>(undefined)
  const [tagPickerOpen, setTagPickerOpen] = useState(false)
  const [appearanceOpen, setAppearanceOpen] = useState(false)
  const [aiMenuOpen, setAIMenuOpen] = useState(false)
  const [aiPendingJobID, setAIPendingJobID] = useState("")
  const [aiPendingOperation, setAIPendingOperation] = useState<AIOperation | null>(null)
  const [aiFlash, setAIFlash] = useState<string | null>(null)
  const aiMenuRef = useRef<HTMLDivElement>(null)
  const aiFlashTimer = useRef<number | undefined>(undefined)
  const [pendingSelection, setPendingSelection] = useState<PendingSelection | null>(null)
  const [noteEditorOpen, setNoteEditorOpen] = useState(false)
  const [noteDraft, setNoteDraft] = useState("")
  const [sourceLeadImageFailed, setSourceLeadImageFailed] = useState(false)
  const [sourceIconFailed, setSourceIconFailed] = useState(false)
  const [showOriginalContent, setShowOriginalContent] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)
  const readerAppearance = useReaderStore((state) => state.readerAppearance)
  const setReaderAppearance = useReaderStore((state) => state.setReaderAppearance)
  const alwaysTranslateContent = useReaderStore((state) => state.alwaysTranslateContent)
  const entry = props.detail ?? props.summary
  const translationLanguage = locale === "zh-CN" ? "Chinese" : "English"
  // Manual "Translate" runs keep the results query alive for the session even
  // when always-translate is off, so the result actually renders.
  const [manualTranslation, setManualTranslation] = useState(false)
  const aiResults = useQuery({
    queryKey: ["ai-results", entry?.id],
    queryFn: ({ signal }) => listAIResults(entry!.id, signal),
    enabled: Boolean(
      entry && (alwaysTranslateContent || manualTranslation) && props.aiProfiles.length > 0,
    ),
  })
  const zoteroStatus = useQuery({
    queryKey: ["zotero-status"],
    queryFn: ({ signal }) => getZoteroStatus(signal),
    staleTime: 15_000,
  })
  const entryZotero = useQuery({
    queryKey: ["entry-zotero", entry?.id],
    queryFn: ({ signal }) => getEntryZoteroStatus(entry!.id, signal),
    enabled: Boolean(entry),
  })
  const zoteroMutation = useMutation({
    mutationFn: (entryID: string) => saveEntryToZotero(entryID),
    onSuccess: (result) => {
      queryClient.setQueryData(["entry-zotero", result.export.entry_id], {
        saved: true,
        export: result.export,
      })
      queryClient.setQueryData(["zotero-status"], result.target)
    },
  })
  const activeAIProfile =
    props.aiProfiles.find((profile) => profile.is_default && profile.enabled) ??
    props.aiProfiles.find((profile) => profile.enabled)
  const flashAIResult = useCallback(
    (operation: AIOperation) => {
      if (!entry) return
      const latest = queryClient
        .getQueryData<ListResponse<AIResult>>(["ai-results", entry.id])
        ?.items.find((item) => item.operation === operation)
      if (!latest) return
      window.clearTimeout(aiFlashTimer.current)
      setAIFlash(formatAIResult(latest))
      aiFlashTimer.current = window.setTimeout(() => setAIFlash(null), 12000)
    },
    [entry, queryClient],
  )
  const aiOperationMutation = useMutation({
    mutationFn: (operation: AIOperation) =>
      runAIOperation(entry!.id, operation, activeAIProfile?.id ?? "", translationLanguage),
    onSuccess: (response, operation) => {
      setAIPendingOperation(response.job ? operation : null)
      setAIPendingJobID(response.job?.id ?? "")
      if (response.result && entry) {
        queryClient.setQueryData<ListResponse<AIResult>>(["ai-results", entry.id], (current) => ({
          items: [
            response.result!,
            ...(current?.items.filter((item) => item.id !== response.result!.id) ?? []),
          ],
        }))
        void queryClient.invalidateQueries({ queryKey: ["entries"] })
        void queryClient.invalidateQueries({ queryKey: ["entry", entry.id] })
        if (operation === "academic_tags") {
          void queryClient.invalidateQueries({ queryKey: ["tags"] })
        }
        if (operation === "key_points" || operation === "academic_tags") {
          flashAIResult(operation)
        }
      }
    },
  })
  const aiJob = useQuery({
    queryKey: ["job", aiPendingJobID],
    queryFn: ({ signal }) => getJob(aiPendingJobID, signal),
    enabled: aiPendingJobID !== "",
    refetchInterval: (query) =>
      query.state.data?.state === "queued" || query.state.data?.state === "running" ? 700 : false,
  })
  const aiJobActive = aiJob.data?.state === "queued" || aiJob.data?.state === "running"
  useEffect(() => {
    if (!aiPendingJobID || !aiJob.data || aiJobActive || aiJob.data.state !== "succeeded") return
    const operation = aiPendingOperation
    void (async () => {
      await queryClient.invalidateQueries({ queryKey: ["ai-results", entry?.id] })
      void queryClient.invalidateQueries({ queryKey: ["entries"] })
      void queryClient.invalidateQueries({ queryKey: ["entry", entry?.id] })
      void queryClient.invalidateQueries({ queryKey: ["ai-usage"] })
      if (operation === "academic_tags") {
        void queryClient.invalidateQueries({ queryKey: ["tags"] })
      }
      setAIPendingJobID("")
      setAIPendingOperation(null)
      if (operation === "key_points" || operation === "academic_tags") {
        flashAIResult(operation)
      }
    })()
  }, [
    aiJob.data,
    aiJobActive,
    aiPendingJobID,
    aiPendingOperation,
    entry?.id,
    queryClient,
    flashAIResult,
  ])
  const aiBusy = aiOperationMutation.isPending || aiJobActive
  const aiError =
    aiOperationMutation.error ??
    (aiJob.data?.state === "failed"
      ? new Error(aiJob.data.error_message ?? t("aiTaskFailed"))
      : null)
  const startAIOperation = (operation: AIOperation) => {
    setAIMenuOpen(false)
    if (!activeAIProfile) {
      props.onConfigureAI()
      return
    }
    if (operation === "translation") setManualTranslation(true)
    aiOperationMutation.mutate(operation)
  }
  useEffect(() => {
    if (!aiMenuOpen) return
    const closeOnOutsidePress = (event: globalThis.PointerEvent) => {
      if (!aiMenuRef.current?.contains(event.target as Node)) setAIMenuOpen(false)
    }
    document.addEventListener("pointerdown", closeOnOutsidePress)
    return () => document.removeEventListener("pointerdown", closeOnOutsidePress)
  }, [aiMenuOpen])
  useEffect(() => () => window.clearTimeout(aiFlashTimer.current), [])
  const translatedContent = aiResults.data?.items.find(
    (result) => result.operation === "translation" && result.language === translationLanguage,
  )?.result_text
  const sourceImage =
    entry?.lead_image_url && !sourceLeadImageFailed
      ? { url: entry.lead_image_url, kind: "lead" as const }
      : props.feedIconURL && !sourceIconFailed
        ? { url: props.feedIconURL, kind: "icon" as const }
        : null
  const annotationsQuery = useQuery({
    queryKey: ["annotations", entry?.id],
    queryFn: ({ signal }) => listEntryAnnotations(entry!.id, signal),
    enabled: Boolean(entry),
  })
  const entryAnnotations = useMemo(() => annotationsQuery.data ?? [], [annotationsQuery.data])
  const createAnnotationMutation = useMutation({
    mutationFn: (input: {
      style: AnnotationStyle
      quote: string
      prefix: string
      suffix: string
      note: string
    }) => createEntryAnnotation(entry!.id, input),
    onSuccess: (created) => {
      queryClient.setQueryData<ReaderAnnotation[]>(["annotations", entry?.id], (current) => [
        ...(current ?? []),
        created,
      ])
      setPendingSelection(null)
      setNoteEditorOpen(false)
      setNoteDraft("")
      window.getSelection()?.removeAllRanges()
    },
  })
  const deleteAnnotationMutation = useMutation({
    mutationFn: (annotationID: string) => deleteEntryAnnotation(entry!.id, annotationID),
    onSuccess: (_result, annotationID) => {
      queryClient.setQueryData<ReaderAnnotation[]>(["annotations", entry?.id], (current) =>
        (current ?? []).filter((annotation) => annotation.id !== annotationID),
      )
    },
  })
  const safeHTML = useMemo(
    () =>
      DOMPurify.sanitize(
        (preferReadability ? props.detail?.readability_html : null) ??
          props.detail?.sanitized_html ??
          "",
        { USE_PROFILES: { html: true, mathMl: true } },
      ),
    [preferReadability, props.detail],
  )
  const displayAuthors = formatAuthors(entry?.author)
  const headline = entry?.title || t("untitled")
  // Abstract-only feeds repeat the abstract in both fields; the body already
  // shows it, so the header copy would duplicate the whole thing.
  const bodyShowsSummary = !safeHTML && !translatedContent
  const showHeaderSummary = Boolean(
    entry?.ai_summary ??
    (entry?.summary && !bodyShowsSummary && !summaryDuplicatesContent(entry.summary, safeHTML)),
  )
  // Fire at most once per entry: if the callback prop identity changes while
  // the PATCH is still in flight (the parent re-renders on every mutation
  // state change), the deps below re-run this effect and re-arm the mutation
  // until React aborts the whole tree with "Maximum update depth exceeded".
  const autoMarkedEntryRef = useRef<string | null>(null)
  useEffect(() => {
    if (!detail || detail.state.is_read) return
    if (autoMarkedEntryRef.current === detail.id) return
    autoMarkedEntryRef.current = detail.id
    if (onAutoMarkRead) onAutoMarkRead(detail)
    else onStateChange(detail, { is_read: true })
  }, [detail, onStateChange, onAutoMarkRead])
  // Reset the readability preference when the article changes. Adjusting during
  // render rather than in an effect keeps the first render of a new entry from
  // briefly reusing the previous entry's choice.
  if (entry?.id !== lastEntryID) {
    setLastEntryID(entry?.id)
    setPreferReadability(false)
  }
  useEffect(() => {
    if (contentRef.current) applyAnnotations(contentRef.current, entryAnnotations)
  }, [entryAnnotations, safeHTML])
  const captureSelection = useCallback(() => {
    window.setTimeout(() => {
      const root = contentRef.current
      const selection = window.getSelection()
      if (!root || !selection || selection.isCollapsed || selection.rangeCount === 0) {
        setPendingSelection(null)
        return
      }
      const range = selection.getRangeAt(0)
      const serialized = serializeSelection(root, range)
      if (!serialized) {
        setPendingSelection(null)
        return
      }
      const rect =
        typeof range.getBoundingClientRect === "function"
          ? range.getBoundingClientRect()
          : root.getBoundingClientRect()
      setPendingSelection({
        ...serialized,
        left: Math.min(Math.max(rect.left + rect.width / 2, 150), window.innerWidth - 150),
        top: Math.max(62, rect.top - 10),
      })
      setNoteEditorOpen(false)
      setNoteDraft("")
    }, 0)
  }, [])

  const createAnnotation = (style: AnnotationStyle, note = "") => {
    if (!entry || !pendingSelection || createAnnotationMutation.isPending) return
    createAnnotationMutation.mutate({
      quote: pendingSelection.quote,
      prefix: pendingSelection.prefix,
      suffix: pendingSelection.suffix,
      style,
      note: note.trim(),
    })
  }

  const saveNote = (event: FormEvent) => {
    event.preventDefault()
    if (noteDraft.trim()) createAnnotation("highlight", noteDraft)
  }

  const scrollToAnnotation = (annotationID: string) => {
    const target = Array.from(
      contentRef.current?.querySelectorAll<HTMLElement>("[data-reflow-annotation]") ?? [],
    ).find((element) => element.dataset.reflowAnnotation === annotationID)
    target?.scrollIntoView({
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
      block: "center",
    })
  }

  const readerStyle = {
    "--reader-content-font":
      readerAppearance.fontFamily === "serif" ? "var(--font-reader-serif)" : "var(--font-ui)",
    "--reader-content-size": `${readerAppearance.fontSize}px`,
    "--reader-content-leading": readerAppearance.lineHeight,
  } as CSSProperties
  if (!entry) {
    return (
      <article className="reader reader--placeholder" aria-label={t("reader")}>
        <div className="reader__placeholder">
          <BookmarkSimple />
          <p>{t("selectArticle")}</p>
        </div>
      </article>
    )
  }
  if (props.isLoading) {
    return (
      <article className="reader reader--placeholder" aria-label={t("reader")}>
        <CircleNotch className="spin reader__loader" aria-label={t("loadingArticle")} />
      </article>
    )
  }
  if (props.error) {
    return (
      <article className="reader reader--placeholder" aria-label={t("reader")}>
        <div className="pane-state" role="alert">
          <WarningCircle />
          <h2>{t("articleUnavailable")}</h2>
          <p>{props.error.message}</p>
          <button className="button button--secondary" type="button" onClick={props.onRetry}>
            {t("retry")}
          </button>
        </div>
      </article>
    )
  }
  return (
    <article
      className={
        appearanceOpen ? "reader reader--article reader--inspector-open" : "reader reader--article"
      }
      aria-label={t("reader")}
      style={readerStyle}
    >
      <div className="reader-toolbar">
        <button
          className="icon-button reader-back"
          type="button"
          aria-label={t("backToTimeline")}
          title={t("backToTimeline")}
          onClick={props.onBack}
        >
          <ArrowLeft />
        </button>
        <div className="reader-toolbar__spacer" />
        <div className="reader-ai-menu" ref={aiMenuRef}>
          <button
            className={
              aiMenuOpen || props.aiOpen ? "icon-button icon-button--active" : "icon-button"
            }
            type="button"
            aria-label={t("aiAssistant")}
            title={t("aiAssistant")}
            aria-haspopup="menu"
            aria-expanded={aiMenuOpen}
            onClick={() => setAIMenuOpen((open) => !open)}
          >
            {aiBusy ? <CircleNotch className="spin" /> : <AIIcon />}
          </button>
          {aiMenuOpen && (
            <div
              className="reader-tag-picker reader-ai-picker"
              role="menu"
              aria-label={t("aiAssistant")}
            >
              {aiQuickOperations.map((operation) => {
                const Icon = operation.icon
                return (
                  <button
                    className="reader-ai-option"
                    type="button"
                    role="menuitem"
                    key={operation.id}
                    disabled={aiBusy}
                    onClick={() => startAIOperation(operation.id)}
                  >
                    <Icon aria-hidden="true" />
                    <span>{t(operation.labelKey)}</span>
                  </button>
                )
              })}
              <div className="reader-ai-picker__divider" role="separator" />
              <button
                className="reader-ai-option"
                type="button"
                role="menuitem"
                onClick={() => {
                  setAIMenuOpen(false)
                  props.onToggleAI?.()
                }}
              >
                <ChatCircle aria-hidden="true" />
                <span>{t("chat")}</span>
              </button>
            </div>
          )}
        </div>
        <button
          className={appearanceOpen ? "icon-button icon-button--active" : "icon-button"}
          type="button"
          aria-label={t("readerAppearance")}
          title={t("readerAppearance")}
          aria-expanded={appearanceOpen}
          onClick={() => setAppearanceOpen((open) => !open)}
        >
          <TextAa />
        </button>
        <div className="reader-tag-menu">
          <button
            className={entry.tag_ids.length > 0 ? "icon-button icon-button--active" : "icon-button"}
            type="button"
            aria-label={t("editArticleTags")}
            title={t("tagsTitle")}
            aria-expanded={tagPickerOpen}
            disabled={props.tags.length === 0 || props.mutationPending}
            onClick={() => setTagPickerOpen((open) => !open)}
          >
            <TagIcon weight={entry.tag_ids.length > 0 ? "fill" : "regular"} />
          </button>
          {tagPickerOpen && (
            <div className="reader-tag-picker" role="group" aria-label={t("articleTags")}>
              {props.tags.map((tag) => {
                const checked = entry.tag_ids.includes(tag.id)
                return (
                  <label key={tag.id}>
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={props.mutationPending}
                      onChange={() =>
                        props.onTagsChange(
                          entry.id,
                          checked
                            ? entry.tag_ids.filter((tagID) => tagID !== tag.id)
                            : [...entry.tag_ids, tag.id],
                        )
                      }
                    />
                    <span
                      className="organization-swatch"
                      style={tag.color ? { backgroundColor: tag.color } : undefined}
                    />
                    <span>{tag.name}</span>
                  </label>
                )
              })}
            </div>
          )}
        </div>
        <button
          className={
            detail?.readability_html && preferReadability
              ? "icon-button icon-button--active"
              : "icon-button"
          }
          type="button"
          aria-label={
            detail?.readability_html
              ? preferReadability
                ? t("useFeedContent")
                : t("useFullText")
              : t("fetchFullText")
          }
          title={
            detail?.readability_html
              ? preferReadability
                ? t("useFeedContent")
                : t("useFullText")
              : t("fetchFullText")
          }
          disabled={props.readabilityPending || !entry.canonical_url}
          onClick={() => {
            if (detail?.readability_html) {
              setPreferReadability((value) => !value)
              return
            }
            // Fetching is an explicit request to read the extraction.
            setPreferReadability(true)
            props.onFetchReadability(entry.id)
          }}
        >
          {props.readabilityPending ? <CircleNotch className="spin" /> : <TextAlignLeft />}
        </button>
        <button
          className={entry.state.is_starred ? "icon-button icon-button--active" : "icon-button"}
          type="button"
          aria-label={entry.state.is_starred ? t("removeStar") : t("starArticle")}
          title={entry.state.is_starred ? t("removeStar") : t("starArticle")}
          disabled={props.mutationPending}
          onClick={() => props.onStateChange(entry, { is_starred: !entry.state.is_starred })}
        >
          <Star weight={entry.state.is_starred ? "fill" : "regular"} />
        </button>
        <button
          className={entry.state.is_read_later ? "icon-button icon-button--active" : "icon-button"}
          type="button"
          aria-label={entry.state.is_read_later ? t("removeReadLater") : t("readLater")}
          title={entry.state.is_read_later ? t("removeReadLater") : t("readLater")}
          disabled={props.mutationPending}
          onClick={() => props.onStateChange(entry, { is_read_later: !entry.state.is_read_later })}
        >
          <ClockCountdown weight={entry.state.is_read_later ? "fill" : "regular"} />
        </button>
        <button
          className="icon-button"
          type="button"
          aria-label={entry.state.is_read ? t("markUnread") : t("markRead")}
          title={entry.state.is_read ? t("markUnread") : t("markRead")}
          disabled={props.mutationPending}
          onClick={() => props.onStateChange(entry, { is_read: !entry.state.is_read })}
        >
          <CheckCircle weight={entry.state.is_read ? "fill" : "regular"} />
        </button>
        <button
          className={entryZotero.data?.saved ? "icon-button icon-button--active" : "icon-button"}
          type="button"
          aria-label={entryZotero.data?.saved ? t("savedToZotero") : t("saveToZotero")}
          title={
            entryZotero.data?.saved
              ? t("savedToZotero")
              : zoteroStatus.data?.available
                ? `${t("saveToZotero")} · ${zoteroStatus.data.collection_name || zoteroStatus.data.library_name || "Zotero"}`
                : t("zoteroUnavailable")
          }
          disabled={zoteroMutation.isPending || entryZotero.data?.saved}
          onClick={() => zoteroMutation.mutate(entry.id)}
        >
          {zoteroMutation.isPending ? (
            <CircleNotch className="spin" />
          ) : entryZotero.data?.saved ? (
            <CheckCircle weight="fill" />
          ) : (
            <Books />
          )}
        </button>
        {entry.canonical_url && (
          <a
            className="icon-button"
            href={entry.canonical_url}
            target="_blank"
            rel="noreferrer"
            aria-label={t("openOriginal")}
            title={t("openOriginal")}
          >
            <ArrowSquareOut />
          </a>
        )}
      </div>
      {zoteroMutation.error && (
        <div className="reader-toast reader-toast--error" role="alert">
          {zoteroMutation.error.message}
        </div>
      )}
      {createAnnotationMutation.error && (
        <div className="reader-toast reader-toast--error" role="alert">
          {createAnnotationMutation.error.message}
        </div>
      )}
      {aiFlash && (
        <div className="reader-toast reader-toast--ai" role="status">
          {aiFlash}
        </div>
      )}
      {aiError && (
        <div className="reader-toast reader-toast--error" role="alert">
          {aiError.message}
        </div>
      )}
      {appearanceOpen && (
        <aside className="reader-inspector" aria-label={t("readerAppearance")}>
          <header className="reader-inspector__header">
            <div>
              <span>{t("readerAppearance")}</span>
              <strong>{entryAnnotations.length}</strong>
            </div>
            <button
              className="icon-button icon-button--small"
              type="button"
              aria-label={t("close")}
              title={t("close")}
              onClick={() => setAppearanceOpen(false)}
            >
              <X />
            </button>
          </header>
          <section className="reader-inspector__section">
            <h2>{t("fontFamily")}</h2>
            <div className="reader-font-choice" role="group" aria-label={t("fontFamily")}>
              <button
                type="button"
                aria-pressed={readerAppearance.fontFamily === "serif"}
                onClick={() => setReaderAppearance({ fontFamily: "serif" })}
              >
                {t("serifFont")}
              </button>
              <button
                type="button"
                aria-pressed={readerAppearance.fontFamily === "sans"}
                onClick={() => setReaderAppearance({ fontFamily: "sans" })}
              >
                {t("sansSerifFont")}
              </button>
            </div>
          </section>
          <section className="reader-inspector__section reader-inspector__range">
            <div>
              <h2>{t("fontSize")}</h2>
              <output>{readerAppearance.fontSize}</output>
            </div>
            <span>
              <Minus />
              <input
                type="range"
                min="16"
                max="24"
                step="1"
                aria-label={t("fontSize")}
                value={readerAppearance.fontSize}
                onChange={(event) => setReaderAppearance({ fontSize: Number(event.target.value) })}
              />
              <Plus />
            </span>
          </section>
          <section className="reader-inspector__section reader-inspector__range">
            <div>
              <h2>{t("lineSpacing")}</h2>
              <output>{readerAppearance.lineHeight.toFixed(1)}</output>
            </div>
            <span>
              <TextAlignLeft />
              <input
                type="range"
                min="1.5"
                max="2.1"
                step="0.1"
                aria-label={t("lineSpacing")}
                value={readerAppearance.lineHeight}
                onChange={(event) =>
                  setReaderAppearance({ lineHeight: Number(event.target.value) })
                }
              />
              <TextAlignLeft />
            </span>
          </section>
          <section className="reader-inspector__section">
            <h2>{t("citation")}</h2>
            <div className="reader-citation-actions">
              <button
                type="button"
                className="button button--secondary"
                onClick={() => {
                  if (!entry) return
                  void navigator.clipboard
                    .writeText(formatBibTeX(entry))
                    .then(() => toast(t("citationCopied")))
                }}
              >
                BibTeX
              </button>
              <button
                type="button"
                className="button button--secondary"
                onClick={() => {
                  if (!entry) return
                  void navigator.clipboard
                    .writeText(formatGBT7714(entry))
                    .then(() => toast(t("citationCopied")))
                }}
              >
                GB/T 7714
              </button>
            </div>
            {entry?.doi && <p className="reader-citation-doi">DOI: {entry.doi}</p>}
          </section>
          <section className="reader-inspector__section reader-annotation-list">
            <h2>{t("annotations")}</h2>
            {entryAnnotations.map((annotation) => (
              <div className="reader-annotation-item" key={annotation.id}>
                <button type="button" onClick={() => scrollToAnnotation(annotation.id)}>
                  <AnnotationStyleIcon style={annotation.style} />
                  <span>
                    <strong>{annotation.quote}</strong>
                    {annotation.note && <small>{annotation.note}</small>}
                  </span>
                </button>
                <button
                  className="icon-button icon-button--small"
                  type="button"
                  aria-label={t("deleteAnnotation")}
                  title={t("deleteAnnotation")}
                  onClick={() => deleteAnnotationMutation.mutate(annotation.id)}
                >
                  <Trash />
                </button>
              </div>
            ))}
            {entryAnnotations.length === 0 && (
              <p className="reader-annotation-empty">{t("noAnnotations")}</p>
            )}
          </section>
        </aside>
      )}
      <div className="reader-scroll">
        <header className="article-header">
          <div className="article-header__source-row">
            <span className="article-header__source-mark" aria-hidden="true">
              {sourceImage ? (
                <img
                  src={sourceImage.url}
                  alt=""
                  loading="lazy"
                  referrerPolicy="no-referrer"
                  onError={() => {
                    if (sourceImage.kind === "lead") setSourceLeadImageFailed(true)
                    else setSourceIconFailed(true)
                  }}
                />
              ) : (
                entry.feed_title.slice(0, 1).toUpperCase()
              )}
            </span>
            <div>
              <p className="article-header__source">{entry.feed_title}</p>
              <div className="article-header__meta">
                {displayAuthors && <span>{displayAuthors}</span>}
                <time dateTime={entry.published_at}>
                  {new Intl.DateTimeFormat(locale, {
                    dateStyle: "medium",
                    timeStyle: "short",
                  }).format(new Date(entry.published_at))}
                </time>
                {entry.doi && (
                  <a
                    className="article-header__doi"
                    href={`https://doi.org/${entry.doi}`}
                    target="_blank"
                    rel="noreferrer"
                    title={t("openDOI")}
                  >
                    DOI {entry.doi}
                  </a>
                )}
              </div>
            </div>
          </div>
          <h1 lang={textLang(headline)}>{headline}</h1>
          {entry.ai_translated_title && (
            <p className="article-header__translation">
              <Translate />
              {entry.ai_translated_title}
            </p>
          )}
          {showHeaderSummary && (
            <div
              className={
                entry.ai_summary
                  ? "article-header__summary article-header__summary--ai"
                  : "article-header__summary"
              }
            >
              {entry.ai_summary && (
                <strong>
                  <Sparkle weight="fill" />
                  {t("aiSummary")}
                </strong>
              )}
              <p>{entry.ai_summary ?? entry.summary}</p>
            </div>
          )}
        </header>
        {translatedContent && (
          <div className="article-translation-switch" role="status">
            <span>
              <Translate />
              {t("translatedContent")}
            </span>
            <button
              className="button button--quiet"
              type="button"
              onClick={() => setShowOriginalContent((current) => !current)}
            >
              {showOriginalContent ? t("showTranslation") : t("showOriginal")}
            </button>
          </div>
        )}
        {translatedContent && !showOriginalContent ? (
          <div ref={contentRef} className="article-content article-content--translation">
            {translatedContent}
          </div>
        ) : safeHTML ? (
          <div
            ref={contentRef}
            className="article-content"
            onPointerUp={captureSelection}
            dangerouslySetInnerHTML={{ __html: safeHTML }}
          />
        ) : (
          <div ref={contentRef} className="article-content" onPointerUp={captureSelection}>
            <p>{entry.summary ?? t("noArticleContent")}</p>
          </div>
        )}
      </div>
      {pendingSelection && (
        <div
          className="reader-selection-tools"
          role="toolbar"
          aria-label={t("annotateSelection")}
          style={{ left: pendingSelection.left, top: pendingSelection.top }}
        >
          {!noteEditorOpen ? (
            <>
              <button
                type="button"
                aria-label={t("highlight")}
                title={t("highlight")}
                disabled={createAnnotationMutation.isPending}
                onClick={() => createAnnotation("highlight")}
              >
                <HighlighterCircle weight="fill" />
              </button>
              <button
                type="button"
                aria-label={t("underline")}
                title={t("underline")}
                disabled={createAnnotationMutation.isPending}
                onClick={() => createAnnotation("underline")}
              >
                <TextUnderline />
              </button>
              <button
                type="button"
                aria-label={t("wavyUnderline")}
                title={t("wavyUnderline")}
                disabled={createAnnotationMutation.isPending}
                onClick={() => createAnnotation("wavy")}
              >
                <WaveSine />
              </button>
              <button
                type="button"
                aria-label={t("addNote")}
                title={t("addNote")}
                disabled={createAnnotationMutation.isPending}
                onClick={() => setNoteEditorOpen(true)}
              >
                <NotePencil />
              </button>
            </>
          ) : (
            <form className="reader-selection-note" onSubmit={saveNote}>
              <input
                autoFocus
                value={noteDraft}
                placeholder={t("notePlaceholder")}
                aria-label={t("addNote")}
                disabled={createAnnotationMutation.isPending}
                onChange={(event) => setNoteDraft(event.target.value)}
              />
              <button
                type="submit"
                disabled={!noteDraft.trim() || createAnnotationMutation.isPending}
              >
                {t("saveNote")}
              </button>
            </form>
          )}
        </div>
      )}
    </article>
  )
}

// 标题的字距按标题自己的文字判定，不按界面 locale：中文期刊的标题在英文界面下仍是
// 中文，反之亦然。CJK 区间命中就交给 zh-CN，:lang(en) 的负字距规则才不会套到中文上。
function textLang(value: string) {
  return /[\u3400-\u9fff]/.test(value) ? "zh-CN" : "en"
}

function AnnotationStyleIcon(props: { style: AnnotationStyle }) {  if (props.style === "underline") return <TextUnderline />
  if (props.style === "wavy") return <WaveSine />
  return <HighlighterCircle weight="fill" />
}
