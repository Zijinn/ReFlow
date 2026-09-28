import { Fragment, useMemo, useState } from "react"

import type { ResearchPaper, ResearchPaperPatch } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { toast } from "../../store/toast"
import { countStageLeaves, computeProgress } from "../../lib/research"
import { ChipEditor, DragHandle, EmptyState, ExpandToggle, InlineText, MenuSelect, Row } from "./shared"
import { displayID, matchesPaperQuery, priorityBadgeClass, priorityDotClass, reorderList } from "./utils"
import { StageTree } from "./StageTree"

const PRIORITIES = [
  { value: "High", key: "priorityHigh" },
  { value: "Medium", key: "priorityMedium" },
  { value: "Average", key: "priorityAverage" },
]

export function ResearchPage(props: {
  papers: ResearchPaper[]
  offline?: boolean
  creating?: boolean
  onCreate: () => void
  onUpdate: (id: string, patch: ResearchPaperPatch) => void
  onDelete: (id: string) => void
  onReorder: (orderedIDs: string[]) => void
  onMove: (id: string) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState("")
  const [priority, setPriority] = useState("")
  const [expandedID, setExpandedID] = useState<string | null>(null)

  const priorityOptions = useMemo(
    () =>
      PRIORITIES.map((option) => ({
        value: option.value,
        label: t(option.key),
        dotClass: priorityDotClass(option.value),
      })),
    [t],
  )

  const filtered = useMemo(() => {
    const query = search.toLowerCase().trim()
    return props.papers.filter((paper) => {
      if (priority && paper.priority !== priority) return false
      if (!query) return true
      return matchesPaperQuery(paper, query)
    })
  }, [props.papers, search, priority])

  const reorder = (fromID: string, toID: string, before: boolean) => {
    props.onReorder(reorderList(props.papers, fromID, toID, before).map((p) => p.id))
  }

  const copyPath = (path: string) => {
    if (!path) return
    void navigator.clipboard?.writeText(path).then(() => toast(t("pathCopied")))
  }

  return (
    <div>
      <div className="wb-toolbar">
        <input
          className="wb-search"
          placeholder={t("searchPapers")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <MenuSelect
          value={priority}
          ariaLabel={t("colPriority")}
          className="wb-filter-menu"
          onChange={setPriority}
          options={[{ value: "", label: t("allPriorities") }, ...priorityOptions]}
        />
        {(search.trim() || priority) && (
          <span className="wb-result-count">
            {filtered.length}/{props.papers.length} {t("filterCountSuffix")}
          </span>
        )}
        <button
          type="button"
          className="wb-btn wb-btn--primary"
          disabled={props.offline || props.creating}
          title={props.offline ? t("workbenchOfflineHint") : undefined}
          onClick={props.onCreate}
        >
          {t("addPaper")}
        </button>
      </div>
      <div className="wb-table-wrap">
        <table className="wb-table wb-table--research">
          <thead>
            <tr>
              <th className="wb-col-grip" aria-label={t("colCode")} />
              <th className="wb-col-title">{t("colTitle")}</th>
              <th className="wb-col-stage">{t("colStage")}</th>
              <th className="wb-col-priority">{t("colPriority")}</th>
              <th className="wb-col-text">{t("targetJournal")}</th>
              <th className="wb-col-text wb-col-note">{t("nextAction")}</th>
              <th className="wb-col-date">{t("lastUpdatedLabel")}</th>
              <th className="wb-col-actions">{t("colActions")}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 ? (
              <tr>
                <td colSpan={8} className="wb-empty">
                  {search.trim() || priority ? (
                    <EmptyState title={t("noMatchingPapers")} hint={t("emptySearchHint")} />
                  ) : (
                    <EmptyState
                      title={t("emptyZeroResearch")}
                      hint={t("emptyZeroResearchHint")}
                      actionLabel={t("addPaper")}
                      onAction={props.onCreate}
                      actionDisabled={props.offline || props.creating}
                    />
                  )}
                </td>
              </tr>
            ) : (
              filtered.map((paper) => {
                const index = props.papers.findIndex((p) => p.id === paper.id)
                const expanded = expandedID === paper.id
                const progress = computeProgress(paper.stages)
                const leaves = countStageLeaves(paper.stages)
                return (
                  <Fragment key={paper.id}>
                    <Row id={paper.id} onReorder={reorder} dataPaperID={paper.id}>
                      <td className="wb-col-grip">
                        <DragHandle />
                        <span className="wb-code">{displayID("research", index)}</span>
                      </td>
                      <td className="wb-col-title">
                        <div className="wb-cell-title">
                          <InlineText
                            value={paper.title}
                            placeholder={t("fillPlaceholder")}
                            onCommit={(title) => props.onUpdate(paper.id, { title })}
                          />
                        </div>
                        <ChipEditor
                          label={t("authors")}
                          items={paper.authors}
                          addPrompt={t("addAuthorPrompt")}
                          onChange={(authors) => props.onUpdate(paper.id, { authors })}
                        />
                      </td>
                      <td className="wb-col-stage">
                        <div className="wb-stage-cell">
                          <span
                            className="wb-progress-track"
                            role="progressbar"
                            aria-valuenow={progress}
                            aria-valuemin={0}
                            aria-valuemax={100}
                          >
                            <span className="wb-progress-fill" style={{ width: `${progress}%` }} />
                          </span>
                          <span className="wb-progress-text">{progress}%</span>
                        </div>
                      </td>
                      <td className="wb-col-priority">
                        <MenuSelect
                          value={paper.priority || "Medium"}
                          ariaLabel={t("colPriority")}
                          className={`wb-priority-pill ${priorityBadgeClass(paper.priority || "Medium")}`}
                          onChange={(value) => props.onUpdate(paper.id, { priority: value })}
                          options={priorityOptions}
                        />
                      </td>
                      <td className="wb-col-text">
                        <InlineText
                          value={paper.target_journal}
                          placeholder={t("fillPlaceholder")}
                          onCommit={(value) => props.onUpdate(paper.id, { target_journal: value })}
                        />
                      </td>
                      <td className="wb-col-text wb-col-note">
                        <InlineText
                          value={paper.next_action}
                          placeholder={t("fillPlaceholder")}
                          onCommit={(value) => props.onUpdate(paper.id, { next_action: value })}
                        />
                      </td>
                      <td className="wb-col-date wb-muted">
                        {(paper.last_updated || "").slice(0, 10) || "—"}
                      </td>
                      <td className="wb-col-actions">
                        <ExpandToggle
                          expanded={expanded}
                          label={expanded ? t("collapseRow") : t("expandRow")}
                          onToggle={() => setExpandedID(expanded ? null : paper.id)}
                        />
                        <button
                          type="button"
                          className="wb-btn wb-flow-btn"
                          disabled={props.offline}
                          title={props.offline ? t("workbenchOfflineHint") : undefined}
                          onClick={() => props.onMove(paper.id)}
                        >
                          {t("flowToSubmitted")}
                        </button>
                        <button
                          type="button"
                          className="wb-icon-btn wb-icon-btn--danger"
                          title={t("delete")}
                          aria-label={`${t("delete")}: ${paper.title || displayID("research", index)}`}
                          disabled={props.offline}
                          onClick={() => props.onDelete(paper.id)}
                        >
                          ✕
                        </button>
                      </td>
                    </Row>
                    {expanded && (
                      <tr className="wb-row-detail">
                        <td colSpan={8}>
                          <div className="wb-detail-grid">
                            <StageTree
                              stages={paper.stages}
                              onChange={(stages) => props.onUpdate(paper.id, { stages })}
                            />
                            <div className="wb-detail-side">
                              <ChipEditor
                                label={t("keywords")}
                                items={paper.keywords}
                                addPrompt={t("addKeywordPrompt")}
                                onChange={(keywords) => props.onUpdate(paper.id, { keywords })}
                              />
                              <div className="wb-path-row">
                                <span className="wb-muted">{t("folderLabel")}</span>
                                <InlineText
                                  className="wb-path-text"
                                  value={paper.file_path}
                                  placeholder={t("fillPlaceholder")}
                                  onCommit={(value) =>
                                    props.onUpdate(paper.id, { file_path: value })
                                  }
                                />
                                <button
                                  type="button"
                                  className="wb-btn"
                                  onClick={() => copyPath(paper.file_path)}
                                >
                                  {t("copyPath")}
                                </button>
                              </div>
                              <div className="wb-muted wb-detail-meta">
                                {t("stageProgress")} · {leaves.done}/{leaves.total}
                              </div>
                            </div>
                          </div>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
