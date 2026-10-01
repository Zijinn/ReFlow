import { afterEach, describe, expect, it, vi } from "vitest"

import { SCROLL_REVEAL_MS, installScrollReveal } from "./scroll-reveal"

const disposers: Array<() => void> = []

function install() {
  disposers.push(installScrollReveal())
}

function scroller(): HTMLElement {
  const element = document.createElement("div")
  document.body.append(element)
  return element
}

afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose()
  document.body.replaceChildren()
  vi.useRealTimers()
})

describe("installScrollReveal", () => {
  it("marks the scroller for exactly the reveal window", () => {
    vi.useFakeTimers()
    install()
    const element = scroller()

    element.dispatchEvent(new Event("scroll"))
    expect(element.dataset.scrolling).toBe("true")

    vi.advanceTimersByTime(SCROLL_REVEAL_MS - 1)
    expect(element.dataset.scrolling).toBe("true")

    vi.advanceTimersByTime(1)
    expect(element.dataset.scrolling).toBeUndefined()
  })

  it("restarts the window on every scroll event instead of stacking timers", () => {
    vi.useFakeTimers()
    install()
    const element = scroller()

    element.dispatchEvent(new Event("scroll"))
    vi.advanceTimersByTime(SCROLL_REVEAL_MS - 1)
    element.dispatchEvent(new Event("scroll"))
    vi.advanceTimersByTime(SCROLL_REVEAL_MS - 1)
    expect(element.dataset.scrolling).toBe("true")

    vi.advanceTimersByTime(1)
    expect(element.dataset.scrolling).toBeUndefined()
  })

  it("keeps one timer per scroller so neighbours do not hide each other", () => {
    vi.useFakeTimers()
    install()
    const first = scroller()
    const second = scroller()

    first.dispatchEvent(new Event("scroll"))
    vi.advanceTimersByTime(300)
    second.dispatchEvent(new Event("scroll"))
    vi.advanceTimersByTime(SCROLL_REVEAL_MS - 300)

    expect(first.dataset.scrolling).toBeUndefined()
    expect(second.dataset.scrolling).toBe("true")
  })

  // scroll 不冒泡：只有 window 上的捕获监听收得到深处那个格子的滚动，
  // 而且不能顺手把祖先也标成"在滚"。
  it("catches a nested scroller without marking its ancestors", () => {
    install()
    const outer = scroller()
    const inner = document.createElement("textarea")
    outer.append(inner)

    inner.dispatchEvent(new Event("scroll"))

    expect(inner.dataset.scrolling).toBe("true")
    expect(outer.dataset.scrolling).toBeUndefined()
  })

  it("ignores the document target of a viewport scroll", () => {
    install()

    document.dispatchEvent(new Event("scroll"))

    expect(document.documentElement.dataset.scrolling).toBeUndefined()
  })

  it("stops marking elements once disposed", () => {
    install()
    const element = scroller()
    for (const dispose of disposers.splice(0)) dispose()

    element.dispatchEvent(new Event("scroll"))

    expect(element.dataset.scrolling).toBeUndefined()
  })
})
