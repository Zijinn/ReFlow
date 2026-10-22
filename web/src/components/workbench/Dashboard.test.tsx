import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchPaper } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { Dashboard } from "./Dashboard"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "p-1",
    kind: "research",
    position: 0,
    title: "A Working Paper",
    authors: [],
    keywords: [],
    file_path: "",
    next_action: "Finalize tables",
    notes: "",
    research_area: "",
    status: "",
    tag_ids: ["t-high"],
    target_journal: "经济研究",
    stages: [
      { name: "Done", done: true, children: [] },
      { name: "Pending", done: false, children: [] },
    ],
    current_journal: "",
    submission_date: "",
    manuscript_id: "",
    submission_count: 0,
    target_level: "",
    editor: "",
    deadline: "",
    history: [],
    abstract: "",
    journal: "",
    language: "",
    year: "",
    volume: "",
    issue: "",
    pages: "",
    doi: "",
    citations: null,
    citation_source: "",
    citation_updated_at: "",
    last_updated: "2026-09-10",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

describe("Dashboard", () => {
  const metric = (label: string): Element | undefined =>
    Array.from(document.querySelectorAll(".wb-ov-metric")).find((element) =>
      element.textContent?.includes(label),
    )

  it("shows the summary metrics and average progress", () => {
    render(
      <Dashboard
        research={[paper()]}
        submitted={[paper({ id: "s-1", kind: "submitted", title: "Under Review" })]}
        published={[paper({ id: "q-1", kind: "published", title: "Published" })]}
        onNavigate={() => {}}
      />,
    )
    expect(metric("Working papers")).toHaveTextContent("1")
    expect(metric("Submissions")).toHaveTextContent("1")
    expect(metric("Publications")).toHaveTextContent("1")
    expect(metric("Average progress")).toHaveTextContent("50%")
    expect(document.querySelector(".wb-overview .wb-daily")).not.toBeNull()
  })

  it("counts priority projects from the top palette tag", () => {
    // 优先项目取调色板最前的标签，与标签叫什么无关：用户改名后这一栏依旧成立。
    const tags = [
      { id: "t-high", name: "冲刺中", position: 0, color: "", color_enabled: true },
      { id: "t-average", name: "Average", position: 2, color: "", color_enabled: true },
    ]
    render(
      <Dashboard
        research={[paper(), paper({ id: "p-2", title: "Low prio", tag_ids: ["t-average"] })]}
        submitted={[]}
        published={[]}
        tags={tags}
        onNavigate={() => {}}
      />,
    )
    expect(metric("Priority projects")).toHaveTextContent("1")
    expect(metric("Priority projects")).toHaveTextContent("冲刺中")
  })

  it("counts a paper wearing the top tag next to others", () => {
    const tags = [
      { id: "t-field", name: "Fieldwork", position: 0, color: "", color_enabled: true },
      { id: "t-high", name: "High", position: 1, color: "", color_enabled: true },
    ]
    render(
      <Dashboard
        research={[paper({ id: "p-9", title: "Both tags", tag_ids: ["t-high", "t-field"] })]}
        submitted={[]}
        published={[]}
        tags={tags}
        onNavigate={() => {}}
      />,
    )
    expect(metric("Priority projects")).toHaveTextContent("1")
  })

  it("navigates to the matching page when a stat card is activated", () => {
    const onNavigate = vi.fn()
    render(
      <Dashboard
        research={[paper()]}
        submitted={[paper({ id: "s-1", kind: "submitted" })]}
        published={[paper({ id: "q-1", kind: "published" })]}
        onNavigate={onNavigate}
      />,
    )
    fireEvent.click(metric("Publications") as Element)
    expect(onNavigate).toHaveBeenLastCalledWith("published")
    fireEvent.click(metric("Average progress") as Element)
    expect(onNavigate).toHaveBeenLastCalledWith("research")
  })

  it("leads with the daily progress card and hides its duplicate action", () => {
    const onConfigureAI = vi.fn()
    render(
      <Dashboard
        research={[paper()]}
        submitted={[paper({ id: "s-1", kind: "submitted", deadline: "2026-09-12" })]}
        published={[]}
        onConfigureAI={onConfigureAI}
        onNavigate={() => {}}
      />,
    )
    const card = screen.getByRole("region", { name: "Today's progress plan" })
    expect(card).toBeInTheDocument()
    expect(document.querySelector(".wb-overview .wb-daily")).toBe(card)
    expect(card).toHaveTextContent("Configure AI to get a daily progress plan here.")
    // The overview header owns the one generate/configure action; the card must
    // not repeat it.
    expect(screen.queryByRole("button", { name: "Configure AI" })).toBeNull()
  })

  it("counts the deadline pressure of the papers it is given", () => {
    const today = Date.now()
    const day = (offset: number) => new Date(today + offset * 86_400_000).toISOString().slice(0, 10)
    render(
      <Dashboard
        research={[paper()]}
        submitted={[
          paper({ id: "s-1", kind: "submitted", deadline: day(2) }),
          paper({ id: "s-2", kind: "submitted", deadline: day(30) }),
          paper({ id: "s-3", kind: "submitted", deadline: "TBD" }),
        ]}
        published={[paper({ id: "q-1", kind: "published" })]}
        onNavigate={() => {}}
      />,
    )
    const rows = Array.from(document.querySelectorAll(".wb-daily dl > div")).map((row) => [
      row.querySelector("dt")?.textContent,
      row.querySelector("dd")?.textContent,
    ])
    // Published papers have no forward deadline, so the plan covers the live
    // queue only; unparseable deadlines are ignored, never guessed.
    expect(rows).toEqual([
      ["Papers", "4"],
      ["Due in 7 days", "1"],
      ["Overdue", "0"],
    ])
  })
})

afterEach(() => cleanup())
