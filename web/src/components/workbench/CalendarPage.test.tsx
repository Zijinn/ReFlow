import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchPaper } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { CalendarPage } from "./CalendarPage"
import { formatDeadline } from "./utils"

// Pinned so the grid window (which spans the neighbouring months) always
// contains every offset used below.
const TODAY = new Date(2026, 2, 15, 12)

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
  // Only Date is faked: React's scheduler keeps using the real timers.
  vi.useFakeTimers({ toFake: ["Date"] })
  vi.setSystemTime(TODAY)
})

function dayOffset(days: number): string {
  const date = new Date(TODAY)
  date.setDate(date.getDate() + days)
  return formatDeadline(date)
}

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "s-1",
    kind: "submitted",
    position: 0,
    title: "Submitted Paper",
    authors: ["Li Si"],
    keywords: [],
    file_path: "",
    next_action: "",
    notes: "",
    research_area: "",
    status: "under_review",
    tag_ids: [],
    target_journal: "",
    stages: [],
    current_journal: "Journal of Development Economics",
    submission_date: "2026-08-01",
    manuscript_id: "",
    submission_count: 1,
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
    last_updated: "",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

describe("CalendarPage", () => {
  it("places each submitted deadline on its day with an urgency tint", () => {
    render(
      <CalendarPage
        papers={[
          paper({ id: "s-over", title: "Overdue", deadline: dayOffset(-2) }),
          paper({ id: "s-today", title: "Due now", deadline: dayOffset(0) }),
          paper({ id: "s-soon", title: "Soon", deadline: dayOffset(3) }),
          paper({ id: "s-far", title: "Later", deadline: dayOffset(15) }),
        ]}
        onSelectPaper={vi.fn()}
      />,
    )
    expect(document.querySelectorAll(".wb-cal-cell")).toHaveLength(42)
    expect(document.querySelectorAll(".wb-cal-event")).toHaveLength(4)
    expect(document.querySelector(".wb-cal-event--overdue")).not.toBeNull()
    expect(document.querySelector(".wb-cal-event--today")).not.toBeNull()
    expect(document.querySelector(".wb-cal-event--soon")).not.toBeNull()
    // The countdown lives in the chip's tooltip, the colour carries the urgency.
    expect(document.querySelector(".wb-cal-event--overdue")?.getAttribute("title")).toContain(
      "2 d overdue",
    )
    expect(screen.getByText("Due now")).toBeInTheDocument()
    // A deadline further out than a week carries no urgency tint.
    expect(screen.getByText("Later").parentElement?.className).toBe("wb-cal-event")
  })

  it("ignores papers without a parseable deadline", () => {
    render(
      <CalendarPage
        papers={[paper({ deadline: "" }), paper({ id: "s-2", deadline: "TBD" })]}
        onSelectPaper={vi.fn()}
      />,
    )
    expect(document.querySelectorAll(".wb-cal-event")).toHaveLength(0)
    expect(screen.getByText(/No deadlines this month/)).toBeInTheDocument()
  })

  it("caps a busy day at three chips and expands on demand", () => {
    const same = dayOffset(5)
    render(
      <CalendarPage
        papers={[1, 2, 3, 4].map((n) =>
          paper({ id: `s-${n}`, title: `Paper ${n}`, deadline: same }),
        )}
        onSelectPaper={vi.fn()}
      />,
    )
    expect(document.querySelectorAll(".wb-cal-event")).toHaveLength(3)
    fireEvent.click(screen.getByRole("button", { name: "+1" }))
    expect(document.querySelectorAll(".wb-cal-event")).toHaveLength(4)
  })

  it("asks the parent to open the paper behind a deadline", () => {
    const onSelectPaper = vi.fn()
    render(
      <CalendarPage
        papers={[paper({ id: "s-jump", title: "Jump target", deadline: dayOffset(1) })]}
        onSelectPaper={onSelectPaper}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /Jump target/ }))
    expect(onSelectPaper).toHaveBeenCalledWith("s-jump")
  })

  it("moves between months and jumps back to today", () => {
    render(<CalendarPage papers={[]} onSelectPaper={vi.fn()} />)
    expect(screen.getByRole("heading", { name: "March 2026" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Next month" }))
    expect(screen.getByRole("heading", { name: "April 2026" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Previous month" }))
    fireEvent.click(screen.getByRole("button", { name: "Previous month" }))
    expect(screen.getByRole("heading", { name: "February 2026" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Today" }))
    expect(screen.getByRole("heading", { name: "March 2026" })).toBeInTheDocument()
    expect(document.querySelector(".wb-cal-day--today")).not.toBeNull()
  })

  // The assistant has to be usable from the calendar too, so the daily plan lives
  // here rather than only on the overview — and it sits above the grid, not
  // inside it, so a narrow viewport never squeezes the month.
  it("hosts the daily progress plan between the month header and the grid", () => {
    render(
      <CalendarPage
        papers={[paper({ id: "s-due", title: "Due soon", deadline: dayOffset(1) })]}
        onSelectPaper={vi.fn()}
      />,
    )
    const card = screen.getByRole("region", { name: "Today's progress plan" })
    expect(card).toBeInTheDocument()
    const calendar = document.querySelector(".wb-calendar") as HTMLElement
    expect(Array.from(calendar.children).indexOf(card)).toBe(1)
    expect(card.previousElementSibling?.className).toBe("wb-cal-head")
    expect(card.nextElementSibling?.className).toBe("wb-cal-grid")
    expect(card).toHaveTextContent("Configure AI to get a daily progress plan here.")
    expect(card).toHaveTextContent("Due in 7 days")
    expect(screen.getByRole("heading", { name: "March 2026" })).toBeInTheDocument()
  })
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})
