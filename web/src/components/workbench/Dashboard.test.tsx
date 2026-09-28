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
    priority: "High",
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
  it("shows counts, average progress and recent updates", () => {
    render(
      <Dashboard
        research={[paper()]}
        submitted={[paper({ id: "s-1", kind: "submitted", title: "Under Review" })]}
        published={[paper({ id: "q-1", kind: "published", title: "Published" })]}
        onNavigate={() => {}}
      />,
    )
    expect(screen.getByText("Working papers")).toBeInTheDocument()
    expect(screen.getAllByText("50%").length).toBeGreaterThan(0)
    expect(screen.getAllByText("A Working Paper").length).toBeGreaterThan(0)
    expect(screen.getAllByText("Under Review").length).toBeGreaterThan(0)
    // Kind labels are translated at render time, not memoised into the data.
    expect(screen.getAllByText("In progress").length).toBeGreaterThan(0)
    expect(screen.getAllByText("Under review").length).toBeGreaterThan(0)
  })

  it("lists high-priority projects with the localized eyebrow", () => {
    render(
      <Dashboard
        research={[paper(), paper({ id: "p-2", title: "Low prio", priority: "Average" })]}
        submitted={[]}
        published={[]}
        onNavigate={() => {}}
      />,
    )
    const compactCards = document.querySelectorAll(".wb-card--compact")
    expect(compactCards).toHaveLength(1)
    expect(compactCards[0]).toHaveTextContent("A Working Paper")
    expect(compactCards[0]!.textContent).toContain("RESEARCH PROJECT")
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
    fireEvent.click(screen.getByRole("button", { name: /Publications/ }))
    expect(onNavigate).toHaveBeenLastCalledWith("published")
    // Average progress has no page of its own, so it gates the papers it measures.
    fireEvent.click(screen.getByRole("button", { name: /Average progress/ }))
    expect(onNavigate).toHaveBeenLastCalledWith("research")
  })
})

afterEach(() => cleanup())
