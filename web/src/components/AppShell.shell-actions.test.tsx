import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import App from "../App"
import { useReaderStore } from "../store/reader"
import { stubMediaQueries } from "../test/media-queries"

// Guards the relocation of the AI / theme / preferences cluster out of the
// reader header's top-right corner: the sidebar hosts it for BOTH views, the
// mobile bar hosts it below the breakpoint where `.sidebar` is `display: none`,
// only one copy is ever in the document, and the search + palette keyboard
// paths are untouched.

const DESKTOP_WIDTH = 1280

beforeEach(() => {
  stubMediaQueries({ "(prefers-color-scheme: dark)": false })
  useReaderStore.setState({
    scope: { kind: "today", title: "Today" },
    readerReturnScope: null,
    selectedEntryID: null,
    search: "",
    viewMode: "standard",
    mobileReaderOpen: false,
    locale: "en-US",
    paneLayout: { sidebarWidth: 246, timelineWidth: 424 },
    openFolders: {},
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
    theme: "system",
    appView: "reader",
  })
  setViewportWidth(DESKTOP_WIDTH)
  vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/api/v1/status")) {
      return Promise.resolve(
        jsonResponse({
          status: "ready",
          version: "test",
          api_version: "v1",
          database_ready: true,
          capabilities: ["rss"],
          device_auth_required: false,
          device_authenticated: false,
        }),
      )
    }
    if (url.includes("/api/v1/entries")) {
      return Promise.resolve(jsonResponse({ items: [], next_cursor: null }))
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  delete document.documentElement.dataset.theme
})

function setViewportWidth(value: number) {
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    writable: true,
    value,
  })
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderApp() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>,
  )
}

async function renderReady() {
  const result = renderApp()
  await screen.findByRole("heading", { name: "Today" })
  return result
}

describe("Shell actions leave the reader header", () => {
  it("keeps only the add-feed control in the header cluster", async () => {
    const { container } = await renderReady()

    expect(container.querySelector(".workspace-header .shell-actions")).toBeNull()
    expect(container.querySelector(".workspace-actions .icon-button")).toBeNull()
    // Search and add-feed stay where they were.
    expect(container.querySelector(".workspace-header #library-search")).not.toBeNull()
    expect(
      container.querySelector(".workspace-actions .workspace-add")?.getAttribute("aria-label"),
    ).toBe("Add feed")
  })

  it("hosts one reachable copy of each control in the sidebar bottom-left", async () => {
    const { container } = await renderReady()

    const block = container.querySelector(".sidebar__actions")
    expect(block?.previousElementSibling?.className).toBe("subscription-section")
    expect(block?.querySelectorAll(".icon-button").length).toBe(3)
    // One copy, not two: a hidden duplicate would double every name and tab stop.
    expect(screen.getAllByRole("button", { name: "AI assistant" })).toHaveLength(1)
    expect(screen.getAllByRole("button", { name: /Switch to (light|dark) theme/ })).toHaveLength(1)
    expect(screen.getAllByRole("button", { name: "Preferences" })).toHaveLength(1)
  })

  it("stays ahead of the article panes in tab order", async () => {
    const { container } = await renderReady()

    const gear = screen.getByRole("button", { name: "Preferences" })
    expect(gear.tagName).toBe("BUTTON")
    expect(gear.tabIndex).toBeGreaterThanOrEqual(0)
    expect(gear.hasAttribute("disabled")).toBe(false)
    // The sidebar is the first child of <main>, so the cluster is among the
    // first tab stops in the document, before the header search input.
    expect(
      gear.compareDocumentPosition(container.querySelector("#library-search") as Node) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })
})

describe("Shell actions drive the app state", () => {
  it("opens the preferences dialog from the sidebar cluster", async () => {
    await renderReady()

    fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
  })

  it("writes a concrete theme through the sidebar cluster", async () => {
    await renderReady()

    fireEvent.click(screen.getByRole("button", { name: "Switch to dark theme" }))
    await waitFor(() => expect(useReaderStore.getState().theme).toBe("dark"))
    expect(document.documentElement.dataset.theme).toBe("dark")
  })

  it("toggles the AI panel from the sidebar cluster", async () => {
    await renderReady()

    const toggle = screen.getByRole("button", { name: "AI assistant" })
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(toggle)
    await waitFor(() => expect(toggle).toHaveAttribute("aria-expanded", "true"))
  })
})

describe("Shell actions survive the workbench view", () => {
  it("stays mounted when the reader header is replaced by the workbench", async () => {
    const { container } = await renderReady()
    const block = container.querySelector(".sidebar__actions")

    act(() => useReaderStore.getState().setAppView("workbench"))

    // The header is gone in this view, which is exactly why the cluster moved
    // to the sidebar: the controls are still there, same DOM node.
    expect(container.querySelector(".workspace-header")).toBeNull()
    expect(container.querySelector(".sidebar__actions")).toBe(block)
    expect(screen.getByRole("button", { name: "Preferences" })).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Preferences" })).toHaveLength(1)
  })

  it("opens preferences from the workbench view", async () => {
    await renderReady()
    act(() => useReaderStore.getState().setAppView("workbench"))

    fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
  })

  it("keeps focus on the cluster across a real view-switch click", async () => {
    const { container } = await renderReady()
    const gear = screen.getByRole("button", { name: "Preferences" })
    gear.focus()
    expect(document.activeElement).toBe(gear)

    fireEvent.click(screen.getByRole("button", { name: "Research Workspace" }))

    await waitFor(() => expect(useReaderStore.getState().appView).toBe("workbench"))
    expect(document.activeElement).toBe(gear)
    expect(container.querySelector(".sidebar__actions")?.contains(gear)).toBe(true)
  })

  // The relocation only pays off if the control does something in this view: the
  // panel used to exist solely inside the reader body, so the toggle was dead
  // here. It now mounts inside `.wb-shell`, in flow, and narrows `.wb-main`.
  it("opens the AI panel inside the workbench shell", async () => {
    const { container } = await renderReady()
    act(() => useReaderStore.getState().setAppView("workbench"))
    await waitFor(() => expect(container.querySelector(".wb-shell")).not.toBeNull())
    expect(container.querySelector(".ai-workbench")).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "AI assistant" }))

    await waitFor(() =>
      expect(container.querySelector(".wb-shell > .ai-workbench")).not.toBeNull(),
    )
    const main = container.querySelector(".wb-main") as HTMLElement
    const panel = container.querySelector(".wb-shell > .ai-workbench") as HTMLElement
    expect(main.nextElementSibling).toBe(panel)
    expect(panel.style.width).toBe("380px")
    expect(within(panel).getByRole("button", { name: "Configure AI" })).toBeInTheDocument()

    fireEvent.click(within(panel).getByRole("button", { name: "Close" }))
    await waitFor(() => expect(container.querySelector(".ai-workbench")).toBeNull())
  })
})

