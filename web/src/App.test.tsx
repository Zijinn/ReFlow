import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import App from "./App"
import { stubMediaQueries } from "./test/media-queries"

const PREFERS_DARK = "(prefers-color-scheme: dark)"
import { useReaderStore } from "./store/reader"

beforeEach(() => {
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
  })
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
    if (url.includes("/api/v1/tags")) {
      return Promise.resolve(
        jsonResponse({
          items: [
            {
              id: "tag-research",
              name: "Research",
              color: "#167a72",
              position: 0,
              created_at: "2026-07-17T00:00:00Z",
            },
          ],
        }),
      )
    }
    if (url.includes("/api/v1/saved-filters")) {
      return Promise.resolve(
        jsonResponse({
          items: [
            {
              id: "filter-favorites",
              name: "Favorites",
              query: { state: "starred" },
              position: 0,
              created_at: "2026-07-17T00:00:00Z",
              updated_at: "2026-07-17T00:00:00Z",
            },
          ],
        }),
      )
    }
    if (
      url.includes("/api/v1/subscriptions") ||
      url.includes("/api/v1/folders") ||
      url.includes("/api/v1/devices") ||
      url.includes("/api/v1/sync/accounts") ||
      url.includes("/api/v1/rules")
    ) {
      return Promise.resolve(jsonResponse({ items: [] }))
    }
    if (url.includes("/api/v1/sync/providers")) {
      return Promise.resolve(
        jsonResponse({
          items: [
            { id: "freshrss", name: "FreshRSS" },
            { id: "miniflux", name: "Miniflux" },
          ],
        }),
      )
    }
    if (url.includes("/api/v1/ai/providers")) {
      return Promise.resolve(
        jsonResponse({
          items: [
            { id: "openai_compatible", name: "OpenAI compatible" },
            { id: "ollama", name: "Ollama" },
          ],
        }),
      )
    }
    if (url.includes("/api/v1/ai/profiles")) {
      return Promise.resolve(jsonResponse({ items: [] }))
    }
    if (url.includes("/api/v1/ai/usage")) {
      return Promise.resolve(jsonResponse({ input_tokens: 0, output_tokens: 0, total_tokens: 0 }))
    }
    if (url.includes("/api/v1/entries")) {
      return Promise.resolve(jsonResponse({ items: [], next_cursor: null }))
    }
    return Promise.resolve(jsonResponse({}, 404))
  })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  delete (window as Window & { _wails?: unknown })._wails
  delete document.documentElement.dataset.desktop
  delete document.documentElement.dataset.theme
})

describe("Bootstrap boundary", () => {
  it("shows a retryable status error and does not request library data", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ detail: "Gateway unavailable" }, 503))

    renderApp()

    expect(await screen.findByRole("heading", { name: "Unable to connect" })).toBeInTheDocument()
    expect(screen.getByText("Gateway unavailable")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument()
    expect(requestedLibraryURLs()).toEqual([])
  })

  it("shows a retryable database state for degraded bootstrap without library requests", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse({
        status: "degraded",
        version: "test",
        api_version: "v1",
        database_ready: false,
        capabilities: ["rss"],
        device_auth_required: false,
        device_authenticated: false,
      }),
    )

    renderApp()

    expect(await screen.findByRole("heading", { name: "Database unavailable" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument()
    expect(requestedLibraryURLs()).toEqual([])
  })

  it("loads the library after a successful status retry", async () => {
    const defaultFetch = vi.mocked(fetch).getMockImplementation()
    let statusAttempts = 0
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = requestURL(input)
      if (url.includes("/api/v1/status")) {
        statusAttempts += 1
        if (statusAttempts === 1) {
          return Promise.resolve(jsonResponse({ detail: "Service warming up" }, 503))
        }
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
      return defaultFetch!(input, init)
    })

    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }))

    expect(await screen.findByRole("heading", { name: "Today" })).toBeInTheDocument()
    await waitFor(() => expect(requestedLibraryURLs().length).toBeGreaterThan(0))
    expect(statusAttempts).toBe(2)
  })
})

