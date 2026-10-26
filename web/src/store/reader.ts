import { create } from "zustand"
import { persist } from "zustand/middleware"

import type { Locale } from "../lib/i18n"
import type { LibraryScope, ViewMode } from "../api/types"
import type { ReaderAnnotation } from "../lib/annotations"
import { clampCanvasPhotoVeil, defaultCanvasPhotoVeil } from "../lib/canvas"

export type ShortcutAction =
  "palette" | "search" | "next" | "previous" | "toggleStar" | "toggleRead"
export type ThemeMode = "system" | "light" | "dark"
export type AccentTheme =
  "academic-blue" | "graphite" | "forest" | "wine" | "indigo" | "warm-paper"
export type CanvasTheme = "aurora" | "meadow" | "orchid" | "slate"
export type SSEState = "live" | "reconnecting"
export type AppView = "reader" | "workbench"
// 个人工作台分为任务管理、截止日期日历与科研工作台三个一级模块。默认科研——
// 第一次进来的人肌肉记忆不变；用过其他模块之后，下次回来停在他自己选的那一块。
export type WorkbenchModule = "tasks" | "calendar" | "research"
export type WorkbenchTab = "dashboard" | "research" | "submitted" | "published"
export type TaskView = "quick" | "long" | "timeline" | "mindmap"
export type SidebarDensity = "compact" | "comfortable"
export interface TaskFocus {
  kind: "quick" | "assignment"
  id: string
}

export const defaultShortcuts: Record<ShortcutAction, string> = {
  palette: "mod+k",
  search: "/",
  next: "j",
  previous: "k",
  toggleStar: "s",
  toggleRead: "m",
}

export interface PaneLayout {
  sidebarWidth: number
  timelineWidth: number
}

export type ReaderFontFamily = "serif" | "sans"

export interface ReaderAppearance {
  fontFamily: ReaderFontFamily
  fontSize: number
  lineHeight: number
}

export const defaultReaderAppearance: ReaderAppearance = {
  // Sans by default: screen-rendered CJK serif (Songti) has uneven stroke
  // weight below ~20px. Serif stays available in the reader inspector.
  fontFamily: "sans",
  fontSize: 19,
  lineHeight: 1.8,
}

export const defaultPaneLayout: PaneLayout = { sidebarWidth: 232, timelineWidth: 376 }

export interface WorkbenchColumnWidths {
  [table: string]: { [column: string]: number }
}

interface ReaderStore {
  scope: LibraryScope
  readerReturnScope: LibraryScope | null
  selectedEntryID: string | null
  search: string
  viewMode: ViewMode
  mobileReaderOpen: boolean
  locale: Locale
  theme: ThemeMode
  accentTheme: AccentTheme
  canvasTheme: CanvasTheme
  canvasPhoto: string
  canvasPhotoVeil: number
  paneLayout: PaneLayout
  aiPanelWidth: number
  openFolders: Record<string, boolean>
  readerAppearance: ReaderAppearance
  annotations: ReaderAnnotation[]
  shortcuts: Record<ShortcutAction, string>
  alwaysTranslateTitles: boolean
  alwaysTranslateContent: boolean
  autoAcademicTags: boolean
  autoAcademicTagFolderIDs: string[]
  autoAcademicTagFeedIDs: string[]
  sseState: SSEState
  appView: AppView
  workbenchModule: WorkbenchModule
  workbenchTab: WorkbenchTab
  taskView: TaskView
  taskFocus: TaskFocus | null
  expandedWorkbenchModules: Record<WorkbenchModule, boolean>
  sidebarDensity: SidebarDensity
  workbenchColumnWidths: WorkbenchColumnWidths
  setAppView: (appView: AppView) => void
  setWorkbenchModule: (workbenchModule: WorkbenchModule) => void
  setWorkbenchTab: (workbenchTab: WorkbenchTab) => void
  setTaskView: (taskView: TaskView) => void
  setTaskFocus: (focus: TaskFocus | null) => void
  clearTaskFocus: () => void
  toggleWorkbenchModuleExpanded: (workbenchModule: WorkbenchModule) => void
  setSidebarDensity: (sidebarDensity: SidebarDensity) => void
  setScope: (scope: LibraryScope) => void
  selectEntry: (entryID: string | null) => void
  setSearch: (search: string) => void
  setViewMode: (viewMode: ViewMode) => void
  closeMobileReader: () => void
  setLocale: (locale: Locale) => void
  setTheme: (theme: ThemeMode) => void
  setAccentTheme: (accentTheme: AccentTheme) => void
  setCanvasTheme: (canvasTheme: CanvasTheme) => void
  setCanvasPhoto: (canvasPhoto: string) => void
  setCanvasPhotoVeil: (canvasPhotoVeil: number) => void
  setPaneLayout: (paneLayout: PaneLayout) => void
  setAIPanelWidth: (width: number) => void
  toggleFolder: (folderID: string) => void
  setReaderAppearance: (appearance: Partial<ReaderAppearance>) => void
  addAnnotation: (annotation: ReaderAnnotation) => void
  removeAnnotation: (annotationID: string) => void
  clearAnnotations: () => void
  setShortcut: (action: ShortcutAction, shortcut: string) => void
  resetShortcuts: () => void
  setAlwaysTranslateTitles: (enabled: boolean) => void
  setAlwaysTranslateContent: (enabled: boolean) => void
  setAutoAcademicTags: (enabled: boolean) => void
  setAutoAcademicTagFolderIDs: (folderIDs: string[]) => void
  setAutoAcademicTagFeedIDs: (feedIDs: string[]) => void
  setSSEState: (sseState: SSEState) => void
  setWorkbenchColumnWidth: (table: string, column: string, width: number) => void
  clearWorkbenchColumnWidth: (table: string, column: string) => void
}

