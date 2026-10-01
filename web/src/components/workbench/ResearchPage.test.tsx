import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchPaper, ResearchTag } from "../../api/types"
import { STAGE_TEMPLATE_KEYS } from "../../lib/research"
import { useReaderStore } from "../../store/reader"
import { ResearchPage } from "./ResearchPage"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

// 调色板顺序 = 服务端 position 升序。前三个是迁移播种的旧优先级名，
// 显示时走 priorityHigh/Medium/Average 的既有 i18n 键；其余原样渲染。
const TAGS: ResearchTag[] = [
  { id: "t-high", name: "High", position: 0 },
  { id: "t-medium", name: "Medium", position: 1 },
  { id: "t-average", name: "Average", position: 2 },
  { id: "t-field", name: "Fieldwork", position: 3 },
]

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
    tag_id: "t-high",
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
    tags: TAGS,
    onCreate: vi.fn(),
    onUpdate: vi.fn(),
    onDelete: vi.fn(),
    onReorder: vi.fn(),
    onMove: vi.fn(),
    // Workbench owns the POST (mocked client there); the page contract is
    // "resolve the created tag, then assign it".
    onCreateTag: vi.fn((name: string) =>
      Promise.resolve({ id: `t-new-${name}`, name, position: TAGS.length }),
    ),
  }
}

