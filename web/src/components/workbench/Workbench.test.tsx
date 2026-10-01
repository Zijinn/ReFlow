import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type {
  AIChatSession,
  AIProfile,
  Job,
  ResearchKind,
  ResearchPaper,
} from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { useToastStore } from "../../store/toast"
import { Workbench, type WorkbenchProps } from "./Workbench"
import { formatDeadline } from "./utils"

// The real module is spread so `APIError` and friends stay usable; every
// network entry point is replaced, so a workbench test can never reach fetch.
vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    cancelJob: vi.fn(),
    createResearchPaper: vi.fn(),
    createResearchTag: vi.fn(),
    deleteResearchPaper: vi.fn(),
    deleteResearchTag: vi.fn(),
    fetchResearchCitation: vi.fn(),
    getAIChat: vi.fn(),
    getJob: vi.fn(),
    listAIResults: vi.fn(),
    listPreferences: vi.fn(() => Promise.resolve({ items: {} })),
    listResearchPapers: vi.fn(),
    listResearchTags: vi.fn(),
    moveResearchPaper: vi.fn(),
    putPreference: vi.fn(),
    reorderResearchPapers: vi.fn(),
    reorderResearchTags: vi.fn(),
    runAIOperation: vi.fn(),
    startAIChat: vi.fn(),
    startAIDailyDigest: vi.fn(),
    startAILibraryChat: vi.fn(),
    startAIPaperChat: vi.fn(),
    updateResearchPaper: vi.fn(),
    updateResearchTag: vi.fn(),
  }
})

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
    tag_ids: [],
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

