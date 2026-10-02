import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
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
// 全部 color: ""（「自动」）：这四枚的存在是为了证明"没有自选色时按下标取色"，
// 自选色的覆盖另起一份调色板（COLOURED_TAGS）测，两边不会互相顶着改。
const TAGS: ResearchTag[] = [
  { id: "t-high", name: "High", position: 0, color: "" },
  { id: "t-medium", name: "Medium", position: 1, color: "" },
  { id: "t-average", name: "Average", position: 2, color: "" },
  { id: "t-field", name: "Fieldwork", position: 3, color: "" },
]

// 带自选色的一版：violet 顶掉下标 0 的红，teal 顶掉下标 2 的灰；
// "chartreuse" 不在八档色板里（脏值/后端将来加色而前端还没跟上），按未知处理回落到下标。
const COLOURED_TAGS: ResearchTag[] = [
  { id: "t-high", name: "High", position: 0, color: "violet" },
  { id: "t-medium", name: "Medium", position: 1, color: "" },
  { id: "t-field", name: "Fieldwork", position: 2, color: "teal" },
  { id: "t-odd", name: "Odd", position: 3, color: "chartreuse" },
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
    tag_ids: ["t-high"],
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
    // "resolve the created tag, then assign it". 行内新建不传颜色，所以后端回来的
    // 永远是 color: ""（自动）。
    onCreateTag: vi.fn((name: string) =>
      Promise.resolve({ id: `t-new-${name}`, name, position: TAGS.length, color: "" }),
    ),
  }
}

function rowTitles(): (string | null)[] {
  return Array.from(document.querySelectorAll("tbody tr .wb-cell-title .wb-editable")).map(
    (node) => node.textContent,
  )
}

