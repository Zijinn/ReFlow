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

  it("adds a notes column whose cell is an inline editor, not an expand entry", () => {
    render(<ResearchPage papers={[paper()]} {...props()} />)
    expect(screen.getByRole("columnheader", { name: "Notes" })).toBeInTheDocument()
    // 旧的"预览按钮 + ✎"入口已删：格子里是 textarea，点它不展开详情行。
    expect(document.querySelector(".wb-notes-cell")).toBeNull()
    expect(screen.getByRole("textbox", { name: "Edit notes: Working Paper One" }).tagName).toBe(
      "TEXTAREA",
    )
  })

  it("edits notes inline and commits on blur without opening the detail row", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ notes: "Draft note" })]} {...handlers} />)
    const area = screen.getByRole("textbox", { name: "Edit notes: Working Paper One" })
    expect(area).toHaveClass("wb-notes-input")
    expect(area).toHaveValue("Draft note")
    fireEvent.change(area, { target: { value: "Reviewer 2 asked for a placebo test" } })
    fireEvent.blur(area)
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", {
      notes: "Reviewer 2 asked for a placebo test",
    })
    expect(document.querySelector("tr.wb-row-detail")).toBeNull()
  })

  it("commits the inline note on Enter and treats Shift+Enter as a newline", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper()]} {...handlers} />)
    const area = screen.getByRole("textbox", { name: "Edit notes: Working Paper One" })
    // 空格子显示占位提示。
    expect(area).toHaveAttribute("placeholder", "Edit notes")
    area.focus()
    fireEvent.change(area, { target: { value: "line one\nline two" } })
    fireEvent.keyDown(area, { key: "Enter", shiftKey: true })
    expect(handlers.onUpdate).not.toHaveBeenCalled()
    fireEvent.keyDown(area, { key: "Enter" })
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { notes: "line one\nline two" })
  })

  it("keeps the detail-row notes editor working", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ notes: "Draft note" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Show or hide research stages" }))
    const editable = document.querySelector(".wb-detail-notes .wb-editable")!
    expect(editable).toHaveTextContent("Draft note")
    fireEvent.doubleClick(editable)
    const area = document.querySelector<HTMLTextAreaElement>(".wb-detail-notes textarea")!
    expect(area).toHaveClass("wb-inline-input")
    fireEvent.change(area, { target: { value: "Updated from the detail row" } })
    fireEvent.blur(area)
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { notes: "Updated from the detail row" })
  })

  it("cycles the priority sort off → high-first → low-first → off and reorders rows", () => {
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Medium One", priority: "Medium" }),
          paper({ id: "r-2", title: "High Two", priority: "High" }),
          paper({ id: "r-3", title: "Blank Three", priority: "" }),
          paper({ id: "r-4", title: "Average Four", priority: "Average" }),
        ]}
        {...props()}
      />,
    )
    const header = screen.getByRole("columnheader", { name: "Priority" })
    const sortButton = screen.getByRole("button", { name: "Sort by priority" })
    const order = () =>
      Array.from(document.querySelectorAll("tbody tr .wb-cell-title .wb-editable")).map(
        (node) => node.textContent,
      )
    // 未排序：保持手工顺序（papers prop 顺序）。
    expect(header).toHaveAttribute("aria-sort", "none")
    expect(order()).toEqual(["Medium One", "High Two", "Blank Three", "Average Four"])
    // 第一击：高到低，空优先级恒在最后。
    fireEvent.click(sortButton)
    expect(header).toHaveAttribute("aria-sort", "descending")
    expect(order()).toEqual(["High Two", "Medium One", "Average Four", "Blank Three"])
    // 第二击：低到高，空优先级仍在最后（不是"最低"）。
    fireEvent.click(sortButton)
    expect(header).toHaveAttribute("aria-sort", "ascending")
    expect(order()).toEqual(["Average Four", "Medium One", "High Two", "Blank Three"])
    // 第三击：回到未排序的手工顺序。
    fireEvent.click(sortButton)
    expect(header).toHaveAttribute("aria-sort", "none")
    expect(order()).toEqual(["Medium One", "High Two", "Blank Three", "Average Four"])
  })

  it("disables row dragging while a priority sort is active", () => {
    const handlers = props()
    render(
      <ResearchPage
        papers={[paper({ priority: "High" }), paper({ id: "r-2", title: "Second", priority: "Average" })]}
        {...handlers}
      />,
    )
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(2)
    fireEvent.click(screen.getByRole("button", { name: "Sort by priority" }))
    expect(document.querySelector(".wb-table--sorted")).not.toBeNull()
    // 把手缺席：Row 只在按住把手时才武装拖拽，所以排序视图里拖不动。
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(0)
    // 即便有游离的 drop 事件落到行上，也绝不能把排序视图写回成手工顺序。
    const rows = document.querySelectorAll("tr.wb-row")
    fireEvent.drop(rows[1]!, {
      dataTransfer: { getData: () => "r-1", types: ["text/wb-row"] },
    })
    expect(handlers.onReorder).not.toHaveBeenCalled()
    // 循环回未排序后把手恢复。
    fireEvent.click(screen.getByRole("button", { name: "Sort by priority" }))
    fireEvent.click(screen.getByRole("button", { name: "Sort by priority" }))
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(2)
    expect(document.querySelector(".wb-table--sorted")).toBeNull()
  })
})

afterEach(() => cleanup())
