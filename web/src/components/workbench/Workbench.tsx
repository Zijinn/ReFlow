import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Books,
  CalendarBlank,
  ChartPieSlice,
  NotePencil,
  PaperPlaneTilt,
} from "@phosphor-icons/react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import {
  createResearchPaper,
  createResearchTag,
  deleteResearchPaper,
  fetchResearchCitation,
  listPreferences,
  listResearchPapers,
  listResearchTags,
  moveResearchPaper,
  putPreference,
  reorderResearchPapers,
  updateResearchPaper,
} from "../../api/client"
import type {
  AIProfile,
  ListResponse,
  ResearchKind,
  ResearchPaper,
  ResearchPaperPatch,
  ResearchTag,
} from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { useOnlineState } from "../../lib/online"
import { isEnglishPaper, normalizeDoi } from "../../lib/research"
import { toast } from "../../store/toast"
import { AIWorkbench } from "../AIWorkbench"
import { ConfirmDialog } from "../ConfirmDialog"
import { CalendarPage } from "./CalendarPage"
import { Dashboard } from "./Dashboard"
import { PublishedPage } from "./PublishedPage"
import { ResearchPage } from "./ResearchPage"
import { SubmittedPage } from "./SubmittedPage"
import { daysUntil, parseDeadline } from "./utils"

type WorkbenchTab = "dashboard" | "research" | "submitted" | "published" | "calendar"

const TABS: Array<{ id: WorkbenchTab; labelKey: string; Icon: typeof Books }> = [
  { id: "dashboard", labelKey: "researchOverview", Icon: ChartPieSlice },
  { id: "research", labelKey: "workingPapers", Icon: NotePencil },
  { id: "submitted", labelKey: "submissions", Icon: PaperPlaneTilt },
  { id: "published", labelKey: "publications", Icon: Books },
  { id: "calendar", labelKey: "calendar", Icon: CalendarBlank },
]

// `/ai/paper-chat` accepts 1..20 ids; anything longer is a 400.
const AI_PAPER_LIMIT = 20

function deadlineDays(paper: ResearchPaper): number {
  const date = parseDeadline(paper.deadline)
  return date ? daysUntil(date) : Number.MAX_SAFE_INTEGER
}

function truncateTitle(title: string): string {
  const trimmed = title.trim()
  if (trimmed.length === 0) return "—"
  return trimmed.length > 36 ? `${trimmed.slice(0, 36)}…` : trimmed
}

function aiContextKey(tab: WorkbenchTab): string {
  if (tab === "research") return "aiContextResearch"
  if (tab === "submitted") return "aiContextSubmitted"
  if (tab === "published") return "aiContextPublished"
  if (tab === "calendar") return "aiContextCalendar"
  return "aiContextOverview"
}

export interface WorkbenchProps {
  /** The shell owns the AI toggle so one button drives reader and workbench. */
  aiOpen?: boolean
  aiProfiles?: AIProfile[]
  aiPanelWidth?: number
  onAIPanelWidthChange?: (width: number) => void
  onAskAI?: () => void
  onCloseAI?: () => void
  onConfigureAI?: () => void
}

