import { afterEach, describe, expect, it, vi } from "vitest"

import { stubMediaQueries } from "../test/media-queries"
import { applyTheme, resolveTheme } from "./theme"

const DARK = "(prefers-color-scheme: dark)"

afterEach(() => {
  vi.unstubAllGlobals()
  delete document.documentElement.dataset.theme
  document.documentElement.style.removeProperty("color-scheme")
})

describe("resolveTheme", () => {
  it("keeps an explicit appearance", () => {
    stubMediaQueries({ [DARK]: true })
    expect(resolveTheme("light")).toBe("light")
    expect(resolveTheme("dark")).toBe("dark")
  })

  it("follows the OS while the mode is system", () => {
    const os = stubMediaQueries({ [DARK]: true })
    expect(resolveTheme("system")).toBe("dark")
    os.set(DARK, false)
    expect(resolveTheme("system")).toBe("light")
  })
})

describe("applyTheme", () => {
  it("writes a resolved palette for system, not the raw mode", () => {
    // styles.css has no prefers-color-scheme fallback, so an unresolved
    // data-theme silently leaves a dark-OS user on the light palette.
    stubMediaQueries({ [DARK]: true })
    applyTheme("system")
    expect(document.documentElement.dataset.theme).toBe("dark")
  })

  it("lets native widgets keep tracking the OS in system mode", () => {
    stubMediaQueries({})
    applyTheme("system")
    expect(document.documentElement.dataset.theme).toBe("light")
    expect(document.documentElement.style.colorScheme).toBe("light dark")
  })

  it("pins native controls to an explicit appearance", () => {
    applyTheme("dark")
    expect(document.documentElement.dataset.theme).toBe("dark")
    expect(document.documentElement.style.colorScheme).toBe("dark")
  })
})