// 相对今天若干天的那条本地日期串：紧急度与"最近截止"排序都按当天算，
// 断言里写死某个日历日会让测试在跨月/跨年那天自己变红。
function isoOffset(days: number): string {
  const base = new Date()
  const date = new Date(base.getFullYear(), base.getMonth(), base.getDate())
  date.setDate(date.getDate() + days)
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

// 截止日期那一格（列头与格子共用 wb-col-deadline 这个钩子）。
function deadlineCell(title: string): HTMLElement {
  const row = rowOf(title)
  return row.querySelector<HTMLElement>("td.wb-col-deadline")!
}

function rowOf(title: string): HTMLElement {
  const row = Array.from(document.querySelectorAll<HTMLElement>("tbody tr.wb-row")).find((node) =>
    node.querySelector(".wb-cell-title")?.textContent?.includes(title),
  )
  if (!row) throw new Error(`no row for ${title}`)
  return row
}

// 标签格不再有整体触发键（每枚药丸自带 ✕，新增走「＋」），所以按行取格子、
// 再用 within 在格子内部找控件——多行渲染时「添加标签」同名。
function tagCell(title: string): HTMLElement {
  return rowOf(title).querySelector<HTMLElement>(".wb-tag-cell")!
}

function chipLabels(cell: HTMLElement): (string | null)[] {
  return Array.from(cell.querySelectorAll(".wb-tag-list .wb-tag-chip-label")).map(
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
          paper({ id: "r-1", title: "Manual First", tag_ids: ["t-medium"], last_updated: "2026-09-01" }),
          paper({ id: "r-2", title: "Newest Second", tag_ids: ["t-high"], last_updated: "2026-09-10" }),
          paper({ id: "r-3", title: "Untagged Third", tag_ids: [], last_updated: "" }),
          paper({ id: "r-4", title: "Tagged Fourth", tag_ids: ["t-field"], last_updated: "2026-09-05" }),
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
    // 更新日期 · 新→旧：ISO 时间字典序倒排，空时间戳最后。
    pickSort("Last updated · newest first")
    expect(rowTitles()).toEqual(["Newest Second", "Tagged Fourth", "Manual First", "Untagged Third"])
    // 更新日期 · 旧→新。
    pickSort("Last updated · oldest first")
    expect(rowTitles()).toEqual(["Untagged Third", "Manual First", "Tagged Fourth", "Newest Second"])
    // 标签顺序：调色板 position 升序（High < Medium < Fieldwork），未挂标签恒最后。
    pickSort("Tag order")
    expect(rowTitles()).toEqual(["Newest Second", "Manual First", "Tagged Fourth", "Untagged Third"])
    // 手动顺序：还原 prop 顺序。
    pickSort("Manual order")
    expect(rowTitles()).toEqual(["Manual First", "Newest Second", "Untagged Third", "Tagged Fourth"])
  })

  it("sorts a multi-tagged paper by its best-ranked tag", () => {
    // 一篇挂好几个标签时按调色板里最靠前的那个比：Fieldwork(3)+High(0) 走在
    // Medium(1) 前面；不在调色板里的 id 没有档位，和未挂标签一起垫底。
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Both", tag_ids: ["t-field", "t-high"] }),
          paper({ id: "r-2", title: "Medium only", tag_ids: ["t-medium"] }),
          paper({ id: "r-3", title: "Ghost only", tag_ids: ["t-ghost"] }),
          paper({ id: "r-4", title: "Untagged", tag_ids: [] }),
        ]}
        {...props()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Sort" }))
    fireEvent.click(screen.getByRole("option", { name: "Tag order" }))
    expect(rowTitles()).toEqual(["Both", "Medium only", "Ghost only", "Untagged"])
  })

  it("disables row dragging while a non-manual sort is active", () => {
    const handlers = props()
    render(
      <ResearchPage
        papers={[paper({ tag_ids: ["t-high"] }), paper({ id: "r-2", title: "Second", tag_ids: ["t-average"] })]}
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

  it("renders one chip per assigned tag and localizes the seeded legacy names", () => {
    // 迁移播种的 High/Medium/Average 走既有 i18n 键（zh 下是"高优先级"…），
    // 自定义名字原样渲染。每枚药丸都是独立控件，移除键的 aria-label 自带名字。
    useReaderStore.setState({ locale: "zh-CN" })
    render(
      <ResearchPage
        papers={[
          paper({ tag_ids: ["t-high", "t-field"] }),
          paper({ id: "r-2", title: "Second", tag_ids: ["t-average"] }),
        ]}
        {...props()}
      />,
    )
    const cell = tagCell("Working Paper One")
    expect(chipLabels(cell)).toEqual(["高优先级", "Fieldwork"])
    expect(within(cell).getByRole("button", { name: "移除该标签: 高优先级" })).toBeInTheDocument()
    expect(within(cell).getByRole("button", { name: "移除该标签: Fieldwork" })).toBeInTheDocument()
    // 第二行独立成格：堆叠是每行自己的事，不共享状态。
    expect(chipLabels(tagCell("Second"))).toEqual(["低优先级"])
    // 色阶跟着调色板下标，落在每一枚药丸自己身上：0 → red，3 → green。
    expect(cell.querySelector(".wb-tag-chip.wb-badge--red")).not.toBeNull()
    expect(cell.querySelector(".wb-tag-chip.wb-badge--green")).not.toBeNull()
  })

  it("labels an id whose tag was deleted from the palette", () => {
    // 标签在偏好设置里被删掉后，论文的 tag_ids 里可能还残留它的 id：
    // 这一枚走中性灰 + 「标签已删除」，不能崩成 undefined 名字。
    render(<ResearchPage papers={[paper({ tag_ids: ["t-ghost"] })]} {...props()} />)
    const cell = tagCell("Working Paper One")
    const chip = cell.querySelector(".wb-tag-chip")!
    expect(chip).toHaveClass("wb-badge--gray")
    expect(chip.querySelector(".wb-tag-chip-label")!.textContent).toBe("Tag removed")
    expect(within(cell).getByRole("button", { name: "Remove this tag: Tag removed" })).toBeInTheDocument()
  })

  it("shows a quiet placeholder for an untagged paper", () => {
    render(<ResearchPage papers={[paper({ tag_ids: [] })]} {...props()} />)
    const cell = tagCell("Working Paper One")
    expect(cell.querySelector(".wb-tag-blank")!.textContent).toBe("No tag")
    // 空态没有药丸，也就没有 ✕；剩下的只有那枚常显的「＋」。
    expect(cell.querySelectorAll(".wb-tag-chip")).toHaveLength(0)
    expect(within(cell).getByRole("button", { name: "Add tag" })).toBeInTheDocument()
  })

  it("appends a toggled tag after the assigned ones and keeps the menu open", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: ["t-high"] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }))
    // 菜单：清空全部(1 个 menuitem) + 调色板顺序的每个标签(menuitemcheckbox) + 新建入口。
    expect(screen.getByRole("menuitem", { name: /Clear all tags/ })).toBeInTheDocument()
    expect(screen.getAllByRole("menuitemcheckbox").map((node) => node.textContent)).toEqual([
      "High✓",
      "Medium",
      // 存的是 "Average"，显示的是最低档那一档的本地名（en 现在是 "Low"）。
      "Low",
      "Fieldwork",
    ])
    expect(screen.getByRole("menuitem", { name: /New tag/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Fieldwork" }))
    // 指派顺序就是 tag_ids 顺序：新勾选的追加到末尾，已有的不动。
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_ids: ["t-high", "t-field"] })
    // 点选不关浮层——多选要能连着点。
    expect(screen.getByRole("menu")).toBeInTheDocument()
  })

  it("untoggles only the picked tag", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: ["t-high", "t-field"] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }))
    expect(
      screen.getByRole("menuitemcheckbox", { name: "Fieldwork" }),
    ).toHaveAttribute("aria-checked", "true")
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Fieldwork" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_ids: ["t-high"] })
  })

  it("removes one tag through its own chip", () => {
    // ✕ 直接长在药丸上：不用开菜单就能摘掉单独一枚，其余顺序不动。
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: ["t-high", "t-field"] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Remove this tag: Fieldwork" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_ids: ["t-high"] })
  })

  it("clears every tag at once", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: ["t-high", "t-medium"] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }))
    fireEvent.click(screen.getByRole("menuitem", { name: /Clear all tags/ }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_ids: [] })
    expect(screen.queryByRole("menu")).not.toBeInTheDocument()
  })

  it("toggles a tag from the keyboard", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: ["t-high"] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }))
    // 打开时高亮落在第一个已勾选的标签上，ArrowDown 一档到 Medium，Enter 提交。
    const menu = screen.getByRole("menu")
    fireEvent.keyDown(menu, { key: "ArrowDown" })
    fireEvent.keyDown(menu, { key: "Enter" })
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { tag_ids: ["t-high", "t-medium"] })
  })

  it("creates a tag inline then appends it to the paper", async () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: ["t-high"] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }))
    fireEvent.click(screen.getByRole("menuitem", { name: /New tag/ }))
    const input = screen.getByRole("textbox", { name: "New tag" })
    fireEvent.change(input, { target: { value: "Placebo" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(handlers.onCreateTag).toHaveBeenCalledWith("Placebo"))
    await waitFor(() =>
      expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", {
        tag_ids: ["t-high", "t-new-Placebo"],
      }),
    )
  })

  it("cancels the inline tag creation on Escape without assigning", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ tag_ids: [] })]} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }))
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

  it("renders every assigned tag instead of collapsing the overflow", () => {
    // 旧的「＋N」折叠整条删掉了：挂几枚就渲染几枚，行高由堆叠的 chip 自己撑开
    // （.wb-tag-cell 换行 + 表格默认行高），所以也不再需要量宽。
    render(
      <ResearchPage
        papers={[paper({ tag_ids: ["t-high", "t-medium", "t-average", "t-field"] })]}
        {...props()}
      />,
    )
    const cell = tagCell("Working Paper One")
    // 每枚药丸都是独立控件，移除键的 aria-label 自带名字。
    // en 的最低档文案已从 "Average" 改成 "Low"（后端现在播的就是 Low 这个名字）。
    expect(chipLabels(cell)).toEqual(["High", "Medium", "Low", "Fieldwork"])
    expect(cell.querySelectorAll(".wb-tag-chip")).toHaveLength(4)
    expect(document.querySelector(".wb-tag-more")).toBeNull()
    expect(document.querySelector(".wb-tag-measure")).toBeNull()
    // 每枚都可单独移除，完整名单不再只活在 title 里。
    expect(
      within(cell).getAllByRole("button", { name: /^Remove this tag:/ }),
    ).toHaveLength(4)
  })

  it("narrows the table by any one of a paper's tags", () => {
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Field Study", tag_ids: ["t-field", "t-high"] }),
          paper({ id: "r-2", title: "High Study", tag_ids: ["t-high"] }),
          paper({ id: "r-3", title: "Untagged Study", tag_ids: [] }),
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
    // 挂了多个标签的论文只要命中其中之一就留下。
    expect(rowTitles()).toEqual(["Field Study"])
    expect(screen.getByText("1/3 results")).toBeInTheDocument()
  })

  it("gives every named column header a width handle", () => {
    // 标题和操作列原本被排除在拖宽之外（弹性列 / 宽度由格子里的控件实测决定），
    // 现在也给了把手：唯一还没有把手的是拖拽柄列，它本身没有宽度语义。
    // 截止日期是第九颗带把手的列头（键位 "deadline"，不能借用 "date"——
    // 那个键位已经被"更新日期"的持久化宽度占了）。
    render(<ResearchPage papers={[paper()]} {...props()} />)
    const heads = Array.from(document.querySelectorAll("thead tr > th"))
    expect(heads).toHaveLength(10)
    expect(
      heads.filter((th) => !th.querySelector(".wb-col-resizer")).map((th) => th.className),
    ).toEqual(["wb-col-grip"])
    expect(screen.getByRole("separator", { name: "Drag to resize, double-click to reset: Title" })).toBeInTheDocument()
    expect(
      screen.getByRole("separator", {
        name: "Drag to resize, double-click to reset: Deadline",
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole("separator", { name: "Drag to resize, double-click to reset: Actions" }),
    ).toBeInTheDocument()
  })

  it("renders the deadline column left of 更新日期 and picks a date from the calendar", () => {
    const handlers = props()
    render(<ResearchPage papers={[paper({ deadline: "2026-12-24" })]} {...handlers} />)
    const head = screen.getByRole("columnheader", { name: "Deadline" })
    expect(head).toHaveClass("wb-col-deadline", "wb-col-date")
    // 两枚日期列归在一起：截止日期紧挨在"更新日期"左边。
    expect(head.nextElementSibling).toBe(
      screen.getByRole("columnheader", { name: "Last updated" }),
    )
    const cell = deadlineCell("Working Paper One")
    const trigger = within(cell).getByRole("button", { name: "Deadline" })
    expect(trigger).toHaveTextContent("2026-12-24")
    // 存的是 2026-12-24，所以浮层开在十二月：断言不需要知道今天是哪天。
    fireEvent.click(trigger)
    const pop = screen.getByRole("dialog", { name: "Choose a date" })
    fireEvent.click(within(pop).getByRole("gridcell", { name: "2026-12-03" }))
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { deadline: "2026-12-03" })
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    // 清除写回空串（后端把 "" 当"未设"）。
    fireEvent.click(trigger)
    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Choose a date" })).getByRole("button", {
        name: "Clear date",
      }),
    )
    expect(handlers.onUpdate).toHaveBeenCalledWith("r-1", { deadline: "" })
  })

  it("keeps an empty deadline cell clickable and muted", () => {
    render(<ResearchPage papers={[paper({ deadline: "" })]} {...props()} />)
    const cell = deadlineCell("Working Paper One")
    const trigger = within(cell).getByRole("button", { name: "Deadline" })
    // 空值是一枚 tertiary 墨的破折号，但整颗格子仍是按钮，点它就能选。
    expect(trigger.querySelector(".wb-ph")!.textContent).toBe("—")
    fireEvent.click(trigger)
    expect(screen.getByRole("dialog", { name: "Choose a date" })).toBeInTheDocument()
  })

  it("marks overdue and due-soon deadlines the way the submissions page does", () => {
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Late", deadline: isoOffset(-3) }),
          paper({ id: "r-2", title: "Soon", deadline: isoOffset(2) }),
          paper({ id: "r-3", title: "Plenty of time", deadline: isoOffset(30) }),
        ]}
        {...props()}
      />,
    )
    const hint = (title: string) => rowOf(title).querySelector(".wb-deadline-hint")
    expect(hint("Late")).toHaveClass("wb-deadline--overdue")
    expect(hint("Late")!.textContent).toBe("3 d overdue")
    expect(hint("Soon")).toHaveClass("wb-deadline--soon")
    expect(hint("Soon")!.textContent).toBe("2 d left")
    // 一周开外不打扰：这一档没有提示行。
    expect(hint("Plenty of time")).toBeNull()
  })

  it("sorts by nearest deadline with unset and unreadable dates last", () => {
    render(
      <ResearchPage
        papers={[
          paper({ id: "r-1", title: "Unset", deadline: "" }),
          paper({ id: "r-2", title: "Far", deadline: isoOffset(20) }),
          paper({ id: "r-3", title: "Overdue", deadline: isoOffset(-4) }),
          paper({ id: "r-4", title: "Tomorrow", deadline: isoOffset(1) }),
          // 日历读不出的自由文本与"没设"同档：不许它冒充一个很近的死线插到前面。
          paper({ id: "r-5", title: "Free text", deadline: "下周三是死线" }),
        ]}
        {...props()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Sort" }))
    fireEvent.click(screen.getByRole("option", { name: "Deadline · nearest first" }))
    expect(rowTitles()).toEqual(["Overdue", "Tomorrow", "Far", "Unset", "Free text"])
    // 排序档仍然是纯视图：把手在这档不渲染，手工顺序不会被写回。
    expect(document.querySelectorAll(".wb-drag-handle")).toHaveLength(0)
  })

  it("paints chips from the stored tag colour and falls back to the palette index", () => {
    render(
      <ResearchPage
        papers={[paper({ tag_ids: ["t-high", "t-medium", "t-field", "t-odd"] })]}
        {...props()}
        tags={COLOURED_TAGS}
      />,
    )
    const cell = tagCell("Working Paper One")
    const chips = Array.from(cell.querySelectorAll<HTMLElement>(".wb-tag-chip"))
    expect(chips).toHaveLength(4)
    // 下标 0 存了 violet：药丸与色点都读它，不再读下标推出来的 red。
    expect(chips[0]).toHaveClass("wb-badge--violet")
    expect(chips[0]!.querySelector(".wb-dot")).toHaveClass("wb-dot--violet")
    // color "" 仍是"按下标取色"：下标 1 → amber。
    expect(chips[1]).toHaveClass("wb-badge--amber")
    // 下标 2 存了 teal，顶掉本该是 gray 的那一档。
    expect(chips[2]).toHaveClass("wb-badge--teal")
    // 不在八档色板里的名字按未知处理：回落到下标 3 → green。
    expect(chips[3]).toHaveClass("wb-badge--green")
  })

  it("tints the row from its highest-priority tag only", () => {
    render(
      <ResearchPage
        papers={[
          // 挂了 Fieldwork(2, teal) 与 High(0, violet)：取胜的是调色板里最靠前的那枚。
          paper({ id: "r-1", title: "Both", tag_ids: ["t-field", "t-high"] }),
          paper({ id: "r-2", title: "Teal only", tag_ids: ["t-field"] }),
          // 只有自动色的那一枚：色相取自下标 1（amber），不是没有颜色。
          paper({ id: "r-3", title: "Auto only", tag_ids: ["t-medium"] }),
          paper({ id: "r-4", title: "Untagged", tag_ids: [] }),
          paper({ id: "r-5", title: "Ghost only", tag_ids: ["t-ghost"] }),
        ]}
        {...props()}
        tags={COLOURED_TAGS}
      />,
    )
    const hue = (title: string) => rowOf(title).style.getPropertyValue("--wb-row-hue")
    expect(rowOf("Both")).toHaveClass("wb-row--tinted")
    expect(hue("Both")).toBe("var(--wb-hue-violet)")
    expect(hue("Teal only")).toBe("var(--wb-hue-teal)")
    expect(hue("Auto only")).toBe("var(--wb-hue-amber)")
    // 没挂标签 / 只剩已删除标签 id 的行：既没有染色类也没有色相变量。
    expect(rowOf("Untagged")).not.toHaveClass("wb-row--tinted")
    expect(hue("Untagged")).toBe("")
    expect(rowOf("Ghost only")).not.toHaveClass("wb-row--tinted")
    expect(hue("Ghost only")).toBe("")
  })
})

afterEach(() => cleanup())