function aiProfile(overrides: Partial<AIProfile> = {}): AIProfile {
  return {
    id: "ai-1",
    provider: "openai_compatible",
    name: "Lab endpoint",
    endpoint: "https://example.test/v1",
    model: "gpt-test",
    enabled: true,
    allow_private_network: false,
    remote_content_approved: true,
    is_default: true,
    last_used_at: null,
    last_error_code: null,
    last_error_message: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

function job(state: Job["state"] = "succeeded"): Job {
  return {
    id: "job-1",
    kind: "ai.research",
    state,
    progress_current: 0,
    progress_total: 0,
    scheduled_at: "",
    started_at: null,
    finished_at: null,
    error_code: null,
    error_message: null,
    created_at: "",
    updated_at: "",
  }
}

function chatSession(content: string): AIChatSession {
  return {
    id: "sess-1",
    ai_profile_id: "ai-1",
    entry_id: null,
    title: "",
    messages: [
      {
        id: "msg-1",
        role: "assistant",
        content,
        status: "completed",
        usage: {},
        created_at: "",
      },
    ],
    created_at: "",
    updated_at: "",
  }
}

// A submitted paper with a deadline `days` out, for the calendar scoping tests.
function dated(id: string, days: number): ResearchPaper {
  return paper({
    id,
    kind: "submitted",
    title: `Submitted ${id}`,
    deadline: formatDeadline(new Date(Date.now() + days * 86_400_000)),
  })
}

function renderWorkbench(props: WorkbenchProps = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Workbench {...props} />
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
  vi.mocked(api.listResearchTags).mockResolvedValue({ tags: [] })
  vi.mocked(api.createResearchTag).mockImplementation((name) =>
    Promise.resolve({ id: `tag-${name}`, name, position: 0 }),
  )
  // The daily-digest card auto-runs once an AI profile exists; default it to the
  // honest degraded path (no provider on the server) so panel tests stay focused
  // and nothing reaches the network.
  vi.mocked(api.startAIDailyDigest).mockRejectedValue(new api.APIError(503, "no provider"))
  vi.mocked(api.getJob).mockResolvedValue(job("succeeded"))
  vi.mocked(api.getAIChat).mockResolvedValue(chatSession("Finish the robustness table."))
  vi.mocked(api.startAIPaperChat).mockResolvedValue({
    job: job("running"),
    session: chatSession("pending"),
  })
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

describe("Workbench research tags", () => {
  it("loads the tag palette and renders the paper's tag as a pill", async () => {
    vi.mocked(api.listResearchTags).mockResolvedValue({
      tags: [
        { id: "t-high", name: "High", position: 0 },
        { id: "t-field", name: "Fieldwork", position: 1 },
      ],
    })
    papersByKind.research = [paper({ tag_ids: ["t-field"] })]
    renderWorkbench()
    goToTab(/Working papers/)
    // 迁移播种的旧名走既有 i18n 键（en: High → High），自定义名原样渲染。
    expect(await screen.findByRole("button", { name: "Tag: Fieldwork" })).toBeInTheDocument()
  })

  it("creates a tag through the API and assigns it to the paper", async () => {
    vi.mocked(api.listResearchTags).mockResolvedValue({ tags: [] })
    vi.mocked(api.createResearchTag).mockResolvedValue({
      id: "t-placebo",
      name: "Placebo",
      position: 0,
    })
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.click(await screen.findByRole("button", { name: "Tag: No tag" }))
    fireEvent.click(screen.getByRole("menuitem", { name: /New tag/ }))
    const input = screen.getByRole("textbox", { name: "New tag" })
    fireEvent.change(input, { target: { value: "Placebo" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(api.createResearchTag).toHaveBeenCalledWith("Placebo"))
    await waitFor(() =>
      expect(api.updateResearchPaper).toHaveBeenCalledWith("r-1", { tag_ids: ["t-placebo"] }),
    )
  })

  it("writes the whole assignment back when a second tag is ticked", async () => {
    // 多选写回的是一整串 tag_ids：已有的保持指派顺序在前，刚勾上的追加在末尾。
    vi.mocked(api.listResearchTags).mockResolvedValue({
      tags: [
        { id: "t-high", name: "High", position: 0 },
        { id: "t-field", name: "Fieldwork", position: 1 },
      ],
    })
    papersByKind.research = [paper({ tag_ids: ["t-field"] })]
    // 让 PATCH 像真服务器那样把改动落进列表：Workbench 成功后会重取 ["research", kind]，
    // 若 listResearchPapers 仍返回写回前的数组，刷新一落地就把乐观更新覆盖成旧勾选。
    vi.mocked(api.updateResearchPaper).mockImplementationOnce((id, patch) => {
      papersByKind.research = papersByKind.research.map((node) =>
        node.id === id ? { ...node, ...patch } : node,
      )
      return Promise.resolve(papersByKind.research[0]!)
    })
    renderWorkbench()
    goToTab(/Working papers/)
    const pill = await screen.findByRole("button", { name: "Tag: Fieldwork" })
    fireEvent.click(pill)
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "High" }))
    await waitFor(() =>
      expect(api.updateResearchPaper).toHaveBeenCalledWith("r-1", {
        tag_ids: ["t-field", "t-high"],
      }),
    )
    // 勾完不关浮层：多选得能连着点。
    expect(screen.getByRole("menu", { name: "Tag" })).toBeInTheDocument()
    expect(screen.getByRole("menuitemcheckbox", { name: "High" })).toHaveAttribute(
      "aria-checked",
      "true",
    )
    // 写回落进列表后，收起态那颗药丸的完整名单也要跟着变（aria-label 是读屏的唯一入口）。
    expect(await screen.findByRole("button", { name: "Tag: Fieldwork, High" })).toBeInTheDocument()
  })

  it("toasts and keeps the draft when the tag name is rejected", async () => {
    vi.mocked(api.listResearchTags).mockResolvedValue({ tags: [] })
    vi.mocked(api.createResearchTag).mockRejectedValue(new api.APIError(409, "duplicate tag"))
    renderWorkbench()
    goToTab(/Working papers/)
    fireEvent.click(await screen.findByRole("button", { name: "Tag: No tag" }))
    fireEvent.click(screen.getByRole("menuitem", { name: /New tag/ }))
    const input = screen.getByRole("textbox", { name: "New tag" })
    fireEvent.change(input, { target: { value: "Duplicate" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() =>
      expect(useToastStore.getState().toasts.some((entry) => /create the tag/i.test(entry.message))).toBe(true),
    )
    // 失败后输入现场保留，论文没有被指派任何标签。
    expect(screen.getByRole("textbox", { name: "New tag" })).toHaveValue("Duplicate")
    expect(api.updateResearchPaper).not.toHaveBeenCalled()
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

// The AI toggle used to be a dead affordance in this view: the panel only lived
// inside the reader's body, so `aiOpen` rendered nothing here. It is now the
// second flex child of `.wb-shell`, which narrows `.wb-main` the way the
// reader's grid column does — no overlay, and the same persisted width.
describe("Workbench AI panel", () => {
  const panelProps: WorkbenchProps = {
    aiOpen: true,
    aiProfiles: [aiProfile()],
    aiPanelWidth: 420,
  }

  // Submits a turn through the panel and returns the body the UI built.
  // The panel paints before the papers query settles and `Ask` stays disabled
  // while the context is empty, so the wait below mirrors what the owner sees
  // rather than clicking a button that cannot answer yet.
  async function ask(text: string) {
    const box = await screen.findByLabelText("Ask about the papers in this tab")
    const send = () => screen.getByRole("button", { name: "Ask" })
    fireEvent.change(box, { target: { value: text } })
    await waitFor(() => expect(send()).not.toBeDisabled())
    const before = vi.mocked(api.startAIPaperChat).mock.calls.length
    fireEvent.click(send())
    await waitFor(() =>
      expect(vi.mocked(api.startAIPaperChat).mock.calls.length).toBeGreaterThan(before),
    )
    return vi.mocked(api.startAIPaperChat).mock.calls[before]![0]
  }

  function contextLabel(container: HTMLElement) {
    return container.querySelector(".ai-workbench__identity small")?.textContent ?? ""
  }

  it("mounts as a sibling of the main column and takes the persisted width", async () => {
    const { container } = renderWorkbench(panelProps)
    await waitFor(() =>
      expect(container.querySelector(".wb-shell > .ai-workbench")).not.toBeNull(),
    )
    const shell = container.querySelector(".wb-shell") as HTMLElement
    const main = container.querySelector(".wb-main") as HTMLElement
    const panel = container.querySelector(".ai-workbench") as HTMLElement
    // In-flow sibling, not a portal/overlay: this is what squeezes `.wb-main`.
    expect(panel.parentElement).toBe(shell)
    expect(main.nextElementSibling).toBe(panel)
    expect(panel.style.width).toBe("420px")
    expect(within(panel).getByRole("separator")).toHaveAttribute("aria-valuenow", "420")
    // Chat only: `/ai/paper-chat` has no per-entry operations.
    expect(panel.querySelector(".ai-mode-tab")).toBeNull()
    expect(within(panel).getByRole("textbox")).toHaveAttribute(
      "aria-label",
      "Ask about the papers in this tab",
    )
  })

  it("closes through the shell callback", async () => {
    const onCloseAI = vi.fn()
    const { container } = renderWorkbench({ ...panelProps, onCloseAI })
    await waitFor(() => expect(container.querySelector(".ai-workbench")).not.toBeNull())
    const panel = container.querySelector(".ai-workbench") as HTMLElement
    fireEvent.click(within(panel).getByRole("button", { name: "Close" }))
    expect(onCloseAI).toHaveBeenCalled()
  })

  it("sends the papers of the tab in view", async () => {
    papersByKind.research = [paper(), paper({ id: "r-2", title: "Second Paper" })]
    papersByKind.submitted = [dated("s-1", 3)]
    papersByKind.published = [paper({ id: "q-1", kind: "published", title: "Old Publication" })]
    const { container } = renderWorkbench(panelProps)
    // The overview spans the whole workspace.
    expect((await ask("where am I blocked?")).paperIDs).toEqual(["r-1", "r-2", "s-1", "q-1"])
    expect(contextLabel(container)).toContain("All papers in the workspace · 4")

    goToTab(/Working papers/)
    expect((await ask("next step")).paperIDs).toEqual(["r-1", "r-2"])
    expect(contextLabel(container)).toContain("Working papers in this tab · 2")

    goToTab(/Submissions/)
    expect((await ask("next step")).paperIDs).toEqual(["s-1"])
    expect(contextLabel(container)).toContain("Submissions in this tab · 1")

    goToTab(/Publications/)
    expect((await ask("next step")).paperIDs).toEqual(["q-1"])
    expect(contextLabel(container)).toContain("Publications in this tab · 1")
  })

  it("scopes the calendar tab to papers that actually have a deadline, soonest first", async () => {
    papersByKind.submitted = [
      dated("s-late", 20),
      dated("s-soon", 2),
      paper({ id: "s-none", kind: "submitted", title: "No deadline", deadline: "next week" }),
    ]
    const { container } = renderWorkbench(panelProps)
    goToTab(/Deadline calendar/)
    expect((await ask("plan my week")).paperIDs).toEqual(["s-soon", "s-late"])
    expect(contextLabel(container)).toContain("Deadline papers in the calendar · 2")
  })

  it("caps the context at the endpoint's twenty-id limit", async () => {
    papersByKind.research = Array.from({ length: 25 }, (_unused, index) =>
      paper({ id: `r-${index}`, title: `Paper ${index}` }),
    )
    renderWorkbench(panelProps)
    goToTab(/Working papers/)
    const input = await ask("summarise the queue")
    expect(input.paperIDs).toHaveLength(20)
    expect(input.paperIDs[0]).toBe("r-0")
    expect(input.paperIDs[19]).toBe("r-19")
    expect(input.message).toBe("summarise the queue")
    expect(input.profileID).toBe("ai-1")
  })

  it("keeps the returned session for the next turn of the same thread", async () => {
    const { container } = renderWorkbench(panelProps)
    const first = await ask("first question")
    expect(first.sessionID).toBeUndefined()
    // The answer arrives through the job poll and then the session read.
    await waitFor(() =>
      expect(container.querySelector(".ai-chat__message--assistant p")?.textContent).toBe(
        "Finish the robustness table.",
      ),
    )
    expect((await ask("follow-up")).sessionID).toBe("sess-1")
  })

  it("narrows to the expanded row and widens back when it closes", async () => {
    papersByKind.research = [paper(), paper({ id: "r-2", title: "Second Paper" })]
    const { container } = renderWorkbench(panelProps)
    goToTab(/Working papers/)
    const toggles = await screen.findAllByRole("button", {
      name: "Show or hide research stages",
    })
    fireEvent.click(toggles[0]!)
    // The row's own state is private to the page component, so the shell reads it
    // back from the markup on the next frame — wait for that to land.
    await waitFor(() => expect(contextLabel(container)).toContain("One paper only · Growth Regression"))
    expect((await ask("what is left here?")).paperIDs).toEqual(["r-1"])

    fireEvent.click(toggles[0]!)
    await waitFor(() => expect(contextLabel(container)).toContain("Working papers in this tab · 2"))
    expect((await ask("what is left?")).paperIDs).toEqual(["r-1", "r-2"])
  })

  it("drops the focused row when the tab changes", async () => {
    papersByKind.research = [paper(), paper({ id: "r-2", title: "Second Paper" })]
    papersByKind.submitted = [dated("s-1", 3)]
    const { container } = renderWorkbench(panelProps)
    goToTab(/Working papers/)
    const toggles = await screen.findAllByRole("button", {
      name: "Show or hide research stages",
    })
    fireEvent.click(toggles[0]!)
    await waitFor(() => expect(contextLabel(container)).toContain("One paper only"))
    goToTab(/Submissions/)
    // The submissions tab has no expanded row, so the stale focus must not leak
    // into it — the id is only ever looked up in the current tab's list anyway.
    await waitFor(() => expect(contextLabel(container)).toContain("Submissions in this tab · 1"))
    expect((await ask("anything due?")).paperIDs).toEqual(["s-1"])
  })

  it("never asks with an empty context", async () => {
    const { container } = renderWorkbench(panelProps)
    goToTab(/Submissions/)
    await waitFor(() => expect(contextLabel(container)).toContain("Submissions in this tab · 0"))
    const box = await screen.findByLabelText("Ask about the papers in this tab")
    fireEvent.change(box, { target: { value: "hello?" } })
    const send = screen.getByRole("button", { name: "Ask" })
    expect(send).toBeDisabled()
    fireEvent.click(send)
    expect(api.startAIPaperChat).not.toHaveBeenCalled()
  })

  it("routes a configured-but-unreachable provider to an inline alert, not a toast", async () => {
    vi.mocked(api.startAIPaperChat).mockRejectedValue(new api.APIError(503, "AI is not configured"))
    const { container } = renderWorkbench(panelProps)
    await ask("anything due?")
    const alert = await within(container.querySelector(".ai-workbench") as HTMLElement).findByRole(
      "alert",
    )
    expect(alert).toHaveTextContent("AI is not configured")
    expect(useToastStore.getState().toasts).toEqual([])
  })
})

// The digest card's action bubbles must land on entries the shell really
// provides: the "ask" bubble uses the shell's AI toggle (onAskAI), and a paper
// bubble switches the tab strip to that paper's own kind.
describe("Workbench digest action wiring", () => {
  it("routes the ask bubble to the shell AI toggle and the paper bubble to its tab", async () => {
    localStorage.clear()
    const onAskAI = vi.fn()
    const doc = JSON.stringify({
      sections: [
        { headline: "先推这条线", action: { label: "陪我推演这条线", kind: "ask" } },
        {
          headline: "稿件状态",
          action: { label: "打开这篇稿子", kind: "paper", paper_id: "r-1" },
        },
      ],
    })
    vi.mocked(api.startAIDailyDigest).mockResolvedValue({
      job: job("succeeded"),
      session: chatSession("pending"),
    })
    vi.mocked(api.getAIChat).mockResolvedValue(chatSession(doc))
    renderWorkbench({ aiProfiles: [aiProfile()], onAskAI })
    await screen.findByText("先推这条线")
    fireEvent.click(screen.getByRole("button", { name: "陪我推演这条线" }))
    expect(onAskAI).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: "打开这篇稿子" }))
    const nav = screen.getByRole("navigation")
    await waitFor(() =>
      expect(within(nav).getByRole("button", { name: /Working papers/ })).toHaveAttribute(
        "aria-current",
        "page",
      ),
    )
    localStorage.clear()
  })
})