export function Workbench(props: WorkbenchProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const online = useOnlineState()
  const [tab, setTab] = useState<WorkbenchTab>("dashboard")
  const [focusPaperID, setFocusPaperID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<{ message: string; action: () => void } | null>(null)
  const [batchPending, setBatchPending] = useState(false)
  const [citationProgress, setCitationProgress] = useState<{ done: number; total: number } | null>(
    null,
  )
  const shellRef = useRef<HTMLDivElement>(null)
  const [openRowID, setOpenRowID] = useState<string | null>(null)
  // Stable so the submissions page's jump effect does not re-run every render.
  const clearFocus = useCallback(() => setFocusPaperID(null), [])

  const kinds: ResearchKind[] = ["research", "submitted", "published"]
  const results = useQueries({
    queries: kinds.map((kind) => ({
      queryKey: ["research", kind],
      queryFn: ({ signal }: { signal: AbortSignal }) => listResearchPapers(kind, signal),
    })),
  })
  // Memoised per kind: the AI context memo below depends on these, and a fresh
  // `?? []` on every render would recompute the paper set (and remount nothing,
  // but re-key nothing) for no reason.
  const researchItems = results[0]?.data?.items
  const submittedItems = results[1]?.data?.items
  const publishedItems = results[2]?.data?.items
  const research = useMemo(() => researchItems ?? [], [researchItems])
  const submitted = useMemo(() => submittedItems ?? [], [submittedItems])
  const published = useMemo(() => publishedItems ?? [], [publishedItems])
  const isLoading = results.some((result) => result.isPending)
  const hasError = results.some((result) => result.isError)
  const retryAll = () => {
    for (const result of results) void result.refetch()
  }

  // The assistant answers about the rows the owner is looking at. Expansion
  // state lives inside each page component, which the shell does not own, so it
  // is read back from the markup the pages already produce: a row is
  // `tr[data-paper-id]` and its toggle carries `aria-expanded`. Changing tab
  // drops the focus here (an event handler, not an effect) because the new tab
  // has no expanded row yet.
  const selectTab = useCallback((next: WorkbenchTab) => {
    setOpenRowID(null)
    setTab(next)
  }, [])

  useEffect(() => {
    const shell = shellRef.current
    if (!shell) return
    let frame = 0
    const read = () => {
      const toggle = shell.querySelector<HTMLElement>('[data-paper-id] [aria-expanded="true"]')
      // Read the attribute, not `dataset.paperID`: the camelCase key for
      // `data-paper-id` is `paperId`, so the dataset form silently returned
      // undefined and the context never narrowed.
      const id = toggle?.closest("[data-paper-id]")?.getAttribute("data-paper-id") ?? null
      setOpenRowID((current) => (current === id ? current : id))
    }
    // React attaches its handlers at the root container, i.e. above this
    // bubble-phase listener, so the state update it schedules has to be given a
    // frame before the DOM reflects the new expanded row.
    const onClick = () => {
      window.cancelAnimationFrame(frame)
      frame = window.requestAnimationFrame(read)
    }
    shell.addEventListener("click", onClick)
    return () => {
      shell.removeEventListener("click", onClick)
      window.cancelAnimationFrame(frame)
    }
  }, [tab])

  // Paper set per tab: the calendar only ever shows submitted deadlines, so it
  // scopes to the papers that actually have one; the overview spans the whole
  // workspace. An expanded row narrows the context to that single paper.
  const tabPapers = useMemo<ResearchPaper[]>(() => {
    if (tab === "research") return research
    if (tab === "submitted") return submitted
    if (tab === "published") return published
    if (tab === "calendar") {
      return submitted
        .filter((paper) => parseDeadline(paper.deadline) !== null)
        .sort((left, right) => deadlineDays(left) - deadlineDays(right))
    }
    return [...research, ...submitted, ...published]
  }, [published, research, submitted, tab])

  const focusedPaper = openRowID ? tabPapers.find((paper) => paper.id === openRowID) ?? null : null
  const aiPaperIDs = useMemo(
    () =>
      (focusedPaper ? [focusedPaper] : tabPapers.slice(0, AI_PAPER_LIMIT)).map((paper) => paper.id),
    [focusedPaper, tabPapers],
  )
  const aiContextLabel = focusedPaper
    ? `${t("aiContextFocusedPaper")} · ${truncateTitle(focusedPaper.title)}`
    : `${t(aiContextKey(tab))} · ${tabPapers.length}`
  // The digest's per-section action bubble jumps to the paper's own view.
  // Submitted papers additionally get the existing calendar-style focus, so
  // the bubble does exactly what the calendar event click already does.
  const openPaperFromDigest = useCallback(
    (paperID: string) => {
      const found = [...research, ...submitted, ...published].find(
        (paper) => paper.id === paperID,
      )
      if (!found) return
      if (found.kind === "submitted") setFocusPaperID(paperID)
      selectTab(found.kind)
    },
    [published, research, selectTab, submitted],
  )
  const preferences = useQuery({
    queryKey: ["preferences"],
    queryFn: ({ signal }) => listPreferences(signal),
  })
  const crossrefEmail = useMemo(() => {
    const raw = preferences.data?.items?.["crossref_email"]
    return typeof raw === "string" ? raw : ""
  }, [preferences.data])

  // 标签调色板：服务端按 position 升序返回，前端直接按下标取色/排序。
  const tagsQuery = useQuery({
    queryKey: ["research-tags"],
    queryFn: ({ signal }: { signal: AbortSignal }) => listResearchTags(signal),
  })
  const tagItems = tagsQuery.data?.tags
  const tags = useMemo(() => tagItems ?? [], [tagItems])

  const invalidate = (kind: ResearchKind) =>
    queryClient.invalidateQueries({ queryKey: ["research", kind] })
  const invalidateAll = () => queryClient.invalidateQueries({ queryKey: ["research"] })

  // 乐观更新：先改本地缓存即时反馈，失败回滚并 toast。
  // 所有快照/恢复都走 ["research", kind] 缓存，与 useQueries 的 queryKey 一致。
  type PaperList = ListResponse<ResearchPaper>
  const snapshotKind = async (kind: ResearchKind) => {
    await queryClient.cancelQueries({ queryKey: ["research", kind] })
    return queryClient.getQueryData<PaperList>(["research", kind])
  }
  const snapshotAll = async () => {
    await queryClient.cancelQueries({ queryKey: ["research"] })
    const snap = new Map<ResearchKind, PaperList | undefined>()
    for (const kind of kinds) snap.set(kind, queryClient.getQueryData<PaperList>(["research", kind]))
    return snap
  }
  const restoreKind = (kind: ResearchKind, prev: PaperList | undefined) =>
    queryClient.setQueryData(["research", kind], prev)
  const restoreAll = (snap: Map<ResearchKind, PaperList | undefined>) => {
    for (const [kind, prev] of snap) queryClient.setQueryData(["research", kind], prev)
  }

  // The workbench has no offline outbox: writes must reach the server
  // immediately, so guard mutating entry points and tell the user why an
  // action did nothing while the browser is offline.
  const requireOnline = (): boolean => {
    if (online) return true
    toast(t("workbenchOfflineHint"))
    return false
  }

  const createMutation = useMutation({
    mutationFn: (kind: ResearchKind) => createResearchPaper({ kind }),
    onSuccess: (_paper, kind) => void invalidate(kind),
    onError: () => toast(t("createPaperFailed")),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: ResearchPaperPatch }) =>
      updateResearchPaper(id, patch),
    onMutate: async ({ id, patch }) => {
      const prev = await snapshotAll()
      for (const kind of kinds) {
        queryClient.setQueryData<PaperList>(["research", kind], (old) =>
          old ? { ...old, items: old.items.map((p) => (p.id === id ? { ...p, ...patch } : p)) } : old,
        )
      }
      return { prev }
    },
    onError: (_error, _vars, context) => {
      if (context) restoreAll(context.prev)
      toast(t("updatePaperFailed"))
    },
    onSuccess: (paper) => void invalidate(paper.kind),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteResearchPaper(id),
    onMutate: async (id) => {
      const prev = await snapshotAll()
      for (const kind of kinds) {
        queryClient.setQueryData<PaperList>(["research", kind], (old) =>
          old ? { ...old, items: old.items.filter((p) => p.id !== id) } : old,
        )
      }
      return { prev }
    },
    onError: (_error, _vars, context) => {
      if (context) restoreAll(context.prev)
      toast(t("deletePaperFailed"))
    },
    onSuccess: () => void invalidateAll(),
  })
  const reorderMutation = useMutation({
    mutationFn: ({ kind, ids }: { kind: ResearchKind; ids: string[] }) =>
      reorderResearchPapers(kind, ids),
    onMutate: async ({ kind, ids }) => {
      const prev = await snapshotKind(kind)
      const rank = new Map(ids.map((id, index) => [id, index] as const))
      queryClient.setQueryData<PaperList>(["research", kind], (old) =>
        old
          ? {
              ...old,
              items: old.items
                .map((p, index) => ({ p, sort: rank.get(p.id) ?? ids.length + index }))
                .sort((a, b) => a.sort - b.sort)
                .map(({ p }) => p),
            }
          : old,
      )
      return { prev }
    },
    onError: (_error, vars, context) => {
      if (context) restoreKind(vars.kind, context.prev)
      toast(t("reorderFailed"))
    },
    onSuccess: (_result, { kind }) => void invalidate(kind),
  })
  const moveMutation = useMutation({
    mutationFn: ({ id, target }: { id: string; target: ResearchKind }) =>
      moveResearchPaper(id, target),
    onMutate: async ({ id, target }) => {
      const prev = await snapshotAll()
      let moving: ResearchPaper | null = null
      for (const kind of kinds) {
        const list = queryClient.getQueryData<PaperList>(["research", kind])
        const found = list?.items.find((p) => p.id === id)
        if (found) {
          moving = found
          queryClient.setQueryData<PaperList>(["research", kind], {
            ...list!,
            items: list!.items.filter((p) => p.id !== id),
          })
        }
      }
      if (moving) {
        const moved: ResearchPaper = { ...moving, kind: target }
        queryClient.setQueryData<PaperList>(["research", target], (old) =>
          old ? { ...old, items: [...old.items, moved] } : old,
        )
      }
      return { prev }
    },
    onError: (_error, _vars, context) => {
      if (context) restoreAll(context.prev)
      toast(t("moveFailed"))
    },
    onSuccess: () => void invalidateAll(),
  })
  const citationMutation = useMutation({
    mutationFn: (id: string) => fetchResearchCitation(id),
    onSuccess: (paper) => {
      void invalidate(paper.kind)
      toast(t("citationUpdated"))
    },
    onError: () => toast(t("citationFetchFailed")),
  })
  const emailMutation = useMutation({
    mutationFn: (email: string) => putPreference("crossref_email", email),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["preferences"] }),
    onError: () => toast(t("emailSaveFailed")),
  })
  // 新建标签：成功后刷新调色板；调用方（在研页的标签格）拿到新标签立即指派。
  // 失败（409 重名 / 400 空名或超长）在这里统一 toast，页面只保留输入现场。
  const createTagMutation = useMutation({
    mutationFn: (name: string) => createResearchTag(name),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["research-tags"] }),
    onError: () => toast(t("tagCreateFailed")),
  })

  const requestConfirm = (message: string, action: () => void) => setConfirm({ message, action })

  const create = (kind: ResearchKind) => {
    if (!requireOnline()) return
    createMutation.mutate(kind)
  }
  const update = (id: string, patch: ResearchPaperPatch) => {
    if (!requireOnline()) return
    updateMutation.mutate({ id, patch })
  }
  const remove = (id: string) => {
    if (!requireOnline()) return
    requestConfirm(t("deletePaperConfirm"), () => deleteMutation.mutate(id))
  }
  const reorder = (kind: ResearchKind, ids: string[]) => {
    if (!requireOnline()) return
    reorderMutation.mutate({ kind, ids })
  }
  const move = (id: string, target: ResearchKind) => {
    if (!requireOnline()) return
    const message =
      target === "submitted"
        ? t("flowToSubmittedConfirm")
        : target === "published"
          ? t("flowToPublishedConfirm")
          : t("flowToResearchConfirm")
    requestConfirm(message, () => moveMutation.mutate({ id, target }))
  }
  const fetchCitation = (id: string) => {
    if (!requireOnline()) return
    citationMutation.mutate(id)
  }
  const creating = createMutation.isPending
  const fetchAllCitations = async () => {
    if (!requireOnline()) return
    if (!crossrefEmail) {
      toast(t("citationNoEmail"))
      return
    }
    const eligible = published.filter((p) => isEnglishPaper(p) && normalizeDoi(p.doi))
    if (eligible.length === 0) {
      toast(t("citationsBatchNoneEligible"))
      return
    }
    setBatchPending(true)
    setCitationProgress({ done: 0, total: eligible.length })
    let ok = 0
    let failed = 0
    // Sequential on purpose: Crossref is rate-limited and the polite pool
    // expects modest concurrency.
    for (const paper of eligible) {
      try {
        await fetchResearchCitation(paper.id)
        ok += 1
      } catch {
        failed += 1
      }
      setCitationProgress({ done: ok + failed, total: eligible.length })
    }
    setBatchPending(false)
    setCitationProgress(null)
    void invalidate("published")
    const message =
      failed > 0
        ? `${ok} ${t("citationsBatchUpdated")} · ${failed} ${t("citationsBatchFailed")}`
        : `${ok} ${t("citationsBatchUpdated")}`
    toast(message)
  }
  const saveEmail = (email: string) => {
    if (!requireOnline()) return
    emailMutation.mutate(email)
  }
  // Resolved value contract for ResearchPage: the created tag on success, null
  // on any failure (offline guard, 400/409 from the server) — the toast above
  // already told the user why.
  const createTag = async (name: string): Promise<ResearchTag | null> => {
    if (!requireOnline()) return null
    try {
      return await createTagMutation.mutateAsync(name)
    } catch {
      return null
    }
  }

  const subtitle: Record<WorkbenchTab, string> = {
    dashboard: t("researchOverviewSubtitle"),
    research: t("workingPapersSubtitle"),
    submitted: t("submissionsSubtitle"),
    published: t("publicationsSubtitle"),
    calendar: t("calendarSubtitle"),
  }
  const title: Record<WorkbenchTab, string> = {
    dashboard: t("researchOverview"),
    research: t("workingPapers"),
    submitted: t("submissions"),
    published: t("publications"),
    calendar: t("calendar"),
  }

  return (
    <div className="wb-shell" ref={shellRef}>
      <div className="wb-main">
        <nav className="wb-nav" aria-label={t("workbench")}>
          {TABS.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className={`wb-nav-item ${tab === entry.id ? "wb-nav-item--active" : ""}`}
              aria-current={tab === entry.id ? "page" : undefined}
              onClick={() => selectTab(entry.id)}
            >
              <entry.Icon size={15} weight={tab === entry.id ? "fill" : "regular"} />
              <span>{t(entry.labelKey)}</span>
            </button>
          ))}
        </nav>
        {!online && (
          <div className="offline-banner" role="status">
            {t("workbenchOfflineHint")}
          </div>
        )}
        <header className="wb-header">
          <div>
            <div className="wb-eyebrow">{t("eyebrowPersonalManagement")}</div>
            <h1>{title[tab]}</h1>
            <p>{subtitle[tab]}</p>
          </div>
          <div className="wb-status-bar">
            <div className="wb-status-item">
              <strong>{research.length}</strong>
              <span>{t("researchCount")}</span>
            </div>
            <div className="wb-status-item">
              <strong>{submitted.length}</strong>
              <span>{t("submittedCount")}</span>
            </div>
            <div className="wb-status-item">
              <strong>{published.length}</strong>
              <span>{t("publishedCount")}</span>
            </div>
          </div>
        </header>
        <section className="wb-content">
          {hasError ? (
            <div className="wb-empty" role="alert">
              <p>{t("papersLoadFailed")}</p>
              <button type="button" className="wb-btn" onClick={retryAll}>
                {t("retry")}
              </button>
            </div>
          ) : isLoading ? (
            <div className="wb-empty">{t("loading")}</div>
          ) : (
            <>
              {tab === "dashboard" && (
                <Dashboard
                  research={research}
                  submitted={submitted}
                  published={published}
                  tags={tags}
                  aiProfiles={props.aiProfiles ?? []}
                  onConfigureAI={props.onConfigureAI}
                  onAskAI={props.onAskAI}
                  onOpenPaper={openPaperFromDigest}
                  onNavigate={selectTab}
                />
              )}
              {tab === "research" && (
                <ResearchPage
                  papers={research}
                  tags={tags}
                  offline={!online}
                  creating={creating}
                  onCreate={() => create("research")}
                  onUpdate={update}
                  onDelete={remove}
                  onReorder={(ids) => reorder("research", ids)}
                  onMove={(id) => move(id, "submitted")}
                  onCreateTag={createTag}
                />
              )}
              {tab === "submitted" && (
                <SubmittedPage
                  papers={submitted}
                  offline={!online}
                  creating={creating}
                  focusPaperID={focusPaperID}
                  onFocusConsumed={clearFocus}
                  onCreate={() => create("submitted")}
                  onUpdate={update}
                  onDelete={remove}
                  onReorder={(ids) => reorder("submitted", ids)}
                  onMove={(id) => move(id, "published")}
                  onMoveBack={(id) => move(id, "research")}
                />
              )}
              {tab === "published" && (
                <PublishedPage
                  papers={published}
                  offline={!online}
                  creating={creating}
                  crossrefEmail={crossrefEmail}
                  aiProfiles={props.aiProfiles ?? []}
                  citationPendingID={
                    citationMutation.isPending ? (citationMutation.variables ?? null) : null
                  }
                  onCreate={() => create("published")}
                  onUpdate={update}
                  onDelete={remove}
                  onReorder={(ids) => reorder("published", ids)}
                  onFetchCitation={fetchCitation}
                  onFetchAllCitations={() => void fetchAllCitations()}
                  batchCitationPending={batchPending}
                  batchCitationProgress={citationProgress}
                  onCrossrefEmailChange={saveEmail}
                />
              )}
              {tab === "calendar" && (
                <CalendarPage
                  papers={submitted}
                  aiProfiles={props.aiProfiles ?? []}
                  onConfigureAI={props.onConfigureAI}
                  onAskAI={props.onAskAI}
                  onSelectPaper={(id) => {
                    setFocusPaperID(id)
                    selectTab("submitted")
                  }}
                />
              )}
            </>
          )}
        </section>
      </div>
      {props.aiOpen && (
        <AIWorkbench
          paperIDs={aiPaperIDs}
          profiles={props.aiProfiles ?? []}
          width={props.aiPanelWidth ?? 380}
          contextLabel={aiContextLabel}
          initialMode="chat"
          onWidthChange={props.onAIPanelWidthChange ?? (() => {})}
          onClose={props.onCloseAI ?? (() => {})}
          onConfigure={props.onConfigureAI ?? (() => {})}
        />
      )}
      <ConfirmDialog
        open={confirm !== null}
        message={confirm?.message ?? ""}
        onOpenChange={(open) => {
          if (!open) setConfirm(null)
        }}
        onConfirm={() => {
          const action = confirm?.action
          setConfirm(null)
          action?.()
        }}
      />
    </div>
  )
}
