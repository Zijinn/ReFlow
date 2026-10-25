import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarBlank, CaretLeft, CaretRight, Plus, WarningCircle } from "@phosphor-icons/react"
import { useMemo, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from "react"

import {
  listQuickTasks,
  listTaskThreads,
  updateQuickTask,
  updateTaskAssignment,
} from "../../api/client"
import type { TaskItemStatus } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { useOnlineState } from "../../lib/online"
import { toast } from "../../store/toast"
import { QUICK_TASK_QUERY_KEY, TASK_QUERY_KEY } from "./taskQueryKeys"
import { formatDeadline, parseDeadline } from "./utils"

type TimelineMode = "week" | "month"
type TimelineState = "active" | "closed" | "overdue" | "idle"

interface TimelineRow {
  key: string
  kind: "quick" | "assignment"
  id: string
  title: string
  subtitle: string
  start: string
  due: string
  state: TimelineState
}

interface TimelineLayer {
  id: string
  name: string
  rows: TimelineRow[]
}

interface TimelineThread {
  id: string
  name: string
  layers: TimelineLayer[]
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate())
}

function addDays(date: Date, count: number): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + count)
}

function diffDays(from: Date, to: Date): number {
  return Math.round((startOfDay(to).getTime() - startOfDay(from).getTime()) / 86_400_000)
}

function startOfWeek(date: Date): Date {
  const start = startOfDay(date)
  const offset = (start.getDay() + 6) % 7
  return addDays(start, -offset)
}

function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

function daysInMonth(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0).getDate()
}

function shiftDate(raw: string, delta: number): string {
  const date = parseDeadline(raw)
  return date ? formatDeadline(addDays(date, delta)) : raw
}

function todayValue(): string {
  return formatDeadline(startOfDay(new Date()))
}

function shortDate(raw: string): string {
  const date = parseDeadline(raw)
  return date ? `${date.getMonth() + 1}/${date.getDate()}` : "—"
}

function dateRange(start: string, due: string): string {
  if (!start && !due) return ""
  return `${start ? shortDate(start) : "—"} – ${due ? shortDate(due) : "—"}`
}

function assignmentSubtitle(
  layerName: string,
  start: string,
  due: string,
  done: number,
  total: number,
  unit: string,
): string {
  const progress = `${layerName} · ${done}/${total} ${unit}`
  const range = dateRange(start, due)
  return range ? `${range} · ${progress}` : progress
}

function stateForItem(status: TaskItemStatus, closed: boolean): TimelineState {
  if (closed) return "closed"
  if (status === "overdue") return "overdue"
  if (status === "locked" || status === "unstarted") return "idle"
  return "active"
}

function isInWindow(start: string, due: string, windowStart: Date, dayCount: number): boolean {
  const startDate = parseDeadline(start) ?? parseDeadline(due)
  const dueDate = parseDeadline(due) ?? parseDeadline(start)
  if (!startDate || !dueDate) return false
  const startOffset = diffDays(windowStart, startDate)
  const endOffset = diffDays(windowStart, dueDate)
  return endOffset >= 0 && startOffset < dayCount
}

function isoWeek(date: Date): number {
  const value = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()))
  const day = value.getUTCDay() || 7
  value.setUTCDate(value.getUTCDate() + 4 - day)
  const yearStart = new Date(Date.UTC(value.getUTCFullYear(), 0, 1))
  return Math.ceil(((value.getTime() - yearStart.getTime()) / 86_400_000 + 1) / 7)
}

