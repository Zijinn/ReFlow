import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type {
  ListResponse,
  TaskAssignment,
  TaskItem,
  TaskLayer,
  TaskStats,
  TaskStep,
  TaskThread,
} from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { useToastStore } from "../../store/toast"
import { TaskBoard } from "./TaskBoard"

// Same harness as the other workbench suites: the real client module is spread
// so `APIError` stays usable, and every entry point this board touches is
// replaced — a task-board test can never reach fetch. `GET /tasks/threads` is
// the only read, so each test hands it one nested tree and asserts on what the
// board renders out of the server's own derived fields.
vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    closeTaskLayer: vi.fn(),
    completeTaskAssignment: vi.fn(),
    createTaskAssignment: vi.fn(),
    createTaskItem: vi.fn(),
    createTaskLayer: vi.fn(),
    createTaskStep: vi.fn(),
    createTaskThread: vi.fn(),
    deleteTaskAssignment: vi.fn(),
    deleteTaskItem: vi.fn(),
    deleteTaskLayer: vi.fn(),
    deleteTaskStep: vi.fn(),
    deleteTaskThread: vi.fn(),
    listResearchPapers: vi.fn(),
    listTaskThreads: vi.fn(),
    reorderTaskItems: vi.fn(),
    updateTaskAssignment: vi.fn(),
    updateTaskItem: vi.fn(),
    updateTaskLayer: vi.fn(),
    updateTaskStep: vi.fn(),
    updateTaskThread: vi.fn(),
  }
})

import * as api from "../../api/client"

const THREAD_ID = "t-1"

// `noUncheckedIndexedAccess` makes `cells[0]` an `HTMLElement | undefined`, and
// `within()` takes an element. Indexing through here keeps the assertion on the
// element rather than on a cast that would pass even when the node is missing.
function at<T>(list: T[], index: number): T {
  const entry = list[index]
  if (entry === undefined) throw new Error(`expected an element at index ${index}, got ${list.length}`)
  return entry
}

function stats(overrides: Partial<TaskStats> = {}): TaskStats {
  return {
    active: 7,
    locked: 3,
    unstarted: 0,
    overdue: 0,
    closed: 1,
    total: 11,
    slots: 13,
    slots_closed: 3,
    ...overrides,
  }
}

function step(id: string, name: string, done: boolean, position = 0): TaskStep {
  return { id, name, done, position, done_at: done ? "2026-10-08T00:00:00Z" : "" }
}

function assignment(
  overrides: Partial<TaskAssignment> & { id: string; layer_id: string },
): TaskAssignment {
  return {
    layer_position: 0,
    position: 0,
    start_date: "2026-10-10",
    due_date: "2026-10-31",
    closed: false,
    closed_at: "",
    steps: [],
    ...overrides,
  }
}

function layer(id: string, name: string, position: number, overrides: Partial<TaskLayer> = {}): TaskLayer {
  return {
    id,
    name,
    position,
    unlocked: true,
    forced_unlock: false,
    item_count: 1,
    closed_count: 0,
    ...overrides,
  }
}

function item(overrides: Partial<TaskItem> & { id: string }): TaskItem {
  return {
    title: "Untitled task",
    notes: "",
    position: 0,
    paper_id: null,
    closed_at: "",
    closed: false,
    layer_count: 1,
    progress: 0,
    status: "active",
    stats: { total_steps: 0, done_steps: 0 },
    assignments: [],
    ...overrides,
  }
}

function thread(overrides: Partial<TaskThread> & { id: string }): TaskThread {
  return {
    name: "Papers and projects",
    position: 0,
    archived: false,
    map_markdown: "",
    layers: [],
    items: [],
    stats: stats(),
    ...overrides,
  }
}

function tree(...threads: TaskThread[]): ListResponse<TaskThread> {
  return { items: threads }
}

