import { Books, House, Star, Tray } from "@phosphor-icons/react"
import type { ReactNode } from "react"

import type { LibraryScope } from "../api/types"
import { localizedScopeTitle, useTranslation } from "../lib/i18n"

interface MobileNavProps {
  scope: LibraryScope
  onScopeChange: (scope: LibraryScope) => void
  onLibrary: () => void
  /**
   * The AI / theme / preferences cluster. Below 900px `.sidebar` is
   * `display: none`, so this fixed bar is the only surface that keeps those
   * three reachable — including in the workbench view, where the reader header
   * that used to carry them is not rendered at all.
   */
  actions?: ReactNode
}

export function MobileNav(props: MobileNavProps) {
  const { locale, t } = useTranslation()
  const items: Array<{ scope: LibraryScope; icon: typeof House }> = [
    { scope: { kind: "today", title: "Today" }, icon: House },
    { scope: { kind: "all", title: "All feeds" }, icon: Books },
    { scope: { kind: "unread", title: "Unread" }, icon: Tray },
    { scope: { kind: "saved", title: "Saved" }, icon: Star },
  ]
  return (
    <nav className="mobile-nav" aria-label={t("mobileNavigation")}>
      {items.map(({ scope, icon: Icon }) => {
        const active = props.scope.kind === scope.kind
        return <button className={active ? "mobile-nav__item mobile-nav__item--active" : "mobile-nav__item"} type="button" aria-current={active ? "page" : undefined} key={scope.kind} onClick={() => props.onScopeChange(scope)}><Icon weight={active ? "fill" : "regular"} /><span>{localizedScopeTitle(scope, locale)}</span></button>
      })}
      <button className="mobile-nav__item" type="button" onClick={props.onLibrary}><Books /><span>{t("library")}</span></button>
      {props.actions}
    </nav>
  )
}
