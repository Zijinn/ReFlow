import { CaretLeft, CaretRight } from "@phosphor-icons/react"
import { useMemo } from "react"
import type { CSSProperties } from "react"

import { useTranslation } from "../../lib/i18n"
import { isoWeek, startOfDay, type GanttWindow } from "./ganttUtils"

export function GanttToolbar(props: { window: GanttWindow }) {
  const { t, locale } = useTranslation()
  const { window } = props
  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }),
    [locale],
  )
  const monthFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { year: "numeric", month: "long" }),
    [locale],
  )
  return (
    <div className="wb-gantt-toolbar">
      <div className="wb-gantt-nav">
        <button
          type="button"
          className="wb-icon-btn"
          aria-label={t("taskTimelinePrev")}
          title={t("taskTimelinePrev")}
          onClick={() => window.move(-1)}
        >
          <CaretLeft size={15} weight="bold" aria-hidden="true" />
        </button>
        <button type="button" className="wb-btn" onClick={window.goToday}>
          {t("taskTimelineToday")}
        </button>
        <button
          type="button"
          className="wb-icon-btn"
          aria-label={t("taskTimelineNext")}
          title={t("taskTimelineNext")}
          onClick={() => window.move(1)}
        >
          <CaretRight size={15} weight="bold" aria-hidden="true" />
        </button>
        <div className="wb-gantt-range">
          <b>
            {window.mode === "week"
              ? `${dayFormat.format(window.windowStart)} – ${dayFormat.format(window.windowEnd)}`
              : monthFormat.format(window.windowStart)}
          </b>
          {window.mode === "week" && (
            <span>
              {t("taskTimelineWeekNumber").replace("{week}", String(isoWeek(window.windowStart)))}
            </span>
          )}
        </div>
      </div>
      <div className="wb-gantt-mode" role="group" aria-label={t("taskTimelineMode")}>
        {(["week", "month"] as const).map((mode) => (
          <button
            key={mode}
            type="button"
            aria-pressed={window.mode === mode}
            className={window.mode === mode ? "wb-gantt-mode--active" : ""}
            onClick={() => window.setMode(mode)}
          >
            {mode === "week" ? t("taskTimelineWeek") : t("taskTimelineMonth")}
          </button>
        ))}
      </div>
    </div>
  )
}

export function GanttDays(props: { window: GanttWindow }) {
  const { locale } = useTranslation()
  const weekdayFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { weekday: "short" }),
    [locale],
  )
  const today = startOfDay(new Date())
  return (
    <div
      className="wb-gantt-days"
      style={{ "--gantt-days": props.window.dayCount } as CSSProperties}
    >
      {props.window.days.map((day) => {
        const isToday = day.getTime() === today.getTime()
        const isWeekend = day.getDay() === 0 || day.getDay() === 6
        return (
          <div
            key={day.toISOString()}
            className={`wb-gantt-day ${isToday ? "wb-gantt-day--today" : ""} ${
              isWeekend ? "wb-gantt-day--weekend" : ""
            }`}
            title={weekdayFormat.format(day)}
          >
            <span>{props.window.mode === "week" ? weekdayFormat.format(day) : ""}</span>
            {(props.window.mode === "week" || day.getDate() % 2 === 1) && <b>{day.getDate()}</b>}
          </div>
        )
      })}
    </div>
  )
}
