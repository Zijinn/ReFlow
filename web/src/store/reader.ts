import { create } from "zustand"
import { persist } from "zustand/middleware"

import type { Locale } from "../lib/i18n"
import type { LibraryScope, ViewMode } from "../api/types"
import type { ReaderAnnotation } from "../lib/annotations"

export type ShortcutAction =
  "palette" | "search" | "next" | "previous" | "toggleStar" | "toggleRead"
export type ThemeMode = "system" | "light" | "dark"
export type AccentTheme =
  "academic-blue" | "graphite" | "forest" | "wine" | "indigo" | "warm-paper"
export type SSEState = "live" | "reconnecting"
export type AppView = "reader" | "workbench"

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
  setAppView: (appView: AppView) => void
  setScope: (scope: LibraryScope) => void
  selectEntry: (entryID: string | null) => void
  setSearch: (search: string) => void
  setViewMode: (viewMode: ViewMode) => void
  closeMobileReader: () => void
  setLocale: (locale: Locale) => void
  setTheme: (theme: ThemeMode) => void
  setAccentTheme: (accentTheme: AccentTheme) => void
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
}

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
      setAppView: (appView) => set({ appView }),
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
    }),
    {
      name: "reflow-reader-preferences",
      partialize: (state) => ({
        viewMode: state.viewMode,
        shortcuts: state.shortcuts,
        locale: state.locale,
        theme: state.theme,
        accentTheme: state.accentTheme,
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
      }),
    },
  ),
)