describe("ReFlow reading experience", () => {
  it("renders the empty reading state and reports a ready library", async () => {
    renderApp()
    expect(await screen.findByRole("heading", { name: "Today" })).toBeInTheDocument()
    expect(await screen.findByText("Your reading trail starts here")).toBeInTheDocument()
    expect(screen.queryByText("Library ready")).not.toBeInTheDocument()
  })

  it("opens the home AI panel without starting a model request", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "AI assistant" }))
    expect(await screen.findByText("ReFlow Insight")).toBeInTheDocument()
    expect(document.querySelector(".workspace-body")).toHaveClass("workspace-body--ai-open")
    expect(
      vi.mocked(fetch).mock.calls.some(([input, init]) => {
        const url =
          typeof input === "string" ? input : input instanceof URL ? input.href : input.url
        return url.includes("/api/v1/ai/library-chat") && init?.method === "POST"
      }),
    ).toBe(false)
  })

  it("opens the add subscription workflow", async () => {
    renderApp()
    const addButtons = await screen.findAllByRole("button", { name: "Add feed" })
    fireEvent.click(addButtons[0]!)
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    expect(await screen.findByRole("heading", { name: "Add subscription" })).toBeInTheDocument()
    expect(await screen.findByLabelText("Feed or website URL")).toBeInTheDocument()
    expect(
      screen.getByText("Supports rsshub://github/trending/daily and direct RSSHub HTTPS URLs."),
    ).toBeInTheDocument()
  })

  it("opens a focused folder manager from the sidebar", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Add folder" }))
    expect(await screen.findByRole("heading", { name: "Manage folders" })).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Tags" })).not.toBeInTheDocument()
  })

  it("opens the command palette with the platform shortcut", async () => {
    renderApp()
    fireEvent.keyDown(window, { key: "k", metaKey: true })
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    expect(screen.getByPlaceholderText("Type a command")).toBeInTheDocument()
  })

  it("does not fire global shortcuts while a dialog is open", async () => {
    renderApp()
    act(() => useReaderStore.getState().selectEntry("entry-1"))
    expect(useReaderStore.getState().mobileReaderOpen).toBe(true)
    const addButtons = await screen.findAllByRole("button", { name: "Add feed" })
    fireEvent.click(addButtons[0]!)
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(useReaderStore.getState().selectedEntryID).toBe("entry-1")
    expect(useReaderStore.getState().mobileReaderOpen).toBe(true)
  })

  it("rejects conflicting shortcut assignments before saving", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    await screen.findByRole("dialog")
    const nextLabel = await screen.findByText("Next article")
    const nextButton = nextLabel.parentElement?.querySelector("button")
    expect(nextButton).not.toBeNull()
    fireEvent.keyDown(nextButton!, { key: "k" })
    expect(screen.getByRole("alert")).toHaveTextContent("already assigned to Previous article")
    expect(useReaderStore.getState().shortcuts.next).toBe("j")
  })

  it("opens the external sync account workflow", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    await screen.findByRole("dialog")
    fireEvent.click(await screen.findByRole("button", { name: "Sync" }))
    const syncSection = screen
      .getByRole("heading", { name: "Reader service sync" })
      .closest("section")
    expect(syncSection).not.toBeNull()
    fireEvent.click(within(syncSection!).getByRole("button", { name: "Add" }))
    expect(await screen.findByRole("heading", { name: "Add sync account" })).toBeInTheDocument()
    await screen.findByRole("option", { name: "FreshRSS" })
    expect(screen.getByRole("combobox", { name: "Provider" })).toHaveValue("freshrss")
    expect(screen.getByLabelText("Allow private network endpoint")).toBeInTheDocument()
  })

  it("configures iCloud Drive without making it exclusive with WebDAV", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    await screen.findByRole("dialog")
    fireEvent.click(await screen.findByRole("button", { name: "Sync" }))
    expect(await screen.findByRole("button", { name: /WebDAV/ })).toBeInTheDocument()
    fireEvent.click(await screen.findByRole("button", { name: /iCloud Drive/ }))
    const provider = await screen.findByRole("combobox", { name: "Provider" })
    expect(provider).toHaveValue("icloud")
    expect(screen.getByLabelText("iCloud file path")).toHaveValue("")
    expect(screen.getByRole("button", { name: "Add account" })).toBeEnabled()
  })

  it("opens the cloud sync add button with a serializable WebDAV provider", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    await screen.findByRole("dialog")
    fireEvent.click(await screen.findByRole("button", { name: "Sync" }))
    const cloudSection = screen
      .getByRole("heading", { name: "Library cloud sync" })
      .closest("section")
    expect(cloudSection).not.toBeNull()
    fireEvent.click(within(cloudSection!).getByRole("button", { name: "Add" }))
    expect(await screen.findByRole("combobox", { name: "Provider" })).toHaveValue("webdav")
  })

  it("requires privacy confirmation before configuring a remote AI provider", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    await screen.findByRole("dialog")
    fireEvent.click(await screen.findByRole("button", { name: "AI & language" }))
    const aiSection = screen.getByRole("heading", { name: "AI providers" }).closest("section")
    expect(aiSection).not.toBeNull()
    fireEvent.click(within(aiSection!).getByRole("button", { name: "Add" }))

    expect(await screen.findByRole("heading", { name: "Add AI provider" })).toBeInTheDocument()
    await screen.findByRole("option", { name: "OpenAI compatible" })
    expect(screen.getByRole("combobox", { name: "Provider" })).toHaveValue("openai_compatible")
    const submit = screen.getByRole("button", { name: "Add provider" })
    expect(submit).toBeDisabled()
    fireEvent.click(screen.getByLabelText("Article content may be sent to this provider"))
    expect(submit).toBeEnabled()
  })

  it("opens the library organization workflow", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    await screen.findByRole("dialog")
    fireEvent.click(await screen.findByRole("button", { name: "Library" }))
    fireEvent.click(await screen.findByRole("button", { name: "Manage" }))
    expect(await screen.findByRole("heading", { name: "Library organization" })).toBeInTheDocument()
    expect(screen.getByLabelText("Folder name")).toBeInTheDocument()
    expect(screen.getByLabelText("Tag name")).toBeInTheDocument()
    expect(screen.getByLabelText("Rule conditions JSON")).toBeInTheDocument()
    expect(screen.getByLabelText("Saved filter query JSON")).toBeInTheDocument()
  })

  it("switches the interface language and persists the choice", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    const language = screen.getByRole("combobox", { name: "Interface language" })
    fireEvent.change(language, { target: { value: "zh-CN" } })
    expect(useReaderStore.getState().locale).toBe("zh-CN")
    expect(screen.getByRole("heading", { name: "界面" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "关闭" }))
    expect(screen.getByRole("heading", { name: "今天" })).toBeInTheDocument()
    expect(localStorage.getItem("reflow-reader-preferences")).toContain("zh-CN")
  })

  it("switches between ReFlow light and dark skins", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Preferences" }))
    const theme = screen.getByRole("combobox", { name: "Theme" })
    fireEvent.change(theme, { target: { value: "dark" } })
    expect(useReaderStore.getState().theme).toBe("dark")
    expect(document.documentElement.dataset.theme).toBe("dark")
    fireEvent.change(theme, { target: { value: "light" } })
    expect(document.documentElement.dataset.theme).toBe("light")
  })

  it("resolves the system theme against the OS and keeps tracking it", async () => {
    // AppShell used to delete data-theme in system mode while no CSS rule reads
    // prefers-color-scheme, so a dark-OS user got the entire light palette.
    const os = stubMediaQueries({ [PREFERS_DARK]: true })
    renderApp()
    await screen.findByRole("button", { name: "Preferences" })
    expect(document.documentElement.dataset.theme).toBe("dark")
    act(() => os.set(PREFERS_DARK, false))
    expect(document.documentElement.dataset.theme).toBe("light")
  })

  it("integrates window controls into the ReFlow header on Windows", async () => {
    const desktopHost = window as Window & { _wails?: { environment: { OS: string } } }
    desktopHost._wails = { environment: { OS: "windows" } }
    document.documentElement.dataset.desktop = "windows"
    renderApp()

    expect(await screen.findByRole("button", { name: "Minimise window" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Maximise or restore window" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Close window" })).toBeInTheDocument()
  })

  it("opens articles in the shared three-pane desktop workspace", async () => {
    const entry = {
      id: "entry-1",
      feed_id: "feed-1",
      feed_title: "The Observatory",
      canonical_url: "https://example.com/story",
      title: "A quieter interface",
      author: "Mira Chen",
      summary: "Designing a calmer relationship with the open web.",
      published_at: "2026-07-17T12:00:00Z",
      discovered_at: "2026-07-17T12:00:00Z",
      lead_image_url: null,
      tag_ids: [],
      state: {
        is_read: true,
        is_starred: false,
        is_read_later: false,
        updated_at: "2026-07-17T12:00:00Z",
      },
    }
    const defaultFetch = vi.mocked(fetch).getMockImplementation()
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url =
        typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
      if (url.includes("/api/v1/entries/entry-1")) {
        return Promise.resolve(
          jsonResponse({ ...entry, sanitized_html: "<p>Article body</p>", readability_html: null }),
        )
      }
      return defaultFetch!(input, init)
    })

    renderApp()
    act(() => useReaderStore.getState().selectEntry(entry.id))
    await screen.findByRole("heading", { level: 1, name: entry.title })

    const shell = document.querySelector(".app-shell")
    const workspaceBody = document.querySelector(".workspace-body")
    expect(shell).toHaveClass("app-shell--reader-open")
    expect(workspaceBody).toContainElement(document.querySelector(".timeline"))
    expect(workspaceBody).toContainElement(document.querySelector(".reader"))
  })

  it("keeps the active library scope when closing an article", () => {
    renderApp()
    act(() => {
      useReaderStore.getState().setScope({ kind: "folder", id: "folder-design", title: "Design" })
      useReaderStore.getState().selectEntry("entry-1")
    })
    act(() => useReaderStore.getState().closeMobileReader())
    expect(useReaderStore.getState().scope).toEqual({
      kind: "folder",
      id: "folder-design",
      title: "Design",
    })
  })

  it("exposes keyboard accessible pane resize separators", async () => {
    renderApp()
    const separators = await screen.findAllByRole("separator")
    expect(separators).toHaveLength(2)
    expect(separators[0]).toHaveAttribute("aria-orientation", "vertical")
    expect(separators[0]).toHaveAttribute("aria-valuenow", "246")
    fireEvent.keyDown(separators[0]!, { key: "ArrowLeft" })
    expect(useReaderStore.getState().paneLayout.sidebarWidth).toBe(230)
  })

  it("uses saved filters and tags as timeline scopes", async () => {
    renderApp()
    fireEvent.click(await screen.findByRole("button", { name: "Favorites" }))
    expect(screen.getByRole("heading", { name: "Favorites" })).toBeInTheDocument()
    await waitFor(() => expect(requestedURLIncludes("state=starred")).toBe(true))

    fireEvent.click(screen.getByRole("button", { name: "Research" }))
    expect(screen.getByRole("heading", { name: "Research" })).toBeInTheDocument()
    await waitFor(() => expect(requestedURLIncludes("tag_id=tag-research")).toBe(true))
  })

  it("exposes library scopes from mobile navigation", async () => {
    renderApp()
    await screen.findByRole("button", { name: "Favorites" })
    fireEvent.click(await screen.findByRole("button", { name: "Library" }))
    const dialog = screen.getByRole("dialog", { name: "Library" })
    fireEvent.click(await within(dialog).findByRole("button", { name: "Favorites" }))
    expect(screen.getByRole("heading", { name: "Favorites" })).toBeInTheDocument()
  })
})

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

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function requestedURLIncludes(value: string) {
  return vi.mocked(fetch).mock.calls.some(([input]) => requestURL(input).includes(value))
}

function requestedLibraryURLs() {
  return vi
    .mocked(fetch)
    .mock.calls.map(([input]) => requestURL(input))
    .filter((url) => !url.includes("/api/v1/status"))
}

function requestURL(input: RequestInfo | URL) {
  return typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
}
