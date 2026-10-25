import { useMemo, useState } from "react"

import type {
  AIProfile,
  QuickTask,
  ResearchKind,
  ResearchPaper,
  TaskThread,
} from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { DailyDigestCard } from "./DailyDigest"
import { daysUntil, displayID, formatDeadline, parseDeadline } from "./utils"

interface DeadlineEvent {
  paperID: string
  code: string
  title: string
  days: number
}

type CalendarTaskKind = "quick" | "assignment"

interface CalendarTaskEvent {
  key: string
  kind: CalendarTaskKind
  id: string
  title: string
  subtitle: string
  start: Date
  due: Date
}

interface UnscheduledTask {
  key: string
  kind: CalendarTaskKind
  id: string
  title: string
  subtitle: string
}

interface TaskSegment {
  event: CalendarTaskEvent
  startColumn: number
  endColumn: number
  continuesLeft: boolean
  continuesRight: boolean
}

function dayKey(date: Date): number {
  return date.getFullYear() * 10000 + (date.getMonth() + 1) * 100 + date.getDate()
}

function sameDay(a: Date, b: Date): boolean {
  return dayKey(a) === dayKey(b)
}

function dayDiff(from: Date, to: Date): number {
  const start = new Date(from.getFullYear(), from.getMonth(), from.getDate())
  const end = new Date(to.getFullYear(), to.getMonth(), to.getDate())
  return Math.round((end.getTime() - start.getTime()) / 86_400_000)
}

function taskWindow(start: Date | null, due: Date | null): { start: Date; due: Date } | null {
  const first = start ?? due
  const last = due ?? start
  if (!first || !last) return null
  return first > last ? { start: last, due: first } : { start: first, due: last }
}

function urgencyClass(days: number): string {
  if (days < 0) return "wb-cal-event--overdue"
  if (days === 0) return "wb-cal-event--today"
  if (days <= 7) return "wb-cal-event--soon"
  return ""
}

