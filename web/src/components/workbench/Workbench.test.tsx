import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchKind, ResearchPaper } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { useToastStore } from "../../store/toast"
import { Workbench } from "./Workbench"

vi.mock("../../api/client", () => ({
  createResearchPaper: vi.fn(),
  deleteResearchPaper: vi.fn(),
  fetchResearchCitation: vi.fn(),
  listPreferences: vi.fn(() => Promise.resolve({ items: {} })),
  listResearchPapers: vi.fn(),
  moveResearchPaper: vi.fn(),
  putPreference: vi.fn(),
  reorderResearchPapers: vi.fn(),
  updateResearchPaper: vi.fn(),
}))

import * as api from "../../api/client"

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "r-1",
    kind: "research",
    position: 0,
    title: "Growth Regression",
    authors: ["Zhang San"],
    keywords: [],
    file_path: "",
    next_action: "",
    notes: "",
    research_area: "",
    status: "",
    priority: "Medium",
    target_journal: "",
    stages: [{ name: "Empirics", done: false, children: [] }],
    current_journal: "",
    submission_date: "",
    manuscript_id: "",
    submission_count: 0,
    target_level: "",
    editor: "",
    deadline: "",
    history: [],
    abstract: "",
    journal: "",
    language: "",
    year: "",
    volume: "",
    issue: "",
    pages: "",
    doi: "",
    citations: null,
    citation_source: "",
    citation_updated_at: "",
    last_updated: "2026-09-01",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

const papersByKind: Record<ResearchKind, ResearchPaper[]> = {
  research: [paper()],
  submitted: [],
  published: [],
}

function renderWorkbench() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Workbench />
    </QueryClientProvider>,
  )
}

// Scoped to the tab strip: the dashboard's stat cards carry the same labels and
// are themselves navigation to those tabs.
function goToTab(name: RegExp) {
  const nav = screen.getByRole("navigation")
  fireEvent.click(within(nav).getByRole("button", { name }))
}

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US" })
  useToastStore.setState({ toasts: [] })
  papersByKind.research = [paper()]
  papersByKind.submitted = []
  papersByKind.published = []
  vi.mocked(api.listResearchPapers).mockImplementation((kind) =>
    Promise.resolve({ items: papersByKind[kind] }),
  )
  vi.mocked(api.createResearchPaper).mockResolvedValue(paper({ id: "new-1" }))
  vi.mocked(api.deleteResearchPaper).mockResolvedValue(undefined)
  vi.mocked(api.updateResearchPaper).mockResolvedValue(paper())
  vi.mocked(api.reorderResearchPapers).mockResolvedValue(undefined)
  vi.mocked(api.moveResearchPaper).mockImplementation((id, kind) =>
    Promise.resolve({ ...paper({ kind }), id }),
  )
  vi.mocked(api.putPreference).mockResolvedValue({})
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Workbench load errors", () => {
  it("shows a retryable error state instead of silently empty lists", async () => {
    vi.mocked(api.listResearchPapers).mockRejectedValue(new Error("offline"))
    renderWorkbench()
    expect(await screen.findByRole("alert")).toHaveTextContent(/Could not load your papers/)
    const retry = screen.getByRole("button", { name: "Retry" })
    const before = vi.mocked(api.listResearchPapers).mock.calls.length
    fireEvent.click(retry)
    await waitFor(() =>
      expect(vi.mocked(api.listResearchPapers).mock.calls.length).toBeGreaterThan(before),
    )
  })

  it("renders papers on success", async () => {
    renderWorkbench()
    await waitFor(() => expect(screen.getAllByText("Growth Regression").length).toBeGreaterThan(0))
  })
})

describe("Workbench table column contract", () => {
  // The tables run on `table-layout: fixed`, which only keeps its geometry while
  // every column declares its width on the header cell. A bare <th> falls back to
  // content sizing, so editing a title or picking a longer status re-flows the
  // whole table — the horizontal jump the fixed layout exists to prevent.
  // Body cells must carry the same class, because narrow screens shed whole
  // columns with `display: none` on `.wb-col-*`: a header-only class would hide
  // the label and leave its cells misaligned in the grid.
  it("gives every column of every paper table an explicit width class", async () => {
    papersByKind.submitted = [paper({ id: "s-1", kind: "submitted", title: "Under Review" })]
    papersByKind.published = [paper({ id: "q-1", kind: "published", title: "Published" })]
    renderWorkbench()
    const column = (cell: Element) => cell.className.split(/\s+/)[0] ?? ""
    for (const tab of [/Working papers/, /Submissions/, /Publications/]) {
      goToTab(tab)
      await waitFor(() => expect(document.querySelector(".wb-table thead")).not.toBeNull())
      const headers = Array.from(document.querySelectorAll(".wb-table thead th"))
      expect(headers.length).toBeGreaterThan(0)
      const headerCells = headers.map(column)
      expect(headerCells.every((name) => name.startsWith("wb-col-"))).toBe(true)
      expect(document.querySelector(".wb-table")?.className).toMatch(/wb-table--/)
      const rows = Array.from(document.querySelectorAll(".wb-table tbody .wb-row"))
      expect(rows.length).toBe(1)
      rows.forEach((row) => expect(Array.from(row.children).map(column)).toEqual(headerCells))
    }
  })
})

