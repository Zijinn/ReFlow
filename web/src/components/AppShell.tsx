import { CircleNotch, WarningCircle } from "@phosphor-icons/react"
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { InfiniteData } from "@tanstack/react-query"
import {
  lazy,
  Suspense,
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react"

import {
  addFeed,
  APIError,
  createAIProfile,
  createEntryAnnotation,
  createFolder,
  createRule,
  createSavedFilter,
  createTag,
  createPairingCode,
  createSyncAccount,
  deleteAIProfile,
  deleteFolder,
  deleteFeed,
  deleteRule,
  deleteSavedFilter,
  deleteTag,
  deleteSyncAccount,
  fetchReadability,
  getEntry,
  getJob,
  getAIUsage,
  getServerStatus,
  importOPML,
  listEntries,
  listDevices,
  listFolders,
  listRules,
  listSavedFilters,
  listAIProfiles,
  listAIProviders,
  listSubscriptions,
  listTags,
  listSyncAccounts,
  listSyncProviders,
  markEntriesRead,
  pairDevice,
  refreshFeed,
  restoreBackup,
  revokeDevice,
  runAIOperation,
  runSyncAccount,
  setEntryTags,
  updateEntryState,
  updateAIProfile,
  updateFeed,
  updateFolder,
  updateSyncAccount,
  testSyncConnection,
} from "../api/client"
import type {
  Entry,
  EntryPage,
  EntryState,
  Folder,
  LibraryScope,
  ListResponse,
  ServerStatus,
  Subscription,
  SyncAccount,
  SyncProvider,
  SyncProviderID,
} from "../api/types"
import { applyCanvas } from "../lib/canvas"
import { useTranslation } from "../lib/i18n"
import { useOnlineState } from "../lib/online"
import { keyboardChord } from "../lib/shortcuts"
import { applyTheme } from "../lib/theme"
import { enqueueStateMutation, flushMutationOutbox, queueStateMutation } from "../offline/database"
import { useReaderStore, type PaneLayout } from "../store/reader"
import { toast } from "../store/toast"
import { Sidebar } from "./Sidebar"
import { ShellActions } from "./ShellActions"
import { TimelinePane } from "./TimelinePane"
import { ConfirmDialog } from "./ConfirmDialog"
import { MobileNav } from "./MobileNav"
import { MobileLibraryDialog } from "./MobileLibraryDialog"
import { PaneDivider } from "./PaneDivider"
import { Toaster } from "./Toaster"
import { WorkspaceHeader } from "./WorkspaceHeader"
import { AIWorkbench } from "./AIWorkbench"

const AddFeedDialog = lazy(() =>
  import("./AddFeedDialog").then((module) => ({ default: module.AddFeedDialog })),
)
const AIProfileDialog = lazy(() =>
  import("./AIProfileDialog").then((module) => ({ default: module.AIProfileDialog })),
)
const PreferencesDialog = lazy(() =>
  import("./PreferencesDialog").then((module) => ({ default: module.PreferencesDialog })),
)
const ReaderPane = lazy(() =>
  import("./ReaderPane").then((module) => ({ default: module.ReaderPane })),
)
const CommandPalette = lazy(() =>
  import("./CommandPalette").then((module) => ({ default: module.CommandPalette })),
)
const PairDeviceDialog = lazy(() =>
  import("./PairDeviceDialog").then((module) => ({ default: module.PairDeviceDialog })),
)
const SyncAccountDialog = lazy(() =>
  import("./SyncAccountDialog").then((module) => ({ default: module.SyncAccountDialog })),
)
const LibraryOrganizationDialog = lazy(() =>
  import("./LibraryOrganizationDialog").then((module) => ({
    default: module.LibraryOrganizationDialog,
  })),
)
const Workbench = lazy(() =>
  import("./workbench/Workbench").then((module) => ({ default: module.Workbench })),
)

const DESKTOP_BREAKPOINT = 900
const SIDEBAR_MIN = 210
const SIDEBAR_MAX = 360
const TIMELINE_MIN = 300
const TIMELINE_MAX = 560
const BUILT_IN_CLOUD_PROVIDERS: SyncProvider[] = [
  { id: "webdav", name: "WebDAV" },
  { id: "icloud", name: "iCloud Drive" },
]
// Guards the legacy-annotation upload against StrictMode's double effect run.
let legacyAnnotationsMigrated = false

export function AppShell() {
  const queryClient = useQueryClient()
  const scope = useReaderStore((state) => state.scope)
  const selectedEntryID = useReaderStore((state) => state.selectedEntryID)
  const search = useReaderStore((state) => state.search)
  const viewMode = useReaderStore((state) => state.viewMode)
  const mobileReaderOpen = useReaderStore((state) => state.mobileReaderOpen)
  const setScope = useReaderStore((state) => state.setScope)
  const selectEntry = useReaderStore((state) => state.selectEntry)
  const setSearch = useReaderStore((state) => state.setSearch)
  const closeMobileReader = useReaderStore((state) => state.closeMobileReader)
  const shortcuts = useReaderStore((state) => state.shortcuts)
  const theme = useReaderStore((state) => state.theme)
  const accentTheme = useReaderStore((state) => state.accentTheme)
  const canvasTheme = useReaderStore((state) => state.canvasTheme)
  const canvasPhoto = useReaderStore((state) => state.canvasPhoto)
  const canvasPhotoVeil = useReaderStore((state) => state.canvasPhotoVeil)
  const setTheme = useReaderStore((state) => state.setTheme)
  const paneLayout = useReaderStore((state) => state.paneLayout)
  const setPaneLayout = useReaderStore((state) => state.setPaneLayout)
  const aiPanelWidth = useReaderStore((state) => state.aiPanelWidth)
  const setAIPanelWidth = useReaderStore((state) => state.setAIPanelWidth)
  const appView = useReaderStore((state) => state.appView)
  const setAppView = useReaderStore((state) => state.setAppView)
  const alwaysTranslateTitles = useReaderStore((state) => state.alwaysTranslateTitles)
  const alwaysTranslateContent = useReaderStore((state) => state.alwaysTranslateContent)
  const autoAcademicTags = useReaderStore((state) => state.autoAcademicTags)
  const autoAcademicTagFolderIDs = useReaderStore((state) => state.autoAcademicTagFolderIDs)
  const autoAcademicTagFeedIDs = useReaderStore((state) => state.autoAcademicTagFeedIDs)
  const setSSEState = useReaderStore((state) => state.setSSEState)
  const { locale, t } = useTranslation()
  const deferredSearch = useDeferredValue(search)
  const [addOpen, setAddOpen] = useState(false)
  const [preferencesOpen, setPreferencesOpen] = useState(false)
  const [commandOpen, setCommandOpen] = useState(false)
  const [syncAccountOpen, setSyncAccountOpen] = useState(false)
  const [syncAccountProvider, setSyncAccountProvider] = useState<SyncProviderID>()
  const [syncAccountEditing, setSyncAccountEditing] = useState<SyncAccount>()
  const [aiProfileOpen, setAIProfileOpen] = useState(false)
  const [organizationOpen, setOrganizationOpen] = useState(false)
  const [organizationMode, setOrganizationMode] = useState<"all" | "folders">("all")
  const [dialogReturnTarget, setDialogReturnTarget] = useState<"preferences" | null>(null)
  const [pendingConfirmation, setPendingConfirmation] = useState<{
    message: string
    action: () => void
  } | null>(null)
  const [mobileLibraryOpen, setMobileLibraryOpen] = useState(false)
  const [aiOpen, setAIOpen] = useState(false)
  const [importJobID, setImportJobID] = useState<string | null>(null)
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  const online = useOnlineState()
  const shellRef = useRef<HTMLElement>(null)
  const dragStartLayout = useRef<PaneLayout>(paneLayout)
  const dragLayout = useRef<PaneLayout>(paneLayout)
  const automaticTranslationAttempts = useRef(new Set<string>())
  const automaticTagAttempts = useRef(new Set<string>())
  const closeSecondaryDialog = useCallback(
    (setOpen: (open: boolean) => void) => {
      setOpen(false)
      if (dialogReturnTarget === "preferences") {
        setDialogReturnTarget(null)
        setPreferencesOpen(true)
      }
    },
    [dialogReturnTarget],
  )
  const requestConfirmation = useCallback((message: string, action: () => void) => {
    setPendingConfirmation({ message, action })
  }, [])

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  useEffect(() => {
    applyTheme(theme)
    if (theme !== "system") return
    // "system" is not a one-time read: the resolved value has to keep tracking the
    // OS, because the stylesheet only knows about light and dark.
    const query = window.matchMedia?.("(prefers-color-scheme: dark)")
    if (!query) return
    const update = () => applyTheme("system")
    query.addEventListener("change", update)
    return () => query.removeEventListener("change", update)
  }, [theme])

  useEffect(() => {
    document.documentElement.dataset.accent = accentTheme
  }, [accentTheme])

  useEffect(() => {
    applyCanvas({ theme: canvasTheme, photo: canvasPhoto, veil: canvasPhotoVeil })
  }, [canvasTheme, canvasPhoto, canvasPhotoVeil])

  useEffect(() => {
    const update = () => setViewportWidth(window.innerWidth)
    window.addEventListener("resize", update)
    return () => window.removeEventListener("resize", update)
  }, [])

  // Only the article pane constrains the sidebar from the right. The workbench
  // column is fluid, so reserving a reader measure there would cap the sidebar
  // against a pane that isn't even rendered.
  const readerMinimum = appView === "reader" ? minimumReaderWidth(viewportWidth) : 0

  // The shell cluster (AI / theme / preferences) has exactly one home per
  // viewport: `.sidebar` is `display: none` at the same 900px the stylesheet
  // switches on, and a hidden copy would still double every accessible name in
  // tests and assistive tech. Pick the surface the pointer can reach.
  const isDesktopShell = viewportWidth > DESKTOP_BREAKPOINT

  const constrainedPaneLayout = useMemo(
    () => constrainPaneLayout(paneLayout, viewportWidth, readerMinimum),
    [paneLayout, viewportWidth, readerMinimum],
  )
  // The constrained layout is display-only: it must never be written back to
  // the persisted store, or temporarily shrinking the window would erase the
  // pane widths the user chose.

  const applyPaneLayout = useCallback((next: PaneLayout) => {
    shellRef.current?.style.setProperty("--sidebar-width", `${next.sidebarWidth}px`)
    shellRef.current?.style.setProperty("--timeline-width", `${next.timelineWidth}px`)
  }, [])
  const startPaneResize = useCallback(() => {
    dragStartLayout.current = constrainedPaneLayout
    dragLayout.current = constrainedPaneLayout
  }, [constrainedPaneLayout])
  const resizePane = useCallback(
    (edge: "sidebar" | "timeline", delta: number) => {
      const base = dragStartLayout.current
      if (edge === "sidebar") {
        const max = Math.max(
          SIDEBAR_MIN,
          Math.min(SIDEBAR_MAX, viewportWidth - base.timelineWidth - readerMinimum),
        )
        dragLayout.current = {
          ...base,
          sidebarWidth: Math.round(clamp(base.sidebarWidth + delta, SIDEBAR_MIN, max)),
        }
      } else {
        const max = Math.max(
          TIMELINE_MIN,
          Math.min(TIMELINE_MAX, viewportWidth - base.sidebarWidth - readerMinimum),
        )
        dragLayout.current = {
          ...base,
          timelineWidth: Math.round(clamp(base.timelineWidth + delta, TIMELINE_MIN, max)),
        }
      }
      applyPaneLayout(dragLayout.current)
    },
    [applyPaneLayout, viewportWidth, readerMinimum],
  )
  const finishPaneResize = useCallback(() => {
    setPaneLayout(dragLayout.current)
  }, [setPaneLayout])
  const sidebarMax = Math.max(
    SIDEBAR_MIN,
    Math.min(SIDEBAR_MAX, viewportWidth - constrainedPaneLayout.timelineWidth - readerMinimum),
  )
  const timelineMax = Math.max(
    TIMELINE_MIN,
    Math.min(TIMELINE_MAX, viewportWidth - constrainedPaneLayout.sidebarWidth - readerMinimum),
  )
  const shellStyle = {
    "--sidebar-width": `${constrainedPaneLayout.sidebarWidth}px`,
    "--timeline-width": `${constrainedPaneLayout.timelineWidth}px`,
    "--ai-panel-width": `${aiPanelWidth}px`,
  } as CSSProperties

  const status = useQuery({
    queryKey: ["server-status"],
    queryFn: ({ signal }) => getServerStatus(signal),
    retry: false,
    refetchInterval: 30_000,
  })
  const bootstrapState = getBootstrapState(status)
  const libraryEnabled = bootstrapState === "ready"

  // One-time migration: highlights used to live in localStorage only. Upload
  // any legacy annotations, then clear the local copy so the server becomes
  // the source of truth and annotations sync across devices.
  useEffect(() => {
    if (!libraryEnabled || legacyAnnotationsMigrated) return
    legacyAnnotationsMigrated = true
    const legacy = useReaderStore.getState().annotations
    if (legacy.length === 0) return
    void (async () => {
      for (const annotation of legacy) {
        try {
          await createEntryAnnotation(annotation.entryID, {
            style: annotation.style,
            quote: annotation.quote,
            prefix: annotation.prefix,
            suffix: annotation.suffix,
            note: annotation.note,
          })
        } catch (error) {
          // Entries pruned since the highlight was made answer 404; drop those
          // but keep the rest queued for the next launch on transient errors.
          if (error instanceof APIError && error.status === 404) continue
          legacyAnnotationsMigrated = false
          return
        }
      }
      useReaderStore.getState().clearAnnotations()
      void queryClient.invalidateQueries({ queryKey: ["annotations"] })
    })()
  }, [libraryEnabled, queryClient])

  const subscriptions = useQuery({
    queryKey: ["subscriptions"],
    queryFn: ({ signal }) => listSubscriptions(signal),
    enabled: libraryEnabled,
  })
  const folders = useQuery({
    queryKey: ["folders"],
    queryFn: ({ signal }) => listFolders(signal),
    enabled: libraryEnabled,
  })
  const tags = useQuery({
    queryKey: ["tags"],
    queryFn: ({ signal }) => listTags(signal),
    enabled: libraryEnabled,
  })
  const rules = useQuery({
    queryKey: ["rules"],
    queryFn: ({ signal }) => listRules(signal),
    enabled: libraryEnabled,
  })
  const savedFilters = useQuery({
    queryKey: ["saved-filters"],
    queryFn: ({ signal }) => listSavedFilters(signal),
    enabled: libraryEnabled,
  })
  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: ({ signal }) => listDevices(signal),
    enabled: libraryEnabled,
  })
  const syncProviders = useQuery({
    queryKey: ["sync-providers"],
    queryFn: ({ signal }) => listSyncProviders(signal),
    enabled: libraryEnabled,
  })
  const syncAccounts = useQuery({
    queryKey: ["sync-accounts"],
    queryFn: ({ signal }) => listSyncAccounts(signal),
    enabled: libraryEnabled,
    refetchInterval: 30_000,
  })
  const availableSyncProviders = useMemo(() => {
    const providers = new Map<SyncProviderID, SyncProvider>()
    for (const provider of BUILT_IN_CLOUD_PROVIDERS) providers.set(provider.id, provider)
    for (const provider of syncProviders.data?.items ?? []) providers.set(provider.id, provider)
    return Array.from(providers.values())
  }, [syncProviders.data?.items])
  const aiProviders = useQuery({
    queryKey: ["ai-providers"],
    queryFn: ({ signal }) => listAIProviders(signal),
    enabled: libraryEnabled,
  })
  const aiProfiles = useQuery({
    queryKey: ["ai-profiles"],
    queryFn: ({ signal }) => listAIProfiles(signal),
    enabled: libraryEnabled,
  })
  const aiUsage = useQuery({
    queryKey: ["ai-usage"],
    queryFn: ({ signal }) => getAIUsage(signal),
    enabled: libraryEnabled,
  })
  const aiLanguage = locale === "zh-CN" ? "Chinese" : "English"
  const entriesQuery = useInfiniteQuery({
    queryKey: ["entries", scope, deferredSearch, aiLanguage],
    queryFn: ({ pageParam, signal }) =>
      listEntries(
        { scope, query: deferredSearch, cursor: pageParam ?? undefined, aiLanguage },
        signal,
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: libraryEnabled,
  })
  const entries = useMemo(
    () => entriesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [entriesQuery.data],
  )
  const selectedEntry = entries.find((entry) => entry.id === selectedEntryID) ?? null
  const automaticAIProfile = useMemo(
    () =>
      aiProfiles.data?.items.find((profile) => profile.is_default && profile.enabled) ??
      aiProfiles.data?.items.find((profile) => profile.enabled),
    [aiProfiles.data?.items],
  )
  const configuredAutoTagFeedIDs = useMemo(
    () =>
      resolveAutoTagFeedIDs(
        folders.data?.items ?? [],
        subscriptions.data?.items ?? [],
        autoAcademicTagFolderIDs,
        autoAcademicTagFeedIDs,
      ),
    [
      autoAcademicTagFeedIDs,
      autoAcademicTagFolderIDs,
      folders.data?.items,
      subscriptions.data?.items,
    ],
  )
  const selectedFeedIconURL = selectedEntry
    ? (subscriptions.data?.items.find(
        (subscription) => subscription.feed_id === selectedEntry.feed_id,
      )?.icon_url ?? null)
    : null
  const entryDetail = useQuery({
    queryKey: ["entry", selectedEntryID, aiLanguage],
    queryFn: ({ signal }) => getEntry(selectedEntryID!, aiLanguage, signal),
    enabled: libraryEnabled && selectedEntryID !== null,
  })
  const importJob = useQuery({
    queryKey: ["job", importJobID],
    queryFn: ({ signal }) => getJob(importJobID!, signal),
    enabled: importJobID !== null,
    refetchInterval: (query) =>
      query.state.data?.state === "queued" || query.state.data?.state === "running" ? 1000 : false,
  })

  useEffect(() => {
    if (!alwaysTranslateTitles || !automaticAIProfile) return
    const candidates = entries
      .filter((entry) => !entry.ai_translated_title && shouldTranslateEntry(entry, locale))
      .filter((entry) => {
        const key = `${automaticAIProfile.id}:title_translation:${aiLanguage}:${entry.id}`
        return !automaticTranslationAttempts.current.has(key)
      })
      .slice(0, 12)
    if (candidates.length === 0) return
    for (const entry of candidates) {
      automaticTranslationAttempts.current.add(
        `${automaticAIProfile.id}:title_translation:${aiLanguage}:${entry.id}`,
      )
    }
    void Promise.allSettled(
      candidates.map((entry) =>
        runAIOperation(entry.id, "title_translation", automaticAIProfile.id, aiLanguage),
      ),
    )
  }, [aiLanguage, alwaysTranslateTitles, automaticAIProfile, entries, locale])

  useEffect(() => {
    if (
      !alwaysTranslateContent ||
      !automaticAIProfile ||
      !selectedEntry ||
      !shouldTranslateEntry(selectedEntry, locale)
    )
      return
    const key = `${automaticAIProfile.id}:translation:${aiLanguage}:${selectedEntry.id}`
    if (automaticTranslationAttempts.current.has(key)) return
    automaticTranslationAttempts.current.add(key)
    void runAIOperation(selectedEntry.id, "translation", automaticAIProfile.id, aiLanguage)
  }, [aiLanguage, alwaysTranslateContent, automaticAIProfile, locale, selectedEntry])

  useEffect(() => {
    if (!autoAcademicTags || !automaticAIProfile || configuredAutoTagFeedIDs.size === 0) return
    let cancelled = false
    const run = async () => {
      const candidatesByID = new Map(
        entries
          .filter(
            (entry) => configuredAutoTagFeedIDs.has(entry.feed_id) && entry.tag_ids.length === 0,
          )
          .map((entry) => [entry.id, entry] as const),
      )
      const pages = await Promise.allSettled(
        Array.from(configuredAutoTagFeedIDs).map((feedID) =>
          listEntries({
            scope: { kind: "feed", id: feedID, title: "" },
            query: "",
            aiLanguage,
          }),
        ),
      )
      if (cancelled) return
      for (const page of pages) {
        if (page.status !== "fulfilled") continue
        for (const entry of page.value.items) {
          if (entry.tag_ids.length === 0) candidatesByID.set(entry.id, entry)
        }
      }
      const candidates = Array.from(candidatesByID.values())
        .sort(
          (left, right) =>
            new Date(right.discovered_at).getTime() - new Date(left.discovered_at).getTime(),
        )
        .filter((entry) => {
          const key = `${automaticAIProfile.id}:academic_tags:${aiLanguage}:${entry.id}`
          return !automaticTagAttempts.current.has(key)
        })
        .slice(0, 12)
      if (candidates.length === 0) return
      for (const entry of candidates) {
        automaticTagAttempts.current.add(
          `${automaticAIProfile.id}:academic_tags:${aiLanguage}:${entry.id}`,
        )
      }
      await Promise.allSettled(
        candidates.map((entry) =>
          runAIOperation(entry.id, "academic_tags", automaticAIProfile.id, aiLanguage),
        ),
      )
      if (cancelled) return
      void queryClient.invalidateQueries({ queryKey: ["entries"] })
      void queryClient.invalidateQueries({ queryKey: ["entry"] })
      void queryClient.invalidateQueries({ queryKey: ["tags"] })
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [
    aiLanguage,
    autoAcademicTags,
    automaticAIProfile,
    configuredAutoTagFeedIDs,
    entries,
    queryClient,
  ])

  const invalidateLibrary = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["entries"] }),
      queryClient.invalidateQueries({ queryKey: ["entry"] }),
      queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
    ])
  }, [queryClient])
  // Apply a confirmed state patch directly to the caches instead of refetching
  // the whole library: the entry stays visible in the current list (e.g. the
  // Unread scope) until the next natural refetch, and SSE bursts no longer
  // double-refetch every article open.
  const applyEntryStateToCache = useCallback(
    (entry: Entry, state: EntryState) => {
      let cachedState: EntryState | undefined
      queryClient.setQueriesData<InfiniteData<EntryPage>>({ queryKey: ["entries"] }, (current) =>
        current
          ? {
              ...current,
              pages: current.pages.map((page) => ({
                ...page,
                items: page.items.map((item) => {
                  if (item.id !== entry.id) return item
                  cachedState ??= item.state
                  return { ...item, state }
                }),
              })),
            }
          : current,
      )
      queryClient.setQueriesData<Entry>({ queryKey: ["entry", entry.id] }, (current) => {
        if (!current) return current
        cachedState ??= current.state
        return { ...current, state }
      })
      const previousState = cachedState ?? entry.state
      if (state.is_read !== previousState.is_read) {
        const delta = state.is_read ? -1 : 1
        queryClient.setQueriesData<ListResponse<Subscription>>(
          { queryKey: ["subscriptions"] },
          (current) =>
            current
              ? {
                  ...current,
                  items: current.items.map((subscription) =>
                    subscription.feed_id === entry.feed_id
                      ? {
                          ...subscription,
                          unread_count: Math.max(0, subscription.unread_count + delta),
                        }
                      : subscription,
                  ),
                }
              : current,
        )
      }
    },
    [queryClient],
  )
  const stateMutationFn = useCallback(
    async ({ entry, patch }: { entry: Entry; patch: Partial<EntryState> }) => {
      const mutationID = crypto.randomUUID()
      const deviceTime = new Date().toISOString()
      try {
        return await queueStateMutation(() =>
          updateEntryState(entry.id, patch, mutationID, deviceTime),
        )
      } catch (error) {
        if (error instanceof APIError) throw error
        await enqueueStateMutation({
          mutationID,
          entryID: entry.id,
          patch,
          deviceTime,
          createdAt: Date.now(),
        })
        return { ...entry.state, ...patch, updated_at: deviceTime }
      }
    },
    [],
  )
  const stateMutation = useMutation({
    mutationFn: stateMutationFn,
    onSuccess: (state, { entry }) => applyEntryStateToCache(entry, state),
    onError: () => toast(t("stateUpdateFailed")),
  })
  // The automatic mark-read on article open runs through its own mutation so
  // reader action buttons don't flash disabled every time an article opens.
  const autoReadMutation = useMutation({
    mutationFn: stateMutationFn,
    onSuccess: (state, { entry }) => applyEntryStateToCache(entry, state),
  })
  const tagMutation = useMutation({
    mutationFn: ({ entryID, tagIDs }: { entryID: string; tagIDs: string[] }) =>
      setEntryTags(entryID, tagIDs),
    onSuccess: invalidateLibrary,
    onError: () => toast(t("tagUpdateFailed")),
  })
  const mutateEntryState = stateMutation.mutate
  const mutateAutoRead = autoReadMutation.mutate
  const markReadMutation = useMutation({
    mutationFn: () => markEntriesRead(scope, deferredSearch),
    onSuccess: invalidateLibrary,
  })
  const refreshMutation = useMutation({
    mutationFn: (feedID: string) => refreshFeed(feedID),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
  })
  const markFeedReadMutation = useMutation({
    mutationFn: (feedID: string) => markEntriesRead({ kind: "feed", id: feedID, title: "" }),
    onSuccess: invalidateLibrary,
  })
  const feedUpdateMutation = useMutation({
    mutationFn: ({
      feedID,
      folderID,
      viewMode,
      refreshPolicy,
      refreshIntervalMinutes,
      position,
      titleOverride,
    }: {
      feedID: string
      folderID?: string | null
      viewMode?: import("../api/types").ViewMode
      refreshPolicy?: import("../api/types").Subscription["refresh_policy"]
      refreshIntervalMinutes?: number
      position?: number
      titleOverride?: string | null
    }) =>
      updateFeed(feedID, {
        ...(folderID !== undefined ? { folder_id: folderID } : {}),
        ...(viewMode !== undefined ? { view_mode: viewMode } : {}),
        ...(refreshPolicy !== undefined ? { refresh_policy: refreshPolicy } : {}),
        ...(refreshIntervalMinutes !== undefined
          ? { refresh_interval_minutes: refreshIntervalMinutes }
          : {}),
        ...(position !== undefined ? { position } : {}),
        ...(titleOverride !== undefined ? { title_override: titleOverride } : {}),
      }),
    onSuccess: invalidateLibrary,
  })
  const feedDeleteMutation = useMutation({
    mutationFn: deleteFeed,
    onSuccess: async (_result, feedID) => {
      if (scope.kind === "feed" && scope.id === feedID) {
        setScope({ kind: "today", title: "Today" })
      }
      await invalidateLibrary()
    },
  })
  const readabilityMutation = useMutation({
    mutationFn: (entryID: string) => fetchReadability(entryID),
  })
  const addMutation = useMutation({
    mutationFn: ({ url, folderID }: { url: string; folderID?: string }) =>
      addFeed({ url, folder_id: folderID ?? null }),
    onSuccess: async (feed) => {
      setAddOpen(false)
      setScope({ kind: "feed", id: feed.id, title: feed.title })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
        queryClient.invalidateQueries({ queryKey: ["folders"] }),
        queryClient.invalidateQueries({ queryKey: ["entries"] }),
      ])
    },
  })
  const importMutation = useMutation({
    mutationFn: importOPML,
    onSuccess: (job) => {
      setImportJobID(job.id)
    },
    onError: () => toast(t("opmlImportFailed")),
  })
  useEffect(() => {
    const state = importJob.data?.state
    if (!state || state === "queued" || state === "running") return
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
      queryClient.invalidateQueries({ queryKey: ["folders"] }),
      queryClient.invalidateQueries({ queryKey: ["entries"] }),
    ])
    if (state === "succeeded") {
      const closeTimer = window.setTimeout(() => {
        setAddOpen(false)
        setImportJobID(null)
      }, 0)
      return () => window.clearTimeout(closeTimer)
    }
  }, [importJob.data?.state, queryClient])
  const restoreMutation = useMutation({
    mutationFn: restoreBackup,
    onSuccess: async () => {
      await queryClient.invalidateQueries()
      setPreferencesOpen(false)
    },
  })
  const pairMutation = useMutation({
    mutationFn: ({ code, name, platform }: Parameters<typeof pairDevice>[0]) =>
      pairDevice({ code, name, platform }),
    onSuccess: async () => {
      await status.refetch()
      await queryClient.invalidateQueries()
    },
  })
  const pairingCodeMutation = useMutation({ mutationFn: createPairingCode })
  const revokeDeviceMutation = useMutation({
    mutationFn: revokeDevice,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["devices"] }),
  })
  const createSyncMutation = useMutation({
    mutationFn: createSyncAccount,
    onSuccess: async () => {
      closeSecondaryDialog(setSyncAccountOpen)
      await queryClient.invalidateQueries({ queryKey: ["sync-accounts"] })
    },
  })
  const updateSyncSettingsMutation = useMutation({
    mutationFn: ({
      accountID,
      patch,
    }: {
      accountID: string
      patch: Parameters<typeof updateSyncAccount>[1]
    }) => updateSyncAccount(accountID, patch),
    onSuccess: async () => {
      setSyncAccountEditing(undefined)
      closeSecondaryDialog(setSyncAccountOpen)
      await queryClient.invalidateQueries({ queryKey: ["sync-accounts"] })
    },
  })
  const toggleSyncMutation = useMutation({
    mutationFn: ({ accountID, enabled }: { accountID: string; enabled: boolean }) =>
      updateSyncAccount(accountID, { enabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["sync-accounts"] }),
  })
  const runSyncMutation = useMutation({
    mutationFn: ({ accountID, mode }: { accountID: string; mode: "auto" | "push" | "pull" }) =>
      runSyncAccount(accountID, mode),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["sync-accounts"] }),
    onError: () => toast(t("syncRunFailed")),
  })
  const deleteSyncMutation = useMutation({
    mutationFn: deleteSyncAccount,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["sync-accounts"] }),
  })
  const createFolderMutation = useMutation({
    mutationFn: createFolder,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["folders"] }),
  })
  const deleteFolderMutation = useMutation({
    mutationFn: deleteFolder,
    onSuccess: async (_result, folderID) => {
      if (scope.kind === "folder" && scope.id === folderID) {
        setScope({ kind: "today", title: "Today" })
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["folders"] }),
        queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
      ])
    },
  })
  const renameFolderMutation = useMutation({
    mutationFn: ({ folderID, name }: { folderID: string; name: string }) =>
      updateFolder(folderID, { name }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["folders"] }),
  })
  const moveFolderMutation = useMutation({
    mutationFn: ({ folderID, parentID }: { folderID: string; parentID: string | null }) =>
      updateFolder(folderID, { parent_id: parentID }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["folders"] }),
  })
  const markEntriesReadMutation = useMutation({
    mutationFn: (target: LibraryScope) => markEntriesRead(target),
    onSuccess: invalidateLibrary,
  })
  const reorderFolder = useCallback(
    async (folderID: string, targetID: string, before: boolean) => {
      if (folderID === targetID) return
      const items = folders.data?.items ?? []
      const source = items.find((item) => item.id === folderID)
      const target = items.find((item) => item.id === targetID)
      if (!source || !target || source.parent_id !== target.parent_id) return
      const siblings = items
        .filter((item) => item.parent_id === source.parent_id)
        .sort((a, b) => a.position - b.position || a.name.localeCompare(b.name))
        .filter((item) => item.id !== source.id)
      const targetIndex = siblings.findIndex((item) => item.id === target.id)
      siblings.splice(Math.max(0, targetIndex + (before ? 0 : 1)), 0, source)
      await Promise.all(siblings.map((item, index) => updateFolder(item.id, { position: index })))
      await queryClient.invalidateQueries({ queryKey: ["folders"] })
    },
    [folders.data?.items, queryClient],
  )
  const reorderFeed = useCallback(
    async (feedID: string, targetFeedID: string, before: boolean) => {
      if (feedID === targetFeedID) return
      const items = subscriptions.data?.items ?? []
      const source = items.find((item) => item.feed_id === feedID)
      const target = items.find((item) => item.feed_id === targetFeedID)
      if (!source || !target) return
      const siblings = items
        .filter((item) => item.folder_id === target.folder_id)
        .sort((a, b) => a.position - b.position || a.title.localeCompare(b.title))
        .filter((item) => item.feed_id !== source.feed_id)
      const targetIndex = siblings.findIndex((item) => item.feed_id === target.feed_id)
      siblings.splice(Math.max(0, targetIndex + (before ? 0 : 1)), 0, source)
      await Promise.all(
        siblings.map((item, index) =>
          updateFeed(item.feed_id, {
            position: index,
            ...(item.feed_id === source.feed_id ? { folder_id: target.folder_id } : {}),
          }),
        ),
      )
      await queryClient.invalidateQueries({ queryKey: ["subscriptions"] })
    },
    [queryClient, subscriptions.data?.items],
  )
  const mergeSubscriptions = useCallback(
    async (feedID: string, targetFeedID: string) => {
      if (feedID === targetFeedID) return
      const items = subscriptions.data?.items ?? []
      const source = items.find((item) => item.feed_id === feedID)
      const target = items.find((item) => item.feed_id === targetFeedID)
      if (!source || !target) return
      let folderID = target.folder_id
      if (!folderID) {
        const folder = await createFolderMutation.mutateAsync({
          name: target.title,
          parent_id: null,
        })
        folderID = folder.id
      }
      await Promise.all([
        updateFeed(source.feed_id, { folder_id: folderID }),
        updateFeed(target.feed_id, { folder_id: folderID }),
      ])
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["folders"] }),
        queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
      ])
    },
    [createFolderMutation, queryClient, subscriptions.data?.items],
  )
  const createTagMutation = useMutation({
    mutationFn: createTag,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["tags"] }),
  })
  const deleteTagMutation = useMutation({
    mutationFn: deleteTag,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["tags"] }),
  })
  const createRuleMutation = useMutation({
    mutationFn: createRule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["rules"] }),
  })
  const deleteRuleMutation = useMutation({
    mutationFn: deleteRule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["rules"] }),
  })
  const createSavedFilterMutation = useMutation({
    mutationFn: createSavedFilter,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["saved-filters"] }),
  })
  const deleteSavedFilterMutation = useMutation({
    mutationFn: deleteSavedFilter,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["saved-filters"] }),
  })
  const createAIProfileMutation = useMutation({
    mutationFn: createAIProfile,
    onSuccess: async () => {
      closeSecondaryDialog(setAIProfileOpen)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["ai-profiles"] }),
        queryClient.invalidateQueries({ queryKey: ["ai-usage"] }),
      ])
    },
  })
  const toggleAIProfileMutation = useMutation({
    mutationFn: ({ profileID, enabled }: { profileID: string; enabled: boolean }) =>
      updateAIProfile(profileID, { enabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ai-profiles"] }),
  })
  const defaultAIProfileMutation = useMutation({
    mutationFn: (profileID: string) => updateAIProfile(profileID, { is_default: true }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ai-profiles"] }),
  })
  const deleteAIProfileMutation = useMutation({
    mutationFn: deleteAIProfile,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ai-profiles"] }),
  })

  const mutateState = useCallback(
    (entry: Entry, patch: Partial<EntryState>) => {
      mutateEntryState({ entry, patch })
    },
    [mutateEntryState],
  )
  // Must stay referentially stable: ReaderPane's auto-mark-read effect lists
  // this prop in its deps, and a fresh closure per render makes the effect
  // re-fire on every mutation state change, looping until React aborts.
  const autoMarkRead = useCallback(
    (entry: Entry) => {
      mutateAutoRead({ entry, patch: { is_read: true } })
    },
    [mutateAutoRead],
  )

  useEffect(() => {
    if (!libraryEnabled || !("EventSource" in window)) return
    const source = new EventSource("/api/v1/events")
    let hasOpened = false
    // Coalesce bursts of SSE events (e.g. an entry.state storm) into a single
    // trailing invalidation per query key.
    const debounceTimers = new Map<string, number>()
    const scheduleInvalidate = (queryKey: string[]) => {
      const id = queryKey.join("")
      window.clearTimeout(debounceTimers.get(id))
      debounceTimers.set(
        id,
        window.setTimeout(() => {
          debounceTimers.delete(id)
          void queryClient.invalidateQueries({ queryKey })
        }, 300),
      )
    }
    const refresh = () => {
      scheduleInvalidate(["entries"])
      scheduleInvalidate(["entry"])
      scheduleInvalidate(["subscriptions"])
    }
    const subscriptionRefresh = () => {
      scheduleInvalidate(["subscriptions"])
      scheduleInvalidate(["folders"])
      scheduleInvalidate(["entries"])
    }
    for (const eventName of [
      "feed.updated",
      "entry.updated",
      "entry.state",
      "entry.bulk_state",
      "job.succeeded",
      "library.restored",
    ]) {
      source.addEventListener(eventName, refresh)
    }
    source.addEventListener("subscriptions.updated", subscriptionRefresh)
    source.addEventListener("entry.annotations", () => scheduleInvalidate(["annotations"]))
    source.addEventListener("job.failed", () => {
      // Feed refresh failures surface through subscription error state.
      scheduleInvalidate(["subscriptions"])
      void queryClient.invalidateQueries({ queryKey: ["job"] })
    })
    source.addEventListener(
      "sync.completed",
      () => void queryClient.invalidateQueries({ queryKey: ["sync-accounts"] }),
    )
    const aiRefresh = () => {
      scheduleInvalidate(["ai-results"])
      scheduleInvalidate(["entries"])
      scheduleInvalidate(["entry"])
      scheduleInvalidate(["ai-chat"])
      scheduleInvalidate(["ai-profiles"])
      scheduleInvalidate(["ai-usage"])
      scheduleInvalidate(["tags"])
    }
    source.addEventListener("ai.result", aiRefresh)
    source.addEventListener("ai.chat", aiRefresh)
    source.addEventListener("open", () => {
      setSSEState("live")
      if (!hasOpened) {
        hasOpened = true
        return
      }
      // A re-open means events may have been dropped while disconnected.
      void invalidateLibrary()
      void flushMutationOutbox().then((completed) => {
        if (completed > 0) void invalidateLibrary()
      })
    })
    source.addEventListener("error", () => {
      if (source.readyState !== EventSource.CLOSED) setSSEState("reconnecting")
    })
    return () => {
      source.close()
      for (const timer of debounceTimers.values()) window.clearTimeout(timer)
    }
  }, [invalidateLibrary, libraryEnabled, queryClient, setSSEState])

  useEffect(() => {
    if (!libraryEnabled || !online) return
    const flush = () => {
      void flushMutationOutbox().then((completed) => {
        if (completed > 0) void invalidateLibrary()
      })
    }
    flush()
    window.addEventListener("focus", flush)
    // Server restarts and flaky networks never fire the browser "online"
    // event; poll on an interval so queued mutations cannot sit forever.
    const interval = window.setInterval(flush, 60_000)
    return () => {
      window.removeEventListener("focus", flush)
      window.clearInterval(interval)
    }
  }, [invalidateLibrary, libraryEnabled, online])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      // A modal dialog owns the keyboard while it is open: Escape must close
      // the dialog, not the mobile reader behind it, and article shortcuts
      // must not fire through it.
      if (document.querySelector('[role="dialog"][data-state="open"]')) return
      const chord = keyboardChord(event)
      if (chord === shortcuts.palette) {
        event.preventDefault()
        setCommandOpen((open) => !open)
        return
      }
      // Reader-only shortcuts stay in the reader view: in the workbench the
      // library DOM is hidden, so j/k/i/u would silently operate on
      // background articles and search would focus an invisible input.
      if (appView !== "reader") return
      const target = event.target as HTMLElement | null
      if (target?.matches("input, textarea, select, [contenteditable='true']")) return
      const currentIndex = entries.findIndex((entry) => entry.id === selectedEntryID)
      if (chord === shortcuts.search) {
        event.preventDefault()
        document.querySelector<HTMLInputElement>("#library-search")?.focus()
      } else if (chord === shortcuts.next && entries.length > 0) {
        event.preventDefault()
        selectEntry(entries[Math.min(entries.length - 1, currentIndex + 1)]?.id ?? entries[0]!.id)
      } else if (chord === shortcuts.previous && entries.length > 0) {
        event.preventDefault()
        selectEntry(entries[Math.max(0, currentIndex <= 0 ? 0 : currentIndex - 1)]!.id)
      } else if (event.key === "Escape") {
        closeMobileReader()
      } else if (chord === shortcuts.toggleStar && selectedEntry) {
        event.preventDefault()
        mutateState(selectedEntry, { is_starred: !selectedEntry.state.is_starred })
      } else if (chord === shortcuts.toggleRead && selectedEntry) {
        event.preventDefault()
        mutateState(selectedEntry, { is_read: !selectedEntry.state.is_read })
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [
    appView,
    closeMobileReader,
    entries,
    mutateState,
    selectEntry,
    selectedEntry,
    selectedEntryID,
    shortcuts,
  ])

  if (bootstrapState !== "ready") {
    return (
      <BootstrapBoundary
        state={bootstrapState}
        error={status.error}
        retrying={status.isFetching}
        onRetry={() => void status.refetch()}
        pairing={
          <Suspense fallback={null}>
            <PairDeviceDialog
              open
              pending={pairMutation.isPending}
              error={pairMutation.error}
              onPair={(code, name, platform) => pairMutation.mutate({ code, name, platform })}
            />
          </Suspense>
        }
      />
    )
  }

  const shellActions = (
    <ShellActions
      theme={theme}
      onThemeChange={setTheme}
      aiOpen={aiOpen}
      onAI={() => setAIOpen((open) => !open)}
      onPreferences={() => setPreferencesOpen(true)}
    />
  )

  return (
    <>
      <a className="skip-link" href="#main-content">
        {t("skipToContent")}
      </a>
      <main
        ref={shellRef}
        id="main-content"
        tabIndex={-1}
        style={shellStyle}
        className={mobileReaderOpen ? "app-shell app-shell--reader-open" : "app-shell"}
      >
        <Sidebar
          appView={appView}
          onAppViewChange={setAppView}
          scope={scope}
          subscriptions={subscriptions.data?.items ?? []}
          folders={folders.data?.items ?? []}
          tags={tags.data?.items ?? []}
          savedFilters={savedFilters.data?.items ?? []}
          actions={isDesktopShell ? shellActions : null}
          onScopeChange={setScope}
          onAdd={() => setAddOpen(true)}
          onOrganizeLibrary={() => {
            setOrganizationMode("folders")
            setOrganizationOpen(true)
          }}
          onMarkFeedRead={(feedID) => markFeedReadMutation.mutate(feedID)}
          onMarkFolderRead={(folderID) =>
            markEntriesReadMutation.mutate({ kind: "folder", id: folderID, title: "" })
          }
          onRefreshFeed={(feedID) => refreshMutation.mutate(feedID)}
          onMoveFeed={(feedID, folderID) => feedUpdateMutation.mutate({ feedID, folderID })}
          onRenameFeed={(feedID, name) =>
            feedUpdateMutation.mutate({ feedID, titleOverride: name })
          }
          onRenameFolder={(folderID, name) => renameFolderMutation.mutate({ folderID, name })}
          onCreateSubfolder={(parentID, name) =>
            createFolderMutation.mutate({ name, parent_id: parentID })
          }
          onDeleteFolder={(folderID) =>
            requestConfirmation(t("deleteFolderConfirmation"), () =>
              deleteFolderMutation.mutate(folderID),
            )
          }
          onMoveFolder={(folderID, parentID) => moveFolderMutation.mutate({ folderID, parentID })}
          onMergeFeeds={(feedID, targetFeedID) => void mergeSubscriptions(feedID, targetFeedID)}
          onReorderFolder={(folderID, targetID, before) =>
            void reorderFolder(folderID, targetID, before)
          }
          onReorderFeed={(feedID, targetID, before) => void reorderFeed(feedID, targetID, before)}
          onDeleteFeed={(feedID) =>
            requestConfirmation(t("unsubscribeConfirmation"), () =>
              feedDeleteMutation.mutate(feedID),
            )
          }
          onChangeFeedView={(feedID, viewMode) => feedUpdateMutation.mutate({ feedID, viewMode })}
          onChangeFeedRefresh={(feedID, refreshPolicy, refreshIntervalMinutes) =>
            feedUpdateMutation.mutate({ feedID, refreshPolicy, refreshIntervalMinutes })
          }
        />
        <section className="workspace">
          {appView === "workbench" ? (
            <Suspense fallback={<ReaderPlaceholder label={t("workbench")} message={t("workbench")} />}>
              <Workbench
                aiOpen={aiOpen}
                aiProfiles={aiProfiles.data?.items ?? []}
                aiPanelWidth={aiPanelWidth}
                onAIPanelWidthChange={setAIPanelWidth}
                onAskAI={() => setAIOpen(true)}
                onCloseAI={() => setAIOpen(false)}
                onConfigureAI={() => setAIProfileOpen(true)}
              />
            </Suspense>
          ) : (
          <>
          <WorkspaceHeader
            scope={scope}
            search={search}
            searchShortcut={shortcuts.search}
            onSearchChange={setSearch}
            onAdd={() => setAddOpen(true)}
          />
          <div className={aiOpen ? "workspace-body workspace-body--ai-open" : "workspace-body"}>
            <TimelinePane
              scope={scope}
              entries={entries}
              subscriptions={subscriptions.data?.items ?? []}
              selectedEntryID={selectedEntryID}
              viewMode={viewMode}
              isLoading={entriesQuery.isPending}
              isFetchingNext={entriesQuery.isFetchingNextPage}
              hasNextPage={entriesQuery.hasNextPage}
              error={entriesQuery.error}
              markReadPending={markReadMutation.isPending}
              refreshPending={refreshMutation.isPending}
              onScopeChange={setScope}
              onSelect={selectEntry}
              onAdd={() => setAddOpen(true)}
              onRetry={() => void entriesQuery.refetch()}
              onLoadMore={() => void entriesQuery.fetchNextPage()}
              onMarkAllRead={() => markReadMutation.mutate()}
              onRefresh={(feedID) => refreshMutation.mutate(feedID)}
              onToggleStar={(entry) => mutateState(entry, { is_starred: !entry.state.is_starred })}
            />
            {selectedEntryID === null ? (
              <ReaderPlaceholder label={t("reader")} message={t("selectArticle")} />
            ) : (
              <Suspense
                fallback={<ReaderPlaceholder label={t("reader")} message={t("loadingArticle")} />}
              >
                <ReaderPane
                  key={selectedEntryID}
                  summary={selectedEntry}
                  detail={entryDetail.data}
                  isLoading={entryDetail.isPending}
                  error={entryDetail.error}
                  mutationPending={stateMutation.isPending || tagMutation.isPending}
                  readabilityPending={readabilityMutation.isPending}
                  aiProfiles={aiProfiles.data?.items ?? []}
                  tags={tags.data?.items ?? []}
                  feedIconURL={selectedFeedIconURL}
                  onBack={closeMobileReader}
                  onRetry={() => void entryDetail.refetch()}
                  onStateChange={mutateState}
                  onAutoMarkRead={autoMarkRead}
                  onTagsChange={(entryID, tagIDs) => tagMutation.mutate({ entryID, tagIDs })}
                  onFetchReadability={(entryID) => readabilityMutation.mutate(entryID)}
                  onConfigureAI={() => setAIProfileOpen(true)}
                  aiOpen={aiOpen}
                  onToggleAI={() => setAIOpen((open) => !open)}
                />
              </Suspense>
            )}
            {aiOpen && (
              <AIWorkbench
                key={selectedEntryID ?? `library-${scope.kind}-${"id" in scope ? scope.id : "all"}`}
                entryID={selectedEntryID ?? undefined}
                entryIDs={
                  selectedEntryID ? undefined : entries.slice(0, 20).map((entry) => entry.id)
                }
                profiles={aiProfiles.data?.items ?? []}
                width={aiPanelWidth}
                contextLabel={selectedEntryID ? t("privateToArticle") : t("currentLibraryContext")}
                initialMode="chat"
                onWidthChange={setAIPanelWidth}
                onClose={() => setAIOpen(false)}
                onConfigure={() => setAIProfileOpen(true)}
              />
            )}
          </div>
          </>
          )}
        </section>
        <PaneDivider
          edge="sidebar"
          value={constrainedPaneLayout.sidebarWidth}
          min={SIDEBAR_MIN}
          max={sidebarMax}
          label={t("resizeSidebar")}
          onStart={startPaneResize}
          onDelta={(delta) => resizePane("sidebar", delta)}
          onEnd={finishPaneResize}
        />
        {appView === "reader" && (
        <PaneDivider
          edge="timeline"
          value={constrainedPaneLayout.timelineWidth}
          min={TIMELINE_MIN}
          max={timelineMax}
          label={t("resizeTimeline")}
          onStart={startPaneResize}
          onDelta={(delta) => resizePane("timeline", delta)}
          onEnd={finishPaneResize}
        />
        )}
        <MobileNav
          scope={scope}
          onScopeChange={setScope}
          onLibrary={() => setMobileLibraryOpen(true)}
          actions={isDesktopShell ? null : shellActions}
        />
        <MobileLibraryDialog
          open={mobileLibraryOpen}
          scope={scope}
          folders={folders.data?.items ?? []}
          subscriptions={subscriptions.data?.items ?? []}
          tags={tags.data?.items ?? []}
          savedFilters={savedFilters.data?.items ?? []}
          onOpenChange={setMobileLibraryOpen}
          onScopeChange={setScope}
        />
        {!online && <div className="offline-banner">{t("offlineMode")}</div>}
        <Toaster />
        {addOpen && (
          <Suspense fallback={null}>
            <AddFeedDialog
              open
              folders={folders.data?.items ?? []}
              addPending={addMutation.isPending}
              importPending={
                importMutation.isPending ||
                importJob.data?.state === "queued" ||
                importJob.data?.state === "running"
              }
              error={
                addMutation.error ??
                importMutation.error ??
                (importJob.data?.state === "failed"
                  ? new Error(importJob.data.error_message ?? t("opmlImportFailed"))
                  : null)
              }
              onOpenChange={(open) => {
                setAddOpen(open)
                if (
                  !open &&
                  importJob.data?.state !== "queued" &&
                  importJob.data?.state !== "running"
                )
                  setImportJobID(null)
              }}
              onAdd={(url, folderID) => addMutation.mutate({ url, folderID })}
              onImport={(file) => importMutation.mutate(file)}
            />
          </Suspense>
        )}
        {preferencesOpen && (
          <Suspense fallback={null}>
            <PreferencesDialog
              open={preferencesOpen}
              theme={theme}
              status={status.data}
              restorePending={restoreMutation.isPending}
              error={
                restoreMutation.error ??
                toggleSyncMutation.error ??
                runSyncMutation.error ??
                deleteSyncMutation.error ??
                toggleAIProfileMutation.error ??
                defaultAIProfileMutation.error ??
                deleteAIProfileMutation.error
              }
              devices={devices.data?.items ?? []}
              syncAccounts={syncAccounts.data?.items ?? []}
              syncPendingID={
                runSyncMutation.isPending ? runSyncMutation.variables?.accountID : undefined
              }
              aiProfiles={aiProfiles.data?.items ?? []}
              aiUsage={aiUsage.data}
              folders={folders.data?.items ?? []}
              subscriptions={subscriptions.data?.items ?? []}
              pairingCode={pairingCodeMutation.data}
              pairingCodePending={pairingCodeMutation.isPending}
              onOpenChange={(open) => {
                setPreferencesOpen(open)
                if (!open) setDialogReturnTarget(null)
              }}
              onRestore={(file) => restoreMutation.mutate(file)}
              onCreatePairingCode={() => pairingCodeMutation.mutate()}
              onRevokeDevice={(deviceID) => revokeDeviceMutation.mutate(deviceID)}
              onAddSyncAccount={(provider) => {
                setDialogReturnTarget("preferences")
                setPreferencesOpen(false)
                setSyncAccountEditing(undefined)
                setSyncAccountProvider(provider)
                setSyncAccountOpen(true)
              }}
              onEditSyncAccount={(account) => {
                setDialogReturnTarget("preferences")
                setPreferencesOpen(false)
                setSyncAccountProvider(account.provider)
                setSyncAccountEditing(account)
                setSyncAccountOpen(true)
              }}
              onOrganizeLibrary={() => {
                setDialogReturnTarget("preferences")
                setPreferencesOpen(false)
                setOrganizationMode("all")
                setOrganizationOpen(true)
              }}
              onToggleSyncAccount={(accountID, enabled) =>
                toggleSyncMutation.mutate({ accountID, enabled })
              }
              onRunSyncAccount={(accountID, mode) => runSyncMutation.mutate({ accountID, mode })}
              onDeleteSyncAccount={(accountID) =>
                requestConfirmation(t("deleteSyncConfirmation"), () =>
                  deleteSyncMutation.mutate(accountID),
                )
              }
              onAddAIProfile={() => {
                setDialogReturnTarget("preferences")
                setPreferencesOpen(false)
                setAIProfileOpen(true)
              }}
              onToggleAIProfile={(profileID, enabled) =>
                toggleAIProfileMutation.mutate({ profileID, enabled })
              }
              onDefaultAIProfile={(profileID) => defaultAIProfileMutation.mutate(profileID)}
              onDeleteAIProfile={(profileID) =>
                requestConfirmation(t("deleteAIConfirmation"), () =>
                  deleteAIProfileMutation.mutate(profileID),
                )
              }
            />
          </Suspense>
        )}
        {commandOpen && (
          <Suspense fallback={null}>
            <CommandPalette
              open={commandOpen}
              shortcuts={shortcuts}
              onOpenChange={setCommandOpen}
              onScopeChange={setScope}
              onAdd={() => setAddOpen(true)}
              onPreferences={() => setPreferencesOpen(true)}
              onMarkAllRead={() => markReadMutation.mutate()}
              onFocusSearch={() =>
                document.querySelector<HTMLInputElement>("#library-search")?.focus()
              }
            />
          </Suspense>
        )}
        {syncAccountOpen && (
          <Suspense fallback={null}>
            <SyncAccountDialog
              key={syncAccountEditing?.id ?? `${syncAccountProvider ?? "default"}-new`}
              open
              providers={availableSyncProviders}
              account={syncAccountEditing}
              initialProvider={syncAccountProvider}
              pending={createSyncMutation.isPending || updateSyncSettingsMutation.isPending}
              error={createSyncMutation.error ?? updateSyncSettingsMutation.error}
              onOpenChange={(open) => {
                if (open) setSyncAccountOpen(true)
                else {
                  setSyncAccountEditing(undefined)
                  closeSecondaryDialog(setSyncAccountOpen)
                }
              }}
              onTest={testSyncConnection}
              onSave={(input) => {
                if (!syncAccountEditing) {
                  createSyncMutation.mutate(input)
                  return
                }
                const hasCredentialChanges = Object.values(input.credentials).some(
                  (value) => typeof value === "string" && value.length > 0,
                )
                updateSyncSettingsMutation.mutate({
                  accountID: syncAccountEditing.id,
                  patch: {
                    name: input.name,
                    endpoint: input.endpoint,
                    allow_private_network: input.allow_private_network,
                    sync_interval_minutes: input.sync_interval_minutes,
                    ...(hasCredentialChanges ? { credentials: input.credentials } : {}),
                  },
                })
              }}
            />
          </Suspense>
        )}
        {aiProfileOpen && (
          <Suspense fallback={null}>
            <AIProfileDialog
              open
              providers={aiProviders.data?.items ?? []}
              pending={createAIProfileMutation.isPending}
              error={createAIProfileMutation.error}
              onOpenChange={(open) => {
                if (open) setAIProfileOpen(true)
                else closeSecondaryDialog(setAIProfileOpen)
              }}
              onCreate={(input) => createAIProfileMutation.mutate(input)}
            />
          </Suspense>
        )}
        {organizationOpen && (
          <Suspense fallback={null}>
            <LibraryOrganizationDialog
              open
              mode={organizationMode}
              folders={folders.data?.items ?? []}
              tags={tags.data?.items ?? []}
              rules={rules.data?.items ?? []}
              savedFilters={savedFilters.data?.items ?? []}
              pending={
                createFolderMutation.isPending ||
                deleteFolderMutation.isPending ||
                createTagMutation.isPending ||
                deleteTagMutation.isPending ||
                createRuleMutation.isPending ||
                deleteRuleMutation.isPending ||
                createSavedFilterMutation.isPending ||
                deleteSavedFilterMutation.isPending
              }
              error={
                createFolderMutation.error ??
                deleteFolderMutation.error ??
                createTagMutation.error ??
                deleteTagMutation.error ??
                createRuleMutation.error ??
                deleteRuleMutation.error ??
                createSavedFilterMutation.error ??
                deleteSavedFilterMutation.error
              }
              onOpenChange={(open) => {
                if (open) setOrganizationOpen(true)
                else closeSecondaryDialog(setOrganizationOpen)
              }}
              onCreateFolder={(input) => createFolderMutation.mutate(input)}
              onDeleteFolder={(folderID) =>
                requestConfirmation(t("deleteFolderConfirmation"), () =>
                  deleteFolderMutation.mutate(folderID),
                )
              }
              onCreateTag={(input) => createTagMutation.mutate(input)}
              onDeleteTag={(tagID) =>
                requestConfirmation(t("deleteTagConfirmation"), () =>
                  deleteTagMutation.mutate(tagID),
                )
              }
              onCreateRule={(input) => createRuleMutation.mutate(input)}
              onDeleteRule={(ruleID) =>
                requestConfirmation(t("deleteRuleConfirmation"), () =>
                  deleteRuleMutation.mutate(ruleID),
                )
              }
              onCreateSavedFilter={(input) => createSavedFilterMutation.mutate(input)}
              onDeleteSavedFilter={(filterID) =>
                requestConfirmation(t("deleteFilterConfirmation"), () =>
                  deleteSavedFilterMutation.mutate(filterID),
                )
              }
            />
          </Suspense>
        )}
      </main>
      <ConfirmDialog
        open={pendingConfirmation !== null}
        message={pendingConfirmation?.message ?? ""}
        onOpenChange={(open) => {
          if (!open) setPendingConfirmation(null)
        }}
        onConfirm={() => {
          const action = pendingConfirmation?.action
          setPendingConfirmation(null)
          action?.()
        }}
      />
    </>
  )
}

type BootstrapState = "loading" | "error" | "database-unavailable" | "pairing" | "ready"

type StatusQuery = {
  data?: ServerStatus
  error: Error | null
  isPending: boolean
  isSuccess: boolean
}

function getBootstrapState(status: StatusQuery): BootstrapState {
  if (status.isPending) return "loading"
  if (!status.isSuccess || !status.data) return "error"
  if (status.data.status !== "ready" || !status.data.database_ready) {
    return "database-unavailable"
  }
  if (status.data.device_auth_required && !status.data.device_authenticated) return "pairing"
  return "ready"
}

function BootstrapBoundary(props: {
  state: BootstrapState
  error: Error | null
  retrying: boolean
  onRetry: () => void
  pairing: ReactNode
}) {
  const { t } = useTranslation()
  if (props.state === "pairing") return props.pairing
  if (props.state === "loading") {
    return (
      <main className="bootstrap-state" aria-label={t("loadingLibrary")}>
        <CircleNotch className="spin bootstrap-state__icon" aria-hidden="true" />
        <h1>{t("loadingLibrary")}</h1>
        <p>{t("loadingLibraryDescription")}</p>
      </main>
    )
  }
  const unavailable = props.state === "database-unavailable"
  return (
    <main className="bootstrap-state" role="alert">
      <WarningCircle className="bootstrap-state__icon" aria-hidden="true" />
      <h1>{t(unavailable ? "databaseUnavailable" : "serverUnavailable")}</h1>
      <p>
        {unavailable
          ? t("databaseUnavailableDescription")
          : props.error?.message || t("serverUnavailableDescription")}
      </p>
      <button
        className="button button--primary"
        type="button"
        disabled={props.retrying}
        onClick={props.onRetry}
      >
        {props.retrying && <CircleNotch className="spin" aria-hidden="true" />}
        {t("retry")}
      </button>
    </main>
  )
}

function ReaderPlaceholder(props: { label: string; message: string }) {
  return (
    <article className="reader reader--placeholder" aria-label={props.label}>
      <div className="reader__placeholder">
        <span className="reader-placeholder-mark" aria-hidden="true" />
        <p>{props.message}</p>
      </div>
    </article>
  )
}

function minimumReaderWidth(viewportWidth: number) {
  return viewportWidth <= 1100 ? 360 : 390
}

function constrainPaneLayout(
  layout: PaneLayout,
  viewportWidth: number,
  readerMinimum: number,
): PaneLayout {
  if (viewportWidth <= DESKTOP_BREAKPOINT) return layout
  let sidebarWidth = clamp(layout.sidebarWidth, SIDEBAR_MIN, SIDEBAR_MAX)
  let timelineWidth = clamp(layout.timelineWidth, TIMELINE_MIN, TIMELINE_MAX)
  let overflow = sidebarWidth + timelineWidth + readerMinimum - viewportWidth
  if (overflow > 0) {
    const timelineReduction = Math.min(overflow, timelineWidth - TIMELINE_MIN)
    timelineWidth -= timelineReduction
    overflow -= timelineReduction
  }
  if (overflow > 0) sidebarWidth -= Math.min(overflow, sidebarWidth - SIDEBAR_MIN)
  return { sidebarWidth: Math.round(sidebarWidth), timelineWidth: Math.round(timelineWidth) }
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}

function shouldTranslateEntry(entry: Entry, locale: "zh-CN" | "en-US") {
  const language = entry.language?.trim().toLowerCase() ?? ""
  const sample = `${entry.title} ${entry.summary ?? ""}`
  if (locale === "zh-CN") {
    if (language.startsWith("zh")) return false
    if (language) return true
    const cjkCount = (sample.match(/[\u3400-\u9fff]/g) ?? []).length
    const latinCount = (sample.match(/[a-z]/gi) ?? []).length
    return latinCount >= 4 && latinCount > cjkCount
  }
  if (language.startsWith("en")) return false
  if (language) return true
  return /[\u3400-\u9fff]/.test(sample)
}

function resolveAutoTagFeedIDs(
  folders: Folder[],
  subscriptions: Subscription[],
  selectedFolderIDs: string[],
  selectedFeedIDs: string[],
) {
  const includedFolders = new Set(selectedFolderIDs)
  let changed = true
  while (changed) {
    changed = false
    for (const folder of folders) {
      if (
        folder.parent_id &&
        includedFolders.has(folder.parent_id) &&
        !includedFolders.has(folder.id)
      ) {
        includedFolders.add(folder.id)
        changed = true
      }
    }
  }
  const feedIDs = new Set(selectedFeedIDs)
  for (const subscription of subscriptions) {
    if (subscription.folder_id && includedFolders.has(subscription.folder_id)) {
      feedIDs.add(subscription.feed_id)
    }
  }
  return feedIDs
}
