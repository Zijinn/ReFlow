import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { Sparkle } from "@phosphor-icons/react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import {
  AI_CONTEXT_CAP,
  createQuickTask,
  createResearchPaper,
  createResearchTag,
  deleteResearchPaper,
  fetchResearchCitation,
  listPreferences,
  listQuickTasks,
  listResearchPapers,
  listResearchTags,
  listTaskThreads,
  moveResearchPaper,
  putPreference,
  reorderResearchPapers,
  updateResearchPaper,
} from "../../api/client"
import type {
  AIProfile,
  ListResponse,
  QuickTask,
  ResearchKind,
  ResearchPaper,
  ResearchPaperPatch,
  ResearchTag,
  TaskThread,
} from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { useOnlineState } from "../../lib/online"
import { isEnglishPaper, normalizeDoi } from "../../lib/research"
import { useReaderStore, type WorkbenchModule, type WorkbenchTab } from "../../store/reader"
import { toast } from "../../store/toast"
import { AIWorkbench } from "../AIWorkbench"
import { ConfirmDialog } from "../ConfirmDialog"
import { CalendarPage } from "./CalendarPage"
import { Dashboard } from "./Dashboard"
import { PublishedPage } from "./PublishedPage"
import { ResearchPage } from "./ResearchPage"
import { SubmittedPage } from "./SubmittedPage"
import { RESEARCH_TABS } from "./researchTabs"
import { TaskManagement } from "./TaskManagement"
import { QUICK_TASK_QUERY_KEY, TASK_QUERY_KEY } from "./taskQueryKeys"
import { deadlineDays, formatDeadline, parseDeadline } from "./utils"

// 个人工作台的三个一级模块：任务管理、日历、科研工作台。日历从科研子标签
// 升上来之后，科研工作台只剩四张表；选中任务或日历时不再渲染科研标签条。
const MODULES: Array<{ id: WorkbenchModule; labelKey: string }> = [
  { id: "calendar", labelKey: "moduleCalendar" },
  { id: "tasks", labelKey: "moduleTasks" },
  { id: "research", labelKey: "moduleResearch" },
]

// `/ai/paper-chat` accepts 1..20 ids; anything longer is a 400. The cap lives in
// the api layer because the panel has to print it when it bites (see contextTotal).

function truncateTitle(title: string): string {
  const trimmed = title.trim()
  if (trimmed.length === 0) return "—"
  return trimmed.length > 36 ? `${trimmed.slice(0, 36)}…` : trimmed
}

function aiContextKey(tab: WorkbenchTab): string {
  if (tab === "research") return "aiContextResearch"
  if (tab === "submitted") return "aiContextSubmitted"
  if (tab === "published") return "aiContextPublished"
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
  onOpenPreferences?: () => void
  onOpenReader?: () => void
}

