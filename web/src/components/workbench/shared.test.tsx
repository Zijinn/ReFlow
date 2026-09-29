import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useReaderStore } from "../../store/reader"
import { ChipEditor, DragHandle, InlineText, MenuSelect, Row } from "./shared"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

afterEach(() => cleanup())

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

  // jsdom performs no layout, and src/test/setup.ts pins every measurement to a
  // fixed box, so the popover placement maths is exercised by replacing those
  // stubs with the geometry a real browser would report.
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