const persistedReaderState = (state: ReaderStore) => ({
  viewMode: state.viewMode,
  shortcuts: state.shortcuts,
  locale: state.locale,
  theme: state.theme,
  accentTheme: state.accentTheme,
  canvasTheme: state.canvasTheme,
  canvasPhoto: state.canvasPhoto,
  canvasPhotoVeil: state.canvasPhotoVeil,
  paneLayout: state.paneLayout,
  aiPanelWidth: state.aiPanelWidth,
  openFolders: state.openFolders,
  readerAppearance: state.readerAppearance,
  annotations: state.annotations,
  alwaysTranslateTitles: state.alwaysTranslateTitles,
  alwaysTranslateContent: state.alwaysTranslateContent,
  autoAcademicTags: state.autoAcademicTags,
  autoAcademicTagFolderIDs: state.autoAcademicTagFolderIDs,
  autoAcademicTagFeedIDs: state.autoAcademicTagFeedIDs,
  appView: state.appView,
  workbenchModule: state.workbenchModule,
  workbenchTab: state.workbenchTab,
  taskView: state.taskView,
  expandedWorkbenchModules: state.expandedWorkbenchModules,
  sidebarDensity: state.sidebarDensity,
  workbenchColumnWidths: state.workbenchColumnWidths,
})

type PersistedReaderState = ReturnType<typeof persistedReaderState>

