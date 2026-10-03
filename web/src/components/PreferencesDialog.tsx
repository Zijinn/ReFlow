import * as Dialog from "@radix-ui/react-dialog"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ArrowCounterClockwise,
  ArrowDown,
  ArrowsClockwise,
  ArrowUp,
  AppleLogo,
  Books,
  Brain,
  Check,
  CircleNotch,
  Cloud,
  CloudArrowDown,
  CloudArrowUp,
  CaretRight,
  Devices,
  DownloadSimple,
  LinkSimple,
  Palette,
  PencilSimple,
  Plus,
  Tag,
  Trash,
  UploadSimple,
  X,
} from "@phosphor-icons/react"
import {
  type DragEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"

import type {
  AIProfile,
  AIUsage,
  Device,
  Folder,
  ResearchTag,
  ServerStatus,
  Subscription,
  SyncAccount,
  SyncProviderID,
  ViewMode,
} from "../api/types"
import { useTranslation, type Locale, type Translator } from "../lib/i18n"
import {
  APIError,
  createResearchTag,
  deleteResearchTag,
  listPreferences,
  listResearchTags,
  putPreference,
  reorderResearchTags,
  updateResearchTag,
} from "../api/client"
import { canvasPhotoVeilMax, canvasPhotoVeilMin, canvasPhotoVeilSafe } from "../lib/canvas"
import { CanvasPhotoError, readCanvasPhoto } from "../lib/canvas-photo"
import { displayShortcut, keyboardChord } from "../lib/shortcuts"
import {
  reorderList,
  TAG_COLOR_NAMES,
  tagDotClass,
  type TagColorName,
} from "./workbench/utils"
import { toast } from "../store/toast"
import { ConfirmDialog } from "./ConfirmDialog"
import {
  defaultShortcuts,
  type AccentTheme,
  type CanvasTheme,
  type ShortcutAction,
  type ThemeMode,
  useReaderStore,
} from "../store/reader"

interface PreferencesDialogProps {
  open: boolean
  theme: ThemeMode
  status?: ServerStatus
  restorePending: boolean
  error: Error | null
  devices: Device[]
  syncAccounts: SyncAccount[]
  syncPendingID?: string
  aiProfiles: AIProfile[]
  aiUsage?: AIUsage
  folders: Folder[]
  subscriptions: Subscription[]
  pairingCode?: { code: string; expires_at: string }
  pairingCodePending: boolean
  onOpenChange: (open: boolean) => void
  onRestore: (file: File) => void
  onCreatePairingCode: () => void
  onRevokeDevice: (deviceID: string) => void
  onAddSyncAccount: (provider?: SyncProviderID) => void
  onEditSyncAccount: (account: SyncAccount) => void
  onToggleSyncAccount: (accountID: string, enabled: boolean) => void
  onRunSyncAccount: (accountID: string, mode: "auto" | "push" | "pull") => void
  onDeleteSyncAccount: (accountID: string) => void
  onOrganizeLibrary: () => void
  onAddAIProfile: () => void
  onEditAIProfile: (profile: AIProfile) => void
  onToggleAIProfile: (profileID: string, enabled: boolean) => void
  onDefaultAIProfile: (profileID: string) => void
  onDeleteAIProfile: (profileID: string) => void
}

type PreferenceTab = "interface" | "tags" | "ai" | "sync" | "library" | "devices"

const tabs: Array<{
  id: PreferenceTab
  labelKey: string
  descriptionKey: string
  icon: typeof Brain
}> = [
  {
    id: "interface",
    labelKey: "interface",
    descriptionKey: "interfaceSettingsDescription",
    icon: Palette,
  },
  // 工作台标签排在界面之后、AI 之前：它是用户每天要进出调整的清单，而 interface 是
  // 默认档，紧随其后是最短路径；它管的也不是资料库那套 `Tag`（另一个端点、另一个
  // 实体），放进 library 反而会被当成订阅标签。
  {
    id: "tags",
    labelKey: "researchTags",
    descriptionKey: "tagsSettingsDescription",
    icon: Tag,
  },
  { id: "ai", labelKey: "aiAndLanguage", descriptionKey: "aiSettingsDescription", icon: Brain },
  { id: "sync", labelKey: "sync", descriptionKey: "syncSettingsDescription", icon: Cloud },
  { id: "library", labelKey: "library", descriptionKey: "librarySettingsDescription", icon: Books },
  {
    id: "devices",
    labelKey: "devices",
    descriptionKey: "deviceSettingsDescription",
    icon: Devices,
  },
]

const viewModes: Array<{ value: ViewMode; labelKey: string }> = [
  { value: "compact", labelKey: "compact" },
  { value: "standard", labelKey: "standard" },
  { value: "card", labelKey: "cards" },
  { value: "magazine", labelKey: "magazine" },
  { value: "image", labelKey: "images" },
]

// 顺序就是偏好面板里的顺序；色卡上的那束渐变由 styles.css 的 data-canvas 一节负责，
// 这里只登记"有哪几档、各叫什么"。
const canvasTiers: Array<{ value: CanvasTheme; labelKey: string }> = [
  { value: "aurora", labelKey: "canvasAurora" },
  { value: "meadow", labelKey: "canvasMeadow" },
  { value: "orchid", labelKey: "canvasOrchid" },
  { value: "slate", labelKey: "canvasSlate" },
]

const shortcutLabelKeys: Record<ShortcutAction, string> = {
  palette: "commandPaletteShortcut",
  search: "search",
  next: "nextArticle",
  previous: "previousArticle",
  toggleStar: "toggleStar",
  toggleRead: "toggleRead",
}

export function PreferencesDialog(props: PreferencesDialogProps) {
  const { locale, t } = useTranslation()
  const queryClient = useQueryClient()
  const preferencesQuery = useQuery({
    queryKey: ["preferences"],
    queryFn: ({ signal }) => listPreferences(signal),
    enabled: props.open,
  })
  const retentionMutation = useMutation({
    mutationFn: (days: number) => putPreference("retention_days", days),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["preferences"] }),
  })
  const retentionDays =
    typeof preferencesQuery.data?.items?.retention_days === "number"
      ? preferencesQuery.data.items.retention_days
      : 0
  const viewMode = useReaderStore((state) => state.viewMode)
  const setViewMode = useReaderStore((state) => state.setViewMode)
  const setLocale = useReaderStore((state) => state.setLocale)
  const setTheme = useReaderStore((state) => state.setTheme)
  const accentTheme = useReaderStore((state) => state.accentTheme)
  const setAccentTheme = useReaderStore((state) => state.setAccentTheme)
  const canvasTheme = useReaderStore((state) => state.canvasTheme)
  const setCanvasTheme = useReaderStore((state) => state.setCanvasTheme)
  const canvasPhoto = useReaderStore((state) => state.canvasPhoto)
  const setCanvasPhoto = useReaderStore((state) => state.setCanvasPhoto)
  const canvasPhotoVeil = useReaderStore((state) => state.canvasPhotoVeil)
  const setCanvasPhotoVeil = useReaderStore((state) => state.setCanvasPhotoVeil)
  const shortcuts = useReaderStore((state) => state.shortcuts)
  const setShortcut = useReaderStore((state) => state.setShortcut)
  const resetShortcuts = useReaderStore((state) => state.resetShortcuts)
  const alwaysTranslateTitles = useReaderStore((state) => state.alwaysTranslateTitles)
  const alwaysTranslateContent = useReaderStore((state) => state.alwaysTranslateContent)
  const setAlwaysTranslateTitles = useReaderStore((state) => state.setAlwaysTranslateTitles)
  const setAlwaysTranslateContent = useReaderStore((state) => state.setAlwaysTranslateContent)
  const autoAcademicTags = useReaderStore((state) => state.autoAcademicTags)
  const autoAcademicTagFolderIDs = useReaderStore((state) => state.autoAcademicTagFolderIDs)
  const autoAcademicTagFeedIDs = useReaderStore((state) => state.autoAcademicTagFeedIDs)
  const setAutoAcademicTags = useReaderStore((state) => state.setAutoAcademicTags)
  const setAutoAcademicTagFolderIDs = useReaderStore((state) => state.setAutoAcademicTagFolderIDs)
  const setAutoAcademicTagFeedIDs = useReaderStore((state) => state.setAutoAcademicTagFeedIDs)
  const [activeTab, setActiveTab] = useState<PreferenceTab>("interface")
  const [conflict, setConflict] = useState("")
  const [autoTagSearch, setAutoTagSearch] = useState("")
  const [pendingConfirmation, setPendingConfirmation] = useState<{
    message: string
    action: () => void
  } | null>(null)
  const [canvasPhotoPending, setCanvasPhotoPending] = useState(false)
  const [canvasPhotoError, setCanvasPhotoError] = useState("")
  const restoreInput = useRef<HTMLInputElement>(null)
  const canvasPhotoInput = useRef<HTMLInputElement>(null)
  // 标签改名格进入编辑时置真：Radix 的 Escape 监听挂在 document 的**捕获**阶段
  // （dismissable-layer 里 addEventListener(..., {capture:true})），事件还没走到
  // React 的合成处理器就已经判定要关对话框了，所以格子里 stopPropagation 拦不住，
  // 只能走 Content 自己的 onEscapeKeyDown 放行。
  const renameGuard = useRef(false)
  const setRenaming = useCallback((active: boolean) => {
    renameGuard.current = active
  }, [])
  const active = tabs.find((tab) => tab.id === activeTab) ?? tabs[0]!
  const serviceAccounts = props.syncAccounts.filter(
    (account) => account.provider !== "webdav" && account.provider !== "icloud",
  )
  const cloudAccounts = props.syncAccounts.filter(
    (account) => account.provider === "webdav" || account.provider === "icloud",
  )
  const activeDevices = props.devices.filter((device) => !device.revoked_at)
  const enabledAI = props.aiProfiles.some((profile) => profile.enabled)
  const normalizedAutoTagSearch = autoTagSearch.trim().toLocaleLowerCase(locale)
  const visibleAutoTagFolders = props.folders.filter(
    (folder) =>
      normalizedAutoTagSearch === "" ||
      folder.name.toLocaleLowerCase(locale).includes(normalizedAutoTagSearch),
  )
  const visibleAutoTagSubscriptions = props.subscriptions.filter(
    (subscription) =>
      normalizedAutoTagSearch === "" ||
      `${subscription.title} ${subscription.feed_url}`
        .toLocaleLowerCase(locale)
        .includes(normalizedAutoTagSearch),
  )

  const captureShortcut = (action: ShortcutAction, event: KeyboardEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    const chord = keyboardChord(event)
    // Bare Escape stays reserved for dismissing dialogs.
    if (!chord || chord === "mod" || chord === "alt" || chord === "shift" || chord === "escape")
      return
    const duplicate = (Object.entries(shortcuts) as Array<[ShortcutAction, string]>).find(
      ([candidate, value]) => candidate !== action && value === chord,
    )
    if (duplicate) {
      setConflict(
        `${displayShortcut(chord)} ${t("alreadyAssigned")} ${t(shortcutLabelKeys[duplicate[0]])}${locale === "zh-CN" ? "。" : "."}`,
      )
      return
    }
    setConflict("")
    setShortcut(action, chord)
  }
  const selectBackup = (file?: File) => {
    if (file)
      setPendingConfirmation({
        message: t("restoreConfirmation"),
        action: () => props.onRestore(file),
      })
    if (restoreInput.current) restoreInput.current.value = ""
  }
  // 背景图不进后端：httpapi 没有二进制路由，导入时压成 data URL 存本地（见
  // lib/canvas-photo）。解码是异步的，所以这里要有 pending 态；失败就在面板里就地
  // 说一句，沿用快捷键冲突那条 .form-error，不另开 toast。
  // 同一张图重选也要清空 input.value，否则浏览器认为文件没变，onChange 不再触发。
  const selectCanvasPhoto = async (file?: File) => {
    if (!file) return
    setCanvasPhotoPending(true)
    setCanvasPhotoError("")
    try {
      setCanvasPhoto(await readCanvasPhoto(file))
    } catch (error) {
      setCanvasPhotoError(
        error instanceof CanvasPhotoError && error.code === "too-large"
          ? t("canvasPhotoTooLarge")
          : t("canvasPhotoFailed"),
      )
    } finally {
      setCanvasPhotoPending(false)
      if (canvasPhotoInput.current) canvasPhotoInput.current.value = ""
    }
  }
  const runCloudSync = (account: SyncAccount, mode: "auto" | "push" | "pull") => {
    if (mode === "push") {
      setPendingConfirmation({
        message: t("cloudPushConfirmation"),
        action: () => props.onRunSyncAccount(account.id, mode),
      })
      return
    }
    if (mode === "pull") {
      setPendingConfirmation({
        message: t("cloudPullConfirmation"),
        action: () => props.onRunSyncAccount(account.id, mode),
      })
      return
    }
    props.onRunSyncAccount(account.id, mode)
  }

  return (
    <>
      <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content
            className="dialog-content preferences-dialog"
            aria-describedby={undefined}
            onEscapeKeyDown={(event) => {
              if (renameGuard.current) event.preventDefault()
            }}
          >
            <div className="preferences-layout">
              <aside className="preferences-nav">
                <div className="preferences-nav__brand">
                  <img src="/icons/reflow-mark-192.png" alt="" draggable={false} />
                  <span>
                    <strong>ReFlow</strong>
                    <small>{t("preferencesTitle")}</small>
                  </span>
                </div>
                <nav aria-label={t("preferencesTitle")}>
                  {tabs.map((tab) => {
                    const Icon = tab.icon
                    return (
                      <button
                        className={
                          activeTab === tab.id
                            ? "preferences-nav__item preferences-nav__item--active"
                            : "preferences-nav__item"
                        }
                        type="button"
                        key={tab.id}
                        aria-current={activeTab === tab.id ? "page" : undefined}
                        onClick={() => setActiveTab(tab.id)}
                      >
                        <Icon />
                        <span>{t(tab.labelKey)}</span>
                      </button>
                    )
                  })}
                </nav>
                <small>ReFlow {props.status?.version ?? ""}</small>
              </aside>

              <main className="preferences-main">
                <div className="dialog-header preferences-header">
                  <div>
                    <Dialog.Title>{t(active.labelKey)}</Dialog.Title>
                    <p>{t(active.descriptionKey)}</p>
                  </div>
                  <Dialog.Close asChild>
                    <button
                      className="icon-button"
                      type="button"
                      aria-label={t("close")}
                      title={t("close")}
                    >
                      <X />
                    </button>
                  </Dialog.Close>
                </div>
                <div className="preferences-scroll">
                  {activeTab === "interface" && (
                    <>
                      <section className="preference-section preference-section--row">
                        <div>
                          <h2>{t("language")}</h2>
                          <p>{t("languageDescription")}</p>
                        </div>
                        <select
                          className="select-input preference-language"
                          aria-label={t("language")}
                          value={locale}
                          onChange={(event) => setLocale(event.target.value as "zh-CN" | "en-US")}
                        >
                          <option value="zh-CN">{t("chinese")}</option>
                          <option value="en-US">{t("english")}</option>
                        </select>
                      </section>
                      <section className="preference-section preference-section--row">
                        <div>
                          <h2>{t("theme")}</h2>
                          <p>{t("themeDescription")}</p>
                        </div>
                        <select
                          className="select-input preference-language"
                          aria-label={t("theme")}
                          value={props.theme}
                          onChange={(event) => setTheme(event.target.value as ThemeMode)}
                        >
                          <option value="system">{t("themeSystem")}</option>
                          <option value="light">{t("themeLight")}</option>
                          <option value="dark">{t("themeDark")}</option>
                        </select>
                      </section>
                      <section className="preference-section preference-section--row">
                        <div>
                          <h2>{t("accentTheme")}</h2>
                          <p>{t("accentThemeDescription")}</p>
                        </div>
                        <select
                          className="select-input preference-language"
                          aria-label={t("accentTheme")}
                          value={accentTheme}
                          onChange={(event) => setAccentTheme(event.target.value as AccentTheme)}
                        >
                          <option value="academic-blue">{t("themeAcademicBlue")}</option>
                          <option value="graphite">{t("themeGraphite")}</option>
                          <option value="forest">{t("themeForest")}</option>
                          <option value="wine">{t("themeWine")}</option>
                          <option value="indigo">{t("themeIndigo")}</option>
                          <option value="warm-paper">{t("themeWarmPaper")}</option>
                        </select>
                      </section>
                      <section className="preference-section">
                        <h2>{t("canvasTheme")}</h2>
                        <p>{t("canvasThemeDescription")}</p>
                        {/* 单选一组色卡。这里用 role="group" + aria-pressed 而不是
                            role="radiogroup"：面板里同类的"多选一"（视图模式、蒙板强度）
                            都是这个写法，radiogroup 还要接管方向键才合规。 */}
                        <div className="canvas-theme-grid" role="group" aria-label={t("canvasTheme")}>
                          {canvasTiers.map((tier) => (
                            <button
                              className={`canvas-theme-card canvas-theme-card--${tier.value}`}
                              type="button"
                              key={tier.value}
                              aria-pressed={canvasTheme === tier.value}
                              onClick={() => setCanvasTheme(tier.value)}
                            >
                              <span className="canvas-theme-card__wash" aria-hidden="true" />
                              <span className="canvas-theme-card__label">{t(tier.labelKey)}</span>
                            </button>
                          ))}
                        </div>
                      </section>
                      <section className="preference-section">
                        <h2>{t("canvasPhoto")}</h2>
                        <p>{t("canvasPhotoDescription")}</p>
                        <div className="canvas-photo-row">
                          {/* 缩略图不写行内样式：applyCanvas 把 url() 挂在 <html> 的
                              --canvas-photo-src 上，这一格继承过来就是当前图。 */}
                          <span
                            className={
                              canvasPhoto
                                ? "canvas-photo-preview"
                                : "canvas-photo-preview canvas-photo-preview--empty"
                            }
                            aria-hidden="true"
                          />
                          <div className="button-group">
                            <input
                              ref={canvasPhotoInput}
                              className="sr-only"
                              type="file"
                              accept="image/*"
                              onChange={(event) => {
                                void selectCanvasPhoto(event.target.files?.[0])
                              }}
                            />
                            <button
                              className="button button--secondary"
                              type="button"
                              disabled={canvasPhotoPending}
                              onClick={() => canvasPhotoInput.current?.click()}
                            >
                              {canvasPhotoPending ? (
                                <CircleNotch className="spin" />
                              ) : (
                                <UploadSimple />
                              )}
                              {t("canvasPhotoChoose")}
                            </button>
                            {canvasPhoto && (
                              <button
                                className="button button--secondary"
                                type="button"
                                onClick={() => setCanvasPhoto("")}
                              >
                                <Trash />
                                {t("canvasPhotoRemove")}
                              </button>
                            )}
                          </div>
                        </div>
                        {canvasPhotoError && (
                          <p className="form-error" role="alert">
                            {canvasPhotoError}
                          </p>
                        )}
                      </section>
                      {canvasPhoto && (
                        <section className="preference-section">
                          <div className="preference-heading">
                            <h2>{t("canvasPhotoVeil")}</h2>
                            <output className="range-output" htmlFor="canvas-photo-veil">
                              {canvasPhotoVeil}%
                            </output>
                          </div>
                          <p>{t("canvasPhotoVeilDescription")}</p>
                          {/* 连续拉轴，不再是三档：用户要的是"这一档还是糊"，那只有
                              中间值能满足。步进 1，落到整数百分比才好读。 */}
                          <input
                            className="range-input canvas-veil-input"
                            id="canvas-photo-veil"
                            type="range"
                            min={canvasPhotoVeilMin}
                            max={canvasPhotoVeilMax}
                            step={1}
                            value={canvasPhotoVeil}
                            onChange={(event) => setCanvasPhotoVeil(Number(event.target.value))}
                          />
                          <div className="canvas-veil-scale" aria-hidden="true">
                            <span>{t("canvasPhotoVeilImageEnd")}</span>
                            <span>{t("canvasPhotoVeilTextEnd")}</span>
                          </div>
                          {/* 地板降到拉轴下限后，低于安全值不再被 CSS 的 max() 偷偷顶
                              回去，代价必须由这一行明说。不加 role="alert"：拖动时它反复
                              出现/消失，读屏会被刷屏。 */}
                          {canvasPhotoVeil < canvasPhotoVeilSafe && (
                            <p className="canvas-veil-hint canvas-veil-hint--low">
                              {t("canvasPhotoVeilLowWarning")}
                            </p>
                          )}
                        </section>
                      )}
                      <section className="preference-section">
                        <h2>{t("timelineView")}</h2>
                        <div
                          className="segmented-control"
                          role="group"
                          aria-label={t("timelineView")}
                        >
                          {viewModes.map((mode) => (
                            <button
                              className={
                                viewMode === mode.value
                                  ? "segmented-control__item segmented-control__item--active"
                                  : "segmented-control__item"
                              }
                              type="button"
                              key={mode.value}
                              onClick={() => setViewMode(mode.value)}
                            >
                              {t(mode.labelKey)}
                            </button>
                          ))}
                        </div>
                      </section>
                      <section className="preference-section">
                        <div className="preference-heading">
                          <h2>{t("keyboard")}</h2>
                          <button
                            className="button button--quiet"
                            type="button"
                            onClick={() => {
                              resetShortcuts()
                              setConflict("")
                            }}
                          >
                            <ArrowCounterClockwise />
                            {t("reset")}
                          </button>
                        </div>
                        <div className="shortcut-list">
                          {(Object.keys(defaultShortcuts) as ShortcutAction[]).map((action) => (
                            <div className="shortcut-row" key={action}>
                              <span>{t(shortcutLabelKeys[action])}</span>
                              <button
                                className="shortcut-key"
                                type="button"
                                title={t("shortcutHint")}
                                onKeyDown={(event) => captureShortcut(action, event)}
                              >
                                {displayShortcut(shortcuts[action])}
                              </button>
                            </div>
                          ))}
                        </div>
                        {conflict && (
                          <p className="form-error" role="alert">
                            {conflict}
                          </p>
                        )}
                      </section>
                    </>
                  )}

                  {activeTab === "tags" && (
                    <TagsSection
                      t={t}
                      onRenaming={setRenaming}
                      onConfirm={(message, action) => setPendingConfirmation({ message, action })}
                    />
                  )}

                  {activeTab === "ai" && (
                    <>
                      <section className="preference-section preference-section--automation">
                        <div className="preference-heading preference-heading--intro">
                          <div>
                            <h2>{t("automaticTranslation")}</h2>
                            <p>
                              {props.aiProfiles.some((profile) => profile.enabled)
                                ? t("aiProviderDescription")
                                : t("automaticTranslationNeedsProvider")}
                            </p>
                          </div>
                        </div>
                        <label className="preference-switch-row">
                          <span>
                            <strong>{t("alwaysTranslateTitles")}</strong>
                            <small>{t("alwaysTranslateTitlesDescription")}</small>
                          </span>
                          <input
                            type="checkbox"
                            checked={alwaysTranslateTitles}
                            disabled={!props.aiProfiles.some((profile) => profile.enabled)}
                            onChange={(event) => setAlwaysTranslateTitles(event.target.checked)}
                          />
                          <i aria-hidden="true" />
                        </label>
                        <label className="preference-switch-row">
                          <span>
                            <strong>{t("alwaysTranslateContent")}</strong>
                            <small>{t("alwaysTranslateContentDescription")}</small>
                          </span>
                          <input
                            type="checkbox"
                            checked={alwaysTranslateContent}
                            disabled={!props.aiProfiles.some((profile) => profile.enabled)}
                            onChange={(event) => setAlwaysTranslateContent(event.target.checked)}
                          />
                          <i aria-hidden="true" />
                        </label>
                      </section>
                      <section className="preference-section preference-section--auto-tags">
                        <div className="preference-heading preference-heading--intro">
                          <div>
                            <h2>{t("automaticTags")}</h2>
                            <p>{t("automaticTagsDescription")}</p>
                          </div>
                          <label className="preference-switch preference-switch--inline">
                            <input
                              type="checkbox"
                              checked={autoAcademicTags}
                              disabled={!enabledAI}
                              aria-label={t("automaticTags")}
                              onChange={(event) => setAutoAcademicTags(event.target.checked)}
                            />
                            <i aria-hidden="true" />
                          </label>
                        </div>
                        {autoAcademicTags && (
                          <div className="auto-tag-settings">
                            <label className="auto-tag-search">
                              <span>{t("automaticTagScope")}</span>
                              <input
                                className="text-input"
                                type="search"
                                value={autoTagSearch}
                                placeholder={t("searchFoldersAndSubscriptions")}
                                onChange={(event) => setAutoTagSearch(event.target.value)}
                              />
                            </label>
                            <p className="auto-tag-selection-summary">
                              {t("automaticTagSelectionSummary")
                                .replace("{folders}", String(autoAcademicTagFolderIDs.length))
                                .replace("{feeds}", String(autoAcademicTagFeedIDs.length))}
                            </p>
                            <div className="auto-tag-scope-grid">
                              <fieldset>
                                <legend>{t("folders")}</legend>
                                <div className="auto-tag-scope-actions">
                                  <button
                                    type="button"
                                    onClick={() =>
                                      setAutoAcademicTagFolderIDs(
                                        Array.from(
                                          new Set([
                                            ...autoAcademicTagFolderIDs,
                                            ...visibleAutoTagFolders.map((folder) => folder.id),
                                          ]),
                                        ),
                                      )
                                    }
                                  >
                                    {t("selectAll")}
                                  </button>
                                  <button
                                    type="button"
                                    onClick={() =>
                                      setAutoAcademicTagFolderIDs(
                                        autoAcademicTagFolderIDs.filter(
                                          (id) =>
                                            !visibleAutoTagFolders.some(
                                              (folder) => folder.id === id,
                                            ),
                                        ),
                                      )
                                    }
                                  >
                                    {t("clear")}
                                  </button>
                                </div>
                                <div className="auto-tag-scope-list">
                                  {visibleAutoTagFolders.map((folder) => (
                                    <label key={folder.id}>
                                      <input
                                        type="checkbox"
                                        checked={autoAcademicTagFolderIDs.includes(folder.id)}
                                        onChange={() =>
                                          setAutoAcademicTagFolderIDs(
                                            toggleID(autoAcademicTagFolderIDs, folder.id),
                                          )
                                        }
                                      />
                                      <span>{folder.name}</span>
                                    </label>
                                  ))}
                                  {visibleAutoTagFolders.length === 0 && (
                                    <span className="preference-empty">{t("noFolders")}</span>
                                  )}
                                </div>
                              </fieldset>
                              <fieldset>
                                <legend>{t("subscriptions")}</legend>
                                <div className="auto-tag-scope-actions">
                                  <button
                                    type="button"
                                    onClick={() =>
                                      setAutoAcademicTagFeedIDs(
                                        Array.from(
                                          new Set([
                                            ...autoAcademicTagFeedIDs,
                                            ...visibleAutoTagSubscriptions.map(
                                              (subscription) => subscription.feed_id,
                                            ),
                                          ]),
                                        ),
                                      )
                                    }
                                  >
                                    {t("selectAll")}
                                  </button>
                                  <button
                                    type="button"
                                    onClick={() =>
                                      setAutoAcademicTagFeedIDs(
                                        autoAcademicTagFeedIDs.filter(
                                          (id) =>
                                            !visibleAutoTagSubscriptions.some(
                                              (subscription) => subscription.feed_id === id,
                                            ),
                                        ),
                                      )
                                    }
                                  >
                                    {t("clear")}
                                  </button>
                                </div>
                                <div className="auto-tag-scope-list">
                                  {visibleAutoTagSubscriptions.map((subscription) => (
                                    <label key={subscription.id}>
                                      <input
                                        type="checkbox"
                                        checked={autoAcademicTagFeedIDs.includes(
                                          subscription.feed_id,
                                        )}
                                        onChange={() =>
                                          setAutoAcademicTagFeedIDs(
                                            toggleID(autoAcademicTagFeedIDs, subscription.feed_id),
                                          )
                                        }
                                      />
                                      <span>
                                        <strong>{subscription.title}</strong>
                                        <small>{subscription.feed_url}</small>
                                      </span>
                                    </label>
                                  ))}
                                  {visibleAutoTagSubscriptions.length === 0 && (
                                    <span className="preference-empty">{t("noSubscriptions")}</span>
                                  )}
                                </div>
                              </fieldset>
                            </div>
                            {autoAcademicTagFolderIDs.length === 0 &&
                              autoAcademicTagFeedIDs.length === 0 && (
                                <p className="field-hint">{t("automaticTagScopeRequired")}</p>
                              )}
                          </div>
                        )}
                      </section>
                      <section className="preference-section preference-section--flush">
                        <div className="preference-heading preference-heading--intro">
                          <div>
                            <h2>{t("aiProviders")}</h2>
                            <p>{t("aiProviderDescription")}</p>
                          </div>
                          <button
                            className="button button--secondary"
                            type="button"
                            onClick={props.onAddAIProfile}
                          >
                            <Plus />
                            {t("add")}
                          </button>
                        </div>
                        <div className="sync-account-list">
                          {props.aiProfiles.map((profile) => (
                            <div className="sync-account-row" key={profile.id}>
                              <label
                                className="sync-account-toggle"
                                title={profile.enabled ? t("disableProvider") : t("enableProvider")}
                              >
                                <input
                                  type="checkbox"
                                  checked={profile.enabled}
                                  onChange={(event) =>
                                    props.onToggleAIProfile(profile.id, event.target.checked)
                                  }
                                />
                                <span className="sr-only">
                                  {t("enable")} {profile.name}
                                </span>
                              </label>
                              <span className="sync-account-copy">
                                <strong>
                                  <Brain />
                                  {profile.name}
                                </strong>
                                <small
                                  className={
                                    profile.last_error_message ? "sync-account-error" : undefined
                                  }
                                >
                                  {profile.last_error_message ??
                                    `${profile.model}${profile.is_default ? ` · ${t("default")}` : ""}`}
                                </small>
                              </span>
                              <span className="sync-account-actions">
                                <button
                                  className="icon-button"
                                  type="button"
                                  aria-label={`${t("edit")} ${profile.name}`}
                                  title={t("editAIProvider")}
                                  onClick={() => props.onEditAIProfile(profile)}
                                >
                                  <PencilSimple />
                                </button>
                                <button
                                  className={
                                    profile.is_default
                                      ? "icon-button icon-button--active"
                                      : "icon-button"
                                  }
                                  type="button"
                                  aria-label={`${t("useByDefault")} ${profile.name}`}
                                  title={t("makeDefault")}
                                  disabled={profile.is_default}
                                  onClick={() => props.onDefaultAIProfile(profile.id)}
                                >
                                  <Check />
                                </button>
                                <button
                                  className="icon-button"
                                  type="button"
                                  aria-label={`${t("delete")} ${profile.name}`}
                                  title={t("deleteAIProvider")}
                                  onClick={() => props.onDeleteAIProfile(profile.id)}
                                >
                                  <Trash />
                                </button>
                              </span>
                            </div>
                          ))}
                          {props.aiProfiles.length === 0 && (
                            <p className="preference-empty">{t("noAIProviders")}</p>
                          )}
                        </div>
                        {props.aiUsage && (
                          <p className="ai-usage">
                            {new Intl.NumberFormat(locale).format(props.aiUsage.total_tokens)}{" "}
                            {t("tokensUsed")}
                          </p>
                        )}
                      </section>
                    </>
                  )}

                  {activeTab === "sync" && (
                    <>
                      <CloudProviderGrid
                        accounts={cloudAccounts}
                        t={t}
                        onSelect={props.onAddSyncAccount}
                      />
                      <SyncAccountSection
                        title={t("libraryCloudSync")}
                        description={t("libraryCloudSyncDescription")}
                        empty={t("noCloudSyncAccounts")}
                        accounts={cloudAccounts}
                        locale={locale}
                        t={t}
                        pendingID={props.syncPendingID}
                        onAdd={() => props.onAddSyncAccount()}
                        onToggle={props.onToggleSyncAccount}
                        onRun={runCloudSync}
                        onEdit={props.onEditSyncAccount}
                        onDelete={props.onDeleteSyncAccount}
                        cloud
                      />
                      <SyncAccountSection
                        title={t("readingServiceSync")}
                        description={t("readingServiceSyncDescription")}
                        empty={t("noServiceSyncAccounts")}
                        accounts={serviceAccounts}
                        locale={locale}
                        t={t}
                        pendingID={props.syncPendingID}
                        onAdd={() => props.onAddSyncAccount("freshrss")}
                        onToggle={props.onToggleSyncAccount}
                        onRun={(account) => props.onRunSyncAccount(account.id, "auto")}
                        onEdit={props.onEditSyncAccount}
                        onDelete={props.onDeleteSyncAccount}
                      />
                    </>
                  )}

                  {activeTab === "library" && (
                    <>
                      <section className="preference-section preference-section--row">
                        <div>
                          <h2>{t("subscriptions")}</h2>
                          <p>{t("subscriptionsDescription")}</p>
                        </div>
                        <div className="button-group">
                          <button
                            className="button button--secondary"
                            type="button"
                            onClick={props.onOrganizeLibrary}
                          >
                            <Books />
                            {t("manage")}
                          </button>
                          <a
                            className="button button--secondary"
                            href="/api/v1/exports/opml"
                            download
                          >
                            <DownloadSimple />
                            {t("export")}
                          </a>
                        </div>
                      </section>
                      <section className="preference-section preference-section--row">
                        <div>
                          <h2>{t("retentionTitle")}</h2>
                          <p>{t("retentionDescription")}</p>
                        </div>
                        <select
                          className="select-input preference-language"
                          aria-label={t("retentionTitle")}
                          value={String(retentionDays)}
                          disabled={retentionMutation.isPending}
                          onChange={(event) => retentionMutation.mutate(Number(event.target.value))}
                        >
                          <option value="0">{t("retentionForever")}</option>
                          <option value="30">{t("retentionDays30")}</option>
                          <option value="90">{t("retentionDays90")}</option>
                          <option value="180">{t("retentionDays180")}</option>
                          <option value="365">{t("retentionDays365")}</option>
                        </select>
                      </section>
                      <section className="preference-section preference-section--row">
                        <div>
                          <h2>{t("libraryBackup")}</h2>
                          <p>{t("backupDescription")}</p>
                        </div>
                        <div className="button-group">
                          <a className="button button--secondary" href="/api/v1/backup" download>
                            <DownloadSimple />
                            {t("backup")}
                          </a>
                          <input
                            ref={restoreInput}
                            className="sr-only"
                            type="file"
                            accept="application/json,.json"
                            onChange={(event) => selectBackup(event.target.files?.[0])}
                          />
                          <button
                            className="button button--secondary"
                            type="button"
                            disabled={props.restorePending}
                            onClick={() => restoreInput.current?.click()}
                          >
                            {props.restorePending ? (
                              <CircleNotch className="spin" />
                            ) : (
                              <UploadSimple />
                            )}
                            {t("restore")}
                          </button>
                        </div>
                      </section>
                    </>
                  )}

                  {activeTab === "devices" && (
                    <section className="preference-section preference-section--flush">
                      <div className="preference-heading preference-heading--intro">
                        <div>
                          <h2>{t("devices")}</h2>
                          <p>{t("devicesDescription")}</p>
                        </div>
                        <button
                          className="button button--secondary"
                          type="button"
                          disabled={props.pairingCodePending}
                          onClick={props.onCreatePairingCode}
                        >
                          {props.pairingCodePending ? (
                            <CircleNotch className="spin" />
                          ) : (
                            <LinkSimple />
                          )}
                          {t("pair")}
                        </button>
                      </div>
                      {props.pairingCode && (
                        <div className="pairing-code-display">
                          <strong>{props.pairingCode.code}</strong>
                          <time dateTime={props.pairingCode.expires_at}>
                            {t("expires")}{" "}
                            {new Intl.DateTimeFormat(locale, { timeStyle: "short" }).format(
                              new Date(props.pairingCode.expires_at),
                            )}
                          </time>
                        </div>
                      )}
                      <div className="device-list">
                        {activeDevices.map((device) => (
                          <div className="device-row" key={device.id}>
                            <span className="device-row__platform">
                              {device.platform.slice(0, 1).toUpperCase()}
                            </span>
                            <span>
                              <strong>{device.name}</strong>
                              <small>
                                {device.last_seen_at
                                  ? `${t("seen")} ${new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(device.last_seen_at))}`
                                  : t("notConnectedYet")}
                              </small>
                            </span>
                            <button
                              className="icon-button"
                              type="button"
                              aria-label={`${t("revokeDevice")} ${device.name}`}
                              title={t("revokeDevice")}
                              onClick={() => props.onRevokeDevice(device.id)}
                            >
                              <Trash />
                            </button>
                          </div>
                        ))}
                      </div>
                      {activeDevices.length === 0 && (
                        <p className="preference-empty">{t("noPairedDevices")}</p>
                      )}
                    </section>
                  )}
                </div>
                {props.error && (
                  <p className="form-error preferences-error" role="alert">
                    {props.error.message}
                  </p>
                )}
                <footer className="dialog-meta">API {props.status?.api_version ?? "v1"}</footer>
              </main>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
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

function toggleID(values: string[], id: string) {
  return values.includes(id) ? values.filter((value) => value !== id) : [...values, id]
}

function SyncAccountSection(props: {
  title: string
  description: string
  empty: string
  accounts: SyncAccount[]
  locale: Locale
  t: Translator
  pendingID?: string
  cloud?: boolean
  onAdd: () => void
  onToggle: (accountID: string, enabled: boolean) => void
  onRun: (account: SyncAccount, mode: "auto" | "push" | "pull") => void
  onEdit: (account: SyncAccount) => void
  onDelete: (accountID: string) => void
}) {
  return (
    <section className="preference-section preference-section--flush">
      <div className="preference-heading preference-heading--intro">
        <div>
          <h2>{props.title}</h2>
          <p>{props.description}</p>
        </div>
        <button className="button button--secondary" type="button" onClick={props.onAdd}>
          <Plus />
          {props.t("add")}
        </button>
      </div>
      <div className="sync-account-list">
        {props.accounts.map((account) => {
          const pending = props.pendingID === account.id
          const conflict = account.last_error_code === "conflict"
          return (
            <div
              className={
                conflict ? "sync-account-row sync-account-row--conflict" : "sync-account-row"
              }
              key={account.id}
            >
              <label
                className="sync-account-toggle"
                title={account.enabled ? props.t("disableAccount") : props.t("enableAccount")}
              >
                <input
                  type="checkbox"
                  checked={account.enabled}
                  onChange={(event) => props.onToggle(account.id, event.target.checked)}
                />
                <span className="sr-only">
                  {props.t("enable")} {account.name}
                </span>
              </label>
              <span className="sync-account-copy">
                <strong>
                  {props.cloud && <Cloud />}
                  {account.name}
                </strong>
                <small className={account.last_error_message ? "sync-account-error" : undefined}>
                  {conflict
                    ? props.t("cloudConflict")
                    : (account.last_error_message ??
                      (account.last_sync_at
                        ? `${props.t("synced")} ${new Intl.DateTimeFormat(props.locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(account.last_sync_at))}`
                        : props.t("notSyncedYet")))}
                </small>
              </span>
              <span className="sync-account-actions">
                <button
                  className="icon-button"
                  type="button"
                  aria-label={`${props.t("edit")} ${account.name}`}
                  title={props.t("editSyncAccount")}
                  onClick={() => props.onEdit(account)}
                >
                  <PencilSimple />
                </button>
                <button
                  className="icon-button"
                  type="button"
                  aria-label={`${props.t("syncNow")} ${account.name}`}
                  title={props.t("syncNow")}
                  disabled={!account.enabled || pending}
                  onClick={() => props.onRun(account, "auto")}
                >
                  {pending ? <CircleNotch className="spin" /> : <ArrowsClockwise />}
                </button>
                {props.cloud && (
                  <button
                    className="icon-button"
                    type="button"
                    aria-label={`${props.t("uploadLocalLibrary")} ${account.name}`}
                    title={props.t("uploadLocalLibrary")}
                    disabled={!account.enabled || pending}
                    onClick={() => props.onRun(account, "push")}
                  >
                    <CloudArrowUp />
                  </button>
                )}
                {props.cloud && (
                  <button
                    className="icon-button"
                    type="button"
                    aria-label={`${props.t("restoreFromCloud")} ${account.name}`}
                    title={props.t("restoreFromCloud")}
                    disabled={!account.enabled || pending}
                    onClick={() => props.onRun(account, "pull")}
                  >
                    <CloudArrowDown />
                  </button>
                )}
                <button
                  className="icon-button"
                  type="button"
                  aria-label={`${props.t("delete")} ${account.name}`}
                  title={props.t("deleteSyncAccount")}
                  onClick={() => props.onDelete(account.id)}
                >
                  <Trash />
                </button>
              </span>
            </div>
          )
        })}
        {props.accounts.length === 0 && <p className="preference-empty">{props.empty}</p>}
      </div>
    </section>
  )
}

function CloudProviderGrid(props: {
  accounts: SyncAccount[]
  t: Translator
  onSelect: (provider: SyncProviderID) => void
}) {
  const providers: Array<{
    id: "webdav" | "icloud"
    name: string
    description: string
    icon: typeof CloudArrowUp
  }> = [
    {
      id: "webdav",
      name: "WebDAV",
      description: props.t("webdavSyncDescription"),
      icon: CloudArrowUp,
    },
    {
      id: "icloud",
      name: props.t("icloudDrive"),
      description: props.t("icloudSyncDescription"),
      icon: AppleLogo,
    },
  ]

  return (
    <section className="preference-section preference-section--flush sync-provider-section">
      <div className="sync-provider-grid">
        {providers.map((provider) => {
          const accounts = props.accounts.filter((account) => account.provider === provider.id)
          const connected = accounts.some((account) => account.enabled)
          const Icon = provider.icon
          return (
            <button
              className={`sync-provider-card sync-provider-card--${provider.id}`}
              type="button"
              key={provider.id}
              onClick={() => props.onSelect(provider.id)}
            >
              <span className="sync-provider-card__icon">
                <Icon />
              </span>
              <span className="sync-provider-card__copy">
                <strong>{provider.name}</strong>
                <small>{provider.description}</small>
                <span>{connected ? props.t("connected") : props.t("notConfigured")}</span>
              </span>
              <CaretRight />
            </button>
          )
        })}
      </div>
    </section>
  )
}

// 标签分区：调色板的唯一管理入口（工作台表格里只留指派和就地新建）。
// 顺序就是优先级——服务端按数组下标写 position，未知 id 被忽略但下标照样占一格，
// 所以每次都提交完整顺序，绝不提交局部片段。
interface TagsResponse {
  tags: ResearchTag[]
}

// 八档色相的显示名键位，键名规则是 color + 首字母大写的色相名（见 i18n）。
// 用 Record 钉死：调色板加一档而这里漏一条时，typecheck 先报错，界面上不会出现
// 一颗没有名字的色块。
const TAG_COLOR_LABEL_KEYS: Record<TagColorName, string> = {
  red: "colorRed",
  amber: "colorAmber",
  gray: "colorGray",
  green: "colorGreen",
  teal: "colorTeal",
  orange: "colorOrange",
  violet: "colorViolet",
  blue: "colorBlue",
}

function TagsSection(props: {
  t: Translator
  onRenaming: (active: boolean) => void
  onConfirm: (message: string, action: () => void) => void
}) {
  const queryClient = useQueryClient()
  const [name, setName] = useState("")
  const tagsQuery = useQuery({
    queryKey: ["research-tags"],
    queryFn: ({ signal }: { signal: AbortSignal }) => listResearchTags(signal),
  })
  const listed = tagsQuery.data?.tags
  const tags = useMemo(() => listed ?? [], [listed])

  // 增删改都会动到论文的 tag_ids（删除会顺带把所有论文上的这个标签摘掉），所以
  // 除了调色板还要刷 ["research"]——它是工作台三种 kind 查询的公共前缀。
  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: ["research-tags"] })
    void queryClient.invalidateQueries({ queryKey: ["research"] })
  }
  // 重名 409 单独说一句：只报"创建失败"用户会以为是网络问题，然后一直重试。
  const failure = (error: Error, fallback: string) =>
    error instanceof APIError && error.status === 409 ? props.t("tagNameTaken") : fallback

  const createMutation = useMutation({
    mutationFn: (tagName: string) => createResearchTag(tagName),
    onSuccess: () => {
      setName("")
      invalidateAll()
      toast(props.t("tagCreated"))
    },
    onError: (error) => toast(failure(error, props.t("tagCreateFailed"))),
  })
  const renameMutation = useMutation({
    mutationFn: ({ tagID, tagName }: { tagID: string; tagName: string }) =>
      updateResearchTag(tagID, { name: tagName }),
    onSuccess: () => invalidateAll(),
    onError: (error) => toast(failure(error, props.t("tagRenameFailed"))),
  })
  // 换色与改名是同一个 PATCH，但这一格只写颜色那一个字段：服务端的契约是"没带的
  // 字段一律不动"，所以不必再把当前名字原样重发一遍。
  // 传空串就是「自动」：颜色退回调色板下标推出来的那一档。
  const colorMutation = useMutation({
    mutationFn: ({ tagID, color }: { tagID: string; color: string }) =>
      updateResearchTag(tagID, { color }),
    onSuccess: () => invalidateAll(),
    onError: (error) => toast(failure(error, props.t("tagColorFailed"))),
  })
  // 染色开关单立一条 mutation：它只写 color_enabled，pending 时不该把整排色块一起
  // 禁掉——用户点完开关往往紧接着就要点色。
  const paintMutation = useMutation({
    mutationFn: ({ tagID, enabled }: { tagID: string; enabled: boolean }) =>
      updateResearchTag(tagID, { color_enabled: enabled }),
    onSuccess: () => invalidateAll(),
    onError: (error) => toast(failure(error, props.t("tagColorFailed"))),
  })
  const deleteMutation = useMutation({
    mutationFn: (tagID: string) => deleteResearchTag(tagID),
    onSuccess: () => {
      invalidateAll()
      toast(props.t("tagDeleted"))
    },
    onError: (error) => toast(failure(error, props.t("tagDeleteFailed"))),
  })
  // 排序只动 position，不碰论文上的关联，所以只刷调色板。缓存先改写：拖完立刻
  // 是新顺序，失败再回滚，沿用工作台论文重排的乐观更新套路。
  const reorderMutation = useMutation({
    mutationFn: (tagIDs: string[]) => reorderResearchTags(tagIDs),
    onMutate: async (tagIDs) => {
      await queryClient.cancelQueries({ queryKey: ["research-tags"] })
      const prev = queryClient.getQueryData<TagsResponse>(["research-tags"])
      queryClient.setQueryData<TagsResponse>(["research-tags"], (old) =>
        old
          ? {
              ...old,
              tags: tagIDs
                .map((id) => old.tags.find((tag) => tag.id === id))
                .filter((tag): tag is ResearchTag => tag !== undefined)
                .map((tag, index) => ({ ...tag, position: index })),
            }
          : old,
      )
      return { prev }
    },
    onError: (error, _tagIDs, context) => {
      if (context?.prev) queryClient.setQueryData(["research-tags"], context.prev)
      toast(failure(error, props.t("tagReorderFailed")))
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["research-tags"] })
    },
  })

  const dragFrom = useRef<string | null>(null)
  const [armed, setArmed] = useState<string | null>(null)
  const [dropHint, setDropHint] = useState<{ id: string; before: boolean } | null>(null)

  const submitCreate = () => {
    const trimmed = name.trim()
    if (!trimmed || createMutation.isPending) return
    createMutation.mutate(trimmed)
  }
  const commit = (next: ResearchTag[]) => reorderMutation.mutate(next.map((tag) => tag.id))
  // 同一枚色块再点一次就是"没打算改"，不发请求（PATCH 成功也会整栏刷新，
  // 白闪一次）。
  const commitColor = (tag: ResearchTag, color: string) => {
    if (tag.color === color || colorMutation.isPending) return
    colorMutation.mutate({ tagID: tag.id, color })
  }
  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= tags.length) return
    const next = tags.slice()
    const [node] = next.splice(index, 1)
    next.splice(target, 0, node!)
    commit(next)
  }
  const drop = (event: DragEvent<HTMLLIElement>, toID: string) => {
    event.preventDefault()
    const fromID = dragFrom.current
    dragFrom.current = null
    setArmed(null)
    setDropHint(null)
    if (!fromID || fromID === toID) return
    const rect = event.currentTarget.getBoundingClientRect()
    commit(reorderList(tags, fromID, toID, event.clientY < rect.top + rect.height / 2))
  }

  return (
    <section className="preference-section preference-section--flush preference-section--tags">
      <div className="pref-tag-create">
        <input
          className="text-input"
          type="text"
          value={name}
          maxLength={40}
          disabled={createMutation.isPending}
          placeholder={props.t("newTagPlaceholder")}
          aria-label={props.t("newTag")}
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault()
              submitCreate()
            }
          }}
        />
        <button
          className="button button--secondary"
          type="button"
          disabled={!name.trim() || createMutation.isPending}
          onClick={submitCreate}
        >
          {createMutation.isPending ? <CircleNotch className="spin" /> : <Plus />}
          {props.t("newTag")}
        </button>
      </div>
      {tagsQuery.isPending ? (
        <p className="preference-empty">{props.t("loading")}</p>
      ) : tags.length === 0 ? (
        <p className="preference-empty">{props.t("noTagsYet")}</p>
      ) : (
        <ul className="pref-tag-list">
          {tags.map((tag, index) => {
            const hint = dropHint?.id === tag.id ? (dropHint.before ? "top" : "bottom") : null
            return (
              <li
                key={tag.id}
                className={`pref-tag-row${hint ? ` pref-tag-row--drop-${hint}` : ""}`}
                // 只有按住把手才给这一行上 draggable：WKWebView 会把整行的
                // mousedown 当拖拽，吞掉改名和按钮的点击（同 shared.tsx 的 Row）。
                draggable={armed === tag.id}
                onPointerDown={(event) =>
                  setArmed(
                    (event.target as HTMLElement).closest(".pref-tag-grip") ? tag.id : null,
                  )
                }
                onDragStart={(event) => {
                  if (armed !== tag.id) {
                    event.preventDefault()
                    return
                  }
                  dragFrom.current = tag.id
                  event.dataTransfer.effectAllowed = "move"
                  event.dataTransfer.setData("text/pref-tag", tag.id)
                }}
                onDragEnd={() => {
                  dragFrom.current = null
                  setArmed(null)
                  setDropHint(null)
                }}
                onDragOver={(event) => {
                  if (!event.dataTransfer.types.includes("text/pref-tag")) return
                  event.preventDefault()
                  const rect = event.currentTarget.getBoundingClientRect()
                  setDropHint({ id: tag.id, before: event.clientY < rect.top + rect.height / 2 })
                }}
                onDragLeave={() => setDropHint(null)}
                onDrop={(event) => drop(event, tag.id)}
              >
                <span className="pref-tag-grip" aria-hidden="true">
                  ⠿
                </span>
                {/* 色点与工作台同一套：先取标签自己存的颜色，没有自选色才回落到
                    调色板下标推出来的那一档。它同时是染色开关的预览——开关关掉这里就
                    变中性灰，所以这一栏里"涂不涂"看一眼就知道。 */}
                <i
                  className={`wb-dot ${tagDotClass(index, tag.color, tag.color_enabled)}`}
                  aria-hidden="true"
                />
                {/* 显示的是存进库的那个名字（不是工作台对旧优先级名的本地化显示），
                    因为这一格要写的就是它。 */}
                <TagNameField
                  value={tag.name}
                  t={props.t}
                  onRenaming={props.onRenaming}
                  onCommit={(tagName) => renameMutation.mutate({ tagID: tag.id, tagName })}
                />
                {/* 第二行：染色开关 + 颜色本身。
                    开关管的是"这枚标签的颜色要不要真的涂上去"，不是标签本身——关掉之后
                    名字照样能挂到论文上，下面这排选中的色块也照旧留着主色环，所以重新
                    打开回到原样。归类的标签（合作者、实证）不该把整行染红，这才是它的用途。
                    颜色：八档既有淡底 +「自动」。自动写回空串，意思是"跟随调色板
                    下标"——所以下面这排在拖动顺序之后仍会整体换色；自选色则钉住不动。
                    色块自己不写颜色字面量：底色读同行的 wb-badge--* 挂上来的
                    --wb-badge-hue，与表里的药丸永远同一档色相、同一套深浅分档。 */}
                <span className="pref-tag-colors">
                  <label className="pref-tag-paint" title={props.t("tagColorEnabledHint")}>
                    <input
                      type="checkbox"
                      checked={tag.color_enabled}
                      disabled={paintMutation.isPending}
                      aria-label={`${props.t("tagColorEnabled")}: ${tag.name}`}
                      onChange={(event) =>
                        paintMutation.mutate({ tagID: tag.id, enabled: event.target.checked })
                      }
                    />
                    <span>{props.t("tagColorEnabled")}</span>
                  </label>
                  <span
                    className={`pref-tag-color-group ${
                      tag.color_enabled ? "" : "pref-tag-color-group--off"
                    }`}
                    role="group"
                    aria-label={props.t("tagColor")}
                  >
                    <button
                      type="button"
                      className={`pref-tag-color-auto ${
                        tag.color === "" ? "pref-tag-color--on" : ""
                      }`}
                      aria-pressed={tag.color === ""}
                      aria-label={`${props.t("tagColor")}: ${props.t("tagColorAuto")}`}
                      title={props.t("tagColorAuto")}
                      disabled={colorMutation.isPending}
                      onClick={() => commitColor(tag, "")}
                    >
                      {props.t("tagColorAuto")}
                    </button>
                    {TAG_COLOR_NAMES.map((name) => {
                      const on = tag.color === name
                      const label = props.t(TAG_COLOR_LABEL_KEYS[name])
                      return (
                        <button
                          key={name}
                          type="button"
                          className={`pref-tag-swatch wb-badge--${name} ${
                            on ? "pref-tag-swatch--on" : ""
                          }`}
                          aria-pressed={on}
                          aria-label={`${props.t("tagColor")}: ${label}`}
                          title={label}
                          disabled={colorMutation.isPending}
                          onClick={() => commitColor(tag, name)}
                        />
                      )
                    })}
                  </span>
                </span>
                <span className="pref-tag-actions">
                  <button
                    className="icon-button icon-button--small"
                    type="button"
                    aria-label={`${props.t("moveUp")} ${tag.name}`}
                    title={props.t("moveUp")}
                    disabled={index === 0 || reorderMutation.isPending}
                    onClick={() => move(index, -1)}
                  >
                    <ArrowUp />
                  </button>
                  <button
                    className="icon-button icon-button--small"
                    type="button"
                    aria-label={`${props.t("moveDown")} ${tag.name}`}
                    title={props.t("moveDown")}
                    disabled={index === tags.length - 1 || reorderMutation.isPending}
                    onClick={() => move(index, 1)}
                  >
                    <ArrowDown />
                  </button>
                  <button
                    className="icon-button icon-button--small pref-tag-delete"
                    type="button"
                    aria-label={`${props.t("delete")} ${tag.name}`}
                    title={props.t("delete")}
                    onClick={() =>
                      props.onConfirm(
                        props.t("deleteTagConfirm").replace("{name}", tag.name),
                        () => deleteMutation.mutate(tag.id),
                      )
                    }
                  >
                    <Trash />
                  </button>
                </span>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

// 改名沿用工作台 InlineText 的提交语义（Enter/失焦写回，Esc 放弃），但进编辑是
// 单击：这一格不在可拖拽的表体里，没有"单击被行选中抢走"的顾虑，双击才能改在这份
// 清单里只是个隐藏手势。样式用偏好面板自己的类名（工作台那份带表格格的负 margin）。
// onRenaming 是这格存在的全部理由：Esc 在偏好面板里意味着"关掉整页设置"，而 Radix
// 的判定发生在 document 的捕获阶段，所以只能由对话框的 onEscapeKeyDown 放行，
// 在这里 stopPropagation 是拦不住的。
function TagNameField(props: {
  value: string
  t: Translator
  onRenaming: (active: boolean) => void
  onCommit: (name: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(props.value)
  const inputRef = useRef<HTMLInputElement | null>(null)

  // 编辑中途整栏被换掉（切标签页、列表重取）时不能把闸门留在开启状态，
  // 否则偏好面板就再也按不动 Esc 了。
  const { onRenaming } = props
  useEffect(() => {
    onRenaming(editing)
    return () => onRenaming(false)
  }, [editing, onRenaming])

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [editing])

  const start = () => {
    setDraft(props.value)
    setEditing(true)
  }
  const commit = () => {
    setEditing(false)
    const next = draft.trim()
    if (next !== props.value) props.onCommit(next)
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        className="text-input pref-tag-name-input"
        type="text"
        value={draft}
        maxLength={40}
        aria-label={props.value}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault()
            commit()
          } else if (event.key === "Escape") {
            setDraft(props.value)
            setEditing(false)
          }
        }}
      />
    )
  }
  return (
    <span
      className="pref-tag-name"
      role="button"
      tabIndex={0}
      title={props.t("editHintClick")}
      onClick={start}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault()
          start()
        }
      }}
    >
      {props.value}
    </span>
  )
}
