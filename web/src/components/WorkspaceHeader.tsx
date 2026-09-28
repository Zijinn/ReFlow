import {
  GearSix,
  MagnifyingGlass,
  Minus,
  Moon,
  Plus,
  Square,
  Sun,
  X,
} from "@phosphor-icons/react"
import { useEffect, useState } from "react"

import type { LibraryScope } from "../api/types"
import { controlDesktopWindow, desktopPlatform } from "../lib/desktop"
import { localizedScopeTitle, useTranslation } from "../lib/i18n"
import type { ThemeMode } from "../store/reader"
import { AIIcon } from "./AIIcon"

interface WorkspaceHeaderProps {
  scope: LibraryScope
  search: string
  searchShortcut: string
  theme: ThemeMode
  onSearchChange: (value: string) => void
  onThemeChange: (theme: ThemeMode) => void
  onPreferences: () => void
  onAdd: () => void
  aiOpen: boolean
  onAI: () => void
}

export function WorkspaceHeader(props: WorkspaceHeaderProps) {
  const { locale, t } = useTranslation()
  const platform = desktopPlatform()
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false,
  )
  const dark = props.theme === "dark" || (props.theme === "system" && systemDark)

  useEffect(() => {
    const query = window.matchMedia?.("(prefers-color-scheme: dark)")
    if (!query) return
    const update = () => setSystemDark(query.matches)
    query.addEventListener("change", update)
    return () => query.removeEventListener("change", update)
  }, [])

  return (
    <header className="workspace-header">
      <div className="workspace-breadcrumb" aria-label={t("currentLocation")}>
        <span>ReFlow</span>
        <i aria-hidden="true">/</i>
        <strong>{localizedScopeTitle(props.scope, locale)}</strong>
      </div>
      <label className="workspace-search" htmlFor="library-search">
        <MagnifyingGlass aria-hidden="true" />
        <span className="sr-only">{t("searchLibrary")}</span>
        <input
          id="library-search"
          type="search"
          value={props.search}
          placeholder={t("searchFeedsAndStories")}
          onChange={(event) => props.onSearchChange(event.target.value)}
        />
        <kbd>{props.searchShortcut}</kbd>
      </label>
      <div className="workspace-actions">
        <button
          className={props.aiOpen ? "icon-button icon-button--active" : "icon-button"}
          type="button"
          aria-label={t("aiAssistant")}
          title={t("aiAssistant")}
          aria-expanded={props.aiOpen}
          onClick={props.onAI}
        >
          <AIIcon />
        </button>
        <button
          className="icon-button"
          type="button"
          aria-label={dark ? t("switchToLight") : t("switchToDark")}
          title={dark ? t("switchToLight") : t("switchToDark")}
          onClick={() => props.onThemeChange(dark ? "light" : "dark")}
        >
          {dark ? <Sun /> : <Moon />}
        </button>
        <button
          className="icon-button"
          type="button"
          aria-label={t("preferences")}
          title={t("preferences")}
          onClick={props.onPreferences}
        >
          <GearSix />
        </button>
        <button
          className="button button--primary workspace-add"
          type="button"
          aria-label={t("addFeed")}
          title={t("addFeed")}
          onClick={props.onAdd}
        >
          <Plus />
          <span>{t("addFeed")}</span>
        </button>
        {platform === "windows" && (
          <div className="window-controls" aria-label={t("windowControls")}>
            <button
              className="window-control window-control--minimise"
              type="button"
              aria-label={t("minimiseWindow")}
              title={t("minimiseWindow")}
              onClick={() => void controlDesktopWindow("minimise")}
            >
              <Minus aria-hidden="true" />
            </button>
            <button
              className="window-control window-control--maximise"
              type="button"
              aria-label={t("maximiseWindow")}
              title={t("maximiseWindow")}
              onClick={() => void controlDesktopWindow("maximise")}
            >
              <Square aria-hidden="true" />
            </button>
            <button
              className="window-control window-control--close"
              type="button"
              aria-label={t("closeWindow")}
              title={t("closeWindow")}
              onClick={() => void controlDesktopWindow("close")}
            >
              <X aria-hidden="true" />
            </button>
          </div>
        )}
      </div>
    </header>
  )
}
