import { useMemo } from "react"

import { Books, ChartPieSlice, CaretRight, NotePencil, PaperPlaneTilt } from "@phosphor-icons/react"

import type { AIProfile, ResearchPaper } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { computeProgress } from "../../lib/research"
import { DailyDigestCard } from "./DailyDigest"
import { EmptyState } from "./shared"
import { relativeTime } from "./utils"

export type DashboardTab = "research" | "submitted" | "published"

export function Dashboard(props: {
  research: ResearchPaper[]
  submitted: ResearchPaper[]
  published: ResearchPaper[]
  aiProfiles?: AIProfile[]
  onConfigureAI?: () => void
  onAskAI?: () => void
  onOpenPaper?: (paperID: string) => void
  onNavigate: (tab: DashboardTab) => void
}) {
  const { t, locale } = useTranslation()
  const { research, submitted, published } = props

  const avg = useMemo(() => {
    if (research.length === 0) return 0
    const total = research.reduce((sum, paper) => sum + computeProgress(paper.stages), 0)
    return Math.round(total / research.length)
  }, [research])

  // Keep the memo free of `t`: store the raw kind discriminator here and
  // translate at render time, otherwise switching locales busts the cache.
  const recent = useMemo(() => {
    const items: Array<{ paper: ResearchPaper; kind: "research" | "submitted" }> = [
      ...research.map((paper) => ({ paper, kind: "research" as const })),
      ...submitted.map((paper) => ({ paper, kind: "submitted" as const })),
    ]
    return items
      .sort((a, b) => (b.paper.last_updated || "").localeCompare(a.paper.last_updated || ""))
      .slice(0, 5)
  }, [research, submitted])

  const priorityProjects = research.filter((paper) => paper.priority === "High")

  // The digest covers the papers that actually have a next step or a deadline.
  const digestPapers = useMemo(() => [...research, ...submitted], [research, submitted])

  return (
    <div className="wb-dashboard">
      <DailyDigestCard
        papers={digestPapers}
        profiles={props.aiProfiles ?? []}
        onConfigure={props.onConfigureAI}
        onAskAI={props.onAskAI}
        onOpenPaper={props.onOpenPaper}
      />
      <div className="wb-stats-grid">
        <StatCard
          label={t("workingPapers")}
          value={research.length}
          foot={t("researchCount")}
          Icon={NotePencil}
          tint="wb-tint--blue"
          onSelect={() => props.onNavigate("research")}
        />
        <StatCard
          label={t("submissions")}
          value={submitted.length}
          foot={t("submittedCount")}
          Icon={PaperPlaneTilt}
          tint="wb-tint--orange"
          onSelect={() => props.onNavigate("submitted")}
        />
        <StatCard
          label={t("publications")}
          value={published.length}
          foot={t("publishedCount")}
          Icon={Books}
          tint="wb-tint--green"
          onSelect={() => props.onNavigate("published")}
        />
        <StatCard
          label={t("averageProgress")}
          value={`${avg}%`}
          foot={t("basedOnWorkingPapers")}
          Icon={ChartPieSlice}
          tint="wb-tint--violet"
          onSelect={() => props.onNavigate("research")}
        />
      </div>
      <div className="wb-dashboard-grid">
        <div className="wb-panel">
          <div className="wb-section-head">
            <div>
              <h2>{t("overallProgress")}</h2>
              <p>{t("overallProgressDesc")}</p>
            </div>
            <b>{avg}%</b>
          </div>
          <div className="wb-progress-track">
            <div className="wb-progress-fill" style={{ width: `${avg}%` }} />
          </div>
          <div className="wb-dashboard-progress-list">
            {research.map((paper) => {
              const progress = computeProgress(paper.stages)
              return (
                <div key={paper.id} className="wb-dashboard-progress-item">
                  <div className="wb-progress-row">
                    <span>{paper.title || "—"}</span>
                    <b>{progress}%</b>
                  </div>
                  <div className="wb-progress-track">
                    <div className="wb-progress-fill" style={{ width: `${progress}%` }} />
                  </div>
                </div>
              )
            })}
          </div>
        </div>
        <div className="wb-panel">
          <div className="wb-section-head">
            <div>
              <h2>{t("recentUpdates")}</h2>
              <p>{t("recentUpdatesDesc")}</p>
            </div>
          </div>
          <div className="wb-timeline">
            {recent.map(({ paper, kind }) => (
              <div key={paper.id} className="wb-timeline-item wb-timeline-item--dot">
                <div className="wb-timeline-date" title={paper.last_updated || undefined}>
                  {relativeTime(paper.last_updated, locale)}
                </div>
                <div className="wb-timeline-content">
                  <strong>{paper.title || "—"}</strong>
                  <span className="wb-muted">
                    {kind === "research" ? t("researchCount") : t("submittedCount")} ·{" "}
                    {paper.next_action || t("noNextAction")}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
      <div className="wb-section-head">
        <div>
          <h2>{t("priorityProjects")}</h2>
          <p>{t("priorityProjectsDesc")}</p>
        </div>
      </div>
      {priorityProjects.length === 0 ? (
        <div className="wb-empty">
          <EmptyState title={t("noPriorityProjects")} />
        </div>
      ) : (
        <div className="wb-cards-grid">
          {priorityProjects.map((paper) => (
            <article key={paper.id} className="wb-card wb-card--compact">
              <div className="wb-card-toprow">
                <span className="wb-card-eyebrow">{t("eyebrowResearchProject")}</span>
                <b className="wb-card-pct">{computeProgress(paper.stages)}%</b>
              </div>
              <div className="wb-card-title">{paper.title || "—"}</div>
              <div className="wb-card-line">
                <span className="wb-muted">{t("targetJournal")}</span>
                <strong>{paper.target_journal || "—"}</strong>
              </div>
              <div className="wb-progress-track">
                <div
                  className="wb-progress-fill"
                  style={{ width: `${computeProgress(paper.stages)}%` }}
                />
              </div>
            </article>
          ))}
        </div>
      )}
    </div>
  )
}

function StatCard(props: {
  label: string
  value: number | string
  foot: string
  Icon: typeof Books
  tint: string
  onSelect: () => void
}) {
  return (
    <button type="button" className="wb-stat-card wb-stat-card--link" onClick={props.onSelect}>
      <div className="wb-stat-top">
        <span className={`wb-stat-icon ${props.tint}`} aria-hidden="true">
          <props.Icon size={16} weight="fill" />
        </span>
        <div className="wb-stat-label">{props.label}</div>
        <CaretRight className="wb-stat-goto" size={14} weight="bold" aria-hidden="true" />
      </div>
      <div className="wb-stat-number">{props.value}</div>
      <div className="wb-stat-foot">{props.foot}</div>
    </button>
  )
}