// The calendar owns no data of its own: every event is a deadline a table
// already holds — 在研 papers set one for their internal due dates, 在投 for
// their submission dates — so editing always happens in the row and this view
// can never drift from it.
export function CalendarPage(props: {
  papers: ResearchPaper[]
  aiProfiles?: AIProfile[]
  onConfigureAI?: () => void
  onAskAI?: () => void
  onSelectPaper: (paperID: string) => void
  quickTasks?: QuickTask[]
  taskThreads?: TaskThread[]
  onSelectTask?: (kind: CalendarTaskKind, taskID: string) => void
}) {
  const { t, locale } = useTranslation()
  const today = new Date()
  const [cursor, setCursor] = useState(() => ({
    year: today.getFullYear(),
    month: today.getMonth(),
  }))
  const [expandedDay, setExpandedDay] = useState<number | null>(null)

  // 编号取的是这篇在它自己那张表里的顺位（R007 / S003），所以日历上的代码和点过去
  // 之后表里看到的那一行对得上；没有日期、不进气泡的行也要占一格，否则顺位会集体前移。
  const eventsByDay = useMemo(() => {
    const map = new Map<number, DeadlineEvent[]>()
    const ordinals = new Map<ResearchKind, number>()
    props.papers.forEach((paper) => {
      const ordinal = ordinals.get(paper.kind) ?? 0
      ordinals.set(paper.kind, ordinal + 1)
      const date = parseDeadline(paper.deadline)
      if (!date) return
      const code = displayID(paper.kind, ordinal)
      const key = dayKey(date)
      const list = map.get(key) ?? []
      list.push({
        paperID: paper.id,
        code,
        title: paper.title.trim() || paper.current_journal.trim() || code,
        days: daysUntil(date),
      })
      map.set(key, list)
    })
    return map
  }, [props.papers])

  // Task ranges are a second layer on the same month: completed tasks are
  // deliberately absent, and tasks without any date live below the grid so the
  // "未排期" bucket survives the calendar integration.
  const taskData = useMemo(() => {
    const events: CalendarTaskEvent[] = []
    const unscheduled: UnscheduledTask[] = []
    for (const task of props.quickTasks ?? []) {
      if (task.completed) continue
      const title = task.title.trim() || t("taskViewQuick")
      const window = taskWindow(parseDeadline(task.start_date), parseDeadline(task.due_date))
      if (!window) {
        unscheduled.push({
          key: `quick-${task.id}`,
          kind: "quick",
          id: task.id,
          title,
          subtitle: t("taskViewQuick"),
        })
        continue
      }
      events.push({
        key: `quick-${task.id}`,
        kind: "quick",
        id: task.id,
        title,
        subtitle: t("taskViewQuick"),
        ...window,
      })
    }
    for (const thread of props.taskThreads ?? []) {
      if (thread.archived) continue
      const layerNames = new Map(thread.layers.map((layer) => [layer.id, layer.name]))
      for (const item of thread.items) {
        if (item.closed) continue
        const title = item.title.trim() || t("taskViewLong")
        if (item.assignments.length === 0) {
          unscheduled.push({
            key: `item-${item.id}`,
            kind: "assignment",
            id: item.id,
            title,
            subtitle: thread.name,
          })
          continue
        }
        for (const assignment of item.assignments) {
          if (assignment.closed) continue
          const subtitle = layerNames.get(assignment.layer_id)?.trim() || thread.name
          const window = taskWindow(
            parseDeadline(assignment.start_date),
            parseDeadline(assignment.due_date),
          )
          if (!window) {
            unscheduled.push({
              key: `assignment-${assignment.id}`,
              kind: "assignment",
              id: assignment.id,
              title,
              subtitle,
            })
            continue
          }
          events.push({
            key: `assignment-${assignment.id}`,
            kind: "assignment",
            id: assignment.id,
            title,
            subtitle,
            ...window,
          })
        }
      }
    }
    events.sort(
      (left, right) => left.start.getTime() - right.start.getTime() || left.title.localeCompare(right.title),
    )
    unscheduled.sort((left, right) => left.title.localeCompare(right.title))
    return { events, unscheduled }
  }, [props.quickTasks, props.taskThreads, t])

  const cells = useMemo(() => {
    const first = new Date(cursor.year, cursor.month, 1)
    const start = new Date(first)
    start.setDate(first.getDate() - ((first.getDay() + 6) % 7))
    return Array.from({ length: 42 }, (_, index) => {
      const date = new Date(start)
      date.setDate(start.getDate() + index)
      return date
    })
  }, [cursor])

  const weeks = useMemo(
    () => Array.from({ length: 6 }, (_, index) => cells.slice(index * 7, index * 7 + 7)),
    [cells],
  )
  const segmentsByWeek = useMemo(
    () =>
      weeks.map((week) => {
        const weekStart = week[0]!
        const weekEnd = week[6]!
        const segments: TaskSegment[] = []
        for (const event of taskData.events) {
          if (event.due < weekStart || event.start > weekEnd) continue
          const start = event.start < weekStart ? weekStart : event.start
          const due = event.due > weekEnd ? weekEnd : event.due
          segments.push({
            event,
            startColumn: dayDiff(weekStart, start),
            endColumn: dayDiff(weekStart, due),
            continuesLeft: event.start < weekStart,
            continuesRight: event.due > weekEnd,
          })
        }
        segments.sort(
          (left, right) =>
            left.startColumn - right.startColumn || left.event.title.localeCompare(right.event.title),
        )
        return segments
      }),
    [taskData.events, weeks],
  )

  const intlLocale = locale === "zh-CN" ? "zh-CN" : "en-US"
  const monthTitle = useMemo(
    () =>
      new Intl.DateTimeFormat(intlLocale, { year: "numeric", month: "long" }).format(
        new Date(cursor.year, cursor.month, 1),
      ),
    [intlLocale, cursor],
  )
  const weekdayNames = useMemo(() => {
    const formatter = new Intl.DateTimeFormat(intlLocale, { weekday: "short" })
    // 2024-01-01 is a Monday, so this yields Monday-first weekday labels.
    return Array.from({ length: 7 }, (_, index) => formatter.format(new Date(2024, 0, 1 + index)))
  }, [intlLocale])

  const monthStart = new Date(cursor.year, cursor.month, 1)
  const monthEnd = new Date(cursor.year, cursor.month + 1, 0)
  const monthCount = cells.reduce(
    (total, date) =>
      date.getMonth() === cursor.month && date.getFullYear() === cursor.year
        ? total + (eventsByDay.get(dayKey(date))?.length ?? 0)
        : total,
    0,
  )
  const monthTaskCount = taskData.events.filter(
    (event) => event.due >= monthStart && event.start <= monthEnd,
  ).length
  const monthSummary =
    monthCount === 0 && monthTaskCount === 0
      ? t("calendarEmpty")
      : [
          monthCount > 0 ? `${monthCount} · ${t("deadlineLabel")}` : null,
          monthTaskCount > 0 ? `${monthTaskCount} ${t("calendarTasks")}` : null,
        ]
          .filter(Boolean)
          .join(" · ")

  const shiftMonth = (delta: number) => {
    setExpandedDay(null)
    setCursor((current) => {
      const next = new Date(current.year, current.month + delta, 1)
      return { year: next.getFullYear(), month: next.getMonth() }
    })
  }

  const urgencyText = (days: number) =>
    days < 0
      ? `${-days} ${t("overdueUnit")}`
      : days === 0
        ? t("dueToday")
        : `${days} ${t("daysLeftUnit")}`

  return (
    <section className="wb-calendar" aria-label={t("calendar")}>
      <div className="wb-cal-head">
        <div>
          <h2>{monthTitle}</h2>
          <p className="wb-muted">{monthSummary}</p>
        </div>
        <div className="wb-cal-nav">
          <button
            type="button"
            className="wb-icon-btn"
            aria-label={t("calendarPrevMonth")}
            title={t("calendarPrevMonth")}
            onClick={() => shiftMonth(-1)}
          >
            ‹
          </button>
          <button
            type="button"
            className="wb-btn"
            onClick={() => {
              setExpandedDay(null)
              setCursor({ year: today.getFullYear(), month: today.getMonth() })
            }}
          >
            {t("calendarToday")}
          </button>
          <button
            type="button"
            className="wb-icon-btn"
            aria-label={t("calendarNextMonth")}
            title={t("calendarNextMonth")}
            onClick={() => shiftMonth(1)}
          >
            ›
          </button>
        </div>
      </div>
      <DailyDigestCard
        papers={props.papers}
        profiles={props.aiProfiles ?? []}
        onConfigure={props.onConfigureAI}
        onAskAI={props.onAskAI}
        onOpenPaper={props.onSelectPaper}
      />
      <div className="wb-cal-grid">
        <div className="wb-cal-weekday-row">
          {weekdayNames.map((name, index) => (
            <div className="wb-cal-weekday" key={`${name}-${index}`}>
              {name}
            </div>
          ))}
        </div>
        {weeks.map((week, weekIndex) => {
          const segments = segmentsByWeek[weekIndex] ?? []
          return (
            <div className="wb-cal-week" key={week[0]!.toISOString()}>
              <div className="wb-cal-week-days">
                {week.map((date) => {
                  const key = dayKey(date)
                  const list = eventsByDay.get(key) ?? []
                  const isToday = sameDay(date, today)
                  const showAll = expandedDay === key
                  const visible = showAll ? list : list.slice(0, 3)
                  return (
                    <div
                      className={[
                        "wb-cal-cell",
                        date.getMonth() === cursor.month ? "" : "wb-cal-cell--other",
                        isToday ? "wb-cal-cell--today" : "",
                      ]
                        .filter(Boolean)
                        .join(" ")}
                      key={key}
                    >
                      <span className={`wb-cal-day ${isToday ? "wb-cal-day--today" : ""}`}>
                        {date.getDate()}
                      </span>
                      {visible.map((event) => (
                        <button
                          type="button"
                          key={event.paperID}
                          className={`wb-cal-event ${urgencyClass(event.days)}`.trim()}
                          title={`${event.code} · ${formatDeadline(date)} · ${urgencyText(event.days)}`}
                          onClick={() => props.onSelectPaper(event.paperID)}
                        >
                          <span className="wb-cal-event-code">{event.code}</span>
                          <span className="wb-cal-event-title">{event.title}</span>
                        </button>
                      ))}
                      {list.length > 3 && !showAll && (
                        <button
                          type="button"
                          className="wb-cal-more"
                          onClick={() => setExpandedDay(key)}
                        >
                          +{list.length - 3}
                        </button>
                      )}
                    </div>
                  )
                })}
              </div>
              {segments.length > 0 && (
                <div
                  className="wb-cal-task-lane"
                  role="group"
                  aria-label={`${t("calendarTasks")} · ${formatDeadline(week[0]!)} – ${formatDeadline(week[6]!)}`}
                >
                  {segments.map((segment) => {
                    const classes = ["wb-cal-task-bar"]
                    if (segment.continuesLeft) classes.push("wb-cal-task-bar--continues-left")
                    if (segment.continuesRight) classes.push("wb-cal-task-bar--continues-right")
                    return (
                      <button
                        type="button"
                        key={`${segment.event.key}-${weekIndex}`}
                        className={classes.join(" ")}
                        style={{
                          gridColumn: `${segment.startColumn + 1} / ${segment.endColumn + 2}`,
                        }}
                        title={`${segment.event.title} · ${formatDeadline(segment.event.start)} – ${formatDeadline(segment.event.due)}`}
                        aria-label={`${segment.event.title} · ${segment.event.subtitle} · ${formatDeadline(segment.event.start)} – ${formatDeadline(segment.event.due)}`}
                        onClick={() =>
                          props.onSelectTask?.(segment.event.kind, segment.event.id)
                        }
                      >
                        <span className="wb-cal-task-bar-title">{segment.event.title}</span>
                      </button>
                    )
                  })}
                </div>
              )}
            </div>
          )
        })}
      </div>
      {taskData.unscheduled.length > 0 && (
        <section className="wb-cal-unscheduled" aria-label={t("calendarUnscheduledTasks")}>
          <h3>{t("calendarUnscheduledTasks")}</h3>
          <ul>
            {taskData.unscheduled.map((task) => (
              <li key={task.key}>
                <button
                  type="button"
                  className="wb-cal-unscheduled-item"
                  onClick={() => props.onSelectTask?.(task.kind, task.id)}
                >
                  <span>{task.title}</span>
                  <small>{task.subtitle}</small>
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
      <div className="wb-cal-legend">
        <span>
          <i className="wb-cal-dot wb-cal-dot--overdue" />
          {t("overdueUnit")}
        </span>
        <span>
          <i className="wb-cal-dot wb-cal-dot--today" />
          {t("dueToday")}
        </span>
        <span>
          <i className="wb-cal-dot wb-cal-dot--soon" />
          {`≤ 7 ${t("daysLeftUnit")}`}
        </span>
        <span>
          <i className="wb-cal-dot wb-cal-dot--task" />
          {t("calendarTasks")}
        </span>
      </div>
    </section>
  )
}
