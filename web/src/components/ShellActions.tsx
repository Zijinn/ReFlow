import { GearSix, Moon, Sun } from "@phosphor-icons/react"
import { useEffect, useState } from "react"

import { useTranslation } from "../lib/i18n"
import type { ThemeMode } from "../store/reader"
import { AIIcon } from "./AIIcon"

interface ShellActionsProps {
  theme: ThemeMode
  onThemeChange: (theme: ThemeMode) => void
  aiOpen: boolean
  onAI: () => void
  onPreferences: () => void
  /**
   * Skin hook for the surface the cluster lands on (`.sidebar__actions` on
   * desktop, the mobile tab bar below 900px). The buttons themselves always
   * carry `.icon-button`, so they read identically in both places.
   */
  className?: string
}

/**
 * The AI / theme / preferences cluster that used to sit in the top-right of
 * the reader header. The reader remounts when the workbench replaces it, and
 * on phones the whole sidebar is `display: none`, so AppShell mounts exactly
 * one copy per surface instead of letting CSS hide a duplicate: the pair of
 * queries would otherwise double every accessible name in the document.
 */
export function ShellActions(props: ShellActionsProps) {
  const { t } = useTranslation()
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
    <div className={props.className ? `shell-actions ${props.className}` : "shell-actions"}>
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
    </div>
  )
}