function renderBoard(input: ListResponse<TaskThread>) {
  vi.mocked(api.listTaskThreads).mockResolvedValue(input)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskBoard />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US", workbenchModule: "research" })
  useToastStore.setState({ toasts: [] })
  vi.mocked(api.listResearchPapers).mockResolvedValue({ items: [] })
  vi.mocked(api.createTaskItem).mockResolvedValue(item({ id: "new-item" }))
  vi.mocked(api.createTaskThread).mockResolvedValue(thread({ id: "new-thread" }))
  vi.mocked(api.createTaskLayer).mockResolvedValue(layer("new-layer", "New layer", 1))
  vi.mocked(api.closeTaskLayer).mockResolvedValue(layer("l-1", "Layer one", 0, { closed_count: 1 }))
  vi.mocked(api.createTaskAssignment).mockResolvedValue(assignment({ id: "new-assign", layer_id: "l-2" }))
  vi.mocked(api.completeTaskAssignment).mockResolvedValue(
    assignment({ id: "a-1", layer_id: "l-1", closed: true }),
  )
  vi.mocked(api.createTaskStep).mockResolvedValue(step("s-new", "new step", false))
  for (const fn of [
    api.updateTaskThread,
    api.deleteTaskThread,
    api.updateTaskLayer,
    api.deleteTaskLayer,
    api.updateTaskItem,
    api.deleteTaskItem,
    api.reorderTaskItems,
    api.updateTaskAssignment,
    api.deleteTaskAssignment,
    api.updateTaskStep,
    api.deleteTaskStep,
  ]) {
    vi.mocked(fn).mockResolvedValue(undefined)
  }
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("TaskBoard stat strip", () => {
  it("renders the five cells straight from stats, green only on a non-zero last one", async () => {
    const { container } = renderBoard(
      tree(thread({ id: THREAD_ID, stats: stats({ active: 7, locked: 3, unstarted: 0, overdue: 0, closed: 1 }) })),
    )
    await screen.findByText("Papers and projects")

    const cells = Array.from(container.querySelectorAll<HTMLElement>(".wb-task-stat"))
    expect(cells).toHaveLength(5)
    expect(cells.map((cell) => cell.textContent?.replace(/[0-9]/g, "").trim())).toEqual([
      "In progress",
      "Locked",
      "Not started",
      "Overdue",
      "Closed",
    ])
    // 大数字压在小标签上：数字格就是服务端那五个计数，一个都不在本地重算。
    expect(within(at(cells, 0)).getByText("7")).toBeInTheDocument()
    expect(within(at(cells, 1)).getByText("3")).toBeInTheDocument()
    expect(within(at(cells, 4)).getByText("1")).toBeInTheDocument()
    // 已收口非零才涂绿；0 收口的这条主线读成绿色反而是误导。
    expect(cells[4]?.className).toContain("wb-task-stat--green")
    expect(container.querySelectorAll(".wb-task-stat--green")).toHaveLength(1)
  })
})

describe("TaskBoard locked layer", () => {
  it("keeps a locked layer's cards rendered and editable, and prints its lock", async () => {
    const locked = layer("l-2", "Third layer", 1, { unlocked: false, item_count: 3, closed_count: 0 })
    renderBoard(
      tree(
        thread({
          id: THREAD_ID,
          layers: [layer("l-1", "First layer", 0), locked],
          items: [
            item({
              id: "i-1",
              title: "Draft the model section",
              status: "locked",
              assignments: [assignment({ id: "a-1", layer_id: "l-2", layer_position: 1 })],
            }),
          ],
        }),
      ),
    )
    const card = await screen.findByText("Draft the model section")
    expect(card).toBeInTheDocument()
    // 锁是给这一层挂的牌子，不是把里面的卡锁进抽屉：这一层里的任务照样看得见、
    // 照样改名（unlocked 只闸"收口"这一条动作，见 ✓ 完成 那颗按钮的门槛）。
    const cardEl = card.closest(".wb-task-card")
    expect(cardEl).not.toBeNull()
    expect(within(cardEl as HTMLElement).getByText(/0\/0 steps/)).toBeInTheDocument()
    expect(within(cardEl as HTMLElement).queryByRole("button", { name: "Complete" })).toBeNull()

    fireEvent.doubleClick(card)
    expect(within(cardEl as HTMLElement).getByDisplayValue("Draft the model section")).toBeInTheDocument()

    // 层块里那一格状态牌子读"Locked"（抬头统计那五格也有一个同名档，所以按块取）。
    const layerBlocks = Array.from(document.querySelectorAll<HTMLElement>(".wb-task-layer"))
    expect(layerBlocks).toHaveLength(2)
    expect(within(at(layerBlocks, 1)).getByText("Locked")).toBeInTheDocument()
    expect(within(at(layerBlocks, 0)).getByText("In progress")).toBeInTheDocument()
  })
})

describe("TaskBoard cards across layers", () => {
  it("shows the span chip only where a task sits in more than one layer", async () => {
    const shared = item({
      id: "i-1",
      title: "Robustness table",
      layer_count: 2,
      progress: 40,
      assignments: [
        assignment({ id: "a-1", layer_id: "l-1", steps: [step("s-1", "collect", true), step("s-2", "refit", true)] }),
        assignment({
          id: "a-2",
          layer_id: "l-2",
          layer_position: 1,
          steps: [step("s-3", "write up", false), step("s-4", "appendix", false), step("s-5", "proof", false)],
        }),
      ],
    })
    const single = item({
      id: "i-2",
      title: "Citation sweep",
      layer_count: 1,
      assignments: [assignment({ id: "a-3", layer_id: "l-1", position: 1 })],
    })
    renderBoard(
      tree(
        thread({
          id: THREAD_ID,
          layers: [
            layer("l-1", "First layer", 0, { item_count: 2 }),
            layer("l-2", "Second layer", 1, { item_count: 1 }),
          ],
          items: [shared, single],
        }),
      ),
    )
    await screen.findAllByText("Robustness table")

    // 一颗卡在这一层里，另一颗在下一层里：同一条任务的药丸出现两次。
    expect(screen.getAllByText("In 2 layers")).toHaveLength(2)
    expect(screen.queryByText(/In 1 layers/)).toBeNull()
    const solo = (await screen.findByText("Citation sweep")).closest(".wb-task-card")
    expect(within(solo as HTMLElement).queryByText(/layers/)).toBeNull()
  })

  it("prints the server's progress on both cards instead of a per-card recomputation", async () => {
    const shared = item({
      id: "i-1",
      title: "Robustness table",
      layer_count: 2,
      // 服务端跨全部层算的那一个数：2/2 在这一层、0/3 在下一层 → 2/5 = 40。
      progress: 40,
      stats: { total_steps: 5, done_steps: 2 },
      assignments: [
        assignment({ id: "a-1", layer_id: "l-1", steps: [step("s-1", "collect", true), step("s-2", "refit", true)] }),
        assignment({
          id: "a-2",
          layer_id: "l-2",
          layer_position: 1,
          steps: [step("s-3", "write up", false), step("s-4", "appendix", false), step("s-5", "proof", false)],
        }),
      ],
    })
    renderBoard(
      tree(
        thread({
          id: THREAD_ID,
          layers: [
            layer("l-1", "First layer", 0),
            layer("l-2", "Second layer", 1, { item_count: 1 }),
          ],
          items: [shared],
          stats: stats({ total: 1, slots: 2, slots_closed: 0, active: 1, locked: 0, closed: 0 }),
        }),
      ),
    )
    const cards = await screen.findAllByText("Robustness table")
    expect(cards).toHaveLength(2)
    // 本层重算会得出 100 与 0；两颗环里都是服务端那一个 40。
    expect(screen.getAllByText("40")).toHaveLength(2)
    const second = cards[1]?.closest(".wb-task-card")
    expect(within(second as HTMLElement).getByText(/0\/3 steps/)).toBeInTheDocument()
    const first = cards[0]?.closest(".wb-task-card")
    expect(within(first as HTMLElement).getByText(/2\/2 steps/)).toBeInTheDocument()
  })
})

describe("TaskBoard writes", () => {
  it("creates an item scoped to the layer whose add slot was clicked", async () => {
    const { container } = renderBoard(
      tree(
        thread({
          id: THREAD_ID,
          layers: [layer("l-1", "First layer", 0), layer("l-2", "Second layer", 1, { item_count: 0 })],
        }),
      ),
    )
    await screen.findByText("Papers and projects")
    const blocks = Array.from(container.querySelectorAll<HTMLElement>(".wb-task-layer"))
    expect(blocks).toHaveLength(2)

    fireEvent.click(within(at(blocks, 1)).getByRole("button", { name: "Add a task in this layer" }))
    await waitFor(() =>
      expect(api.createTaskItem).toHaveBeenCalledWith(THREAD_ID, { layer_id: "l-2" }),
    )

    fireEvent.click(within(at(blocks, 0)).getByRole("button", { name: "Add a task in this layer" }))
    await waitFor(() =>
      expect(api.createTaskItem).toHaveBeenCalledWith(THREAD_ID, { layer_id: "l-1" }),
    )
  })

  it("closes one card's steps through the complete endpoint", async () => {
    const first = layer("l-1", "First layer", 0, { item_count: 1 })
    renderBoard(
      tree(
        thread({
          id: THREAD_ID,
          layers: [first],
          items: [
            item({
              id: "i-1",
              title: "Collect the register data",
              assignments: [assignment({ id: "a-1", layer_id: "l-1", steps: [step("s-1", "pull", false)] })],
            }),
          ],
        }),
      ),
    )
    const complete = await screen.findByRole("button", { name: "Complete" })
    fireEvent.click(complete)
    await waitFor(() => expect(api.completeTaskAssignment).toHaveBeenCalledWith("a-1"))
  })

  it("puts the unlock pill between a fully closed layer and the layer it opened", async () => {
    const { container } = renderBoard(
      tree(
        thread({
          id: THREAD_ID,
          layers: [
            layer("l-1", "First layer", 0, { item_count: 3, closed_count: 3 }),
            layer("l-2", "Second layer", 1, { item_count: 7 }),
            layer("l-3", "Third layer", 2, { unlocked: false, item_count: 3 }),
          ],
          stats: stats({ slots: 13, slots_closed: 3 }),
        }),
      ),
    )
    await screen.findByText("Papers and projects")

    const pills = container.querySelectorAll(".wb-task-unlock")
    // 只有第一层（全部收口）和第二层之间有一颗；第二层没收口，第三层还在闸外。
    expect(pills).toHaveLength(1)
    expect(pills[0]?.textContent).toContain("Next layer unlocked")
    expect(pills[0]?.previousElementSibling?.classList.contains("wb-task-layer")).toBe(true)
    expect(pills[0]?.nextElementSibling?.classList.contains("wb-task-layer")).toBe(true)
    expect(container.querySelectorAll(".wb-task-layer")).toHaveLength(3)
  })
})
