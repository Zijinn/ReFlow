import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import type { AIProfile, EntryDetail, Tag } from "../api/types"
import { useReaderStore } from "../store/reader"
import { ReaderPane } from "./ReaderPane"

afterEach(() => cleanup())

const aiProfile: AIProfile = {
  id: "profile-1",
  provider: "openai_compatible",
  name: "Test AI",
  endpoint: "https://ai.example.com",
  model: "test-model",
  enabled: true,
  allow_private_network: false,
  remote_content_approved: true,
  is_default: true,
  last_used_at: null,
  last_error_code: null,
  last_error_message: null,
  created_at: "2026-07-17T00:00:00Z",
  updated_at: "2026-07-17T00:00:00Z",
}

const detail: EntryDetail = {
  id: "entry-1",
  feed_id: "feed-1",
  feed_title: "ReFlow Notes",
  canonical_url: "https://example.com/entry",
  title: "A tagged article",
  author: null,
  summary: null,
  published_at: "2026-07-17T00:00:00Z",
  discovered_at: "2026-07-17T00:00:00Z",
  lead_image_url: null,
  tag_ids: ["tag-important"],
  state: {
    is_read: true,
    is_starred: false,
    is_read_later: false,
    updated_at: "2026-07-17T00:00:00Z",
  },
  sanitized_html: "<p>Article body</p>",
  readability_html: null,
}

const tags: Tag[] = [
  {
    id: "tag-important",
    name: "Important",
    color: "#b3413a",
    position: 0,
    created_at: "2026-07-17T00:00:00Z",
  },
  {
    id: "tag-research",
    name: "Research",
    color: "#167a72",
    position: 1,
    created_at: "2026-07-17T00:00:00Z",
  },
]

it("shows and updates article tags", () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const onTagsChange = vi.fn()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={detail}
        detail={detail}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[]}
        tags={tags}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={onTagsChange}
        onFetchReadability={vi.fn()}
        onConfigureAI={vi.fn()}
      />
    </QueryClientProvider>,
  )

  fireEvent.click(screen.getByRole("button", { name: "Edit article tags" }))
  expect(screen.getByRole("checkbox", { name: "Important" })).toBeChecked()
  fireEvent.click(screen.getByRole("checkbox", { name: "Research" }))
  expect(onTagsChange).toHaveBeenCalledWith("entry-1", ["tag-important", "tag-research"])
})

it("shows cached AI title translation and summary in the reading header", () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={{
          ...detail,
          ai_translated_title: "一篇带标签的文章",
          ai_summary: "这是缓存的 AI 摘要。",
        }}
        detail={{
          ...detail,
          ai_translated_title: "一篇带标签的文章",
          ai_summary: "这是缓存的 AI 摘要。",
        }}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[]}
        tags={[]}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={vi.fn()}
        onFetchReadability={vi.fn()}
        onConfigureAI={vi.fn()}
      />
    </QueryClientProvider>,
  )
  expect(screen.getByText("一篇带标签的文章")).toBeInTheDocument()
  expect(screen.getByText("AI summary")).toBeInTheDocument()
  expect(screen.getByText("这是缓存的 AI 摘要。")).toBeInTheDocument()
})

it("adjusts reading typography from the right-side inspector", () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  renderReader()

  fireEvent.click(screen.getByRole("button", { name: "Reading appearance" }))
  fireEvent.click(screen.getByRole("button", { name: "Sans serif" }))
  fireEvent.change(screen.getByRole("slider", { name: "Text size" }), {
    target: { value: "22" },
  })

  expect(useReaderStore.getState().readerAppearance).toMatchObject({
    fontFamily: "sans",
    fontSize: 22,
  })
  expect(screen.getByRole("article", { name: "Reader" })).toHaveStyle({
    "--reader-content-size": "22px",
  })
})

it("writes to Zotero only after the save button is clicked", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/integrations/zotero/status")) {
      return Promise.resolve(
        jsonResponse({
          available: true,
          editable: true,
          library_id: "1",
          library_name: "My Library",
          collection_id: "227",
          collection_name: "Research",
        }),
      )
    }
    if (url.includes("/entries/entry-1/zotero") && init?.method === "POST") {
      return Promise.resolve(
        jsonResponse({
          saved: true,
          duplicate: false,
          target: { available: true, editable: true },
          export: {
            entry_id: "entry-1",
            metadata_fingerprint: "hash",
            exported_at: "2026-07-24T00:00:00Z",
            updated_at: "2026-07-24T00:00:00Z",
          },
        }),
      )
    }
    if (url.includes("/entries/entry-1/zotero")) {
      return Promise.resolve(jsonResponse({ saved: false }))
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })

  renderReader()
  await screen.findByRole("button", { name: "Save to Zotero" })
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false)

  fireEvent.click(screen.getByRole("button", { name: "Save to Zotero" }))
  await waitFor(() =>
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(true),
  )
  expect(await screen.findByRole("button", { name: "Saved to Zotero" })).toBeDisabled()

  fetchMock.mockRestore()
})

