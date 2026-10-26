import { Books, ChartPieSlice, NotePencil, PaperPlaneTilt } from "@phosphor-icons/react"

import type { WorkbenchTab } from "../../store/reader"

export const RESEARCH_TABS: Array<{
  id: WorkbenchTab
  labelKey: string
  Icon: typeof Books
}> = [
  { id: "dashboard", labelKey: "researchOverview", Icon: ChartPieSlice },
  { id: "research", labelKey: "workingPapers", Icon: NotePencil },
  { id: "submitted", labelKey: "submissions", Icon: PaperPlaneTilt },
  { id: "published", labelKey: "publications", Icon: Books },
]
