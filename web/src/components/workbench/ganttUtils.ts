import { useMemo, useState } from "react"

import { parseDeadline } from "./utils"

export type GanttMode = "week" | "month"

export function startOfDay(date: Date): Date {
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
  return addDays(start, -((start.getDay() + 6) % 7))
}

function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

function daysInMonth(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0).getDate()
}

export function isoWeek(date: Date): number {
  const value = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()))
  const day = value.getUTCDay() || 7
  value.setUTCDate(value.getUTCDate() + 4 - day)
  const yearStart = new Date(Date.UTC(value.getUTCFullYear(), 0, 1))
  return Math.ceil(((value.getTime() - yearStart.getTime()) / 86_400_000 + 1) / 7)
}

export interface GanttWindow {
  mode: GanttMode
  setMode: (mode: GanttMode) => void
  anchor: Date
  setAnchor: (date: Date) => void
  windowStart: Date
  windowEnd: Date
  dayCount: number
  days: Date[]
  move: (delta: number) => void
  goToday: () => void
}

export function useGanttWindow(initialMode: GanttMode = "week"): GanttWindow {
  const [mode, setMode] = useState<GanttMode>(initialMode)
  const [anchor, setAnchor] = useState<Date>(() => startOfDay(new Date()))
  const windowStart = mode === "week" ? startOfWeek(anchor) : startOfMonth(anchor)
  const dayCount = mode === "week" ? 7 : daysInMonth(anchor)
  const days = useMemo(
    () => Array.from({ length: dayCount }, (_, index) => addDays(windowStart, index)),
    [dayCount, windowStart],
  )
  const move = (delta: number) => {
    setAnchor((current) =>
      mode === "week"
        ? addDays(current, delta * 7)
        : new Date(current.getFullYear(), current.getMonth() + delta, 1),
    )
  }
  return {
    mode,
    setMode,
    anchor,
    setAnchor,
    windowStart,
    windowEnd: addDays(windowStart, dayCount - 1),
    dayCount,
    days,
    move,
    goToday: () => setAnchor(startOfDay(new Date())),
  }
}


export function ganttBar(
  startDate: string,
  dueDate: string,
  windowStart: Date,
  dayCount: number,
): { left: number; width: number } | null {
  const start = parseDeadline(startDate) ?? parseDeadline(dueDate)
  const due = parseDeadline(dueDate) ?? start
  if (!start || !due) return null
  const offset = diffDays(windowStart, start)
  const length = diffDays(start, due) + 1
  if (offset + length <= 0 || offset >= dayCount) return null
  const visibleStart = Math.max(offset, 0)
  const visibleEnd = Math.min(offset + length, dayCount)
  return {
    left: (visibleStart / dayCount) * 100,
    width: Math.max(((visibleEnd - visibleStart) / dayCount) * 100, 0.8),
  }
}