export function TaskTimeline(props: { offline?: boolean; onNewQuick: () => void }) {
  const { t, locale } = useTranslation()
  const queryClient = useQueryClient()
  const detected = useOnlineState()
  const online = props.offline === undefined ? detected : !props.offline
  const [mode, setMode] = useState<TimelineMode>("week")
  const [anchor, setAnchor] = useState<Date>(() => startOfDay(new Date()))

  const threadsQuery = useQuery({
    queryKey: TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listTaskThreads(signal),
  })
  const quickQuery = useQuery({
    queryKey: QUICK_TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listQuickTasks(signal),
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: TASK_QUERY_KEY })
    void queryClient.invalidateQueries({ queryKey: QUICK_TASK_QUERY_KEY })
  }
  const updateMutation = useMutation({
    mutationFn: async (input: {
      kind: TimelineRow["kind"]
      id: string
      start: string
      due: string
    }) => {
      if (input.kind === "quick") {
        await updateQuickTask(input.id, { start_date: input.start, due_date: input.due })
      } else {
        await updateTaskAssignment(input.id, { start_date: input.start, due_date: input.due })
      }
    },
    onSuccess: invalidate,
    onError: () => toast(t("taskTimelineDragFailed")),
  })

  const requireOnline = (): boolean => {
    if (online) return true
    toast(t("workbenchOfflineHint"))
    return false
  }

  const windowStart = mode === "week" ? startOfWeek(anchor) : startOfMonth(anchor)
  const dayCount = mode === "week" ? 7 : daysInMonth(anchor)
  const windowEnd = addDays(windowStart, dayCount - 1)
  const today = startOfDay(new Date())
  const dayKeys = Array.from({ length: dayCount }, (_, index) => addDays(windowStart, index))
  const weekdayFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { weekday: "short" }),
    [locale],
  )
  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }),
    [locale],
  )
  const monthFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { year: "numeric", month: "long" }),
    [locale],
  )
  const rangeLabel =
    mode === "week"
      ? `${dayFormat.format(windowStart)} – ${dayFormat.format(windowEnd)}`
      : monthFormat.format(windowStart)
  const weekLabel = t("taskTimelineWeekNumber").replace("{week}", String(isoWeek(windowStart)))

  const threads = useMemo(() => threadsQuery.data?.items ?? [], [threadsQuery.data?.items])
  const quickTasks = useMemo(() => quickQuery.data?.items ?? [], [quickQuery.data?.items])

  const quickRows = useMemo<TimelineRow[]>(
    () =>
      quickTasks.map((task) => {
        const start = task.start_date && task.due_date && task.start_date > task.due_date ? task.due_date : task.start_date
        const due = task.start_date && task.due_date && task.start_date > task.due_date ? task.start_date : task.due_date
        return {
        key: `quick-${task.id}`,
        kind: "quick",
        id: task.id,
        title: task.title,
        subtitle: dateRange(start, due) || t("taskViewQuick"),
        start,
        due,
        state: task.completed
          ? "closed"
          : task.due_date && task.due_date < todayValue()
            ? "overdue"
            : "active",
        }
      }),
    [quickTasks, t],
  )

  const longThreads = useMemo<TimelineThread[]>(() => {
    const visible = [...threads].filter((thread) => !thread.archived).sort(
      (left, right) => left.position - right.position,
    )
    return visible.map((thread) => ({
      id: thread.id,
      name: thread.name,
      layers: [...thread.layers]
        .sort((left, right) => left.position - right.position)
        .map((layer) => ({
          id: layer.id,
          name: layer.name,
          rows: thread.items
            .flatMap((item) =>
              item.assignments
                .filter((assignment) => assignment.layer_id === layer.id)
                .map((assignment) => {
                  const start = assignment.start_date && assignment.due_date && assignment.start_date > assignment.due_date ? assignment.due_date : assignment.start_date
                  const due = assignment.start_date && assignment.due_date && assignment.start_date > assignment.due_date ? assignment.start_date : assignment.due_date
                  return {
                  key: `assignment-${assignment.id}`,
                  kind: "assignment" as const,
                  id: assignment.id,
                  title: item.title,
                  subtitle: assignmentSubtitle(
                    layer.name,
                    start,
                    due,
                    assignment.steps.filter((step) => step.done).length,
                    assignment.steps.length,
                    t("taskStepUnit"),
                  ),
                  start,
                  due,
                  state: stateForItem(item.status, assignment.closed),
                  }
                }),
            )
            .sort((left, right) => left.title.localeCompare(right.title)),
        })),
    }))
  }, [threads, t])

  const unassignedLongRows = useMemo<TimelineRow[]>(
    () =>
      threads
        .filter((thread) => !thread.archived)
        .flatMap((thread) =>
          thread.items
            .filter((item) => !item.closed && item.assignments.length === 0)
            .map((item) => ({
              key: `item-${item.id}`,
              kind: "assignment" as const,
              id: item.id,
              title: item.title,
              subtitle: thread.name,
              start: "",
              due: "",
              state: stateForItem(item.status, item.closed),
            })),
        ),
    [threads],
  )

  const allRows = useMemo(
    () => [
      ...quickRows,
      ...longThreads.flatMap((thread) => thread.layers.flatMap((layer) => layer.rows)),
      ...unassignedLongRows,
    ],
    [longThreads, quickRows, unassignedLongRows],
  )

  const stats = (() => {
    let longInWindow = 0
    let quickInWindow = 0
    let overdue = 0
    let doneInWindow = 0
    let unscheduled = 0
    for (const row of quickRows) {
      const inWindow = isInWindow(row.start, row.due, windowStart, dayCount)
      if (inWindow) quickInWindow += 1
      if (!row.start && !row.due && row.state !== "closed") unscheduled += 1
      if (row.state === "overdue") overdue += 1
      if (row.state === "closed") {
        const task = quickTasks.find((entry) => entry.id === row.id)
        const done = task?.completed_at ? new Date(task.completed_at) : null
        if (done && done >= windowStart && done <= addDays(windowEnd, 1)) doneInWindow += 1
      }
    }
    for (const thread of threads) {
      for (const item of thread.items) {
        for (const assignment of item.assignments) {
          const inWindow = isInWindow(assignment.start_date, assignment.due_date, windowStart, dayCount)
          if (inWindow) longInWindow += 1
          if (!assignment.start_date && !assignment.due_date && !assignment.closed) unscheduled += 1
          if (
            !assignment.closed &&
            assignment.due_date &&
            assignment.due_date < todayValue()
          ) {
            overdue += 1
          }
          if (assignment.closed && assignment.closed_at) {
            const done = new Date(assignment.closed_at)
            if (done >= windowStart && done <= addDays(windowEnd, 1)) doneInWindow += 1
          }
        }
      }
    }
    // A long-task card with no placement at all is still work the owner has
    // not scheduled; it belongs in the same 未排期 bucket as a dated layer slot
    // that has not received its dates yet.
    unscheduled += unassignedLongRows.length
    return { longInWindow, quickInWindow, overdue, doneInWindow, unscheduled }
  })()

  const scheduledQuick = quickRows.filter((row) => row.start || row.due)
  const scheduledLong = longThreads
    .map((thread) => ({
      ...thread,
      layers: thread.layers
        .map((layer) => ({
          ...layer,
          rows: layer.rows.filter((row) => row.start || row.due),
        }))
        .filter((layer) => layer.rows.length > 0),
    }))
    .filter((thread) => thread.layers.length > 0)
  const unscheduled = allRows.filter((row) => !row.start && !row.due && row.state !== "closed")

  const move = (delta: number) => {
    setAnchor((current) =>
      mode === "week"
        ? addDays(current, delta * 7)
        : new Date(current.getFullYear(), current.getMonth() + delta, 1),
    )
  }

  const shiftRow = (row: TimelineRow, delta: number) => {
    if (!requireOnline()) return
    updateMutation.mutate({
      kind: row.kind,
      id: row.id,
      start: shiftDate(row.start, delta),
      due: shiftDate(row.due, delta),
    })
  }

  const loading = threadsQuery.isPending || quickQuery.isPending
  const errored = threadsQuery.isError || quickQuery.isError

  return (
    <div className="wb-tm-timeline">
      <div className="wb-tm-tl-toolbar">
        <div className="wb-tm-tl-nav">
          <button
            type="button"
            className="wb-icon-btn"
            aria-label={t("taskTimelinePrev")}
            title={t("taskTimelinePrev")}
            onClick={() => move(-1)}
          >
            <CaretLeft size={15} weight="bold" aria-hidden="true" />
          </button>
          <button type="button" className="wb-btn" onClick={() => setAnchor(startOfDay(new Date()))}>
            {t("taskTimelineToday")}
          </button>
          <button
            type="button"
            className="wb-icon-btn"
            aria-label={t("taskTimelineNext")}
            title={t("taskTimelineNext")}
            onClick={() => move(1)}
          >
            <CaretRight size={15} weight="bold" aria-hidden="true" />
          </button>
          <div className="wb-tm-tl-range">
            <b>{rangeLabel}</b>
            <span>{weekLabel}</span>
          </div>
        </div>
        <div className="wb-tm-tl-controls">
          <div className="wb-tm-tl-mode" role="group" aria-label={t("taskTimelineMode")}>
            {(["week", "month"] as const).map((item) => (
              <button
                key={item}
                type="button"
                aria-pressed={mode === item}
                className={mode === item ? "wb-tm-tl-mode--active" : ""}
                onClick={() => setMode(item)}
              >
                {item === "week" ? t("taskTimelineWeek") : t("taskTimelineMonth")}
              </button>
            ))}
          </div>
          <button type="button" className="wb-btn" onClick={props.onNewQuick}>
            <Plus size={14} weight="bold" aria-hidden="true" />
            {t("taskQuickAdd")}
          </button>
        </div>
      </div>

      <div className="wb-tm-tl-stats">
        <TlStat label={t("taskTimelineStatLong")} value={stats.longInWindow} />
        <TlStat label={t("taskTimelineStatQuick")} value={stats.quickInWindow} />
        <TlStat label={t("taskTimelineStatOverdue")} value={stats.overdue} tone="warn" />
        <TlStat label={t("taskTimelineStatDone")} value={stats.doneInWindow} tone="done" />
        <TlStat label={t("taskTimelineStatUnscheduled")} value={stats.unscheduled} />
      </div>

      <div className="wb-tm-tl-hint">
        <CalendarBlank size={13} aria-hidden="true" />
        {t("taskTimelineHint")}
      </div>

      {loading ? (
        <div className="wb-empty">{t("loading")}</div>
      ) : errored ? (
        <div className="wb-empty" role="alert">
          <p>{t("taskLoadFailed")}</p>
          <button
            type="button"
            className="wb-btn"
            onClick={() => {
              void threadsQuery.refetch()
              void quickQuery.refetch()
            }}
          >
            {t("retry")}
          </button>
        </div>
      ) : (
        <div className="wb-tm-tl-card" style={{ "--tm-days": dayCount } as CSSProperties}>
          <div className="wb-tm-tl-header">
            <div className="wb-tm-tl-header-label">{t("taskTimelineColumns")}</div>
            <div className="wb-tm-tl-header-days">
              {dayKeys.map((day) => {
                const isToday = day.getTime() === today.getTime()
                const isWeekend = day.getDay() === 0 || day.getDay() === 6
                return (
                  <div
                    key={day.toISOString()}
                    className={`wb-tm-tl-day ${isToday ? "wb-tm-tl-day--today" : ""} ${isWeekend ? "wb-tm-tl-day--weekend" : ""}`}
                  >
                    <span>{mode === "week" ? weekdayFormat.format(day) : day.getDate()}</span>
                    <b>{day.getDate()}</b>
                  </div>
                )
              })}
            </div>
          </div>

          {scheduledQuick.length > 0 && (
            <>
              <TimelineGroupHead label={t("taskViewQuick")} count={scheduledQuick.length} />
              {scheduledQuick.map((row) => (
                <TimelineRowView
                  key={row.key}
                  row={row}
                  windowStart={windowStart}
                  dayCount={dayCount}
                  today={today}
                  onShift={shiftRow}
                />
              ))}
            </>
          )}

          {scheduledLong.map((thread) => (
            <div key={thread.id} className="wb-tm-tl-thread">
              <TimelineGroupHead label={thread.name} count={thread.layers.reduce((sum, layer) => sum + layer.rows.length, 0)} />
              {thread.layers.map((layer) => (
                <div key={layer.id} className="wb-tm-tl-layer">
                  <div className="wb-tm-tl-layer-head">
                    <span>{layer.name}</span>
                    <b>{layer.rows.length}</b>
                  </div>
                  {layer.rows.map((row) => (
                    <TimelineRowView
                      key={row.key}
                      row={row}
                      windowStart={windowStart}
                      dayCount={dayCount}
                      today={today}
                      onShift={shiftRow}
                    />
                  ))}
                </div>
              ))}
            </div>
          ))}

          {unscheduled.length > 0 && (
            <>
              <TimelineGroupHead label={t("taskTimelineUnscheduled")} count={unscheduled.length} />
              {unscheduled.map((row) => (
                <div key={row.key} className="wb-tm-tl-row wb-tm-tl-row--unscheduled">
                  <div className="wb-tm-tl-label">
                    <span>{row.title}</span>
                    <small>{row.subtitle}</small>
                  </div>
                  <div className="wb-tm-tl-track">
                    <span className="wb-tm-tl-unscheduled-text">{t("taskTimelineUnscheduled")}</span>
                  </div>
                </div>
              ))}
            </>
          )}

          {scheduledQuick.length === 0 && scheduledLong.length === 0 && unscheduled.length === 0 && (
            <div className="wb-tm-tl-empty">{t("taskTimelineEmpty")}</div>
          )}
        </div>
      )}
    </div>
  )
}

