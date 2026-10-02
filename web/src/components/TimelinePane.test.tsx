import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import type { Entry, LibraryScope, Subscription } from "../api/types"
import { useReaderStore } from "../store/reader"
import { TimelinePane } from "./TimelinePane"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

afterEach(() => cleanup())

function makeEntry(overrides: Partial<Entry> = {}): Entry {
  return {
    id: "entry-1",
    feed_id: "feed-1",
    feed_title: "经济研究-CNKI",
    canonical_url: "https://example.com/entry",
    title: "数据流动、数据价值实现与福利效应",
    author: null,
    summary: null,
    published_at: "2026-06-20T00:00:00Z",
    discovered_at: "2026-06-20T00:00:00Z",
    lead_image_url: null,
    tag_ids: [],
    state: {
      is_read: false,
      is_starred: false,
      is_read_later: false,
      updated_at: "2026-06-20T00:00:00Z",
    },
    ...overrides,
  }
}

function makeSubscription(overrides: Partial<Subscription> = {}): Subscription {
  return {
    id: "subscription-1",
    feed_id: "feed-1",
    folder_id: null,
    position: 0,
    title: "经济研究-CNKI",
    icon_url: "https://example.com/icon.png",
    feed_url: "https://example.com/feed.xml",
    site_url: "https://example.com",
    unread_count: 1,
    failure_count: 0,
    last_error_code: null,
    last_error_message: null,
    last_success_at: null,
    view_mode: "standard",
    refresh_policy: "inherit",
    refresh_interval_minutes: 0,
    hide_from_timeline: false,
    created_at: "2026-06-20T00:00:00Z",
    updated_at: "2026-06-20T00:00:00Z",
    ...overrides,
  }
}

function renderPane(options: {
  scope: LibraryScope
  entries: Entry[]
  subscriptions: Subscription[]
  viewMode?: Subscription["view_mode"]
}) {
  return render(
    <TimelinePane
      scope={options.scope}
      entries={options.entries}
      subscriptions={options.subscriptions}
      selectedEntryID={null}
      viewMode={options.viewMode ?? "standard"}
      isLoading={false}
      isFetchingNext={false}
      hasNextPage={false}
      error={null}
      markReadPending={false}
      refreshPending={false}
      onScopeChange={vi.fn()}
      onSelect={vi.fn()}
      onAdd={vi.fn()}
      onRetry={vi.fn()}
      onLoadMore={vi.fn()}
      onMarkAllRead={vi.fn()}
      onRefresh={vi.fn()}
      onToggleStar={vi.fn()}
    />,
  )
}

const feedScope: LibraryScope = { kind: "feed", id: "feed-1", title: "经济研究-CNKI" }

it("applies a per-feed view mode over the global preference", () => {
  const { container } = renderPane({
    scope: feedScope,
    entries: [makeEntry()],
    subscriptions: [makeSubscription({ view_mode: "compact" })],
    viewMode: "standard",
  })
  expect(container.querySelector(".timeline-entry--compact")).not.toBeNull()
  expect(container.querySelector(".timeline-entry--standard")).toBeNull()
})

it("falls back to the global view mode outside a feed scope", () => {
  const { container } = renderPane({
    scope: { kind: "all", title: "All feeds" },
    entries: [makeEntry()],
    subscriptions: [makeSubscription({ view_mode: "compact" })],
    viewMode: "card",
  })
  expect(container.querySelector(".timeline-entry--card")).not.toBeNull()
})

it("keeps the mark-all-read action out of the filter group", () => {
  const { container } = renderPane({
    scope: { kind: "all", title: "All feeds" },
    entries: [makeEntry()],
    subscriptions: [makeSubscription()],
  })
  // 读屏进到这个 group 念出来的必须是"三条筛选"，而不是"四条筛选其中一条不是筛选"。
  const group = container.querySelector('[role="group"][aria-label="Article filters"]')
  expect(group?.querySelectorAll("button")).toHaveLength(3)
  const markRead = container.querySelector<HTMLButtonElement>(".timeline-mark-read")
  expect(markRead).not.toBeNull()
  expect(group?.contains(markRead as Node)).toBe(false)
  expect(
    container.querySelector(".timeline-filterbar__actions")?.contains(markRead as Node),
  ).toBe(true)
  // ≤620 那一档把文字藏掉只留 38px 方盒：没有图标它就是一个空盒子。
  expect(markRead?.querySelector("svg")).not.toBeNull()
  // WCAG 2.5.3：无障碍名字要含得住看得见的那几个字，所以两者必须是同一句。
  expect(markRead?.textContent).toBe("Mark all read")
  expect(markRead?.getAttribute("aria-label")).toBe("Mark all read")
})

it("omits the image element when an entry has no lead image", () => {
  const { container } = renderPane({
    scope: feedScope,
    entries: [makeEntry()],
    subscriptions: [makeSubscription()],
  })
  // Journal entries have no per-entry image; the feed icon must not stand in.
  expect(container.querySelector(".timeline-entry__image")).toBeNull()
  expect(container.querySelector(".timeline-entry--has-image")).toBeNull()
})

it("renders the lead image when one is present", () => {
  const { container } = renderPane({
    scope: feedScope,
    entries: [makeEntry({ lead_image_url: "https://example.com/lead.jpg" })],
    subscriptions: [makeSubscription()],
  })
  expect(container.querySelector(".timeline-entry--has-image")).not.toBeNull()
  expect(container.querySelector(".timeline-entry__image img")?.getAttribute("src")).toBe(
    "https://example.com/lead.jpg",
  )
})

it("cleans semicolon-separated authors", () => {
  renderPane({
    scope: feedScope,
    entries: [makeEntry({ author: "李世杰;吴楚豪;" })],
    subscriptions: [makeSubscription()],
  })
  expect(screen.getByText("李世杰 · 吴楚豪")).toBeTruthy()
})

it("shows the feed icon only when the scope spans feeds", () => {
  const { container: feedScoped } = renderPane({
    scope: feedScope,
    entries: [makeEntry()],
    subscriptions: [makeSubscription()],
  })
  expect(feedScoped.querySelector(".timeline-entry__feed-icon")).toBeNull()

  const { container: allScoped } = renderPane({
    scope: { kind: "all", title: "All feeds" },
    entries: [makeEntry()],
    subscriptions: [makeSubscription()],
  })
  expect(allScoped.querySelector(".timeline-entry__feed-icon")).not.toBeNull()
})