it("restores saved highlights and notes for the current article", async () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/entries/entry-1/annotations")) {
      return Promise.resolve(
        jsonResponse({
          items: [
            {
              id: "annotation-1",
              entry_id: detail.id,
              quote: "Article body",
              prefix: "",
              suffix: "",
              style: "highlight",
              note: "Return to this idea",
              created_at: "2026-07-18T00:00:00Z",
              updated_at: "2026-07-18T00:00:00Z",
            },
          ],
        }),
      )
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })
  renderReader()

  await waitFor(() =>
    expect(document.querySelector(".reader-annotation--highlight")).toHaveTextContent(
      "Article body",
    ),
  )
  expect(document.querySelector(".reader-annotation--highlight")).toHaveAttribute(
    "title",
    "Return to this idea",
  )
  fetchMock.mockRestore()
})

it("creates a persistent highlight from selected article text", async () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  let postedBody: Record<string, unknown> | null = null
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/entries/entry-1/annotations") && init?.method === "POST") {
      postedBody = JSON.parse(init.body as string) as Record<string, unknown>
      return Promise.resolve(
        jsonResponse(
          {
            id: "annotation-new",
            entry_id: detail.id,
            quote: postedBody.quote,
            prefix: postedBody.prefix ?? "",
            suffix: postedBody.suffix ?? "",
            style: postedBody.style,
            note: postedBody.note ?? "",
            created_at: "2026-08-16T00:00:00Z",
            updated_at: "2026-08-16T00:00:00Z",
          },
          201,
        ),
      )
    }
    if (url.includes("/entries/entry-1/annotations")) {
      return Promise.resolve(jsonResponse({ items: [] }))
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })
  renderReader()

  const paragraph = screen.getByText("Article body")
  const text = paragraph.firstChild
  expect(text).not.toBeNull()
  const range = document.createRange()
  range.setStart(text!, 0)
  range.setEnd(text!, 7)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
  fireEvent.pointerUp(paragraph)

  fireEvent.click(await screen.findByRole("button", { name: "Highlight" }))
  await waitFor(() => expect(postedBody).not.toBeNull())
  expect(postedBody).toMatchObject({
    quote: "Article",
    style: "highlight",
  })
  await waitFor(() =>
    expect(document.querySelector(".reader-annotation--highlight")).toHaveTextContent("Article"),
  )
  fetchMock.mockRestore()
})

it("keeps a failed annotation selection and retries it successfully", async () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  let attempts = 0
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/entries/entry-1/annotations") && init?.method === "POST") {
      attempts++
      if (attempts === 1) {
        return Promise.resolve(jsonResponse({ detail: "Annotation unavailable" }, 503))
      }
      const posted = JSON.parse(init.body as string) as Record<string, string>
      return Promise.resolve(
        jsonResponse(
          {
            id: "annotation-retried",
            entry_id: detail.id,
            quote: posted.quote,
            prefix: posted.prefix,
            suffix: posted.suffix,
            style: posted.style,
            note: posted.note,
            created_at: "2026-08-16T00:00:00Z",
            updated_at: "2026-08-16T00:00:00Z",
          },
          201,
        ),
      )
    }
    if (url.includes("/entries/entry-1/annotations")) {
      return Promise.resolve(jsonResponse({ items: [] }))
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })
  renderReader()

  selectArticleText("Article body", 0, 7)
  fireEvent.click(await screen.findByRole("button", { name: "Add note" }))
  fireEvent.change(screen.getByRole("textbox", { name: "Add note" }), {
    target: { value: "Keep this draft" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Save note" }))

  expect(await screen.findByRole("alert")).toHaveTextContent("Annotation unavailable")
  expect(screen.getByRole("textbox", { name: "Add note" })).toHaveValue("Keep this draft")
  expect(screen.getByRole("toolbar", { name: "Annotate selected text" })).toBeInTheDocument()

  fireEvent.click(screen.getByRole("button", { name: "Save note" }))
  await waitFor(() => expect(attempts).toBe(2))
  await waitFor(() =>
    expect(
      screen.queryByRole("toolbar", { name: "Annotate selected text" }),
    ).not.toBeInTheDocument(),
  )
  expect(document.querySelector(".reader-annotation--highlight")).toHaveTextContent("Article")
  fetchMock.mockRestore()
})

it("runs AI quick actions from the toolbar menu without opening the panel", async () => {
  let summaryPosted = false
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/entries/entry-1/ai/summary") && init?.method === "POST") {
      summaryPosted = true
      return Promise.resolve(
        jsonResponse({
          result: {
            id: "result-1",
            ai_profile_id: "profile-1",
            entry_id: "entry-1",
            operation: "summary",
            language: "English",
            input_hash: "hash",
            result_text: "A concise summary.",
            usage: { total_tokens: 12 },
            created_at: "2026-08-15T00:00:00Z",
          },
          job: null,
        }),
      )
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const onToggleAI = vi.fn()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={detail}
        detail={detail}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[aiProfile]}
        tags={[]}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={vi.fn()}
        onFetchReadability={vi.fn()}
        onConfigureAI={vi.fn()}
        onToggleAI={onToggleAI}
      />
    </QueryClientProvider>,
  )

  fireEvent.click(screen.getByRole("button", { name: "AI assistant" }))
  fireEvent.click(screen.getByRole("menuitem", { name: "Summary" }))
  await waitFor(() => expect(summaryPosted).toBe(true))
  expect(onToggleAI).not.toHaveBeenCalled()
  fetchMock.mockRestore()
})

