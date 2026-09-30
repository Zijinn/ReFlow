import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useReaderStore, type ThemeMode } from "../store/reader"
import { stubMediaQueries, type MediaQueryStub } from "../test/media-queries"
import { ShellActions } from "./ShellActions"

// The cluster that used to live in the reader header's top-right corner now
// hosts the sidebar bottom-left and the mobile bar. These guards pin the three
// accessible names to the strings the header already shipped (i18n is owned
// elsewhere) and the `system` theme resolution that moved with the button.

let media: MediaQueryStub

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
  media = stubMediaQueries({ "(prefers-color-scheme: dark)": false })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

function renderActions(
  overrides: {
    theme?: ThemeMode
    aiOpen?: boolean
    onAI?: () => void
    onThemeChange?: (theme: ThemeMode) => void
    onPreferences?: () => void
  } = {},
) {
  const handlers = {
    onAI: overrides.onAI ?? vi.fn(),
    onThemeChange: overrides.onThemeChange ?? vi.fn(),
    onPreferences: overrides.onPreferences ?? vi.fn(),
  }
  const result = render(
    <ShellActions
      theme={overrides.theme ?? "light"}
      onThemeChange={handlers.onThemeChange}
      aiOpen={overrides.aiOpen ?? false}
      onAI={handlers.onAI}
      onPreferences={handlers.onPreferences}
    />,
  )
  return { ...result, ...handlers }
}

describe("ShellActions", () => {
  it("keeps the three labels the reader header used", () => {
    renderActions()

    expect(screen.getByRole("button", { name: "AI assistant" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Switch to dark theme" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Preferences" })).toBeInTheDocument()
  })

  it("mirrors the AI panel state on the toggle", () => {
    const { onAI, rerender } = renderActions()
    const toggle = screen.getByRole("button", { name: "AI assistant" })
    expect(toggle).toHaveAttribute("aria-expanded", "false")

    fireEvent.click(toggle)
    expect(onAI).toHaveBeenCalledTimes(1)

    rerender(
      <ShellActions
        theme="light"
        onThemeChange={vi.fn()}
        aiOpen
        onAI={vi.fn()}
        onPreferences={vi.fn()}
      />,
    )
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(toggle.className).toContain("icon-button--active")
  })

  it("opens the preferences dialog through the new host", () => {
    const { onPreferences } = renderActions()

    fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
    expect(onPreferences).toHaveBeenCalledTimes(1)
  })

  it("writes the concrete mode the stylesheet can read", () => {
    const { onThemeChange } = renderActions({ theme: "dark" })

    fireEvent.click(screen.getByRole("button", { name: "Switch to light theme" }))
    expect(onThemeChange).toHaveBeenCalledWith("light")
  })

  it("keeps tracking prefers-color-scheme while the mode is system", () => {
    const { onThemeChange } = renderActions({ theme: "system" })

    // Same shape as App.test.tsx: the listener callback lands synchronously,
    // so act() only has to flush the resulting state update.
    act(() => media.set("(prefers-color-scheme: dark)", true))
    expect(screen.getByRole("button", { name: "Switch to light theme" })).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Switch to light theme" }))
    expect(onThemeChange).toHaveBeenCalledWith("light")
  })

  it("applies the surface skin without inventing a wrapper role", () => {
    const { container } = renderActions()

    expect(container.querySelector(".shell-actions")).not.toBeNull()
    expect(container.querySelector("[role='group'], [role='toolbar']")).toBeNull()
  })
})
