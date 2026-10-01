import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchPaper, SubmissionRecord } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { SubmittedPage } from "./SubmittedPage"
import { formatDeadline } from "./utils"

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
    tag_ids: [],
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
    onMoveBack: vi.fn(),
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

  it("picks the deadline from a calendar cell and still flags how urgent it is", () => {
    const handlers = props()
    const soon = new Date()
    soon.setDate(soon.getDate() + 3)
    const soonISO = formatDeadline(soon)
    const todayISO = formatDeadline(new Date())
    render(<SubmittedPage papers={[paper({ deadline: soonISO })]} {...handlers} />)
    expect(document.querySelector(".wb-deadline--soon")).not.toBeNull()

    // 手打日期的 input 已经换成整颗日历按钮。
    const trigger = screen.getByRole("button", { name: "Deadline" })
    expect(trigger).toHaveTextContent(soonISO)
    expect(document.querySelector(".wb-col-date input")).toBeNull()

    // 静置时浮层不在表格里：它是点开才长出来的。
    expect(screen.queryByRole("dialog")).toBeNull()
    fireEvent.click(trigger)
    const dialog = screen.getByRole("dialog", { name: "Choose a date" })
    fireEvent.click(within(dialog).getByRole("gridcell", { name: todayISO }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", { deadline: todayISO })
  })

  it("clears a deadline from the calendar popover", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper({ deadline: "2026-12-31" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Deadline" }))
    fireEvent.click(screen.getByRole("button", { name: "Clear date" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", { deadline: "" })
  })

  it("chooses the submission count from a dropdown instead of typing it", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper({ submission_count: 1 })]} {...handlers} />)
    const trigger = screen.getByRole("button", { name: "Submission count" })
    expect(trigger).toHaveTextContent("1")
    expect(document.querySelector(".wb-col-count input")).toBeNull()
    fireEvent.click(trigger)
    fireEvent.click(screen.getByRole("option", { name: "4" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", { submission_count: 4 })
  })

  it("reads an unset count as not filled and never offers a negative one", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper({ submission_count: 3 })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Submission count" }))
    const options = screen.getAllByRole("option")
    // 后端对负数报 400：能选出来的只有"未填"和 0…12，手打那条路已经没了。
    expect(options[0]).toHaveTextContent("Not filled")
    expect(options.some((option) => Number(option.textContent) < 0)).toBe(false)
    fireEvent.click(screen.getByRole("option", { name: "Not filled" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", { submission_count: 0 })
  })

  it("labels an unset count with the not-filled option", () => {
    render(<SubmittedPage papers={[paper({ submission_count: 0 })]} {...props()} />)
    expect(screen.getByRole("button", { name: "Submission count" })).toHaveTextContent("Not filled")
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

  it("moves back to research through the parent callback", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper()]} {...handlers} />)
    // The move-back button sits before "Move to publications" in the actions cell.
    fireEvent.click(screen.getByRole("button", { name: /Move back to working/ }))
    expect(handlers.onMoveBack).toHaveBeenCalledWith("s-1")
    expect(handlers.onMove).not.toHaveBeenCalled()
  })

  it("adds a notes column whose cell is an in-place editor", () => {
    const notes = "AEJR desk-rejected; next try JBF with the revised intro"
    render(<SubmittedPage papers={[paper({ notes })]} {...props()} />)
    expect(screen.getByRole("columnheader", { name: "Notes" })).toBeInTheDocument()
    const cell = screen.getByRole("textbox", { name: "Edit notes: Submitted Paper" })
    expect(cell).toHaveValue(notes)
  })

  it("commits an in-cell note edit through onUpdate", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper({ notes: "Referee 2 due 12 Oct" })]} {...handlers} />)
    const cell = screen.getByRole<HTMLTextAreaElement>("textbox", {
      name: "Edit notes: Submitted Paper",
    })
    fireEvent.change(cell, { target: { value: "R&R resubmission window closes 2026-10-12" } })
    fireEvent.blur(cell)
    expect(handlers.onUpdate).toHaveBeenCalledWith("s-1", {
      notes: "R&R resubmission window closes 2026-10-12",
    })
  })

  it("commits a multiline note edit through onUpdate", () => {
    const handlers = props()
    render(<SubmittedPage papers={[paper({ notes: "Referee 2 due 12 Oct" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Expand details" }))
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
