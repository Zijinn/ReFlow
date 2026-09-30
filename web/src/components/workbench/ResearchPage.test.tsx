import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchPaper } from "../../api/types"
import { STAGE_TEMPLATE_KEYS } from "../../lib/research"
import { useReaderStore } from "../../store/reader"
import { ResearchPage } from "./ResearchPage"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "r-1",
    kind: "research",
    position: 0,
    title: "Working Paper One",
    authors: ["Zhang San"],
    keywords: [],
    file_path: "/papers/one",
    next_action: "Run robustness checks",
    notes: "",
    research_area: "Development economics",
    status: "",
    priority: "High",
    target_journal: "经济研究",
    stages: [
      { name: "Intro", done: true, children: [] },
      { name: "Empirics", done: false, children: [] },
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
    last_updated: "2026-09-01",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

function props() {
  return {
    onCreate: vi.fn(),
    onUpdate: vi.fn(),
    onDelete: vi.fn(),
    onReorder: vi.fn(),
    onMove: vi.fn(),
  }
}

describe("ResearchPage", () => {
  it("renders the sequential code and title as table cells", () => {
    render(<ResearchPage papers={[paper()]} {...props()} />)
    expect(document.querySelector(".wb-table thead")).not.toBeNull()
    expect(screen.getByText("R001")).toBeInTheDocument()
    expect(screen.getByText("Working Paper One")).toBeInTheDocument()
  })

  it("filters by whitelisted text fields", () => {
    render(
      <ResearchPage
        papers={[paper(), paper({ id: "r-2", title: "Behavioural Contracts", research_area: "" })]}
        {...props()}
      />,
    )
    const search = screen.getByPlaceholderText(/Search title/i)
    fireEvent.change(search, { target: { value: "development" } })
    expect(screen.getByText("Working Paper One")).toBeInTheDocument()
    expect(screen.queryByText("Behavioural Contracts")).not.toBeInTheDocument()
  })

  it("asks the parent to move a paper through the flow button", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper()]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: /Move to submissions/ }))
    expect(handlers.onMove).toHaveBeenCalledWith("r-1")
  })

  it("exposes an aria-labelled delete button", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper()]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Delete: Working Paper One" }))
    expect(handlers.onDelete).toHaveBeenCalledWith("r-1")
  })

  it("keeps stage data visible while editing stages", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper()]} {...handlers} />)
    // The stage tree lives in the detail row, and the stage cell opens it.
    fireEvent.click(screen.getByRole("button", { name: "Show or hide research stages" }))
    expect(screen.getByText("Intro")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Toggle completion: Intro" }))
    // Toggling an already-done leaf flips it to undone via onUpdate(stages).
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", {
      stages: [
        { name: "Intro", done: false, children: [] },
        { name: "Empirics", done: false, children: [] },
      ],
    })
  })

  it("drops the separate expand control from the actions column", () => {
    render(<ResearchPage papers={[paper()]} {...props()} />)
    expect(screen.queryByRole("button", { name: "Expand details" })).not.toBeInTheDocument()
  })

  it("reports stage progress as a segmented rail in the stage column", () => {
    render(<ResearchPage papers={[paper()]} {...props()} />)
    const rail = screen.getByRole("progressbar")
    expect(rail).toHaveAttribute("aria-valuenow", "50")
    expect(screen.getByText("50%")).toBeInTheDocument()
    // One segment per top-level stage, and the cursor sits on the unfinished one.
    expect(rail.children).toHaveLength(2)
    expect(rail.children[1]).toHaveClass("wb-stage-tick--current")
    expect(rail.children[0]).not.toHaveClass("wb-stage-tick--current")
    // The label names where the paper actually is.
    expect(screen.getByText("Empirics")).toBeInTheDocument()
  })

  it("shows the standard pipeline for a paper without stages and writes on first tick", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ stages: [] })]} {...handlers} />)
    const rail = screen.getByRole("progressbar")
    expect(rail).toHaveAttribute("aria-valuenow", "0")
    fireEvent.click(screen.getByRole("button", { name: "Show or hide research stages" }))
    expect(screen.getByText("Data cleaning")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Toggle completion: Data cleaning" }))
    const patch = handlers.onUpdate.mock.calls[0]![1] as { stages: ResearchPaper["stages"] }
    expect(patch.stages).toHaveLength(STAGE_TEMPLATE_KEYS.length)
    expect(patch.stages.map((stage) => ({ name: stage.name, done: stage.done }))).toContainEqual({
      name: "Data cleaning",
      done: true,
    })
  })

  it("disables the add-paper button while a create is in flight", () => {
    render(<ResearchPage papers={[paper()]} {...props()} creating />)
    expect(screen.getByRole("button", { name: /Add paper/ })).toBeDisabled()
  })

  it("adds a notes column that previews the note or offers a pen when empty", () => {
    render(<ResearchPage papers={[paper()]} {...props()} />)
    expect(screen.getByRole("columnheader", { name: "Notes" })).toBeInTheDocument()
    const cell = screen.getByRole("button", { name: "Edit notes: Working Paper One" })
    expect(cell).toHaveClass("wb-notes-cell")
    expect(cell).toHaveTextContent("✎")
  })

  it("opens the detail row from the notes cell and focuses the notes field", () => {
    const notes = "CSMAR sample runs to 2024; robustness needs an IV"
    render(<ResearchPage papers={[paper({ notes })]} {...props()} />)
    const cell = screen.getByRole("button", { name: "Edit notes: Working Paper One" })
    // The cell is a preview, not an editor: the full note lives in the detail row.
    expect(cell).toHaveTextContent(notes)
    expect(document.querySelector("tr.wb-row-detail")).toBeNull()
    fireEvent.click(cell)
    const field = document.querySelector<HTMLElement>("[data-notes-field] .wb-editable")
    expect(field).not.toBeNull()
    expect(field).toHaveTextContent(notes)
    expect(field).toHaveFocus()
  })

  it("commits a multiline note edit through onUpdate", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ notes: "Draft note" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Edit notes: Working Paper One" }))
    fireEvent.doubleClick(document.querySelector("[data-notes-field] .wb-editable")!)
    // 工具栏的搜索框也是 textbox，所以这一格要从详情行里取，不能用 getByRole。
    const area = document.querySelector<HTMLTextAreaElement>("[data-notes-field] textarea")!
    expect(area).toHaveClass("wb-inline-input")
    fireEvent.change(area, { target: { value: "Reviewer 2 asked for a placebo test" } })
    // Multiline fields commit on blur, never on Enter (Enter writes a new line).
    fireEvent.keyDown(area, { key: "Enter" })
    expect(handlers.onUpdate).not.toHaveBeenCalled()
    fireEvent.blur(area)
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", {
      notes: "Reviewer 2 asked for a placebo test",
    })
  })
})

afterEach(() => cleanup())