function rowTitles(): (string | null)[] {
  return Array.from(document.querySelectorAll("tbody tr .wb-cell-title .wb-editable")).map(
    (node) => node.textContent,
  )
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

  it("shows the tag column header and drops the old header sort button", () => {
    render(<ResearchPage papers={[paper()]} {...props()} />)
    const header = screen.getByRole("columnheader", { name: "Tag" })
    // 三态列头排序已删：没有排序按钮、没有 aria-sort、没有 sorted 表类。
    expect(header).not.toHaveAttribute("aria-sort")
    expect(document.querySelector(".wb-th-sort")).toBeNull()
    expect(document.querySelector(".wb-table--sorted")).toBeNull()
    expect(screen.queryByRole("button", { name: "Sort by priority" })).not.toBeInTheDocument()
    // 排序搬进工具栏，成为筛选之后、新增论文之前的独立控件。
    expect(screen.getByRole("button", { name: "Sort" })).toBeInTheDocument()
  })

  it("reorders rows per sort-menu option and restores manual order", () => {
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Manual First", tag_id: "t-medium", last_updated: "2026-09-01" }),
          paper({ id: "r-2", title: "Newest Second", tag_id: "t-high", last_updated: "2026-09-10" }),
          paper({ id: "r-3", title: "Untagged Third", tag_id: "", last_updated: "" }),
          paper({ id: "r-4", title: "Tagged Fourth", tag_id: "t-field", last_updated: "2026-09-05" }),
        ]}
        {...props()}
      />,
    )
    const pickSort = (option: string) => {
      fireEvent.click(screen.getByRole("button", { name: "Sort" }))
      fireEvent.click(screen.getByRole("option", { name: option }))
    }
    // 默认：手动顺序（papers prop 顺序）。
    expect(rowTitles()).toEqual(["Manual First", "Newest Second", "Untagged Third", "Tagged Fourth"])
    // 最近更新 · 新→旧：ISO 时间字典序倒排，空时间戳最后。
    pickSort("Recently updated · newest first")
    expect(rowTitles()).toEqual(["Newest Second", "Tagged Fourth", "Manual First", "Untagged Third"])
    // 最近更新 · 旧→新。
    pickSort("Recently updated · oldest first")
    expect(rowTitles()).toEqual(["Untagged Third", "Manual First", "Tagged Fourth", "Newest Second"])
    // 标签顺序：调色板 position 升序（High < Medium < Fieldwork），未挂标签恒最后。
    pickSort("Tag order")
    expect(rowTitles()).toEqual(["Newest Second", "Manual First", "Tagged Fourth", "Untagged Third"])
    // 手动顺序：还原 prop 顺序。
    pickSort("Manual order")
    expect(rowTitles()).toEqual(["Manual First", "Newest Second", "Untagged Third", "Tagged Fourth"])
  })

  it("disables row dragging while a non-manual sort is active", () => {
    const handlers = props()
    render(
      <ResearchPage
        papers={[paper({ tag_id: "t-high" }), paper({ id: "r-2", title: "Second", tag_id: "t-average" })]}
        {...handlers}
      />,
    )
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(2)
    fireEvent.click(screen.getByRole("button", { name: "Sort" }))
    fireEvent.click(screen.getByRole("option", { name: "Tag order" }))
    // 把手缺席：Row 只在按住把手时才武装拖拽，所以排序视图里拖不动。
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(0)
    // 即便有游离的 drop 事件落到行上，也绝不能把排序视图写回成手工顺序。
    const rows = document.querySelectorAll("tr.wb-row")
    fireEvent.drop(rows[1]!, {
      dataTransfer: { getData: () => "r-1", types: ["text/wb-row"] },
    })
    expect(handlers.onReorder).not.toHaveBeenCalled()
    // 回到手动顺序后把手恢复。
    fireEvent.click(screen.getByRole("button", { name: "Sort" }))
    fireEvent.click(screen.getByRole("option", { name: "Manual order" }))
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(2)
  })

  it("renders the paper's tag as a pill and localizes the seeded legacy names", () => {
    // 迁移播种的 High/Medium/Average 走既有 i18n 键（zh 下是"高优先级"…），
    // 自定义名字原样渲染。
    useReaderStore.setState({ locale: "zh-CN" })
    render(
      <ResearchPage
        papers={[paper({ tag_id: "t-high" }), paper({ id: "r-2", title: "Second", tag_id: "t-field" })]}
        {...props()}
      />,
    )
    expect(screen.getByRole("button", { name: "标签: 高优先级" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "标签: Fieldwork" })).toBeInTheDocument()
    const tinted = document.querySelector(".wb-tag-pill.wb-badge--red")
    expect(tinted).not.toBeNull()
    // 第四档起循环其余既有 wb-badge 淡底（下标 3 → green）。
    expect(document.querySelector(".wb-tag-pill.wb-badge--green")).not.toBeNull()
  })

  it("shows a quiet placeholder pill for an untagged paper", () => {
    render(<ResearchPage papers={[paper({ tag_id: "" })]} {...props()} />)
    const pill = screen.getByRole("button", { name: "Tag: No tag" })
    expect(pill).toHaveClass("wb-tag-pill--empty")
    expect(pill).toHaveTextContent("No tag")
  })

  it("assigns a tag from the pill menu via onUpdate tag_id", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_id: "" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Tag: No tag" }))
    // 菜单：无标签 + 调色板顺序的每个标签 + 新建入口。
    expect(screen.getByRole("menuitem", { name: /No tag/ })).toBeInTheDocument()
    const items = screen.getAllByRole("menuitem").map((node) => node.textContent)
    expect(items).toEqual(["No tag✓", "High", "Medium", "Average", "Fieldwork", "＋New tag"])
    fireEvent.click(screen.getByRole("menuitem", { name: "Fieldwork" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_id: "t-field" })
  })

  it("clears the tag by picking the untagged entry", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_id: "t-high" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Tag: High" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "No tag" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_id: "" })
  })

  it("creates a tag inline then assigns it to the paper", async () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_id: "" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Tag: No tag" }))
    fireEvent.click(screen.getByRole("menuitem", { name: /New tag/ }))
    const input = screen.getByRole("textbox", { name: "New tag" })
    fireEvent.change(input, { target: { value: "Placebo" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(handlers.onCreateTag).toHaveBeenCalledWith("Placebo"))
    await waitFor(() =>
      expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_id: "t-new-Placebo" }),
    )
  })

  it("cancels the inline tag creation on Escape without assigning", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_id: "" })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Tag: No tag" }))
    fireEvent.click(screen.getByRole("menuitem", { name: /New tag/ }))
    const input = screen.getByRole("textbox", { name: "New tag" })
    fireEvent.change(input, { target: { value: "Abandoned" } })
    fireEvent.keyDown(input, { key: "Escape" })
    // Esc 退回菜单（不是关窗），也没有任何写回。
    expect(screen.queryByRole("textbox", { name: "New tag" })).not.toBeInTheDocument()
    expect(screen.getByRole("menuitem", { name: /New tag/ })).toBeInTheDocument()
    expect(handlers.onCreateTag).not.toHaveBeenCalled()
    expect(handlers.onUpdate).not.toHaveBeenCalled()
  })

  it("narrows the table by tag through the toolbar filter", () => {
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Field Study" , tag_id: "t-field" }),
          paper({ id: "r-2", title: "High Study", tag_id: "t-high" }),
          paper({ id: "r-3", title: "Untagged Study", tag_id: "" }),
        ]}
        {...props()}
      />,
    )
    // 全部标签（默认）→ 三行都在。
    const filter = screen.getByRole("button", { name: "Tag" })
    expect(filter).toHaveTextContent("All tags")
    expect(rowTitles()).toEqual(["Field Study", "High Study", "Untagged Study"])
    fireEvent.click(filter)
    fireEvent.click(screen.getByRole("option", { name: "Fieldwork" }))
    expect(rowTitles()).toEqual(["Field Study"])
    expect(screen.getByText("1/3 results")).toBeInTheDocument()
  })
})

afterEach(() => cleanup())
