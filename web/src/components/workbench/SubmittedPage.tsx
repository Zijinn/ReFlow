import { Fragment, useEffect, useMemo, useState } from "react"

import type { ResearchPaper, ResearchPaperPatch, SubmissionRecord } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { SUBMISSION_STATUS_OPTIONS } from "../../lib/research"
import { toast } from "../../store/toast"
import { ChipEditor, DragHandle, EmptyState, ExpandToggle, InlineText, MenuSelect, Row } from "./shared"
import {
  daysUntil,
  displayID,
  matchesPaperQuery,
  normalizeDeadlineInput,
  parseDeadline,
  reorderList,
  statusBadgeClass,
  statusDotClass,
} from "./utils"

function todayISO(): string {
  return new Date().toISOString().slice(0, 10)
}

function deadlineUrgency(days: number): { className: string; key: string; count?: number } {
  if (days < 0) return { className: "wb-deadline--overdue", key: "overdueUnit", count: -days }
  if (days === 0) return { className: "wb-deadline--today", key: "dueToday" }
  if (days <= 7) return { className: "wb-deadline--soon", key: "daysLeftUnit", count: days }
  return { className: "", key: "" }
}

export function SubmittedPage(props: {
  papers: ResearchPaper[]
  offline?: boolean
  creating?: boolean
  focusPaperID?: string | null
  onFocusConsumed?: () => void
  onCreate: () => void
  onUpdate: (id: string, patch: ResearchPaperPatch) => void
  onDelete: (id: string) => void
  onReorder: (orderedIDs: string[]) => void
  onMove: (id: string) => void
}) {
  const { t } = useTranslation()
  const onFocusConsumed = props.onFocusConsumed
  const [search, setSearch] = useState("")
  const [status, setStatus] = useState("")
  // A calendar jump mounts this page with focusPaperID set. Snapshot it: the
  // parent clears the prop right away, but the open row and the one-shot
  // highlight have to survive that for this mount.
  const [mountedFocus] = useState(() => props.focusPaperID ?? null)
  const [expandedID, setExpandedID] = useState<string | null>(mountedFocus)
  // Inline "add record" form replaces window.prompt, which never works in the
  // desktop WKWebView shell. At most one row shows the form at a time.
  const [historyDraft, setHistoryDraft] = useState<{
    paperID: string
    journal: string
    date: string
  } | null>(null)

  useEffect(() => {
    if (!mountedFocus) return
    document
      .querySelector(`tr[data-paper-id="${mountedFocus}"]`)
      ?.scrollIntoView({ block: "center", behavior: "smooth" })
    onFocusConsumed?.()
  }, [mountedFocus, onFocusConsumed])

  const statusOptions = useMemo(
    () =>
      SUBMISSION_STATUS_OPTIONS.map((option) => ({
        value: option.value,
        label: t(option.key),
        dotClass: statusDotClass(option.value),
      })),
    [t],
  )

  const filtered = useMemo(() => {
    const query = search.toLowerCase().trim()
    return props.papers.filter((paper) => {
      if (status && paper.status !== status) return false
      if (!query) return true
      return matchesPaperQuery(paper, query)
    })
  }, [props.papers, search, status])

  const reorder = (fromID: string, toID: string, before: boolean) => {
    props.onReorder(reorderList(props.papers, fromID, toID, before).map((p) => p.id))
  }
  const copyPath = (path: string) => {
    if (!path) return
    void navigator.clipboard?.writeText(path).then(() => toast(t("pathCopied")))
  }

  const openHistoryAdd = (paper: ResearchPaper) =>
    setHistoryDraft({ paperID: paper.id, journal: "", date: todayISO() })
  const commitHistoryAdd = () => {
    const draft = historyDraft
    setHistoryDraft(null)
    if (!draft) return
    const journal = draft.journal.trim()
    if (!journal) return
    const paper = props.papers.find((item) => item.id === draft.paperID)
    if (!paper) return
    const record: SubmissionRecord = {
      journal,
      date: draft.date.trim() || todayISO(),
      status: "submitted",
    }
    props.onUpdate(paper.id, { history: [...paper.history, record] })
  }
  const updateHistory = (paper: ResearchPaper, index: number, patch: Partial<SubmissionRecord>) => {
    const history = paper.history.map((record, i) =>
      i === index ? { ...record, ...patch } : record,
    )
    props.onUpdate(paper.id, { history })
  }
  const deleteHistory = (paper: ResearchPaper, index: number) => {
    const history = paper.history.slice()
    history.splice(index, 1)
    props.onUpdate(paper.id, { history })
  }

  return (
    <div>
      <div className="wb-toolbar">
        <input
          className="wb-search"
          placeholder={t("searchSubmissions")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <MenuSelect
          value={status}
          ariaLabel={t("colStatus")}
          className="wb-filter-menu"
          onChange={setStatus}
          options={[{ value: "", label: t("allStatuses") }, ...statusOptions]}
        />
        {(search.trim() || status) && (
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
          {t("addSubmission")}
        </button>
      </div>
      <div className="wb-table-wrap">
        <table className="wb-table wb-table--submitted">
          <thead>
            <tr>
              <th className="wb-col-grip" aria-label={t("colCode")} />
              <th className="wb-col-title">{t("colTitle")}</th>
              <th className="wb-col-text">{t("currentJournal")}</th>
              <th className="wb-col-status">{t("colStatus")}</th>
              <th className="wb-col-text wb-col-note">{t("nextAction")}</th>
              <th className="wb-col-date">{t("deadlineLabel")}</th>
              <th className="wb-col-count">{t("submissionCountLabel")}</th>
              <th className="wb-col-actions">{t("colActions")}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 ? (
              <tr>
                <td colSpan={8} className="wb-empty">
                  {search.trim() || status ? (
                    <EmptyState title={t("noMatchingPapers")} hint={t("emptySearchHint")} />
                  ) : (
                    <EmptyState
                      title={t("emptyZeroSubmitted")}
                      hint={t("emptyZeroSubmittedHint")}
                      actionLabel={t("addSubmission")}
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
                const deadlineDate = parseDeadline(paper.deadline)
                const urgency = deadlineDate ? deadlineUrgency(daysUntil(deadlineDate)) : null
                return (
                  <Fragment key={paper.id}>
                    <Row
                      id={paper.id}
                      onReorder={reorder}
                      dataPaperID={paper.id}
                      className={mountedFocus === paper.id ? "wb-row--flash" : ""}
                    >
                      <td className="wb-col-grip">
                        <DragHandle />
                        <span className="wb-code">{displayID("submitted", index)}</span>
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
                      <td className="wb-col-text">
                        <InlineText
                          value={paper.current_journal}
                          placeholder={t("fillPlaceholder")}
                          onCommit={(value) => props.onUpdate(paper.id, { current_journal: value })}
                        />
                      </td>
                      <td className="wb-col-status">
                        <MenuSelect
                          value={paper.status || "submitted"}
                          ariaLabel={t("colStatus")}
                          className={`wb-status-pill ${statusBadgeClass(paper.status || "submitted")}`}
                          onChange={(value) => props.onUpdate(paper.id, { status: value })}
                          options={statusOptions}
                        />
                      </td>
                      <td className="wb-col-text wb-col-note">
                        <InlineText
                          value={paper.next_action}
                          placeholder={t("fillPlaceholder")}
                          onCommit={(value) => props.onUpdate(paper.id, { next_action: value })}
                        />
                      </td>
                      <td className="wb-col-date">
                        <InlineText
                          value={paper.deadline}
                          placeholder={t("fillPlaceholder")}
                          onCommit={(value) =>
                            props.onUpdate(paper.id, { deadline: normalizeDeadlineInput(value) })
                          }
                        />
                        {urgency && urgency.key && (
                          <span className={`wb-deadline-hint ${urgency.className}`}>
                            {urgency.count === undefined
                              ? t(urgency.key)
                              : `${urgency.count} ${t(urgency.key)}`}
                          </span>
                        )}
                      </td>
                      <td className="wb-col-count">
                        <InlineText
                          value={String(paper.submission_count || "")}
                          placeholder={t("fillPlaceholder")}
                          onCommit={(value) =>
                            props.onUpdate(paper.id, { submission_count: Number(value) || 0 })
                          }
                        />
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
                          {t("flowToPublished")}
                        </button>
                        <button
                          type="button"
                          className="wb-icon-btn wb-icon-btn--danger"
                          title={t("delete")}
                          aria-label={`${t("delete")}: ${paper.title || displayID("submitted", index)}`}
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
                            <div className="wb-history">
                              <div className="wb-history-head">
                                <span className="wb-muted">{t("submissionHistory")}</span>
                                <button
                                  type="button"
                                  className="wb-icon-btn"
                                  title={t("addRecord")}
                                  aria-label={`${t("addRecord")}: ${paper.title}`}
                                  disabled={props.offline}
                                  onClick={() => openHistoryAdd(paper)}
                                >
                                  ＋
                                </button>
                              </div>
                              {historyDraft?.paperID === paper.id && (
                                <div className="wb-history-add">
                                  <input
                                    className="wb-inline-input"
                                    autoFocus
                                    type="text"
                                    value={historyDraft.journal}
                                    placeholder={t("addHistoryPrompt")}
                                    aria-label={t("addHistoryPrompt")}
                                    onChange={(e) =>
                                      setHistoryDraft({ ...historyDraft, journal: e.target.value })
                                    }
                                    onKeyDown={(e) => {
                                      if (e.key === "Enter") {
                                        e.preventDefault()
                                        commitHistoryAdd()
                                      }
                                      if (e.key === "Escape") setHistoryDraft(null)
                                    }}
                                  />
                                  <input
                                    className="wb-inline-input"
                                    type="text"
                                    value={historyDraft.date}
                                    placeholder={t("addHistoryDatePrompt")}
                                    aria-label={t("addHistoryDatePrompt")}
                                    onChange={(e) =>
                                      setHistoryDraft({ ...historyDraft, date: e.target.value })
                                    }
                                    onKeyDown={(e) => {
                                      if (e.key === "Enter") {
                                        e.preventDefault()
                                        commitHistoryAdd()
                                      }
                                      if (e.key === "Escape") setHistoryDraft(null)
                                    }}
                                  />
                                  <button
                                    type="button"
                                    className="wb-btn"
                                    onClick={commitHistoryAdd}
                                  >
                                    {t("add")}
                                  </button>
                                  <button
                                    type="button"
                                    className="wb-btn"
                                    onClick={() => setHistoryDraft(null)}
                                  >
                                    {t("cancel")}
                                  </button>
                                </div>
                              )}
                              {paper.history.length === 0 ? (
                                <div className="wb-muted">{t("noSubmissionHistory")}</div>
                              ) : (
                                <div className="wb-timeline">
                                  {paper.history.map((record, historyIndex) => (
                                    <div
                                      className="wb-timeline-item"
                                      key={`${record.date}|${record.journal}|${historyIndex}`}
                                    >
                                      <div className="wb-timeline-date">
                                        <InlineText
                                          value={record.date}
                                          placeholder={t("fillPlaceholder")}
                                          onCommit={(value) =>
                                            updateHistory(paper, historyIndex, { date: value })
                                          }
                                        />
                                      </div>
                                      <div className="wb-timeline-content">
                                        <strong>
                                          <InlineText
                                            value={record.journal}
                                            placeholder={t("fillPlaceholder")}
                                            onCommit={(value) =>
                                              updateHistory(paper, historyIndex, { journal: value })
                                            }
                                          />
                                        </strong>
                                        <MenuSelect
                                          value={record.status}
                                          ariaLabel={t("colStatus")}
                                          className={`wb-status-pill wb-status-pill--sm ${statusBadgeClass(record.status)}`}
                                          onChange={(value) =>
                                            updateHistory(paper, historyIndex, { status: value })
                                          }
                                          options={statusOptions}
                                        />
                                        <button
                                          type="button"
                                          className="wb-icon-btn wb-icon-btn--danger"
                                          title={t("deleteRecord")}
                                          aria-label={`${t("deleteRecord")}: ${record.journal || record.date}`}
                                          disabled={props.offline}
                                          onClick={() => deleteHistory(paper, historyIndex)}
                                        >
                                          ✕
                                        </button>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              )}
                            </div>
                            <div className="wb-detail-side">
                              <div className="wb-card-line">
                                <span className="wb-muted">{t("manuscriptIdLabel")}</span>
                                <strong>
                                  <InlineText
                                    value={paper.manuscript_id}
                                    placeholder={t("fillPlaceholder")}
                                    onCommit={(value) =>
                                      props.onUpdate(paper.id, { manuscript_id: value })
                                    }
                                  />
                                </strong>
                              </div>
                              <div className="wb-card-line">
                                <span className="wb-muted">{t("submissionDateLabel")}</span>
                                <strong>
                                  <InlineText
                                    value={paper.submission_date}
                                    placeholder={t("fillPlaceholder")}
                                    onCommit={(value) =>
                                      props.onUpdate(paper.id, { submission_date: value })
                                    }
                                  />
                                </strong>
                              </div>
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
