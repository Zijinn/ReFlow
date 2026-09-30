import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchPaper, SubmissionRecord } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { SubmittedPage } from "./SubmittedPage"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "s-1",
    kind: "submitted",
    position: 0,
    title: "Submitted Paper",
    authors: ["Li Si"],
    keywords: [],
    file_path: "",
    next_action: "",
    notes: "",
    research_area: "",
    status: "under_review",
    priority: "",
    target_journal: "",
    stages: [],
    current_journal: "Journal of Development Economics",
    submission_date: "2026-08-01",
    manuscript_id: "JDE-26-0042",
    submission_count: 1,
    target_level: "",
    editor: "",
    deadline: "",
    history: [{ journal: "Review of Finance", date: "2026-01-15", status: "rejected" }],
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
    last_updated: "",
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

// Submission history lives in the expandable detail row.
function expandRow() {
  fireEvent.click(screen.getByRole("button", { name: "Expand details" }))
}

describe("SubmittedPage", () => {
  it("renders history rows with translated status badges and i18n titles", () => {
    render(<SubmittedPage papers={[paper()]} {...props()} />)
    expect(screen.getByText("S001")).toBeInTheDocument()
    expect(screen.getByText("Submitted Paper")).toBeInTheDocument()
    expandRow()
    expect(screen.getByText("Review of Finance")).toBeInTheDocument()
    // Badge (plus the status <option>s) all render the translated label.
    const badges = document.querySelectorAll(".wb-badge--red")
    expect(badges).toHaveLength(1)
    expect(badges[0]).toHaveTextContent("Rejected")
    expect(screen.getByRole("button", { name: "Add record: Submitted Paper" })).toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "Delete record: Review of Finance" }),
    ).toBeInTheDocument()
  })

  it("adds a history record through an inline form, not window.prompt", () => {
    const promptSpy = vi.spyOn(window, "prompt").mockImplementation(() => null)
    const handlers = props()
    render(<SubmittedPage papers={[paper()]} {...handlers} />)
    expandRow()
    fireEvent.click(screen.getByRole("button", { name: "Add record: Submitted Paper" }))
    const journal = screen.getByLabelText("Enter the journal name:")
    fireEvent.change(journal, { target: { value: "Review of Economic Studies" } })
    fireEvent.keyDown(journal, { key: "Enter" })
    expect(promptSpy).not.toHaveBeenCalled()
    expect(handlers.onUpdate).toHaveBeenCalledTimes(1)
    const history = (handlers.onUpdate.mock.calls[0]![1] as { history: SubmissionRecord[] }).history
    expect(history).toHaveLength(2)
    expect(history[0]).toEqual({
      journal: "Review of Finance",
      date: "2026-01-15",
      status: "rejected",
    })
    expect(history[1]!.journal).toBe("Review of Economic Studies")
    expect(history[1]!.date).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(history[1]!.status).toBe("submitted")
  })

  it("ignores a record without a journal name", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper()]} {...handlers} />)
    expandRow()
    fireEvent.click(screen.getByRole("button", { name: "Add record: Submitted Paper" }))
    fireEvent.keyDown(screen.getByLabelText("Enter the journal name:"), { key: "Enter" })
    expect(handlers.onUpdate).not.toHaveBeenCalled()
  })

  it("deletes a history record via onUpdate with the row removed", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper()]} {...handlers} />)
    expandRow()
    fireEvent.click(screen.getByRole("button", { name: "Delete record: Review of Finance" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", { history: [] })
  })

  it("normalizes a deadline entry and flags how urgent it is", () => {
    const handlers = props()
    const soon = new Date()
    soon.setDate(soon.getDate() + 3)
    const soonISO = soon.toISOString().slice(0, 10)
    render(<SubmittedPage papers={[paper({ deadline: soonISO })]} {...handlers} />)
    expect(document.querySelector(".wb-deadline--soon")).not.toBeNull()
    expect(screen.getByText(soonISO)).toBeInTheDocument()

    fireEvent.doubleClick(screen.getByText(soonISO))
    const input = screen.getByDisplayValue(soonISO)
    fireEvent.change(input, { target: { value: "2026/12/31" } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", { deadline: "2026-12-31" })
  })

  it("opens and highlights the row the calendar jumped to", () => {
    render(
      <SubmittedPage
        papers={[paper()]}
        focusPaperID="s-1"
        onFocusConsumed={vi.fn()}
        {...props()}
      />,
    )
    // The detail row is already open, so the toggle reads "Collapse details".
    expect(screen.getByRole("button", { name: "Collapse details" })).toBeInTheDocument()
    expect(screen.getByText("Review of Finance")).toBeInTheDocument()
    expect(document.querySelector("tr.wb-row--flash")).not.toBeNull()
  })

  it("moves to published through the parent callback", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper()]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: /Move to publications/ }))
    expect(handlers.onMove).toHaveBeenCalledWith("s-1")
  })

  it("adds a notes column that previews the note or offers a pen when empty", () => {
    render(<SubmittedPage papers={[paper()]} {...props()} />)
    expect(screen.getByRole("columnheader", { name: "Notes" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Edit notes: Submitted Paper" })).toHaveTextContent(
      "✎",
    )
  })

  it("opens the detail row from the notes cell and focuses the notes field", () => {
    const notes = "AEJR desk-rejected; next try JBF with the revised intro"
    render(<SubmittedPage papers={[paper({ notes })]} {...props()} />)
    const cell = screen.getByRole("button", { name: "Edit notes: Submitted Paper" })
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
    render(<SubmittedPage papers={[paper({ notes: "Referee 2 due 12 Oct" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Edit notes: Submitted Paper" }))
    fireEvent.doubleClick(document.querySelector("[data-notes-field] .wb-editable")!)
    // 工具栏的搜索框也是 textbox，所以备注那一格从详情行里取。
    const area = document.querySelector<HTMLTextAreaElement>("[data-notes-field] textarea")!
    expect(area).toHaveClass("wb-inline-input")
    fireEvent.change(area, { target: { value: "R&R resubmission window closes 2026-10-12" } })
    fireEvent.blur(area)
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", {
      notes: "R&R resubmission window closes 2026-10-12",
    })
  })
})

afterEach(() => cleanup())