export function Workbench(props: WorkbenchProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const online = useOnlineState()
  const tab = useReaderStore((state) => state.workbenchTab)
  const setTab = useReaderStore((state) => state.setWorkbenchTab)
  const digestRunRef = useRef<(() => void) | null>(null)
  const [digestState, setDigestState] = useState({ running: false, hasText: false })
  const registerDigestRun = useCallback((run: (() => void) | null) => {
    digestRunRef.current = run
  }, [])
  const handleDigestState = useCallback(
    (state: { running: boolean; hasText: boolean }) => setDigestState(state),
    [],
  )
  const aiReady = (props.aiProfiles ?? []).some((profile) => profile.enabled)
  const now = new Date()
  const todayLabel = `${now.getFullYear()}.${String(now.getMonth() + 1).padStart(2, "0")}.${String(
    now.getDate(),
  ).padStart(2, "0")}`
  // 模块选择存在 reader store 里（和 appView、栏宽同一条 persist 通道）：
  // 这是"用户上次在看哪一块"，重开应用该回到同一块，而不是每次都回科研。
  const workbenchModule = useReaderStore((state) => state.workbenchModule)
  const setWorkbenchModule = useReaderStore((state) => state.setWorkbenchModule)
  const setTaskView = useReaderStore((state) => state.setTaskView)
  const setTaskFocus = useReaderStore((state) => state.setTaskFocus)
  const taskFocus = useReaderStore((state) => state.taskFocus)
  const clearTaskFocus = useReaderStore((state) => state.clearTaskFocus)
  const [focusPaperID, setFocusPaperID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<{ message: string; action: () => void } | null>(null)
  const [batchPending, setBatchPending] = useState(false)
  const [citationProgress, setCitationProgress] = useState<{ done: number; total: number } | null>(
    null,
  )
  const shellRef = useRef<HTMLDivElement>(null)
  const [openRowID, setOpenRowID] = useState<string | null>(null)
  const [pendingNextActionPaperIDs, setPendingNextActionPaperIDs] = useState<Set<string>>(
    () => new Set(),
  )
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
  }, [setTab])

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

  // Paper set per tab: the calendar shows the deadlines that actually exist on
  // either table — an internal due date on a 在研 paper counts just as much as a
  // submission date — so it scopes to papers carrying one and the overview spans
  // the whole workspace. An expanded row narrows the context to that single paper.
  const tabPapers = useMemo<ResearchPaper[]>(() => {
    if (workbenchModule === "calendar") {
      return [...research, ...submitted]
        .filter((paper) => parseDeadline(paper.deadline) !== null)
        .sort((left, right) => deadlineDays(left) - deadlineDays(right))
    }
    if (tab === "research") return research
    if (tab === "submitted") return submitted
    if (tab === "published") return published
    return [...research, ...submitted, ...published]
  }, [published, research, submitted, tab, workbenchModule])

  const focusedPaper = openRowID ? tabPapers.find((paper) => paper.id === openRowID) ?? null : null
  const aiPaperIDs = useMemo(
    () =>
      (focusedPaper ? [focusedPaper] : tabPapers.slice(0, AI_CONTEXT_CAP)).map((paper) => paper.id),
    [focusedPaper, tabPapers],
  )
  const calendarPapers = useMemo(() => [...research, ...submitted], [research, submitted])
  const aiContextLabel = focusedPaper
    ? `${t("aiContextFocusedPaper")} · ${truncateTitle(focusedPaper.title)}`
    : `${t(workbenchModule === "calendar" ? "aiContextCalendar" : aiContextKey(tab))} · ${tabPapers.length}`
  // The digest's per-section action bubble jumps to the paper's own view.
  // Submitted papers additionally get the existing calendar-style focus, so
  // the bubble does exactly what the calendar event click already does.
  const openPaperFromDigest = useCallback(
    (paperID: string) => {
      const found = [...research, ...submitted, ...published].find(
        (paper) => paper.id === paperID,
      )
      if (!found) return
      setWorkbenchModule("research")
      if (found.kind === "submitted") setFocusPaperID(paperID)
      selectTab(found.kind)
    },
    [published, research, selectTab, setWorkbenchModule, submitted],
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

  // The calendar and the next-action button read the same task cache as the
  // task module, so opening either surface later does not fetch a second copy.
  const quickTasksQuery = useQuery({
    queryKey: QUICK_TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listQuickTasks(signal),
  })
  const taskThreadsQuery = useQuery({
    queryKey: TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listTaskThreads(signal),
  })
  const quickTasks = useMemo<QuickTask[]>(
    () => quickTasksQuery.data?.items ?? [],
    [quickTasksQuery.data?.items],
  )
  const taskThreads = useMemo<TaskThread[]>(
    () => taskThreadsQuery.data?.items ?? [],
    [taskThreadsQuery.data?.items],
  )
  // Only quick tasks created from a next action drive the "已加入任务" state;
  // a long-task link is a different relationship and must not block this button.
  const linkedPaperIDs = useMemo(() => {
    const ids = new Set<string>()
    for (const task of quickTasks) {
      if (task.paper_id && !task.completed) ids.add(task.paper_id)
    }
    return ids
  }, [quickTasks])

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
  const addNextActionTaskMutation = useMutation({
    mutationFn: (paper: ResearchPaper) => {
      const today = formatDeadline(new Date())
      const due = parseDeadline(paper.deadline) ? paper.deadline.trim() : ""
      // A deadline already in the past cannot be the end of a window that
      // starts today; keep it as a one-day overdue marker instead.
      const start = due && due < today ? "" : today
      return createQuickTask({
        title: paper.next_action.trim(),
        paper_id: paper.id,
        start_date: start,
        due_date: due,
      })
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: QUICK_TASK_QUERY_KEY })
      toast(t("nextActionTaskAdded"))
    },
    onError: () => toast(t("nextActionTaskFailed")),
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
  const addNextActionTask = (paper: ResearchPaper) => {
    if (!requireOnline() || !paper.next_action.trim()) return
    if (pendingNextActionPaperIDs.has(paper.id)) return
    setPendingNextActionPaperIDs((current) => new Set(current).add(paper.id))
    addNextActionTaskMutation.mutate(paper, {
      onSettled: () => {
        setPendingNextActionPaperIDs((current) => {
          const next = new Set(current)
          next.delete(paper.id)
          return next
        })
      },
    })
  }
  const openTaskFromCalendar = useCallback(
    (kind: "quick" | "assignment", taskID: string) => {
      setWorkbenchModule("tasks")
      setTaskView(kind === "quick" ? "quick" : "long")
      setTaskFocus({ kind, id: taskID })
    },
    [setTaskFocus, setTaskView, setWorkbenchModule],
  )
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
  }
  const title: Record<WorkbenchTab, string> = {
    dashboard: t("researchOverview"),
    research: t("workingPapers"),
    submitted: t("submissions"),
    published: t("publications"),
  }

  const isTasks = workbenchModule === "tasks"
  const isCalendar = workbenchModule === "calendar"
  const isResearch = workbenchModule === "research"
  const heading = isTasks ? t("moduleTasks") : isCalendar ? t("calendar") : title[tab]
  const subheading = isTasks
    ? t("tasksModuleSubtitle")
    : isCalendar
      ? t("calendarSubtitle")
      : subtitle[tab]

  return (
    <div className="wb-shell" ref={shellRef}>
      <div className="wb-main" data-wb-tab={isTasks ? "tasks" : isCalendar ? "calendar" : tab}>
        {/* 模块切换条。它是 role="group" 而不是第二条 <nav>：这一屏本来就有一条
            导航（下面那五格），多一条同级 landmark 会让"这页有几条导航"变得读不出
            来，而这一排只是在一块板子和五张表之间二选一。 */}
        <div className="wb-nav wb-task-module-nav" role="group" aria-label={t("workbench")}>
          {MODULES.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className={`wb-nav-item ${workbenchModule === entry.id ? "wb-nav-item--active" : ""}`}
              aria-current={workbenchModule === entry.id ? "page" : undefined}
              onClick={() => setWorkbenchModule(entry.id)}
            >
              <span>{t(entry.labelKey)}</span>
            </button>
          ))}
        </div>
        {isResearch && (
          <nav className="wb-nav wb-research-nav" aria-label={t("moduleResearch")}>
            {RESEARCH_TABS.map((entry) => (
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
        )}
        {!online && (
          <div className="offline-banner" role="status">
            {t("workbenchOfflineHint")}
          </div>
        )}
        <header className="wb-header">
          <div>
            <div className="wb-eyebrow">{t("eyebrowPersonalManagement")}</div>
            <h1>{heading}</h1>
            <p>{subheading}</p>
          </div>
          {/* 总览抬头只留日期与生成按钮；三组计数已经进了总览的指标卡。
              其他科研页仍保留原来的三格状态栏。 */}
          {isResearch &&
            (tab === "dashboard" ? (
              <div className="wb-overview-actionbar">
                <span className="wb-overview-date">{todayLabel}</span>
                <button
                  type="button"
                  className="wb-btn wb-btn--primary"
                  disabled={digestState.running}
                  onClick={() =>
                    aiReady ? digestRunRef.current?.() : props.onConfigureAI?.()
                  }
                >
                  <Sparkle size={14} weight="fill" aria-hidden="true" />
                  {digestState.running
                    ? t("overviewGenerating")
                    : digestState.hasText
                      ? t("overviewRegenerate")
                      : aiReady
                        ? t("overviewGenerate")
                        : t("configureAI")}
                </button>
              </div>
            ) : (
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
            ))}
        </header>
        <section className="wb-content">
          {isTasks ? (
            <TaskManagement
              offline={!online}
              focus={taskFocus}
              onFocusConsumed={clearTaskFocus}
            />
          ) : hasError ? (
            <div className="wb-empty" role="alert">
              <p>{t("papersLoadFailed")}</p>
              <button type="button" className="wb-btn" onClick={retryAll}>
                {t("retry")}
              </button>
            </div>
          ) : isLoading ? (
            <div className="wb-empty">{t("loading")}</div>
          ) : isCalendar ? (
            <CalendarPage
              papers={calendarPapers}
              aiProfiles={props.aiProfiles ?? []}
              onConfigureAI={props.onConfigureAI}
              onAskAI={props.onAskAI}
              onSelectPaper={openPaperFromDigest}
              quickTasks={quickTasks}
              taskThreads={taskThreads}
              onSelectTask={openTaskFromCalendar}
            />
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
                  onNavigate={(next) => {
                    if (next === "calendar") {
                      setWorkbenchModule("calendar")
                      return
                    }
                    selectTab(next)
                  }}
                  onOpenTasks={() => setWorkbenchModule("tasks")}
                  onOpenPreferences={props.onOpenPreferences}
                  onOpenReader={props.onOpenReader}
                  onRegisterDigestRun={registerDigestRun}
                  onDigestStateChange={handleDigestState}
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
                  linkedPaperIDs={linkedPaperIDs}
                  addingTaskPaperIDs={pendingNextActionPaperIDs}
                  onAddNextActionTask={addNextActionTask}
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
                  linkedPaperIDs={linkedPaperIDs}
                  addingTaskPaperIDs={pendingNextActionPaperIDs}
                  onAddNextActionTask={addNextActionTask}
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
            </>
          )}
        </section>
      </div>
      {props.aiOpen && (
        <AIWorkbench
          paperIDs={aiPaperIDs}
          contextTotal={focusedPaper ? aiPaperIDs.length : tabPapers.length}
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
