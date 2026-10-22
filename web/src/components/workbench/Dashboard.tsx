import "./overview-dashboard.css"

import {
  Books,
  Brain,
  CalendarBlank,
  ChartPieSlice,
  ListChecks,
  NewspaperClipping,
  NotePencil,
  PaperPlaneTilt,
  Sparkle,
  Star,
  Tag,
  WarningCircle,
} from "@phosphor-icons/react"
import { useMemo } from "react"

import type { AIProfile, ResearchPaper, ResearchTag } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { computeProgress } from "../../lib/research"
import { DailyDigestCard } from "./DailyDigest"
import { daysUntil, parseDeadline } from "./utils"

export type DashboardTab = "research" | "submitted" | "published" | "calendar"

const CHART_COLORS = {
  blue: "var(--wb-hue-blue)",
  amber: "var(--wb-hue-amber)",
  teal: "var(--wb-hue-teal)",
  warm: "var(--wb-hue-orange)",
} as const

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate())
}

function startOfWeek(date: Date): Date {
  const start = startOfDay(date)
  const offset = (start.getDay() + 6) % 7
  start.setDate(start.getDate() - offset)
  return start
}

function parsePaperDate(raw: string): Date | null {
  const date = parseDeadline(raw)
  if (date) return date
  const parsed = new Date(raw)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

function percent(part: number, total: number): string {
  if (total <= 0) return "0%"
  return `${Math.round((part / total) * 100)}%`
}

function monthDay(date: Date): string {
  return `${date.getMonth() + 1}月${date.getDate()}日`
}

function daysBetween(from: Date, to: Date): number {
  return Math.round((startOfDay(to).getTime() - startOfDay(from).getTime()) / 86_400_000)
}

export function Dashboard(props: {
  research: ResearchPaper[]
  submitted: ResearchPaper[]
  published: ResearchPaper[]
  tags?: ResearchTag[]
  aiProfiles?: AIProfile[]
  onConfigureAI?: () => void
  onAskAI?: () => void
  onOpenPaper?: (paperID: string) => void
  onNavigate: (tab: DashboardTab) => void
  onOpenTasks?: () => void
  onOpenPreferences?: () => void
  onOpenReader?: () => void
  onRegisterDigestRun?: (run: (() => void) | null) => void
  onDigestStateChange?: (state: { running: boolean; hasText: boolean }) => void
}) {
  const { t } = useTranslation()
  const { research, submitted, published } = props

  const avg = useMemo(() => {
    if (research.length === 0) return 0
    const total = research.reduce((sum, paper) => sum + computeProgress(paper.stages), 0)
    return Math.round(total / research.length)
  }, [research])

  const overdueItems = useMemo(
    () =>
      [...research, ...submitted].filter((paper) => {
        const date = parseDeadline(paper.deadline)
        return date !== null && daysUntil(date) < 0
      }),
    [research, submitted],
  )
  const dueSoon = useMemo(
    () =>
      [...research, ...submitted].filter((paper) => {
        const date = parseDeadline(paper.deadline)
        if (!date) return false
        const days = daysUntil(date)
        return days >= 0 && days <= 7
      }).length,
    [research, submitted],
  )

  const priorityTag = useMemo(
    () =>
      [...(props.tags ?? [])].sort((left, right) => left.position - right.position)[0] ??
      undefined,
    [props.tags],
  )
  const priorityProjects = useMemo(
    () =>
      priorityTag ? research.filter((paper) => paper.tag_ids.includes(priorityTag.id)) : [],
    [priorityTag, research],
  )

  const progressBuckets = useMemo(() => {
    let nearDone = 0
    let midway = 0
    let early = 0
    for (const paper of research) {
      const progress = computeProgress(paper.stages)
      if (progress >= 80) nearDone += 1
      else if (progress >= 40) midway += 1
      else early += 1
    }
    return { nearDone, midway, early }
  }, [research])

  const weeklyUpdates = useMemo(() => {
    const currentWeek = startOfWeek(new Date())
    const buckets = Array.from({ length: 8 }, () => 0)
    for (const paper of [...research, ...submitted, ...published]) {
      const date = parsePaperDate(paper.last_updated)
      if (!date) continue
      const weeksAgo = Math.round(daysBetween(startOfWeek(date), currentWeek) / 7)
      const index = 7 - weeksAgo
      if (index >= 0 && index < buckets.length) buckets[index] = (buckets[index] ?? 0) + 1
    }
    return buckets
  }, [published, research, submitted])

  const digestPapers = useMemo(() => [...research, ...submitted], [research, submitted])
  const paperTotal = research.length + submitted.length + published.length
  const hasThisWeekUpdate = useMemo(() => {
    const currentWeek = startOfWeek(new Date())
    return [...research, ...submitted, ...published].some((paper) => {
      const date = parsePaperDate(paper.last_updated)
      return date !== null && daysBetween(startOfWeek(date), currentWeek) === 0
    })
  }, [published, research, submitted])

  const runDigest = () => {
    document.querySelector<HTMLElement>(".wb-main")?.scrollTo({ top: 0, behavior: "smooth" })
    // The header owns the registered run; this quick entry only jumps and calls.
    const header = document.querySelector<HTMLButtonElement>(".wb-overview-actionbar .wb-btn")
    header?.click()
  }

  return (
    <div className="wb-overview">
      <section className="wb-ov-section">
        <SectionLabel>{t("overviewToday")}</SectionLabel>
        <DailyDigestCard
          papers={digestPapers}
          profiles={props.aiProfiles ?? []}
          onConfigure={props.onConfigureAI}
          onAskAI={props.onAskAI}
          onOpenPaper={props.onOpenPaper}
          hideAction
          onRegisterRun={props.onRegisterDigestRun}
          onStateChange={props.onDigestStateChange}
        />
      </section>

      <section className="wb-ov-section">
        <SectionLabel>{t("overviewSummary")}</SectionLabel>
        <div className="wb-ov-metrics">
          <MetricCard
            label={t("workingPapers")}
            value={research.length}
            foot={t("overviewViewAll")}
            badge={hasThisWeekUpdate ? t("overviewThisWeek") : undefined}
            Icon={NotePencil}
            onClick={() => props.onNavigate("research")}
          />
          <MetricCard
            label={t("submissions")}
            value={submitted.length}
            foot={t("overviewViewAll")}
            Icon={PaperPlaneTilt}
            onClick={() => props.onNavigate("submitted")}
          />
          <MetricCard
            label={t("publications")}
            value={published.length}
            foot={t("overviewViewAll")}
            Icon={Books}
            onClick={() => props.onNavigate("published")}
          />
          <MetricCard
            label={t("averageProgress")}
            value={`${avg}%`}
            foot={t("overviewBasedOnWorking").replace("{count}", String(research.length))}
            Icon={ChartPieSlice}
            onClick={() => props.onNavigate("research")}
          />
          <MetricCard
            label={t("overviewDueSoon")}
            value={dueSoon}
            foot={dueSoon === 0 ? t("overviewNoneDueSoon") : t("overviewDueInDays").replace("{count}", String(dueSoon))}
            Icon={CalendarBlank}
            onClick={() => props.onNavigate("calendar")}
          />
          <MetricCard
            label={t("overviewOverdue")}
            value={overdueItems.length}
            foot={overdueItems[0]?.title ?? t("overviewNoneOverdue")}
            badge={overdueItems.length > 0 ? t("overviewNeedsAttention") : undefined}
            tone={overdueItems.length > 0 ? "warn" : undefined}
            Icon={WarningCircle}
            onClick={() => props.onNavigate("research")}
          />
          <MetricCard
            label={t("overviewPriority")}
            value={priorityProjects.length}
            foot={priorityTag?.name ?? t("overviewNoPriorityTag")}
            Icon={Star}
            onClick={() => props.onNavigate("research")}
          />
          <MetricCard
            label={t("overviewTags")}
            value={props.tags?.length ?? 0}
            foot={t("overviewManageTags")}
            Icon={Tag}
            onClick={() => props.onOpenPreferences?.()}
          />
        </div>
      </section>

      <section className="wb-ov-section">
        <SectionLabel>{t("overviewInsights")}</SectionLabel>
        <div className="wb-ov-charts">
          <article className="wb-ov-card wb-ov-chart">
            <ChartHead title={t("overviewPaperMix")} sub={t("overviewPaperMixDesc")} tag={t("overviewByStatus")} />
            <div className="wb-ov-donut-body">
              <Donut
                segments={[
                  { label: t("researchCount"), value: research.length, color: CHART_COLORS.blue },
                  { label: t("submittedCount"), value: submitted.length, color: CHART_COLORS.amber },
                  { label: t("publishedCount"), value: published.length, color: CHART_COLORS.teal },
                ]}
                center={paperTotal}
                centerSub={t("overviewPapersUnit")}
              />
              <div className="wb-ov-legend">
                {[
                  { label: t("researchCount"), value: research.length, color: CHART_COLORS.blue },
                  { label: t("submittedCount"), value: submitted.length, color: CHART_COLORS.amber },
                  { label: t("publishedCount"), value: published.length, color: CHART_COLORS.teal },
                ].map((entry) => (
                  <div className="wb-ov-legend-row" key={entry.label}>
                    <i style={{ background: entry.color }} />
                    <span>{entry.label}</span>
                    <b>{entry.value}</b>
                    <em>{percent(entry.value, paperTotal)}</em>
                  </div>
                ))}
              </div>
            </div>
          </article>

          <article className="wb-ov-card wb-ov-chart wb-ov-chart--wide">
            <ChartHead
              title={t("overviewTrend")}
              sub={t("overviewTrendDesc")}
              tag={t("overviewLast8Weeks")}
            />
            <TrendChart values={weeklyUpdates} />
            <p className="wb-ov-chart-foot">{t("overviewTrendNote")}</p>
          </article>

          <article className="wb-ov-card wb-ov-chart">
            <ChartHead
              title={t("overviewProgressMix")}
              sub={t("overviewProgressMixDesc")}
              tag={t("overviewWorkingTag").replace("{count}", String(research.length))}
            />
            <ProgressChart
              avg={avg}
              segments={[
                { label: t("overviewNearDone"), value: progressBuckets.nearDone, color: CHART_COLORS.blue, note: "≥80%" },
                { label: t("overviewMidway"), value: progressBuckets.midway, color: CHART_COLORS.teal, note: "40–79%" },
                { label: t("overviewEarly"), value: progressBuckets.early, color: CHART_COLORS.warm, note: "<40%" },
              ]}
              total={research.length}
            />
          </article>
        </div>
      </section>

      <section className="wb-ov-section">
        <SectionLabel>{t("overviewQuick")}</SectionLabel>
        <div className="wb-ov-quick">
          <QuickEntry
            Icon={NotePencil}
            title={t("overviewQuickResearch")}
            desc={t("overviewQuickResearchDesc").replace("{count}", String(research.length))}
            onClick={() => props.onNavigate("research")}
          />
          <QuickEntry
            Icon={PaperPlaneTilt}
            title={t("overviewQuickSubmitted")}
            desc={t("overviewQuickSubmittedDesc").replace("{count}", String(submitted.length))}
            onClick={() => props.onNavigate("submitted")}
          />
          <QuickEntry
            Icon={Books}
            title={t("overviewQuickPublished")}
            desc={t("overviewQuickPublishedDesc").replace("{count}", String(published.length))}
            onClick={() => props.onNavigate("published")}
          />
          <QuickEntry
            Icon={CalendarBlank}
            title={t("calendar")}
            desc={t("overviewQuickCalendarDesc")
              .replace("{overdue}", String(overdueItems.length))
              .replace("{soon}", String(dueSoon))}
            onClick={() => props.onNavigate("calendar")}
          />
          <QuickEntry
            Icon={Tag}
            title={t("overviewTags")}
            desc={t("overviewQuickTagsDesc").replace("{count}", String(props.tags?.length ?? 0))}
            onClick={() => props.onOpenPreferences?.()}
          />
          <QuickEntry
            Icon={Star}
            title={t("overviewPriority")}
            desc={t("overviewQuickPriorityDesc").replace("{count}", String(priorityProjects.length))}
            onClick={() => props.onNavigate("research")}
          />
          <QuickEntry
            Icon={Sparkle}
            title={t("overviewQuickDigest")}
            desc={t("overviewQuickDigestDesc")}
            onClick={runDigest}
          />
          <QuickEntry
            Icon={ListChecks}
            title={t("moduleTasks")}
            desc={t("overviewQuickTasksDesc")}
            onClick={() => props.onOpenTasks?.()}
          />
          <QuickEntry
            Icon={NewspaperClipping}
            title={t("reader")}
            desc={t("overviewQuickReaderDesc")}
            onClick={() => props.onOpenReader?.()}
          />
        </div>
      </section>
      <div className="wb-ov-footnote">
        <Brain size={13} aria-hidden="true" />
        {t("overviewPrivacyNote")}
      </div>
    </div>
  )
}

function SectionLabel(props: { children: string }) {
  return (
    <div className="wb-ov-section-label">
      <span>{props.children}</span>
    </div>
  )
}

function MetricCard(props: {
  label: string
  value: number | string
  foot: string
  badge?: string
  tone?: "warn"
  Icon: typeof NotePencil
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className={`wb-ov-card wb-ov-metric ${props.tone ? "wb-ov-metric--warn" : ""}`}
      onClick={props.onClick}
    >
      <div className="wb-ov-metric-top">
        <span>{props.label}</span>
        {props.badge && <b className="wb-ov-badge">{props.badge}</b>}
      </div>
      <div className="wb-ov-metric-value">{props.value}</div>
      <div className="wb-ov-metric-foot">
        <span>{props.foot}</span>
        <span className="wb-ov-arrow">→</span>
      </div>
      <props.Icon className="wb-ov-metric-ghost" size={54} weight="thin" aria-hidden="true" />
    </button>
  )
}

function ChartHead(props: { title: string; sub: string; tag: string }) {
  return (
    <div className="wb-ov-chart-head">
      <div>
        <h2>{props.title}</h2>
        <p>{props.sub}</p>
      </div>
      <span>{props.tag}</span>
    </div>
  )
}

function Donut(props: {
  segments: Array<{ label: string; value: number; color: string }>
  center: number
  centerSub: string
}) {
  const total = props.segments.reduce((sum, segment) => sum + segment.value, 0)
  const radius = 46
  const circumference = 2 * Math.PI * radius
  const arcs = props.segments.map((segment, index) => {
    const before = props.segments
      .slice(0, index)
      .reduce((sum, previous) => sum + previous.value, 0)
    const ratio = total > 0 ? segment.value / total : 0
    return {
      segment,
      length: Math.max(ratio * circumference - 2, 0),
      dashOffset: -(before / Math.max(total, 1)) * circumference,
    }
  })
  return (
    <div className="wb-ov-donut">
      <svg viewBox="0 0 120 120" role="img" aria-label={props.centerSub}>
        <circle cx="60" cy="60" r={radius} fill="none" stroke="var(--border)" strokeWidth="14" />
        {arcs.map((arc) => (
          <circle
            key={arc.segment.label}
            cx="60"
            cy="60"
            r={radius}
            fill="none"
            stroke={arc.segment.color}
            strokeWidth="14"
            strokeDasharray={`${arc.length} ${circumference - arc.length}`}
            strokeDashoffset={arc.dashOffset}
            transform="rotate(-90 60 60)"
          />
        ))}
      </svg>
      <div className="wb-ov-donut-center">
        <b>{props.center}</b>
        <span>{props.centerSub}</span>
      </div>
    </div>
  )
}

function TrendChart(props: { values: number[] }) {
  const { t } = useTranslation()
  const width = 340
  const height = 168
  const left = 24
  const right = 316
  const bottom = 128
  const top = 20
  const max = Math.max(1, ...props.values)
  const points = props.values.map((value, index) => {
    const x = left + (index * (right - left)) / Math.max(props.values.length - 1, 1)
    const y = bottom - (value / max) * (bottom - top)
    return { x, y, value }
  })
  const line = points.map((point) => `${point.x.toFixed(1)} ${point.y.toFixed(1)}`).join(" L ")
  const area = `M ${left} ${bottom} L ${line} L ${right} ${bottom} Z`
  const last = points[points.length - 1]
  const currentWeek = startOfWeek(new Date())
  const labels = [0, Math.floor(props.values.length / 2), props.values.length - 1]
    .filter((value, index, array) => array.indexOf(value) === index)
    .map((index) => {
      const date = new Date(currentWeek)
      date.setDate(date.getDate() - (props.values.length - 1 - index) * 7)
      return { x: points[index]?.x ?? left, text: monthDay(date), index }
    })
  return (
    <div className="wb-ov-line">
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={t("overviewTrend")}>
        {[0, 1, 2, 3].map((line) => (
          <line
            key={line}
            x1={left}
            x2={right}
            y1={top + ((bottom - top) / 3) * line}
            y2={top + ((bottom - top) / 3) * line}
            stroke="var(--border)"
            strokeWidth="1"
          />
        ))}
        <path d={area} fill={CHART_COLORS.blue} opacity=".08" />
        <path
          d={`M ${line}`}
          fill="none"
          stroke={CHART_COLORS.blue}
          strokeWidth="1.8"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        {points.slice(0, -1).map((point, index) => (
          <circle
            key={index}
            cx={point.x}
            cy={point.y}
            r="2.6"
            fill="var(--surface-raised)"
            stroke={CHART_COLORS.blue}
            strokeWidth="1.6"
          >
            <title>{`${labels.find((label) => label.index === index)?.text ?? ""} · ${point.value}`}</title>
          </circle>
        ))}
        {last && (
          <>
            <circle cx={last.x} cy={last.y} r="4.2" fill={CHART_COLORS.blue} />
            <circle cx={last.x} cy={last.y} r="8" fill="none" stroke={CHART_COLORS.blue} opacity=".25" />
            <rect x={Math.min(last.x - 46, width - 48)} y={Math.max(last.y - 26, 2)} width="46" height="18" rx="6" fill="var(--accent-soft)" />
            <text
              x={Math.min(last.x - 23, width - 25)}
              y={Math.max(last.y - 14, 14)}
              textAnchor="middle"
              fontSize="10"
              fontWeight="600"
              fill="var(--accent-strong)"
            >
              {`${t("overviewThisWeek")} ${last.value}`}
            </text>
          </>
        )}
        {labels.map((label) => (
          <text key={label.index} x={label.x} y="148" textAnchor="middle" fontSize="9.5" fill="var(--text-tertiary)">
            {label.text}
          </text>
        ))}
      </svg>
    </div>
  )
}

function ProgressChart(props: {
  avg: number
  total: number
  segments: Array<{ label: string; value: number; color: string; note: string }>
}) {
  const { t } = useTranslation()
  const percentages = props.segments.map((segment) =>
    props.total > 0 ? (segment.value / props.total) * 100 : 0,
  )
  const arcs = props.segments.map((segment, index) => ({
    segment,
    length: Math.max(percentages[index] ?? 0, 0),
    offset: percentages.slice(0, index).reduce((sum, value) => sum + value, 0),
  }))
  return (
    <div className="wb-ov-progress">
      <div className="wb-ov-gauge">
        <svg viewBox="0 0 140 84" role="img" aria-label={`${props.avg}%`}>
          <path
            d="M 20 70 A 50 50 0 0 1 120 70"
            fill="none"
            stroke="var(--border)"
            strokeWidth="15"
            pathLength="100"
            strokeLinecap="round"
          />
          {arcs.map((arc) => (
            <path
              key={arc.segment.label}
              d="M 20 70 A 50 50 0 0 1 120 70"
              fill="none"
              stroke={arc.segment.color}
              strokeWidth="15"
              pathLength="100"
              strokeDasharray={`${Math.max(arc.length - 1.5, 0)} 100`}
              strokeDashoffset={-arc.offset}
              strokeLinecap="butt"
            />
          ))}
        </svg>
        <div className="wb-ov-gauge-center">
          <b>{props.avg}%</b>
          <span>{t("overviewAvgProgress")}</span>
        </div>
      </div>
      <div className="wb-ov-legend">
        {props.segments.map((segment) => (
          <div className="wb-ov-legend-row" key={segment.label}>
            <i style={{ background: segment.color }} />
            <span>{segment.label}</span>
            <b>{segment.value}</b>
            <em>{segment.note}</em>
          </div>
        ))}
      </div>
    </div>
  )
}

function QuickEntry(props: {
  Icon: typeof NotePencil
  title: string
  desc: string
  onClick: () => void
}) {
  return (
    <button type="button" className="wb-ov-card wb-ov-quick-card" onClick={props.onClick}>
      <span className="wb-ov-quick-icon">
        <props.Icon size={17} weight="regular" aria-hidden="true" />
      </span>
      <span className="wb-ov-quick-text">
        <b>{props.title}</b>
        <span>{props.desc}</span>
      </span>
      <span className="wb-ov-arrow">→</span>
    </button>
  )
}