export const useReaderStore = create<ReaderStore>()(
  persist(
    (set) => ({
      scope: { kind: "today", title: "Today" },
      readerReturnScope: null,
      selectedEntryID: null,
      search: "",
      viewMode: "standard",
      mobileReaderOpen: false,
      locale: "zh-CN",
      theme: "system",
      accentTheme: "academic-blue",
      canvasTheme: "aurora",
      canvasPhoto: "",
      // 拉轴区间见 lib/canvas；persist 回填不走 setter，所以进 CSS 前还有一道 clamp。
      canvasPhotoVeil: defaultCanvasPhotoVeil,
      paneLayout: defaultPaneLayout,
      aiPanelWidth: 380,
      openFolders: {},
      readerAppearance: defaultReaderAppearance,
      annotations: [],
      shortcuts: defaultShortcuts,
      alwaysTranslateTitles: false,
      alwaysTranslateContent: false,
      autoAcademicTags: false,
      autoAcademicTagFolderIDs: [],
      autoAcademicTagFeedIDs: [],
      sseState: "live",
      appView: "reader",
      workbenchModule: "research",
      workbenchTab: "dashboard",
      taskView: "quick",
      taskFocus: null,
      // 两个模块的二级菜单各管各的：默认展开当前模块，另一个可随时点开。
      expandedWorkbenchModules: { tasks: false, calendar: false, research: true },
      // 订阅列表默认走宽松档；需要更密时可在订阅栏一键切回紧凑档。
      sidebarDensity: "comfortable",
      // 只存用户拖过的列：没落键的列在 CSS 里退回 width: var(--wb-col-w, 默认)，
      // 默认宽度因此仍归样式表管，加一档列不用动这里。
      workbenchColumnWidths: {},
      setAppView: (appView) => set({ appView }),
      setWorkbenchModule: (workbenchModule) =>
        set((state) => ({
          workbenchModule,
          expandedWorkbenchModules: {
            ...state.expandedWorkbenchModules,
            [workbenchModule]: true,
          },
        })),
      setWorkbenchTab: (workbenchTab) => set({ workbenchTab }),
      setTaskView: (taskView) => set({ taskView }),
      setTaskFocus: (taskFocus) => set({ taskFocus }),
      clearTaskFocus: () => set({ taskFocus: null }),
      toggleWorkbenchModuleExpanded: (workbenchModule) =>
        set((state) => ({
          expandedWorkbenchModules: {
            ...state.expandedWorkbenchModules,
            [workbenchModule]: !state.expandedWorkbenchModules[workbenchModule],
          },
        })),
      setSidebarDensity: (sidebarDensity) => set({ sidebarDensity }),
      setScope: (scope) =>
        set({ scope, readerReturnScope: null, selectedEntryID: null, mobileReaderOpen: false }),
      selectEntry: (selectedEntryID) =>
        set((state) => ({
          selectedEntryID,
          readerReturnScope: selectedEntryID === null ? null : state.scope,
          mobileReaderOpen: selectedEntryID !== null,
        })),
      setSearch: (search) => set({ search, readerReturnScope: null, selectedEntryID: null }),
      setViewMode: (viewMode) => set({ viewMode }),
      closeMobileReader: () =>
        set((state) => ({
          selectedEntryID: null,
          mobileReaderOpen: false,
          scope: state.readerReturnScope ?? state.scope,
          readerReturnScope: null,
        })),
      setLocale: (locale) => set({ locale }),
      setTheme: (theme) => set({ theme }),
      setAccentTheme: (accentTheme) => set({ accentTheme }),
      setCanvasTheme: (canvasTheme) => set({ canvasTheme }),
      setCanvasPhoto: (canvasPhoto) => set({ canvasPhoto }),
      setCanvasPhotoVeil: (canvasPhotoVeil) =>
        set({ canvasPhotoVeil: clampCanvasPhotoVeil(canvasPhotoVeil) }),
      setPaneLayout: (paneLayout) => set({ paneLayout }),
      setAIPanelWidth: (aiPanelWidth) => set({ aiPanelWidth }),
      toggleFolder: (folderID) =>
        set((state) => ({
          openFolders: { ...state.openFolders, [folderID]: !(state.openFolders[folderID] ?? true) },
        })),
      setReaderAppearance: (appearance) =>
        set((state) => ({ readerAppearance: { ...state.readerAppearance, ...appearance } })),
      addAnnotation: (annotation) =>
        set((state) => ({ annotations: [...state.annotations, annotation].slice(-500) })),
      removeAnnotation: (annotationID) =>
        set((state) => ({
          annotations: state.annotations.filter((annotation) => annotation.id !== annotationID),
        })),
      clearAnnotations: () => set({ annotations: [] }),
      setShortcut: (action, shortcut) =>
        set((state) => ({ shortcuts: { ...state.shortcuts, [action]: shortcut } })),
      resetShortcuts: () => set({ shortcuts: defaultShortcuts }),
      setAlwaysTranslateTitles: (alwaysTranslateTitles) => set({ alwaysTranslateTitles }),
      setAlwaysTranslateContent: (alwaysTranslateContent) => set({ alwaysTranslateContent }),
      setAutoAcademicTags: (autoAcademicTags) => set({ autoAcademicTags }),
      setAutoAcademicTagFolderIDs: (autoAcademicTagFolderIDs) =>
        set({ autoAcademicTagFolderIDs: Array.from(new Set(autoAcademicTagFolderIDs)) }),
      setAutoAcademicTagFeedIDs: (autoAcademicTagFeedIDs) =>
        set({ autoAcademicTagFeedIDs: Array.from(new Set(autoAcademicTagFeedIDs)) }),
      setSSEState: (sseState) => set({ sseState }),
      setWorkbenchColumnWidth: (table, column, width) =>
        set((state) => ({
          workbenchColumnWidths: {
            ...state.workbenchColumnWidths,
            [table]: { ...state.workbenchColumnWidths[table], [column]: width },
          },
        })),
      clearWorkbenchColumnWidth: (table, column) =>
        set((state) => {
          const columns = { ...state.workbenchColumnWidths[table] }
          if (!(column in columns)) return state
          delete columns[column]
          return {
            workbenchColumnWidths: { ...state.workbenchColumnWidths, [table]: columns },
          }
        }),
    }),
    {
      name: "reflow-reader-preferences",
      version: 2,
      // 旧版本默认紧凑，升级后第一次打开订阅列表时把它放宽一次；之后用户
      // 自己选的档位照常保留。日历从科研子标签升为一级模块后，旧的 calendar
      // 子标签回到科研总览，避免落在一条已经不存在的标签上。
      migrate: (persisted, version) => {
        const state = persisted as PersistedReaderState
        let next = state
        if (version < 1) next = { ...next, sidebarDensity: "comfortable" as const }
        if (version < 2) {
          next = {
            ...next,
            workbenchTab: String(next.workbenchTab) === "calendar" ? "dashboard" : next.workbenchTab,
            expandedWorkbenchModules: {
              ...next.expandedWorkbenchModules,
              calendar: next.expandedWorkbenchModules?.calendar ?? false,
            },
          }
        }
        return next
      },
      partialize: persistedReaderState,
    },
  ),
)
