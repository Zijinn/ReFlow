import { CalendarBlank, Lightning, ListChecks, TreeStructure } from "@phosphor-icons/react"

import type { TaskView } from "../../store/reader"

export const TASK_VIEWS: Array<{ id: TaskView; labelKey: string; Icon: typeof Lightning }> = [
  { id: "quick", labelKey: "taskViewQuick", Icon: Lightning },
  { id: "long", labelKey: "taskViewLong", Icon: ListChecks },
  { id: "timeline", labelKey: "taskViewTimeline", Icon: CalendarBlank },
  { id: "mindmap", labelKey: "taskViewMindMap", Icon: TreeStructure },
]