function TlStat(props: { label: string; value: number; tone?: "warn" | "done" }) {
  return (
    <div className={`wb-tm-tl-stat ${props.tone ? `wb-tm-tl-stat--${props.tone}` : ""}`}>
      <b>{props.value}</b>
      <span>{props.label}</span>
    </div>
  )
}

function TimelineGroupHead(props: { label: string; count: number }) {
  return (
    <div className="wb-tm-tl-row wb-tm-tl-group-head">
      <div className="wb-tm-tl-label">
        <span>{props.label}</span>
        <b>{props.count}</b>
      </div>
      <div className="wb-tm-tl-track" />
    </div>
  )
}

function TimelineRowView(props: {
  row: TimelineRow
  windowStart: Date
  dayCount: number
  today: Date
  onShift: (row: TimelineRow, delta: number) => void
}) {
  const { row, windowStart, dayCount, today, onShift } = props
  const todayOffset = diffDays(windowStart, today)
  return (
    <div className="wb-tm-tl-row">
      <div className="wb-tm-tl-label">
        <span>{row.title}</span>
        <small>{row.subtitle}</small>
      </div>
      <div className="wb-tm-tl-track">
        {todayOffset >= 0 && todayOffset < dayCount && (
          <span
            className="wb-tm-tl-today"
            style={{ left: `${(todayOffset / dayCount) * 100}%` }}
            aria-hidden="true"
          />
        )}
        <TimelineBar row={row} windowStart={windowStart} dayCount={dayCount} onShift={onShift} />
      </div>
    </div>
  )
}

