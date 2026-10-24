import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import type { QuickTask } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import * as api from "../../api/client"
import { QuickTasks } from "./QuickTasks"

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    createQuickTask: vi.fn(),
    deleteQuickTask: vi.fn(),
    listQuickTasks: vi.fn(),
    updateQuickTask: vi.fn(),
  }
})

function quickTask(overrides: Partial<QuickTask> = {}): QuickTask {
  return {
    id: "q-1",
    title: "采购硬盘",
    notes: "",
    start_date: "2026-10-20",
    due_date: "2026-10-21",
    paper_id: null,
    completed_at: "",
    completed: false,
    position: 0,
    ...overrides,
  }
}

function renderQuick(onOpenTimeline = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <QuickTasks onOpenTimeline={onOpenTimeline} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US" })
  vi.mocked(api.listQuickTasks).mockResolvedValue({ items: [quickTask()] })
  vi.mocked(api.createQuickTask).mockResolvedValue(quickTask({ id: "q-new" }))
  vi.mocked(api.updateQuickTask).mockResolvedValue(quickTask({ completed: true }))
  vi.mocked(api.deleteQuickTask).mockResolvedValue(undefined)
})

describe("QuickTasks", () => {
  it("lists quick tasks and creates one from the inline form", async () => {
    renderQuick()
    expect(await screen.findByText("采购硬盘")).toBeInTheDocument()
    expect(screen.getByText("Overdue")).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText("Add a quick task…"), {
      target: { value: "给审稿人回信" },
    })
    fireEvent.click(screen.getByRole("button", { name: /^Add$/ }))
    await waitFor(() =>
      expect(api.createQuickTask).toHaveBeenCalledWith({
        title: "给审稿人回信",
        start_date: "",
        due_date: "",
      }),
    )
  })

  it("completes a task and exposes the timeline jump", async () => {
    renderQuick()
    await screen.findByText("采购硬盘")
    fireEvent.click(screen.getByRole("button", { name: "Mark complete" }))
    await waitFor(() =>
      expect(api.updateQuickTask).toHaveBeenCalledWith("q-1", { completed: true }),
    )
  })
})