describe("Shell actions below the sidebar breakpoint", () => {
  it("hands the cluster to the mobile bar when the sidebar cannot show it", async () => {
    setViewportWidth(390)
    const { container } = renderApp()
    await screen.findByRole("heading", { name: "Today" })

    expect(container.querySelector(".sidebar__actions")).toBeNull()
    expect(container.querySelector(".mobile-nav .shell-actions")).not.toBeNull()
    expect(container.querySelectorAll(".shell-actions")).toHaveLength(1)
    // The mobile bar still leads with the five library scopes.
    expect(container.querySelectorAll(".mobile-nav .mobile-nav__item")).toHaveLength(5)

    fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
  })

  it("re-homes the cluster on the sidebar when the viewport grows back", async () => {
    setViewportWidth(390)
    const { container } = renderApp()
    await screen.findByRole("heading", { name: "Today" })
    expect(container.querySelector(".sidebar__actions")).toBeNull()

    act(() => {
      setViewportWidth(DESKTOP_WIDTH)
      window.dispatchEvent(new Event("resize"))
    })

    expect(container.querySelector(".sidebar__actions")).not.toBeNull()
    expect(container.querySelectorAll(".shell-actions")).toHaveLength(1)
    expect(container.querySelector(".mobile-nav .shell-actions")).toBeNull()
  })
})

describe("Keyboard paths the relocation must not touch", () => {
  it("keeps the search chord on the header input and the palette open", async () => {
    const { container } = await renderReady()

    fireEvent.keyDown(document.body, { key: "/" })
    expect(document.activeElement?.id).toBe("library-search")

    fireEvent.keyDown(document.body, { key: "k", metaKey: true })
    const palette = await screen.findByRole("dialog")
    expect(palette).toBeInTheDocument()
    expect(screen.getByRole("option", { name: /Open preferences/ })).toBeInTheDocument()
    expect(container.querySelector("#library-search")).not.toBeNull()
  })
})