function TimelineBar(props: {
  row: TimelineRow
  windowStart: Date
  dayCount: number
  onShift: (row: TimelineRow, delta: number) => void
}) {
  const { row, windowStart, dayCount, onShift } = props
  const [drag, setDrag] = useState<{ startX: number; pxPerDay: number; delta: number } | null>(null)
  const start = parseDeadline(row.start) ?? parseDeadline(row.due)
  const due = parseDeadline(row.due) ?? start
  if (!start || !due) return null
  const offset = diffDays(windowStart, start)
  const length = diffDays(start, due) + 1
  if (offset + length <= 0 || offset >= dayCount) return null
  const visibleStart = Math.max(offset, 0)
  const visibleEnd = Math.min(offset + length, dayCount)
  const left = (visibleStart / dayCount) * 100
  const width = Math.max(((visibleEnd - visibleStart) / dayCount) * 100, 0.8)
  const pixelPerDay = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const track = event.currentTarget.parentElement
    return Math.max((track?.clientWidth ?? 1) / dayCount, 1)
  }

  return (
    <button
      type="button"
      className={`wb-tm-tl-bar wb-tm-tl-bar--${row.state} ${drag ? "wb-tm-tl-bar--dragging" : ""}`}
      style={{
        left: `calc(${left}% + ${((drag?.delta ?? 0) / dayCount) * 100}%)`,
        width: `${width}%`,
      }}
      title={row.title}
      aria-label={`${row.title} · ${row.start || "—"} – ${row.due || "—"}`}
      onPointerDown={(event) => {
        event.currentTarget.focus()
        event.preventDefault()
        event.currentTarget.setPointerCapture(event.pointerId)
        setDrag({ startX: event.clientX, pxPerDay: pixelPerDay(event), delta: 0 })
      }}
      onKeyDown={(event) => {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
        event.preventDefault()
        onShift(row, event.key === "ArrowLeft" ? -1 : 1)
      }}
      onPointerMove={(event) => {
        if (!drag) return
        event.preventDefault()
        setDrag({
          ...drag,
          delta: Math.round((event.clientX - drag.startX) / drag.pxPerDay),
        })
      }}
      onPointerUp={() => {
        if (!drag) return
        const delta = drag.delta
        setDrag(null)
        if (delta !== 0) onShift(row, delta)
      }}
      onPointerCancel={() => setDrag(null)}
    >
      {row.state === "overdue" && <WarningCircle size={12} weight="fill" aria-hidden="true" />}
      <span>{row.title}</span>
    </button>
  )
}
