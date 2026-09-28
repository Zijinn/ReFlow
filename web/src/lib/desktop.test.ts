import { afterEach, describe, expect, it, vi } from "vitest"

import { applyDesktopPlatform, trackDesktopPlatform } from "./desktop"

type WailsWindow = Window & { _wails?: { environment?: { OS?: string } } }

function injectHostOS(os: string | undefined) {
  ;(window as WailsWindow)._wails = os ? { environment: { OS: os } } : undefined
}

afterEach(() => {
  vi.useRealTimers()
  delete document.documentElement.dataset.desktop
  injectHostOS(undefined)
})

describe("applyDesktopPlatform", () => {
  it("labels a macOS host so the titlebar inset rules apply", () => {
    injectHostOS("darwin")
    expect(applyDesktopPlatform()).toBe(true)
    expect(document.documentElement.dataset.desktop).toBe("macos")
  })

  it("reports nothing to apply in a plain browser", () => {
    expect(applyDesktopPlatform()).toBe(false)
    expect(document.documentElement.dataset.desktop).toBeUndefined()
  })
})

describe("trackDesktopPlatform", () => {
  // Wails injects window._wails.environment from the native side, and it can
  // land after main.tsx evaluates. Without the retry the brand mark renders
  // under the traffic lights because :root[data-desktop="macos"] never matches.
  it("applies the platform when the environment arrives late", () => {
    vi.useFakeTimers()
    trackDesktopPlatform()
    expect(document.documentElement.dataset.desktop).toBeUndefined()

    injectHostOS("windows")
    vi.advanceTimersByTime(50)
    expect(document.documentElement.dataset.desktop).toBe("windows")
  })

  it("gives up instead of polling a browser session forever", () => {
    vi.useFakeTimers()
    trackDesktopPlatform({ intervalMs: 50, maxTicks: 3 })
    vi.advanceTimersByTime(150)

    injectHostOS("darwin")
    vi.advanceTimersByTime(500)
    expect(document.documentElement.dataset.desktop).toBeUndefined()
  })
})
