import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useReaderStore } from "../../store/reader"
import {
  ChipEditor,
  ColumnHead,
  DatePickerCell,
  DragHandle,
  InlineText,
  MenuSelect,
  Row,
  WbTableWrap,
} from "./shared"
import { formatDeadline } from "./utils"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

afterEach(() => cleanup())

// jsdom performs no layout, and src/test/setup.ts pins every measurement to a
// fixed box, so the popover placement maths is exercised by replacing those
// stubs with the geometry a real browser would report.
// 假尺要 spy 在 HTMLElement.prototype：全局那把就定义在那里，挂到 Element.prototype
// 会被按原型链遮住，一次都跑不到。
function stubGeometry(rect: { top: number; bottom: number; left: number; right: number }) {
  const height = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(160)
  const width = vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(160)
  const bounds = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockReturnValue(rect as DOMRect)
  return () => {
    height.mockRestore()
    width.mockRestore()
    bounds.mockRestore()
  }
}

describe("InlineText", () => {
  it("renders the value and an i18n edit hint (no hard-coded Chinese)", () => {
    render(<InlineText value="Smith J" placeholder="(fill in)" onCommit={() => {}} />)
    const span = screen.getByText("Smith J")
    expect(span).toHaveAttribute("title", expect.stringContaining("Double-click"))
    expect(span.getAttribute("title")).not.toContain("双击")
  })

  it("is keyboard reachable and starts editing on Enter", () => {
    render(<InlineText value="Smith J" placeholder="(fill in)" onCommit={() => {}} />)
    const span = screen.getByText("Smith J")
    expect(span).toHaveAttribute("role", "button")
    expect(span).toHaveAttribute("tabindex", "0")
    fireEvent.keyDown(span, { key: "Enter" })
    expect(screen.getByDisplayValue("Smith J")).toBeInTheDocument()
  })

  it("commits on Enter only with the trimmed changed value", () => {
    const onCommit = vi.fn()
    render(<InlineText value="A Journal" placeholder="(fill in)" onCommit={onCommit} />)
    fireEvent.doubleClick(screen.getByText("A Journal"))
    const input = screen.getByDisplayValue("A Journal")
    fireEvent.change(input, { target: { value: "  B Journal  " } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(onCommit).toHaveBeenCalledTimes(1)
    expect(onCommit).toHaveBeenCalledWith("B Journal")
  })

  it("does not PATCH when blurring without a real change", () => {
    const onCommit = vi.fn()
    render(<InlineText value="Same value" placeholder="(fill in)" onCommit={onCommit} />)
    fireEvent.doubleClick(screen.getByText("Same value"))
    const input = screen.getByDisplayValue("Same value")
    // Trailing whitespace added by the user is trimmed away; the stored value
    // is unchanged, so no network write should happen.
    fireEvent.change(input, { target: { value: "Same value   " } })
    fireEvent.blur(input)
    expect(onCommit).not.toHaveBeenCalled()
  })

  it("Escape cancels without committing", () => {
    const onCommit = vi.fn()
    render(<InlineText value="Original" placeholder="(fill in)" onCommit={onCommit} />)
    fireEvent.doubleClick(screen.getByText("Original"))
    const input = screen.getByDisplayValue("Original")
    fireEvent.change(input, { target: { value: "Discarded" } })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByText("Original")).toBeInTheDocument()
  })
})

describe("ChipEditor", () => {
  it("adds a chip through an inline input instead of window.prompt", () => {
    const promptSpy = vi.spyOn(window, "prompt").mockImplementation(() => null)
    const onChange = vi.fn()
    render(
      <ChipEditor label="Authors" items={["Ada"]} addPrompt="Enter author:" onChange={onChange} />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    const input = screen.getByPlaceholderText("Enter author:")
    fireEvent.change(input, { target: { value: " Grace " } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(promptSpy).not.toHaveBeenCalled()
    expect(onChange).toHaveBeenCalledWith(["Ada", "Grace"])
  })

  it("ignores empty additions", () => {
    const onChange = vi.fn()
    render(
      <ChipEditor label="Authors" items={["Ada"]} addPrompt="Enter author:" onChange={onChange} />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    const input = screen.getByPlaceholderText("Enter author:")
    fireEvent.change(input, { target: { value: "   " } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(onChange).not.toHaveBeenCalled()
  })

  it("removes chips via labelled buttons", () => {
    const onChange = vi.fn()
    render(
      <ChipEditor
        label="Authors"
        items={["Ada", "Bob"]}
        addPrompt="Enter author:"
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Delete: Ada" }))
    expect(onChange).toHaveBeenCalledWith(["Bob"])
  })
})

describe("MenuSelect", () => {
  const options = [
    { value: "High", label: "High priority", dotClass: "wb-dot--red" },
    { value: "Medium", label: "Medium priority", dotClass: "wb-dot--amber" },
  ]

  it("flips up and right-aligns when the trigger hugs the bottom-right corner", () => {
    // 768x1024 viewport: 36px below the trigger, 124px past the right edge.
    const restore = stubGeometry({ top: 700, bottom: 732, left: 900, right: 1010 })
    try {
      render(
        <MenuSelect value="Medium" options={options} onChange={() => {}} ariaLabel="Priority" />,
      )
      fireEvent.click(screen.getByRole("button", { name: "Priority" }))
      const pop = screen.getByRole("listbox")
      expect(pop).toHaveClass("wb-menu-pop--up")
      expect(pop).toHaveClass("wb-menu-pop--right")
      expect(pop.style.maxHeight).toBe("686px")
    } finally {
      restore()
    }
  })

  it("drops down when there is room below", () => {
    const restore = stubGeometry({ top: 120, bottom: 152, left: 40, right: 150 })
    try {
      render(
        <MenuSelect value="Medium" options={options} onChange={() => {}} ariaLabel="Priority" />,
      )
      fireEvent.click(screen.getByRole("button", { name: "Priority" }))
      const pop = screen.getByRole("listbox")
      expect(pop).not.toHaveClass("wb-menu-pop--up")
      expect(pop).not.toHaveClass("wb-menu-pop--right")
    } finally {
      restore()
    }
  })

  it("marks the current option as the selected one", () => {
    render(<MenuSelect value="Medium" options={options} onChange={() => {}} ariaLabel="Priority" />)
    fireEvent.click(screen.getByRole("button", { name: "Priority" }))
    expect(screen.getByRole("option", { name: "Medium priority" })).toHaveAttribute(
      "aria-selected",
      "true",
    )
  })

  // The menu lives inside the scrolling workbench: a focus() that also scrolls
  // would slide the table out from under the reader every time they arrow
  // through options, which is the "it jumps when I pick something" report.
  it("moves the keyboard highlight without letting focus scroll the page", () => {
    render(<MenuSelect value="Medium" options={options} onChange={() => {}} ariaLabel="Priority" />)
    fireEvent.click(screen.getByRole("button", { name: "Priority" }))
    const first = screen.getByRole("option", { name: "High priority" })
    const focus = vi.spyOn(first, "focus")
    fireEvent.keyDown(screen.getByRole("listbox"), { key: "ArrowDown" })
    expect(focus).toHaveBeenCalledWith({ preventScroll: true })
  })
})

describe("DatePickerCell", () => {
  function renderPicker(value = "", onCommit = vi.fn()) {
    render(
      <table>
        <tbody>
          <tr>
            <td className="wb-col-date">
              <DatePickerCell
                value={value}
                placeholder="(fill in)"
                ariaLabel="Deadline"
                onCommit={onCommit}
              />
            </td>
          </tr>
        </tbody>
      </table>,
    )
    const trigger = screen.getByRole("button", { name: "Deadline" })
    return { onCommit, trigger }
  }
  const openDialog = (trigger: HTMLElement) => {
    fireEvent.click(trigger)
    return screen.getByRole("dialog", { name: "Choose a date" })
  }
  const day = (dialog: HTMLElement, iso: string) =>
    within(dialog).getByRole("gridcell", { name: iso })

  it("shows the stored date, or the placeholder until one is picked", () => {
    const { trigger } = renderPicker("2026-10-15")
    expect(trigger).toHaveTextContent("2026-10-15")
    cleanup()
    const empty = renderPicker("")
    expect(empty.trigger).toHaveTextContent("(fill in)")
  })

  it("opens a month grid on the day it holds and keeps focus inside the popover", () => {
    const { trigger } = renderPicker("2026-10-15")
    const dialog = openDialog(trigger)
    expect(within(dialog).getByText("October 2026")).toBeInTheDocument()
    expect(within(dialog).getAllByRole("columnheader")).toHaveLength(7)
    expect(day(dialog, "2026-10-15")).toBeInTheDocument()
    // 表体在滚动容器里：焦点要是跑到格子外面，浏览器就会为了露出它把整张表滚走。
    expect(document.activeElement).toHaveAttribute("aria-label", "2026-10-15")
  })

  it("commits the picked day as YYYY-MM-DD and closes the popover", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    const dialog = openDialog(trigger)
    fireEvent.click(day(dialog, "2026-10-31"))
    expect(onCommit).toHaveBeenCalledTimes(1)
    expect(onCommit).toHaveBeenCalledWith("2026-10-31")
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("does not write when the same day is picked again", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    fireEvent.click(day(openDialog(trigger), "2026-10-15"))
    expect(onCommit).not.toHaveBeenCalled()
  })

  it("commits today through the shortcut", () => {
    const { onCommit, trigger } = renderPicker("")
    const dialog = openDialog(trigger)
    fireEvent.click(within(dialog).getByRole("button", { name: "Today" }))
    expect(onCommit).toHaveBeenCalledWith(formatDeadline(new Date()))
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("clears the deadline to an empty string", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    fireEvent.click(within(openDialog(trigger)).getByRole("button", { name: "Clear date" }))
    expect(onCommit).toHaveBeenCalledWith("")
  })

  it("closes on Escape without committing and hands the focus back to the cell", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    fireEvent.keyDown(openDialog(trigger), { key: "Escape" })
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(document.activeElement).toBe(trigger)
  })

  it("closes on Tab without stealing the focus back to the trigger", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    const dialog = openDialog(trigger)
    // 打开时焦点已经在回顾格上，Tab 的意义是"从这里走出去"。
    expect(document.activeElement).not.toBe(trigger)
    fireEvent.keyDown(dialog, { key: "Tab" })
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.queryByRole("dialog")).toBeNull()
    // 曾经这一支和 Escape 合并写成 preventDefault + closePicker()，焦点被送回触发格，
    // 键盘用户按 Tab 又被弹回同一枚按钮，走不到下一个字段。
    expect(document.activeElement).not.toBe(trigger)
  })

  it("walks days, weeks and months with the keyboard and commits on Enter", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    const dialog = openDialog(trigger)
    const grid = within(dialog).getByRole("grid")
    fireEvent.keyDown(grid, { key: "ArrowRight" })
    expect(document.activeElement).toHaveAttribute("aria-label", "2026-10-16")
    fireEvent.keyDown(grid, { key: "ArrowUp" })
    expect(document.activeElement).toHaveAttribute("aria-label", "2026-10-09")
    // 2026-10-15 是周四：Home/End 落在这周的周一与周日。
    fireEvent.keyDown(grid, { key: "Home" })
    expect(document.activeElement).toHaveAttribute("aria-label", "2026-10-05")
    fireEvent.keyDown(grid, { key: "End" })
    expect(document.activeElement).toHaveAttribute("aria-label", "2026-10-11")
    fireEvent.keyDown(grid, { key: "PageDown" })
    expect(within(dialog).getByText("November 2026")).toBeInTheDocument()
    expect(document.activeElement).toHaveAttribute("aria-label", "2026-11-11")
    fireEvent.keyDown(grid, { key: "Enter" })
    expect(onCommit).toHaveBeenCalledWith("2026-11-11")
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("drops a neighbour-month day rather than navigating away from it", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    const dialog = openDialog(trigger)
    fireEvent.click(day(dialog, "2026-11-07"))
    expect(onCommit).toHaveBeenCalledWith("2026-11-07")
  })

  it("flips up and right-aligns when the cell hugs the bottom-right corner", () => {
    // 768x1024 viewport: 36px below the trigger, 9px past the right edge.
    const restore = stubGeometry({ top: 700, bottom: 732, left: 900, right: 1010 })
    try {
      const { trigger } = renderPicker("2026-10-15")
      const dialog = openDialog(trigger)
      expect(dialog).toHaveClass("wb-menu-pop--up")
      expect(dialog).toHaveClass("wb-menu-pop--right")
      expect(dialog.style.maxHeight).toBe("686px")
    } finally {
      restore()
    }
  })

  it("closes when the pointer lands outside the cell", () => {
    const { onCommit, trigger } = renderPicker("2026-10-15")
    openDialog(trigger)
    fireEvent.pointerDown(document.body)
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(onCommit).not.toHaveBeenCalled()
  })
})

describe("Row", () => {
  const renderRow = () =>
    render(
      <table>
        <tbody>
          <Row id="p1" onReorder={() => {}}>
            <td className="wb-col-grip">
              <DragHandle />
            </td>
            <td>
              <button type="button">Toggle stages</button>
            </td>
          </Row>
        </tbody>
      </table>,
    )

  // A row that is always draggable makes WKWebView claim every mousedown in it
  // for the drag, so the stage cell never receives the click that expands it.
  it("arms dragging from the handle only", () => {
    renderRow()
    const row = screen.getByRole("button", { name: "Toggle stages" }).closest("tr")
    expect(row).not.toBeNull()
    expect(row).toHaveAttribute("draggable", "false")

    fireEvent.pointerDown(screen.getByText("⠿"))
    expect(row).toHaveAttribute("draggable", "true")

    fireEvent.pointerDown(screen.getByRole("button", { name: "Toggle stages" }))
    expect(row).toHaveAttribute("draggable", "false")
  })
})

describe("ColumnHead", () => {
  // jsdom 不做布局：getBoundingClientRect 恒为 0，样式表里的 --wb-col-min 也读不到。
  // 给一张"标题 300 / 内容列 176"的假尺，拖拽算式就能在测试里和浏览器里走同一条路
  // （真实数值见 styles.css 那批实测注释）。
  const titleWidth = 300
  const columnWidth = 176
  const box = (width: number): DOMRect => ({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: width,
    bottom: 40,
    width,
    height: 40,
    toJSON: () => ({}),
  })

  const renderHead = () =>
    render(
      <table className="wb-table wb-table--submitted">
        <thead>
          <tr>
            <th className="wb-col-title">Title</th>
            <ColumnHead table="submitted" column="notes" className="wb-col-notes">
              Notes
            </ColumnHead>
          </tr>
        </thead>
      </table>,
    )

  const cell = () => document.querySelector<HTMLElement>(".wb-col-notes")

  beforeEach(() => {
    useReaderStore.setState({ workbenchColumnWidths: {} })
    // 假尺也要跟着"已经拖过"的宽度走，否则连着按两次方向键会算回同一个起点。
    // 覆盖的是 HTMLElement.prototype：test/setup.ts 的那把全局假尺就定义在那里，
    // 挂在 Element.prototype 上会被它按原型链遮住，一次都跑不到。
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (
      this: HTMLElement,
    ) {
      if (this.classList.contains("wb-col-title")) return box(titleWidth)
      const declared = Number.parseFloat(this.style.getPropertyValue("--wb-col-w"))
      return box(Number.isFinite(declared) ? declared : columnWidth)
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("puts the persisted width on the header cell as --wb-col-w", () => {
    useReaderStore.setState({ workbenchColumnWidths: { submitted: { notes: 240 } } })
    renderHead()
    expect(cell()?.style.getPropertyValue("--wb-col-w")).toBe("240px")
  })

  it("leaves the cell alone until a width is chosen, so CSS keeps its default", () => {
    renderHead()
    expect(cell()?.style.getPropertyValue("--wb-col-w")).toBe("")
  })

  it("exposes the handle as a separator named after its column", () => {
    renderHead()
    const handle = screen.getByRole("separator", { name: /Notes/ })
    expect(handle).toHaveAttribute("aria-orientation", "vertical")
    expect(handle.getAttribute("aria-label")).toContain("double-click")
  })

  it("previews while dragging and writes the store once on release", () => {
    renderHead()
    const handle = screen.getByRole("separator", { name: /Notes/ })
    fireEvent.pointerDown(handle, { clientX: 500, pointerId: 1 })
    fireEvent.pointerMove(handle, { clientX: 550, pointerId: 1 })
    expect(cell()?.style.getPropertyValue("--wb-col-w")).toBe("226px")
    expect(useReaderStore.getState().workbenchColumnWidths).toEqual({})
    fireEvent.pointerUp(handle, { clientX: 550, pointerId: 1 })
    expect(useReaderStore.getState().workbenchColumnWidths.submitted?.notes).toBe(226)
  })

  it("cannot take the title below its own floor", () => {
    renderHead()
    const handle = screen.getByRole("separator", { name: /Notes/ })
    fireEvent.pointerDown(handle, { clientX: 500, pointerId: 1 })
    fireEvent.pointerMove(handle, { clientX: 1500, pointerId: 1 })
    fireEvent.pointerUp(handle, { clientX: 1500, pointerId: 1 })
    // 176 + (300 - 64) = 412：标题还剩它的 min-width，表格不会横向溢出。
    expect(useReaderStore.getState().workbenchColumnWidths.submitted?.notes).toBe(412)
  })

  it("keeps a column already wider than the cap from snapping back", () => {
    // 标题列也能拖以后，它的现宽就是吸收完整张表余量的结果，可以远超 COLUMN_MAX。
    // 闸必须至少放到现宽，否则每拖一次都先把这一列弹回 640 再往前走。
    useReaderStore.setState({ workbenchColumnWidths: { submitted: { notes: 900 } } })
    renderHead()
    const handle = screen.getByRole("separator", { name: /Notes/ })
    fireEvent.pointerDown(handle, { clientX: 500, pointerId: 1 })
    fireEvent.pointerMove(handle, { clientX: 700, pointerId: 1 })
    fireEvent.pointerUp(handle, { clientX: 700, pointerId: 1 })
    expect(useReaderStore.getState().workbenchColumnWidths.submitted?.notes).toBe(900)
  })

  it("nudges with the arrow keys", () => {
    renderHead()
    const handle = screen.getByRole("separator", { name: /Notes/ })
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    expect(useReaderStore.getState().workbenchColumnWidths.submitted?.notes).toBe(192)
    fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(useReaderStore.getState().workbenchColumnWidths.submitted?.notes).toBe(176)
  })

  it("double-click drops the override so the stylesheet default comes back", () => {
    useReaderStore.setState({ workbenchColumnWidths: { submitted: { notes: 240 } } })
    renderHead()
    fireEvent.doubleClick(screen.getByRole("separator", { name: /Notes/ }))
    expect(useReaderStore.getState().workbenchColumnWidths.submitted?.notes).toBeUndefined()
    expect(cell()?.style.getPropertyValue("--wb-col-w")).toBe("")
  })
})

describe("WbTableWrap", () => {
  // jsdom 不做布局，"表比卡片宽"这件事得自己搭出来：卡片给一个 clientWidth，表的矩形
  // 按它所有列的现宽之和算（fixed 布局在浏览器里就是这个式子），每一列读自己身上的
  // --wb-col-w、没写过就退回 styles.css 那批默认宽度的替身。于是"写下上限 → 表变窄 →
  // 再量一次"这条收敛路在测试里走的是和浏览器里同一个算式，而不是把结果硬编码进去。
  const gripWidth = 24
  const defaults: Record<string, number> = { title: 300, notes: 176 }

  const box = (width: number): DOMRect => ({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: width,
    bottom: 40,
    width,
    height: 40,
    toJSON: () => ({}),
  })

  let wrapWidth: number
  let triggerResize: () => void

  function renderWrap() {
    return render(
      <WbTableWrap table="research">
        <table className="wb-table wb-table--research">
          <thead>
            <tr>
              <th className="wb-col-grip" aria-label="Code" />
              <ColumnHead table="research" column="title" className="wb-col-title">
                Title
              </ColumnHead>
              <ColumnHead table="research" column="notes" className="wb-col-notes">
                Notes
              </ColumnHead>
            </tr>
          </thead>
        </table>
      </WbTableWrap>,
    )
  }

  const declared = (column: string) =>
    document.querySelector<HTMLElement>(`.wb-col-${column}`)?.style.getPropertyValue("--wb-col-w")

  beforeEach(() => {
    useReaderStore.setState({ workbenchColumnWidths: {} })
    wrapWidth = 500

    // setup.ts 那把 ResizeObserver 永不触发，而"卡片自己变窄/变宽"正是这道闸要接的
    // 事件，所以这一组要一把能把回调叫出来的替身。
    const callbacks: ResizeObserverCallback[] = []
    class TrackingObserver {
      constructor(callback: ResizeObserverCallback) {
        callbacks.push(callback)
      }
      observe() {}
      unobserve() {}
      disconnect() {}
      takeRecords(): ResizeObserverEntry[] {
        return []
      }
    }
    vi.stubGlobal("ResizeObserver", TrackingObserver)
    triggerResize = () => callbacks.forEach((callback) => callback([], {} as ResizeObserver))

    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (
      this: HTMLElement,
    ) {
      return this.classList.contains("wb-table-wrap") ? wrapWidth : 1280
    })
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (
      this: HTMLElement,
    ) {
      if (this.classList.contains("wb-table-wrap")) return box(wrapWidth)
      if (this.classList.contains("wb-col-grip")) return box(gripWidth)
      if (this.tagName === "TABLE") {
        const row = this.querySelector("thead tr")
        if (!row) return box(wrapWidth)
        const sum = Array.from(row.children).reduce(
          (total, child) => total + child.getBoundingClientRect().width,
          0,
        )
        // 列宽之和装不满容器时，浏览器会把 fixed 布局的表按比例拉开补满，所以表的
        // 现宽永远不会小于内容宽。这把尺要照这个行为做，否则"容器变宽"会读成一个
        // 假负余量，放宽的闸门就白测了。
        return box(Math.max(sum, wrapWidth))
      }
      const key = this.dataset.wbColumn
      if (!key) return box(0)
      const own = Number.parseFloat(this.style.getPropertyValue("--wb-col-w"))
      return box(Number.isFinite(own) ? own : (defaults[key] ?? 0))
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it("brings a table wider than its card back to the edge", () => {
    // 24 + 400 + 176 = 600，卡片只有 500。多出的 100 按各列 own 下限之上的余量摊：
    // 标题 336、备注 112，于是 75 与 25，收完正好 500。
    useReaderStore.setState({ workbenchColumnWidths: { research: { title: 400 } } })
    renderWrap()
    expect(declared("title")).toBe("325px")
    expect(declared("notes")).toBe("151px")
  })

  it("takes the shortfall only from the columns that still have slack", () => {
    // 备注已被拖到 40，它自己就是下限（收无可收），44 的缺口全落在标题上。
    wrapWidth = 420
    useReaderStore.setState({ workbenchColumnWidths: { research: { title: 400, notes: 40 } } })
    renderWrap()
    expect(declared("title")).toBe("356px")
    expect(declared("notes")).toBe("40px")
  })

  it("leaves a table that fits the card alone", () => {
    wrapWidth = 900
    useReaderStore.setState({ workbenchColumnWidths: { research: { title: 400 } } })
    renderWrap()
    expect(declared("title")).toBe("400px")
    expect(declared("notes")).toBe("")
  })

  it("caps in the view only, so a dragged width survives in storage", () => {
    useReaderStore.setState({ workbenchColumnWidths: { research: { title: 400 } } })
    renderWrap()
    expect(declared("title")).toBe("325px")
    expect(useReaderStore.getState().workbenchColumnWidths.research).toEqual({ title: 400 })
  })

  it("releases the cap when the card widens", () => {
    useReaderStore.setState({ workbenchColumnWidths: { research: { title: 400 } } })
    renderWrap()
    expect(declared("title")).toBe("325px")
    wrapWidth = 900
    act(triggerResize)
    expect(declared("title")).toBe("400px")
    expect(declared("notes")).toBe("")
  })
})
