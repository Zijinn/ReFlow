import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import type { QuickTask } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import * as api from "../../api/client"
import { TaskManagement } from "./TaskManagement"

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    getDidaSyncStatus: vi.fn(),
    listQuickTasks: vi.fn(),
    listResearchPapers: vi.fn(),
    listResearchTags: vi.fn(),
    listTaskThreads: vi.fn(),
  }
})

function quickTask(overrides: Partial<QuickTask> = {}): QuickTask {
  return {
    id: "q-1",
    title: "采购硬盘",
    notes: "",
    tags: [],
    start_date: "2026-10-20",
    due_date: "2026-10-21",
    paper_id: null,
    completed_at: "",
    completed: false,
    position: 0,
    ...overrides,
  }
}

function renderManagement() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskManagement />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US", taskView: "quick" })
  vi.mocked(api.listQuickTasks).mockResolvedValue({ items: [quickTask()] })
  vi.mocked(api.getDidaSyncStatus).mockResolvedValue({ available: true, project: "Reflow" })
  vi.mocked(api.listTaskThreads).mockResolvedValue({ items: [] })
  vi.mocked(api.listResearchPapers).mockResolvedValue({ items: [] })
  vi.mocked(api.listResearchTags).mockResolvedValue({ tags: [] })
})

describe("TaskManagement", () => {
  it("defaults to quick tasks and switches between the four views", async () => {
    renderManagement()
    expect(await screen.findByText("采购硬盘")).toBeInTheDocument()
    const nav = screen.getByRole("navigation", { name: "Task views" })
    expect(within(nav).getByRole("button", { name: /Timeline/ })).toBeInTheDocument()

    fireEvent.click(within(nav).getByRole("button", { name: /Timeline/ }))
    expect(await screen.findByText("Long tasks in view")).toBeInTheDocument()
    expect(screen.getByText("Quick tasks in view")).toBeInTheDocument()

    fireEvent.click(within(nav).getByRole("button", { name: /Mind map/ }))
    expect(await screen.findByRole("textbox", { name: "Markdown source" })).toBeInTheDocument()
    expect(screen.getByText("新的长期主线")).toBeInTheDocument()

    fireEvent.click(within(nav).getByRole("button", { name: /Long-term tasks/ }))
    expect(await screen.findByText("No threads yet")).toBeInTheDocument()
  })
})
