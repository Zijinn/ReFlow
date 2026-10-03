import { useMemo, useState } from "react"

import type { AIProfile, ResearchKind, ResearchPaper } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { DailyDigestCard } from "./DailyDigest"
import { daysUntil, displayID, formatDeadline, parseDeadline } from "./utils"

interface DeadlineEvent {
  paperID: string
  code: string
  title: string
  days: number
}

function dayKey(date: Date): number {
  return date.getFullYear() * 10000 + (date.getMonth() + 1) * 100 + date.getDate()
}

function sameDay(a: Date, b: Date): boolean {
  return dayKey(a) === dayKey(b)
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

  const monthCount = cells.reduce(
    (total, date) => total + (eventsByDay.get(dayKey(date))?.length ?? 0),
    0,
  )

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
          <p className="wb-muted">
            {monthCount ? `${monthCount} · ${t("deadlineLabel")}` : t("calendarEmpty")}
          </p>
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
        {weekdayNames.map((name, index) => (
          <div className="wb-cal-weekday" key={`${name}-${index}`}>
            {name}
          </div>
        ))}
        {cells.map((date) => {
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
                <button type="button" className="wb-cal-more" onClick={() => setExpandedDay(key)}>
                  +{list.length - 3}
                </button>
              )}
            </div>
          )
        })}
      </div>
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
      </div>
    </section>
  )
}
