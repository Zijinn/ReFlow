import * as Dialog from "@radix-ui/react-dialog"
import { Fragment, useEffect, useMemo, useRef, useState } from "react"

import { fillMetadataWithAI, type AIMetadataFill } from "../../api/client"
import type { AIProfile, ResearchPaper, ResearchPaperPatch } from "../../api/types"
import { parseGb7714, type ParsedReference } from "../../lib/gb7714"
import { useTranslation } from "../../lib/i18n"
import {
  buildGb7714Reference,
  citationSourceKey,
  isEnglishPaper,
  MANUAL_CITATION_SOURCE,
  normalizeDoi,
  sortReferences,
  type ReferenceSort,
} from "../../lib/research"
import { downloadPublicationsExport } from "../../lib/researchExport"
import { toast } from "../../store/toast"
import {
  ChipEditor,
  ColumnHead,
  DragHandle,
  EmptyState,
  ExpandToggle,
  InlineText,
  MenuSelect,
  Row,
} from "./shared"
import { displayID, matchesPaperQuery, reorderList } from "./utils"

const SORT_OPTIONS: Array<{ value: ReferenceSort; key: string }> = [
  { value: "year-desc", key: "sortYearDesc" },
  { value: "year-asc", key: "sortYearAsc" },
  { value: "solo-first", key: "sortSoloFirst" },
  { value: "coauthor-first", key: "sortCoauthorFirst" },
  { value: "author", key: "sortByAuthor" },
]

// Only the bibliographic fields travel in the patch; empties are omitted so a
// partially parsed reference never overwrites anything with blank strings.
function patchFromParsed(parsed: ParsedReference): ResearchPaperPatch {
  const patch: ResearchPaperPatch = {}
  if (parsed.title) patch.title = parsed.title
  if (parsed.authors.length) patch.authors = parsed.authors
  if (parsed.journal) patch.journal = parsed.journal
  if (parsed.year) patch.year = parsed.year
  if (parsed.volume) patch.volume = parsed.volume
  if (parsed.issue) patch.issue = parsed.issue
  if (parsed.pages) patch.pages = parsed.pages
  if (parsed.doi) patch.doi = parsed.doi
  return patch
}

// The AI answer only fills gaps: whatever the local GB/T 7714 parse already
// read wins, so deterministic parsing is never overwritten by a model guess.
function mergeAIFill(base: ResearchPaperPatch, filled: AIMetadataFill): ResearchPaperPatch {
  const patch: ResearchPaperPatch = { ...base }
  const assign = (
    key: "title" | "journal" | "year" | "volume" | "issue" | "pages" | "doi",
    value: string | undefined,
  ) => {
    const next = (value ?? "").trim()
    if (next && !(patch[key] ?? "").trim()) patch[key] = next
  }
  assign("title", filled.title)
  assign("journal", filled.journal)
  assign("year", filled.year)
  assign("volume", filled.volume)
  assign("issue", filled.issue)
  assign("pages", filled.pages)
  assign("doi", filled.doi)
  if (!patch.authors?.length && filled.authors?.length) {
    const authors = filled.authors.map((name) => name.trim()).filter(Boolean)
    if (authors.length) patch.authors = authors
  }
  return patch
}

