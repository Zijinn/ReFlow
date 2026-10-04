import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import App from "../App"
import type { AIProfile } from "../api/types"
import { stubMediaQueries } from "../test/media-queries"
import { useReaderStore } from "../store/reader"

// 编辑/保存一个 AI 提供商要经过「偏好设置卸载 → 编辑框 → 保存后重新挂载」的往返，
// 栏位若还是面板自己的 state，回来就会被重置成「界面」。缺陷只在父组件重挂载子组件
// 时出现，单独渲染 PreferencesDialog 测不到，所以整条路径从 AppShell 走真实应用。

const DESKTOP_WIDTH = 1280

const gateway: AIProfile = {
  id: "profile-1",
  provider: "openai_compatible",
  name: "Lab gateway",
  endpoint: "https://gateway.example.edu/v1",
  model: "gpt-4.1-mini",
  enabled: true,
  allow_private_network: false,
  remote_content_approved: true,
  is_default: true,
  last_used_at: null,
  last_error_code: null,
  last_error_message: null,
  created_at: "2026-07-22T00:00:00Z",
  updated_at: "2026-07-22T00:00:00Z",
}

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
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    writable: true,
    value: DESKTOP_WIDTH,
  })
  vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url = requestURL(input)
    const method = (init?.method ?? "GET").toUpperCase()
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
    if (url.includes("/api/v1/ai/profiles")) {
      return Promise.resolve(jsonResponse(method === "PATCH" ? gateway : { items: [gateway] }))
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
    if (url.includes("/api/v1/ai/usage")) {
      return Promise.resolve(jsonResponse({ input_tokens: 0, output_tokens: 0, total_tokens: 0 }))
    }
    if (url.includes("/api/v1/preferences")) {
      return Promise.resolve(jsonResponse({ items: {} }))
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

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function requestURL(input: string | URL | globalThis.Request) {
  return typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
}

// 面板的对话框名就是当前栏位的标题，所以「栏位有没有丢」在这里是可读的。
function aiTabButton() {
  return screen.getByRole("button", { name: "AI & language" })
}

async function openPreferencesOnAITab() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>,
  )
  await screen.findByRole("heading", { name: "Today" })
  fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
  await screen.findByRole("dialog", { name: "Interface" })
  fireEvent.click(aiTabButton())
  await screen.findByRole("heading", { name: "AI providers" })
}

describe("Preferences tab survives the AI provider round-trip", () => {
  it("stays on the AI tab after editing and saving a provider", async () => {
    await openPreferencesOnAITab()
    expect(aiTabButton()).toHaveAttribute("aria-current", "page")

    fireEvent.click(screen.getByRole("button", { name: "Edit Lab gateway" }))
    const editor = await screen.findByRole("dialog", { name: "Edit AI provider" })
    fireEvent.click(within(editor).getByRole("button", { name: "Save changes" }))

    // 编辑框确实提交过，否则「面板回来了」也可能只是什么都没发生。
    await waitFor(() =>
      expect(
        vi
          .mocked(fetch)
          .mock.calls.some(
            ([input, init]) =>
              requestURL(input).includes("/api/v1/ai/profiles/profile-1") &&
              init?.method === "PATCH",
          ),
      ).toBe(true),
    )

    const preferences = await screen.findByRole("dialog", { name: "AI & language" })
    expect(within(preferences).getByRole("heading", { name: "AI providers" })).toBeInTheDocument()
    expect(aiTabButton()).toHaveAttribute("aria-current", "page")
    expect(aiTabButton()).toHaveClass("preferences-nav__item--active")
  })

  it("falls back to the interface tab once the dialog is closed for real", async () => {
    await openPreferencesOnAITab()

    fireEvent.click(
      within(screen.getByRole("dialog", { name: "AI & language" })).getByRole("button", {
        name: "Close",
      }),
    )
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
    const reopened = await screen.findByRole("dialog", { name: "Interface" })
    expect(within(reopened).getByRole("button", { name: "AI & language" })).not.toHaveAttribute(
      "aria-current",
    )
  })
})
