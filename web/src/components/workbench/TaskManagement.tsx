import "./tasks.css"
import "./task-management.css"

import { useTranslation } from "../../lib/i18n"
import { useReaderStore, type TaskFocus } from "../../store/reader"
import { QuickTasks } from "./QuickTasks"
import { TaskBoard } from "./TaskBoard"
import { TaskMindMap } from "./TaskMindMap"
import { TaskTimeline } from "./TaskTimeline"
import { TASK_VIEWS } from "./taskViews"

export function TaskManagement(props: {
  offline?: boolean
  focus?: TaskFocus | null
  onFocusConsumed?: () => void
}) {
  const { t } = useTranslation()
  // 桌面端由侧栏承担视图切换；这条条子只在侧栏隐藏的窄屏出现（见 apple-glass.css）。
  const view = useReaderStore((state) => state.taskView)
  const setView = useReaderStore((state) => state.setTaskView)

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
        <QuickTasks
          offline={props.offline}
          onOpenTimeline={() => setView("timeline")}
          focus={props.focus}
          onFocusConsumed={props.onFocusConsumed}
        />
      )}
      {view === "long" && (
        <TaskBoard
          offline={props.offline}
          focus={props.focus}
          onFocusConsumed={props.onFocusConsumed}
        />
      )}
      {view === "timeline" && (
        <TaskTimeline offline={props.offline} onNewQuick={() => setView("quick")} />
      )}
      {view === "mindmap" && (
        <TaskMindMap offline={props.offline} onImported={() => setView("long")} />
      )}
    </div>
  )
}