export function PublishedPage(props: {
  papers: ResearchPaper[]
  crossrefEmail: string
  citationPendingID: string | null
  offline?: boolean
  creating?: boolean
  batchCitationPending?: boolean
  aiProfiles?: AIProfile[]
  onCreate: () => void
  onUpdate: (id: string, patch: ResearchPaperPatch) => void
  onDelete: (id: string) => void
  onReorder: (orderedIDs: string[]) => void
  onFetchCitation: (id: string) => void
  onFetchAllCitations: () => void
  batchCitationProgress?: { done: number; total: number } | null
  onCrossrefEmailChange: (email: string) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState("")
  const [view, setView] = useState<"table" | "references">("table")
  const [expandedID, setExpandedID] = useState<string | null>(null)
  const [sortZh, setSortZh] = useState<ReferenceSort>("year-desc")
  const [sortEn, setSortEn] = useState<ReferenceSort>("year-desc")
  // null means "no local edits yet": the input mirrors the async prop until
  // the user types, so a late-arriving crossrefEmail backfills correctly.
  const [emailDraft, setEmailDraft] = useState<string | null>(null)
  // GB/T 7714 paste dialog state.
  const [pasteOpen, setPasteOpen] = useState(false)
  const [pasteText, setPasteText] = useState("")
  const [aiFillRunning, setAiFillRunning] = useState(false)
  // Creating an entry from a pasted reference is two mutations: onCreate only
  // accepts kind/title/authors (and the Workbench handler sends just kind), so
  // the remaining fields ride along here and are PATCHed through onUpdate once
  // the created paper shows up in the refreshed list. Both steps stay on the
  // existing mutation props — the page never fetches on its own.
  const pendingFillRef = useRef<{ knownIDs: Set<string>; patch: ResearchPaperPatch } | null>(null)

  const { papers, onUpdate } = props
  useEffect(() => {
    const pending = pendingFillRef.current
    if (!pending) return
    const created = papers.find((paper) => !pending.knownIDs.has(paper.id))
    if (!created) return
    pendingFillRef.current = null
    onUpdate(created.id, pending.patch)
  }, [papers, onUpdate])

  const aiProfile = useMemo(() => {
    const enabled = (props.aiProfiles ?? []).filter((profile) => profile.enabled)
    return enabled.find((profile) => profile.is_default) ?? enabled[0] ?? null
  }, [props.aiProfiles])

  const commitEmail = (raw: string) => {
    const next = raw.trim()
    setEmailDraft(null)
    // Never PUT an empty string: that would silently wipe the stored email.
    if (!next) return
    if (next === props.crossrefEmail.trim()) return
    props.onCrossrefEmailChange(next)
  }

  const filtered = useMemo(() => {
    const query = search.toLowerCase().trim()
    if (!query) return props.papers
    return props.papers.filter((paper) => matchesPaperQuery(paper, query))
  }, [props.papers, search])

  const reorder = (fromID: string, toID: string, before: boolean) => {
    props.onReorder(reorderList(props.papers, fromID, toID, before).map((p) => p.id))
  }

  const copyAllReferences = () => {
    const zh = sortReferences(
      props.papers.filter((p) => !isEnglishPaper(p)),
      sortZh,
    )
    const en = sortReferences(
      props.papers.filter((p) => isEnglishPaper(p)),
      sortEn,
    )
    const format = (list: ResearchPaper[]) =>
      list.map((p, i) => `[${i + 1}] ${buildGb7714Reference(p)}`).join("\n")
    const parts: string[] = []
    if (zh.length) parts.push(`${t("chinesePublications")}\n${format(zh)}`)
    if (en.length) parts.push(`${t("englishPublications")}\n${format(en)}`)
    const text = parts.join("\n\n")
    if (!text) return
    void navigator.clipboard?.writeText(text).then(() => toast(t("referencesCopied")))
  }

  const exportPublications = () => {
    if (props.papers.length === 0) {
      toast(t("exportEmpty"))
      return
    }
    downloadPublicationsExport(props.papers)
    toast(t("exportDone"))
  }

  const createFromPatch = (patch: ResearchPaperPatch) => {
    pendingFillRef.current = {
      knownIDs: new Set(props.papers.map((paper) => paper.id)),
      patch,
    }
    props.onCreate()
  }

  const applyPaste = () => {
    const parsed = parseGb7714(pasteText)
    if (!parsed) {
      toast(t("pasteGbInvalid"))
      return
    }
    createFromPatch(patchFromParsed(parsed))
    setPasteOpen(false)
    setPasteText("")
    toast(t("pasteGbApplied"))
  }

  const runAIFill = async () => {
    if (!aiProfile) return
    const raw = pasteText.trim()
    if (!raw) return
    setAiFillRunning(true)
    try {
      const filled = await fillMetadataWithAI(aiProfile.id, raw)
      const local = parseGb7714(raw)
      const patch = mergeAIFill(local ? patchFromParsed(local) : {}, filled)
      if (!patch.title && !patch.authors?.length) {
        toast(t("aiFillFailed"))
        return
      }
      createFromPatch(patch)
      setPasteOpen(false)
      setPasteText("")
      toast(t("aiFillApplied"))
    } catch {
      toast(t("aiFillFailed"))
    } finally {
      setAiFillRunning(false)
    }
  }

  const renderRow = (paper: ResearchPaper) => {
    const index = props.papers.findIndex((p) => p.id === paper.id)
    const english = isEnglishPaper(paper)
    const doi = normalizeDoi(paper.doi)
    const combinedVolumeIssue = paper.issue ? `${paper.volume}(${paper.issue})` : paper.volume
    const expanded = expandedID === paper.id
    return (
      <Fragment key={paper.id}>
        <Row id={paper.id} onReorder={reorder} dataPaperID={paper.id}>
          <td className="wb-col-grip">
            <DragHandle />
            <span className="wb-code">{displayID("published", index)}</span>
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
          <td className="wb-col-year">
            {/* 语义绿只给"已发表"这个事实：年份还没填时套绿药丸，等于用一枚
                成功徽章包着一个空占位。空值走中性正文，填上才成药丸。 */}
            {paper.year ? (
              <span className="wb-badge wb-badge--green">
                <InlineText
                  value={paper.year}
                  placeholder={t("yearLabel")}
                  onCommit={(value) => props.onUpdate(paper.id, { year: value })}
                />
              </span>
            ) : (
              <InlineText
                value={paper.year}
                placeholder={t("yearLabel")}
                onCommit={(value) => props.onUpdate(paper.id, { year: value })}
              />
            )}
          </td>
          <td className="wb-col-text">
            <InlineText
              value={paper.journal}
              placeholder={t("fillPlaceholder")}
              onCommit={(value) => props.onUpdate(paper.id, { journal: value })}
            />
          </td>
          <td className="wb-col-vol">
            <InlineText
              value={combinedVolumeIssue}
              placeholder={t("fillPlaceholder")}
              onCommit={(value) => {
                const match = value.match(/^\s*([^(]*?)\s*(?:\(([^()]*)\)\s*)?$/)
                props.onUpdate(paper.id, {
                  volume: (match?.[1] ?? "").trim(),
                  issue: (match?.[2] ?? "").trim(),
                })
              }}
            />
            {paper.pages ? <span className="wb-muted">: {paper.pages}</span> : null}
          </td>
          <td className="wb-col-doi">
            <InlineText
              value={paper.doi}
              placeholder={t("fillPlaceholder")}
              onCommit={(value) => props.onUpdate(paper.id, { doi: value })}
            />
          </td>
          <td className="wb-col-citations">
            <div
              className={`wb-citation-box ${english ? "wb-citation--live" : "wb-citation--static"}`}
            >
              <div className="wb-citation-main">
                <span className="wb-citation-number">
                  <InlineText
                    value={paper.citations == null ? "" : String(paper.citations)}
                    placeholder={t("fillPlaceholder")}
                    onCommit={(value) =>
                      props.onUpdate(paper.id, {
                        citations: value === "" ? null : Number(value) || 0,
                        // Stable enum key; the UI maps it via i18n (legacy rows may
                        // still hold the literal Chinese label).
                        citation_source: MANUAL_CITATION_SOURCE,
                      })
                    }
                  />
                </span>
                {english && (
                  <button
                    type="button"
                    className="wb-btn wb-citation-fetch"
                    disabled={props.citationPendingID === paper.id || props.offline === true}
                    title={props.offline ? t("workbenchOfflineHint") : undefined}
                    onClick={() => {
                      // The button only renders for English papers; non-English
                      // guidance is the inline citationManualHint on that row.
                      if (!doi) {
                        toast(t("citationNoDoi"))
                        return
                      }
                      if (!props.crossrefEmail) {
                        toast(t("citationNoEmail"))
                        return
                      }
                      props.onFetchCitation(paper.id)
                    }}
                  >
                    {props.citationPendingID === paper.id
                      ? t("updatingCrossref")
                      : t("fetchFromCrossref")}
                  </button>
                )}
              </div>
              <span className="wb-citation-label">
                {english
                  ? citationSourceKey(paper.citation_source) === "crossref" &&
                    paper.citation_updated_at
                    ? `Crossref (${paper.citation_updated_at.slice(0, 10)})`
                    : t("citationLiveHint")
                  : t("citationManualHint")}
              </span>
            </div>
          </td>
          <td className="wb-col-actions">
            <ExpandToggle
              expanded={expanded}
              label={expanded ? t("collapseRow") : t("expandRow")}
              onToggle={() => setExpandedID(expanded ? null : paper.id)}
            />
            <button
              type="button"
              className="wb-icon-btn wb-icon-btn--danger"
              title={t("delete")}
              aria-label={`${t("delete")}: ${paper.title || displayID("published", index)}`}
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
                <div>
                  <div className="wb-muted wb-abstract-label">{t("abstractLabel")}</div>
                  <div className="wb-abstract">
                    <InlineText
                      value={paper.abstract}
                      placeholder={t("fillPlaceholder")}
                      multiline
                      onCommit={(value) => props.onUpdate(paper.id, { abstract: value })}
                    />
                  </div>
                </div>
                <div className="wb-detail-side">
                  <ChipEditor
                    label={t("keywords")}
                    items={paper.keywords}
                    addPrompt={t("addKeywordPrompt")}
                    onChange={(keywords) => props.onUpdate(paper.id, { keywords })}
                  />
                  <div className="wb-muted wb-detail-meta">
                    {t("lastUpdatedLabel")}: {paper.last_updated || "—"}
                  </div>
                </div>
              </div>
            </td>
          </tr>
        )}
      </Fragment>
    )
  }

  const sortOptions = SORT_OPTIONS.map((option) => ({
    value: option.value,
    label: t(option.key),
  }))

  const renderReferenceColumn = (
    title: string,
    language: "zh" | "en",
    sortMode: ReferenceSort,
    setSort: (mode: ReferenceSort) => void,
  ) => {
    const source = props.papers.filter((p) =>
      language === "zh" ? !isEnglishPaper(p) : isEnglishPaper(p),
    )
    const list = sortReferences(source, sortMode)
    return (
      <section className="wb-reference-column">
        <div className="wb-reference-column-head">
          <div>
            <h2>{title}</h2>
            <p>
              {list.length} · {t("sequentialCoding")}
            </p>
          </div>
          <MenuSelect
            value={sortMode}
            ariaLabel={title}
            className="wb-filter-menu wb-menu--sm"
            onChange={(value) => setSort(value as ReferenceSort)}
            options={sortOptions}
          />
        </div>
        <ol className="wb-reference-list">
          {list.length ? (
            list.map((paper) => (
              <li key={paper.id}>
                <span className="wb-reference-text">{buildGb7714Reference(paper)}</span>
                <span className="wb-reference-meta">
                  {paper.authors.length === 1 ? t("solo") : t("coauthor")}
                </span>
              </li>
            ))
          ) : (
            <li className="wb-reference-empty">
              {language === "zh" ? t("noChinesePublications") : t("noEnglishPublications")}
            </li>
          )}
        </ol>
      </section>
    )
  }

  return (
    <div>
      <div className="wb-toolbar">
        <input
          className="wb-search"
          placeholder={t("searchPublications")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        {search.trim() && (
          <span className="wb-result-count">
            {filtered.length}/{props.papers.length} {t("filterCountSuffix")}
          </span>
        )}
        <div className="wb-view-switch">
          <button
            type="button"
            className={`wb-view-btn ${view === "table" ? "wb-view-btn--active" : ""}`}
            onClick={() => setView("table")}
          >
            {t("tableView")}
          </button>
          <button
            type="button"
            className={`wb-view-btn ${view === "references" ? "wb-view-btn--active" : ""}`}
            onClick={() => setView("references")}
          >
            {t("referenceView")}
          </button>
        </div>
        <button
          type="button"
          className="wb-btn"
          disabled={props.offline}
          title={props.offline ? t("workbenchOfflineHint") : undefined}
          onClick={exportPublications}
        >
          {t("exportPublications")}
        </button>
        <button
          type="button"
          className="wb-btn"
          disabled={props.offline || props.batchCitationPending}
          title={props.offline ? t("workbenchOfflineHint") : undefined}
          onClick={props.onFetchAllCitations}
        >
          {props.batchCitationPending
            ? props.batchCitationProgress
              ? `${t("fetchingAllCitations")} ${props.batchCitationProgress.done}/${props.batchCitationProgress.total}`
              : t("fetchingAllCitations")
            : t("fetchAllCitations")}
        </button>
        <button
          type="button"
          className="wb-btn"
          disabled={props.offline}
          title={props.offline ? t("workbenchOfflineHint") : undefined}
          onClick={() => setPasteOpen(true)}
        >
          {t("pasteGbTitle")}
        </button>
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
      <div className="wb-helper-note">{t("referenceNote")}</div>
      <div className="wb-email-row">
        <span className="wb-muted">{t("crossrefEmail")}</span>
        <input
          className="wb-email-input"
          placeholder={t("crossrefEmailPlaceholder")}
          aria-label={t("crossrefEmail")}
          value={emailDraft ?? props.crossrefEmail}
          onChange={(e) => setEmailDraft(e.target.value)}
          onBlur={(e) => commitEmail(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") commitEmail(e.currentTarget.value)
          }}
        />
        <span className="wb-muted">
          {props.crossrefEmail ? t("crossrefEmailSaved") : t("crossrefEmailUnset")}
        </span>
      </div>
      <div className="wb-reference-copy-row">
        <button type="button" className="wb-btn wb-btn--primary" onClick={copyAllReferences}>
          {t("copyReferences")}
        </button>
      </div>
      {view === "table" ? (
        <div className="wb-table-wrap">
          <table className="wb-table wb-table--published">
            <thead>
              <tr>
                <th className="wb-col-grip" aria-label={t("colCode")} />
                <ColumnHead table="published" column="title" className="wb-col-title">
                  {t("colTitle")}
                </ColumnHead>
                <ColumnHead table="published" column="year" className="wb-col-year">
                  {t("yearLabel")}
                </ColumnHead>
                <ColumnHead table="published" column="journal" className="wb-col-text">
                  {t("journalLabel")}
                </ColumnHead>
                <ColumnHead table="published" column="vol" className="wb-col-vol">
                  {`${t("volumeIssueLabel")} / ${t("pagesLabel")}`}
                </ColumnHead>
                <ColumnHead table="published" column="doi" className="wb-col-doi">
                  {t("doiLabel")}
                </ColumnHead>
                <ColumnHead table="published" column="citations" className="wb-col-citations">
                  {t("citationsLabel")}
                </ColumnHead>
                <ColumnHead table="published" column="actions" className="wb-col-actions">
                  {t("colActions")}
                </ColumnHead>
              </tr>
            </thead>
            <tbody>
              {filtered.length === 0 ? (
                <tr>
                  <td colSpan={8} className="wb-empty">
                    {search.trim() ? (
                      <EmptyState title={t("noMatchingPapers")} hint={t("emptySearchHint")} />
                    ) : (
                      <EmptyState
                        title={t("emptyZeroPublished")}
                        hint={t("emptyZeroPublishedHint")}
                        actionLabel={t("addPaper")}
                        onAction={props.onCreate}
                        actionDisabled={props.offline || props.creating}
                      />
                    )}
                  </td>
                </tr>
              ) : (
                filtered.map(renderRow)
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="wb-reference-layout">
          {renderReferenceColumn(t("chinesePublications"), "zh", sortZh, setSortZh)}
          {renderReferenceColumn(t("englishPublications"), "en", sortEn, setSortEn)}
        </div>
      )}
      <Dialog.Root open={pasteOpen} onOpenChange={setPasteOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content className="dialog-content wb-paste-dialog" aria-describedby={undefined}>
            <div className="dialog-header">
              <Dialog.Title>{t("pasteGbTitle")}</Dialog.Title>
            </div>
            <div className="wb-paste-body">
              <textarea
                className="wb-paste-input"
                rows={4}
                placeholder={t("pasteGbPlaceholder")}
                aria-label={t("pasteGbTitle")}
                value={pasteText}
                onChange={(e) => setPasteText(e.target.value)}
              />
              <p className="wb-paste-hint">{t("pasteGbHint")}</p>
              <div className="dialog-actions wb-paste-actions">
                <Dialog.Close asChild>
                  <button type="button" className="wb-btn">
                    {t("cancel")}
                  </button>
                </Dialog.Close>
                <div className="wb-paste-submit">
                  <button
                    type="button"
                    className="wb-btn"
                    disabled={
                      !aiProfile || aiFillRunning || props.offline === true || !pasteText.trim()
                    }
                    title={!aiProfile ? t("aiFillNoProfile") : undefined}
                    onClick={() => void runAIFill()}
                  >
                    {aiFillRunning ? t("aiFillRunning") : t("aiFillMetadata")}
                  </button>
                  <button
                    type="button"
                    className="wb-btn wb-btn--primary"
                    disabled={props.offline || props.creating || !pasteText.trim()}
                    title={props.offline ? t("workbenchOfflineHint") : undefined}
                    onClick={applyPaste}
                  >
                    {t("pasteGbApply")}
                  </button>
                </div>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  )
}
