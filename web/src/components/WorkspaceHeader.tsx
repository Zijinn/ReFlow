import { MagnifyingGlass, Minus, Plus, Square, X } from "@phosphor-icons/react"

import type { LibraryScope } from "../api/types"
import { controlDesktopWindow, desktopPlatform } from "../lib/desktop"
import { localizedScopeTitle, useTranslation } from "../lib/i18n"

interface WorkspaceHeaderProps {
  scope: LibraryScope
  search: string
  searchShortcut: string
  onSearchChange: (value: string) => void
  onAdd: () => void
}

export function WorkspaceHeader(props: WorkspaceHeaderProps) {
  const { locale, t } = useTranslation()
  const platform = desktopPlatform()

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
      {/* AI / theme / preferences used to sit here (top-right, reader only).
          They live in Sidebar.tsx's bottom-left cluster and MobileNav.tsx now,
          so the reader and the workbench share one entry point. Add-feed stays:
          it is a library action, the sidebar already owns an add button, and
          this is the only add entry the phone layout has above the hidden
          sidebar. */}
      <div className="workspace-actions">
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
