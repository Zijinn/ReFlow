import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import type { QuickTask, TaskThread } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import * as api from "../../api/client"
import { TaskTimeline } from "./TaskTimeline"

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    listQuickTasks: vi.fn(),
    listTaskThreads: vi.fn(),
    updateQuickTask: vi.fn(),
    updateTaskAssignment: vi.fn(),
  }
})

// The timeline opens on the current week, so fixed fixture dates drift out of
// view as time passes. Anchor the bars around today instead.
function relativeDate(days: number): string {
  const date = new Date()
  date.setDate(date.getDate() + days)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`
}

const quick: QuickTask = {
  id: "q-1",
  title: "采购硬盘",
  notes: "",
  tags: [],
  start_date: relativeDate(-2),
  due_date: relativeDate(0),
  paper_id: null,
  completed_at: "",
  completed: false,
  position: 0,
}

const thread: TaskThread = {
  id: "t-1",
  name: "论文推进",
  position: 0,
  archived: false,
  map_markdown: "",
  layers: [
    {
      id: "l-1",
      name: "第一层",
      position: 0,
      unlocked: true,
      forced_unlock: false,
      item_count: 1,
      closed_count: 0,
    },
  ],
  items: [
    {
      id: "i-1",
      title: "修改正文",
      notes: "",
      position: 0,
      paper_id: null,
      closed_at: "",
      closed: false,
      layer_count: 1,
      progress: 0,
      status: "active",
      stats: { total_steps: 1, done_steps: 0 },
      assignments: [
        {
          id: "a-1",
          layer_id: "l-1",
          layer_position: 0,
          position: 0,
          start_date: relativeDate(-3),
          due_date: relativeDate(1),
          closed: false,
          closed_at: "",
          steps: [{ id: "s-1", name: "改摘要", done: false, position: 0, done_at: "" }],
        },
      ],
    },
  ],
  stats: {
    active: 1,
    locked: 0,
    unstarted: 0,
    overdue: 0,
    closed: 0,
    total: 1,
    slots: 1,
    slots_closed: 0,
  },
}

function renderTimeline() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskTimeline onNewQuick={() => {}} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US" })
  vi.mocked(api.listQuickTasks).mockResolvedValue({ items: [quick] })
  vi.mocked(api.listTaskThreads).mockResolvedValue({ items: [thread] })
})

describe("TaskTimeline", () => {
  it("renders quick tasks, long-task rows and their bars", async () => {
    renderTimeline()
    expect(await screen.findByText("Long tasks in view")).toBeInTheDocument()
    expect((await screen.findAllByText("修改正文")).length).toBeGreaterThan(0)
    expect(screen.getAllByText("采购硬盘").length).toBeGreaterThan(0)
    expect(document.querySelectorAll(".wb-tm-tl-bar").length).toBe(2)
    expect(document.querySelectorAll(".wb-tm-tl-bar--soon").length).toBe(2)
  })
})