it("shows key point results in a toast instead of opening the panel", async () => {
  const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url
    if (url.includes("/entries/entry-1/ai/key-points") && init?.method === "POST") {
      return Promise.resolve(
        jsonResponse({
          result: {
            id: "result-2",
            ai_profile_id: "profile-1",
            entry_id: "entry-1",
            operation: "key_points",
            language: "English",
            input_hash: "hash",
            result_text: "Point one\nPoint two",
            usage: { total_tokens: 20 },
            created_at: "2026-08-15T00:00:00Z",
          },
          job: null,
        }),
      )
    }
    return Promise.resolve(jsonResponse({ items: [] }))
  })
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const onToggleAI = vi.fn()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={detail}
        detail={detail}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[aiProfile]}
        tags={[]}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={vi.fn()}
        onFetchReadability={vi.fn()}
        onConfigureAI={vi.fn()}
        onToggleAI={onToggleAI}
      />
    </QueryClientProvider>,
  )

  fireEvent.click(screen.getByRole("button", { name: "AI assistant" }))
  fireEvent.click(screen.getByRole("menuitem", { name: "Key points" }))
  expect(await screen.findByRole("status")).toHaveTextContent("Point one")
  expect(onToggleAI).not.toHaveBeenCalled()
  fetchMock.mockRestore()
})

it("opens the AI panel only from the chat menu item", () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const onToggleAI = vi.fn()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={detail}
        detail={detail}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[aiProfile]}
        tags={[]}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={vi.fn()}
        onFetchReadability={vi.fn()}
        onConfigureAI={vi.fn()}
        onToggleAI={onToggleAI}
      />
    </QueryClientProvider>,
  )

  fireEvent.click(screen.getByRole("button", { name: "AI assistant" }))
  expect(screen.getByRole("menuitem", { name: "Summary" })).toBeInTheDocument()
  expect(onToggleAI).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole("menuitem", { name: "Chat" }))
  expect(onToggleAI).toHaveBeenCalledTimes(1)
})

it("asks for AI configuration when no profile is enabled", () => {
  useReaderStore.setState({
    locale: "en-US",
    theme: "system",
    readerAppearance: { fontFamily: "serif", fontSize: 19, lineHeight: 1.8 },
    annotations: [],
  })
  const onConfigureAI = vi.fn()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={detail}
        detail={detail}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[]}
        tags={[]}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={vi.fn()}
        onFetchReadability={vi.fn()}
        onConfigureAI={onConfigureAI}
        onToggleAI={vi.fn()}
      />
    </QueryClientProvider>,
  )

  fireEvent.click(screen.getByRole("button", { name: "AI assistant" }))
  fireEvent.click(screen.getByRole("menuitem", { name: "Summary" }))
  expect(onConfigureAI).toHaveBeenCalledTimes(1)
})

function selectArticleText(textContent: string, start: number, end: number) {
  const paragraph = screen.getByText(textContent)
  const text = paragraph.firstChild
  expect(text).not.toBeNull()
  const range = document.createRange()
  range.setStart(text!, start)
  range.setEnd(text!, end)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
  fireEvent.pointerUp(paragraph)
}

function renderReader() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ReaderPane
        summary={detail}
        detail={detail}
        isLoading={false}
        error={null}
        mutationPending={false}
        readabilityPending={false}
        aiProfiles={[]}
        tags={[]}
        onBack={vi.fn()}
        onRetry={vi.fn()}
        onStateChange={vi.fn()}
        onTagsChange={vi.fn()}
        onFetchReadability={vi.fn()}
        onConfigureAI={vi.fn()}
      />
    </QueryClientProvider>,
  )
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}
