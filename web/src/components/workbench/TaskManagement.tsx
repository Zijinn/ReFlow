import "./tasks.css"
import "./task-management.css"

import {
  CalendarBlank,
  Lightning,
  ListChecks,
  TreeStructure,
} from "@phosphor-icons/react"
import { useState } from "react"

import { useTranslation } from "../../lib/i18n"
import { QuickTasks } from "./QuickTasks"
import { TaskBoard } from "./TaskBoard"
import { TaskMindMap } from "./TaskMindMap"
import { TaskTimeline } from "./TaskTimeline"

export type TaskView = "quick" | "long" | "timeline" | "mindmap"

const TASK_VIEWS: Array<{ id: TaskView; labelKey: string; Icon: typeof Lightning }> = [
  { id: "quick", labelKey: "taskViewQuick", Icon: Lightning },
  { id: "long", labelKey: "taskViewLong", Icon: ListChecks },
  { id: "timeline", labelKey: "taskViewTimeline", Icon: CalendarBlank },
  { id: "mindmap", labelKey: "taskViewMindMap", Icon: TreeStructure },
]

export function TaskManagement(props: { offline?: boolean }) {
  const { t } = useTranslation()
  const [view, setView] = useState<TaskView>("quick")

  return (
    <div className="wb-tm">
      <nav className="wb-nav wb-task-view-nav" aria-label={t("taskViewNavLabel")}>
        {TASK_VIEWS.map((entry) => (
          <button
            key={entry.id}
            type="button"
            className={`wb-nav-item ${view === entry.id ? "wb-nav-item--active" : ""}`}
            aria-current={view === entry.id ? "page" : undefined}
            onClick={() => setView(entry.id)}
          >
            <entry.Icon size={15} weight={view === entry.id ? "fill" : "regular"} />
            <span>{t(entry.labelKey)}</span>
          </button>
        ))}
      </nav>
      {view === "quick" && (
        <QuickTasks offline={props.offline} onOpenTimeline={() => setView("timeline")} />
      )}
      {view === "long" && <TaskBoard offline={props.offline} />}
      {view === "timeline" && (
        <TaskTimeline offline={props.offline} onNewQuick={() => setView("quick")} />
      )}
      {view === "mindmap" && (
        <TaskMindMap offline={props.offline} onImported={() => setView("long")} />
      )}
    </div>
  )
}
