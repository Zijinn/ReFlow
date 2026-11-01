import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { QuickTask } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import * as api from "../../api/client"
import { QuickTasks } from "./QuickTasks"

function relativeDate(days: number): string {
  const date = new Date()
  date.setDate(date.getDate() + days)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`
}

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    createQuickTask: vi.fn(),
    deleteQuickTask: vi.fn(),
    getDidaSyncStatus: vi.fn(),
    getRemindersSyncStatus: vi.fn(),
    syncTasksWithReminders: vi.fn(),
    listQuickTasks: vi.fn(),
    pullTasksFromDida: vi.fn(),
    pushTasksToDida: vi.fn(),
    updateQuickTask: vi.fn(),
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

afterEach(() => cleanup())

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US" })
  vi.mocked(api.listQuickTasks).mockResolvedValue({ items: [quickTask()] })
  vi.mocked(api.getDidaSyncStatus).mockResolvedValue({ available: true, project: "Reflow" })
  vi.mocked(api.getRemindersSyncStatus).mockResolvedValue({
    available: false,
    list_name: "Reflow",
    list_exists: false,
    reminder_count: 0,
    linked_tasks: 0,
  })
  vi.mocked(api.createQuickTask).mockResolvedValue(quickTask({ id: "q-new" }))
  vi.mocked(api.updateQuickTask).mockResolvedValue(quickTask({ completed: true }))
  vi.mocked(api.deleteQuickTask).mockResolvedValue(undefined)
})

describe("QuickTasks", () => {
  it("syncs with Apple Reminders from the split button", async () => {
    vi.mocked(api.getRemindersSyncStatus).mockResolvedValue({
      available: true,
      list_name: "Reflow",
      list_exists: true,
      reminder_count: 1,
      linked_tasks: 1,
      last_sync: "2026-11-01T00:00:00Z",
    })
    vi.mocked(api.syncTasksWithReminders).mockResolvedValue({
      direction: "merge",
      dry_run: false,
      list: { id: "list-1", name: "Reflow" },
      state_file: "/tmp/reminders-sync.json",
      created_in_reminders: 1,
      created_in_reflow: 0,
      updated_in_reminders: 0,
      updated_in_reflow: 0,
    })
    renderQuick()
    fireEvent.click(await screen.findByRole("button", { name: "Reminders" }))
    await waitFor(() => expect(api.syncTasksWithReminders).toHaveBeenCalledWith("merge", false))
  })

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
    const check = screen.getByRole("button", { name: "Mark complete" })
    fireEvent.click(check)
    await waitFor(() => expect(check).toHaveAttribute("data-anim", "complete"))
    expect(check.querySelector(".wb-tm-check-path")).toBeTruthy()
    await waitFor(() =>
      expect(api.updateQuickTask).toHaveBeenCalledWith("q-1", { completed: true }),
    )
    const toggle = await screen.findByRole("button", { name: /Completed/ })
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(toggle.closest(".wb-tm-quick-done")?.querySelector("ul")?.textContent).toContain(
      "采购硬盘",
    )
  })

  it("groups open tasks by the urgency of their due date", async () => {
    vi.mocked(api.listQuickTasks).mockResolvedValue({
      items: [
        quickTask({ id: "q-late", title: "Overdue item", due_date: relativeDate(-1) }),
        quickTask({ id: "q-today", title: "Today item", due_date: relativeDate(0) }),
        quickTask({ id: "q-week", title: "This week item", due_date: relativeDate(3) }),
        quickTask({ id: "q-far", title: "Far item", due_date: relativeDate(12) }),
        quickTask({ id: "q-none", title: "No due item", start_date: "", due_date: "" }),
      ],
    })
    const { container } = renderQuick()
    await screen.findByText("Overdue item")

    const labels = within(container)
      .getAllByRole("heading", { level: 3 })
      .map((heading) => heading.querySelector("span")?.textContent)
    expect(labels).toEqual(["Overdue", "Due today", "Next 7 days", "Later", "Unscheduled"])

    for (const [label, title, urgency] of [
      ["Overdue", "Overdue item", "overdue"],
      ["Due today", "Today item", "soon"],
      ["Next 7 days", "This week item", "soon"],
      ["Later", "Far item", "future"],
      ["Unscheduled", "No due item", null],
    ] as const) {
      const section = within(container)
        .getByRole("heading", { level: 3, name: new RegExp(`^${label}`) })
        .closest("section")
      if (!section) throw new Error(`missing section for ${label}`)
      expect(within(section).getByText(title)).toBeInTheDocument()
      const row = within(section).getByText(title).closest("[data-quick-task-id]")
      const dates = row?.querySelector(".wb-tm-quick-dates")
      if (urgency) expect(dates).toHaveAttribute("data-urgency", urgency)
      else expect(dates).not.toHaveAttribute("data-urgency")
    }
  })

  it("pushes tasks to Dida from the header", async () => {
    const { container } = renderQuick()
    await within(container).findByText("采购硬盘")
    vi.mocked(api.pushTasksToDida).mockResolvedValue({
      project: { id: "p-1", name: "Reflow" },
      dry_run: false,
      state_file: "",
      created_in_dida: 1,
      created_in_reflow: 0,
      updated_in_dida: 0,
      updated_in_reflow: 0,
    })
    fireEvent.click(within(container).getByRole("button", { name: "Push to Dida" }))
    await waitFor(() => expect(api.pushTasksToDida).toHaveBeenCalledTimes(1))
  })

  it("edits tags and notes on a quick task", async () => {
    const { container } = renderQuick()
    await within(container).findByText("采购硬盘")

    fireEvent.click(within(container).getByRole("button", { name: /Tags/ }))
    const tagInput = within(container).getByRole("textbox", { name: "Tags, comma separated" })
    fireEvent.change(tagInput, { target: { value: "写作, urgent" } })
    fireEvent.keyDown(tagInput, { key: "Enter" })
    await waitFor(() =>
      expect(api.updateQuickTask).toHaveBeenCalledWith("q-1", { tags: ["写作", "urgent"] }),
    )

    const notes = container.querySelector(".wb-tm-quick-notes")
    if (!notes) throw new Error("missing quick-task notes")
    fireEvent.doubleClick(notes)
    const editor = container.querySelector<HTMLTextAreaElement>("textarea.wb-inline-input")
    if (!editor) throw new Error("notes editor did not open")
    fireEvent.change(editor, { target: { value: "记得报销" } })
    fireEvent.blur(editor)
    await waitFor(() =>
      expect(api.updateQuickTask).toHaveBeenCalledWith("q-1", { notes: "记得报销" }),
    )
  })
})