describe("Workbench mutation failures toast every write", () => {
  it("create failure", async () => {
    vi.mocked(api.createResearchPaper).mockRejectedValue(new Error("boom"))
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.click(await screen.findByRole("button", { name: /Add paper/ }))
    await waitFor(() =>
      expect(
        useToastStore
          .getState()
          .toasts.some((item) => /Could not add the paper/.test(item.message)),
      ).toBe(true),
    )
  })

  it("delete failure", async () => {
    vi.mocked(api.deleteResearchPaper).mockRejectedValue(new Error("boom"))
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.click(await screen.findByRole("button", { name: "Delete: Growth Regression" }))
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }))
    await waitFor(() =>
      expect(
        useToastStore
          .getState()
          .toasts.some((item) => /Could not delete the paper/.test(item.message)),
      ).toBe(true),
    )
  })

  it("move failure", async () => {
    vi.mocked(api.moveResearchPaper).mockRejectedValue(new Error("boom"))
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.click(await screen.findByRole("button", { name: /Move to submissions/ }))
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }))
    await waitFor(() =>
      expect(
        useToastStore
          .getState()
          .toasts.some((item) => /Could not move the paper/.test(item.message)),
      ).toBe(true),
    )
  })

  it("update failure", async () => {
    vi.mocked(api.updateResearchPaper).mockRejectedValue(new Error("boom"))
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.doubleClick(await screen.findByText("Growth Regression"))
    const input = screen.getByDisplayValue("Growth Regression")
    fireEvent.change(input, { target: { value: "Renamed" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() =>
      expect(
        useToastStore
          .getState()
          .toasts.some((item) => /Could not update the paper/.test(item.message)),
      ).toBe(true),
    )
  })

  it("crossref email save failure", async () => {
    vi.mocked(api.putPreference).mockRejectedValue(new Error("boom"))
    renderWorkbench()
    goToTab(/Publications/)
    const input = await screen.findByLabelText("Crossref contact email")
    fireEvent.change(input, { target: { value: "me@lab.org" } })
    fireEvent.blur(input)
    await waitFor(() =>
      expect(
        useToastStore
          .getState()
          .toasts.some((item) => /Could not save the Crossref email/.test(item.message)),
      ).toBe(true),
    )
  })
})

describe("Workbench paper move", () => {
  it("moves via the dedicated endpoint without locally clearing stages", async () => {
    // The server keeps stages/history on move; the frontend must not "help"
    // by PATCHing stripped fields afterwards.
    vi.mocked(api.moveResearchPaper).mockImplementation((id, kind) => {
      const moved = { ...paper({ kind }), id }
      papersByKind.research = []
      papersByKind.submitted = [moved]
      return Promise.resolve(moved)
    })
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.click(await screen.findByRole("button", { name: /Move to submissions/ }))
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.moveResearchPaper).toHaveBeenCalledWith("r-1", "submitted"))
    await waitFor(() => expect(api.updateResearchPaper).not.toHaveBeenCalled())
    goToTab(/Submissions/)
    await screen.findByText("Growth Regression")
    // And the refetched record still carries its stages untouched.
    expect(papersByKind.submitted[0]!.stages).toEqual([
      { name: "Empirics", done: false, children: [] },
    ])
  })
})

describe("Workbench navigation", () => {
  it("keeps the tab bar on top of the content, not in a side rail", async () => {
    renderWorkbench()
    const nav = await screen.findByRole("navigation", { name: "Research Workspace" })
    expect(nav.querySelectorAll(".wb-nav-item")).toHaveLength(5)
    expect(document.querySelector(".wb-main")?.firstElementChild).toBe(nav)
  })

  it("jumps from a calendar deadline to the expanded submission row", async () => {
    papersByKind.submitted = [
      paper({
        id: "s-1",
        kind: "submitted",
        title: "Submitted One",
        deadline: new Date().toISOString().slice(0, 10),
      }),
    ]
    renderWorkbench()
    goToTab(/Deadline calendar/)
    fireEvent.click(await screen.findByRole("button", { name: /Submitted One/ }))
    // The click switches to the submissions table and opens that row.
    expect(await screen.findByRole("button", { name: "Collapse details" })).toBeInTheDocument()
    expect(screen.getByText("Submitted One")).toBeInTheDocument()
    expect(document.querySelector('tr[data-paper-id="s-1"]')).not.toBeNull()
  })
})

describe("Workbench offline policy", () => {
  it("shows the offline banner and blocks writes with a hint", async () => {
    Object.defineProperty(window.navigator, "onLine", {
      configurable: true,
      get: () => false,
    })
    try {
      renderWorkbench()
      await waitFor(() =>
        expect(screen.getAllByText("Growth Regression").length).toBeGreaterThan(0),
      )
      goToTab(/Working papers/)
      expect(
        await screen.findByText(
          "Offline: the research workspace needs a connection to save edits.",
        ),
      ).toBeInTheDocument()
      const add = screen.getByRole("button", { name: /Add paper/ })
      expect(add).toBeDisabled()
      fireEvent.click(add)
      expect(api.createResearchPaper).not.toHaveBeenCalled()
    } finally {
      delete (window.navigator as { onLine?: boolean }).onLine
    }
  })
})
