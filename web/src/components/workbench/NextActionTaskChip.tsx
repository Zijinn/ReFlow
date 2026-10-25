import { CheckCircle, ListPlus } from "@phosphor-icons/react"

import type { ResearchPaper } from "../../api/types"
import { useTranslation } from "../../lib/i18n"

export function NextActionTaskChip(props: {
  paper: ResearchPaper
  linked: boolean
  pending: boolean
  offline?: boolean
  onAdd?: (paper: ResearchPaper) => void
}) {
  const { t } = useTranslation()
  const { paper, linked, pending, offline, onAdd } = props
  if (!onAdd || paper.next_action.trim() === "") return null
  return (
    <button
      type="button"
      className={`wb-next-action-task${linked ? " wb-next-action-task--linked" : ""}`}
      aria-label={linked ? t("nextActionTaskAdded") : t("nextActionAddTask")}
      title={
        linked
          ? t("nextActionTaskAdded")
          : offline
            ? t("workbenchOfflineHint")
            : `${t("nextActionAddTask")} · ${paper.next_action}`
      }
      disabled={linked || offline || pending}
      onClick={() => onAdd(paper)}
    >
      {linked ? (
        <CheckCircle size={13} weight="fill" aria-hidden="true" />
      ) : (
        <ListPlus size={13} aria-hidden="true" />
      )}
      <span>{linked ? t("nextActionTaskAddedShort") : t("nextActionTaskShort")}</span>
    </button>
  )
}
