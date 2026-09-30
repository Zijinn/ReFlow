import { Fragment, useMemo, useState } from "react"

import { CaretRight } from "@phosphor-icons/react"

import type { ResearchPaper, ResearchPaperPatch } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { toast } from "../../store/toast"
import {
  buildStageTemplate,
  countStageLeaves,
  computeProgress,
  stageTicks,
} from "../../lib/research"
import { ChipEditor, DragHandle, EmptyState, InlineText, MenuSelect, NotesCell, Row } from "./shared"
import { displayID, matchesPaperQuery, priorityBadgeClass, priorityDotClass, reorderList } from "./utils"
import { StageTree } from "./StageTree"

const PRIORITIES = [
  { value: "High", key: "priorityHigh" },
  { value: "Medium", key: "priorityMedium" },
  { value: "Average", key: "priorityAverage" },
]

// 优先级排序的语义档位：High > Medium > Average。空串/未知取值一律视为
// "没有优先级"（档 0），两个方向都排在最后——空值不是"最低优先级"，
// 而是"没填"，把它混进 Average 之下会假装它是一个真实档位。
const PRIORITY_RANK: Record<string, number> = { High: 3, Medium: 2, Average: 1 }

// 三态循环：off（手工顺序）→ desc（高到低）→ asc（低到高）→ off。
type PrioritySort = "off" | "desc" | "asc"

const NEXT_SORT: Record<PrioritySort, PrioritySort> = { off: "desc", desc: "asc", asc: "off" }

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
  // 优先级列头排序是纯前端视图排序：只改渲染顺序，不写回后端、不碰手工顺序。
  const [prioritySort, setPrioritySort] = useState<PrioritySort>("off")
  const sorting = prioritySort !== "off"

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

  // 视图排序：papers prop 的副本，同档按原（手工）顺序稳定排列，空优先级恒在最后。
  const visible = useMemo(() => {
    if (prioritySort === "off") return filtered
    const direction = prioritySort === "desc" ? -1 : 1
    return filtered
      .map((paper, index) => ({ paper, index }))
      .sort((a, b) => {
        const rankA = PRIORITY_RANK[a.paper.priority ?? ""] ?? 0
        const rankB = PRIORITY_RANK[b.paper.priority ?? ""] ?? 0
        if (rankA === 0 || rankB === 0) {
          if (rankA === rankB) return a.index - b.index
          return rankA === 0 ? 1 : -1
        }
        if (rankA !== rankB) return (rankA - rankB) * direction
        return a.index - b.index
      })
      .map((entry) => entry.paper)
  }, [filtered, prioritySort])

  // title 说清三态循环："当前 · 下一次点击"。aria-sort 挂在 th 上供读屏播报，
  // 按钮的可及名保持稳定（sortByPriority），状态由列头的 aria-sort 表达。
  const sortStateLabel =
    prioritySort === "desc"
      ? t("prioritySortHighFirst")
      : prioritySort === "asc"
        ? t("prioritySortLowFirst")
        : t("sortByPriority")
  const sortNextLabel =
    prioritySort === "off"
      ? t("prioritySortHighFirst")
      : prioritySort === "desc"
        ? t("prioritySortLowFirst")
        : t("prioritySortOff")
  const sortTitle = `${sortStateLabel} · ${sortNextLabel}`

  const reorder = (fromID: string, toID: string, before: boolean) => {
    // 排序视图里的行序不是手工顺序，绝不能被拖拽写回（把手此时也不渲染，
    // 这里是第二道保险）。
    if (sorting) return
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
        <table
          className={`wb-table wb-table--research${sorting ? " wb-table--sorted" : ""}`}
        >
          <thead>
            <tr>
              <th className="wb-col-grip" aria-label={t("colCode")} />
              <th className="wb-col-title">{t("colTitle")}</th>
              <th className="wb-col-stage">{t("colStage")}</th>
              <th
                className="wb-col-priority"
                aria-label={t("colPriority")}
                aria-sort={
                  prioritySort === "off"
                    ? "none"
                    : prioritySort === "desc"
                      ? "descending"
                      : "ascending"
                }
              >
                <button
                  type="button"
                  className={`wb-th-sort${sorting ? " wb-th-sort--active" : ""}`}
                  aria-label={t("sortByPriority")}
                  title={sortTitle}
                  onClick={() => setPrioritySort((prev) => NEXT_SORT[prev])}
                >
                  {t("colPriority")}
                  <span className="wb-th-sort-arrow" aria-hidden="true">
                    {prioritySort === "desc" ? "↓" : prioritySort === "asc" ? "↑" : "↕"}
                  </span>
                </button>
              </th>
              <th className="wb-col-text">{t("targetJournal")}</th>
              <th className="wb-col-text wb-col-note">{t("nextAction")}</th>
              <th className="wb-col-date">{t("lastUpdatedLabel")}</th>
              <th className="wb-col-notes">{t("colNotes")}</th>
              <th className="wb-col-actions">{t("colActions")}</th>
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 ? (
              <tr>
                <td colSpan={9} className="wb-empty">
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
              visible.map((paper) => {
                const index = props.papers.findIndex((p) => p.id === paper.id)
                const expanded = expandedID === paper.id
                // 没有阶段的论文按标准流程"虚拟"展示：不写库，点任一阶段才落。
                const virtualStages = paper.stages.length === 0
                const stages = virtualStages ? buildStageTemplate(t) : paper.stages
                const progress = computeProgress(stages)
                const leaves = countStageLeaves(stages)
                const ticks = stageTicks(stages)
                const currentTickIndex = ticks.findIndex((tick) => tick.fraction < 1)
                const currentStage =
                  currentTickIndex >= 0 ? ticks[currentTickIndex]!.name : t("stageAllDone")
                return (
                  <Fragment key={paper.id}>
                    <Row id={paper.id} onReorder={reorder} dataPaperID={paper.id}>
                      <td className="wb-col-grip">
                        {/* 排序激活时不渲染把手：Row 只在按住把手时才武装拖拽，
                            把手缺席即拖拽禁用，排序视图不会被误存成手工顺序。 */}
                        {!sorting && <DragHandle />}
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
                        <button
                          type="button"
                          className={`wb-stage-cell${expanded ? " wb-stage-cell--open" : ""}`}
                          aria-expanded={expanded}
                          aria-label={t("expandStageHint")}
                          title={t("expandStageHint")}
                          onClick={() => setExpandedID(expanded ? null : paper.id)}
                        >
                          <span
                            className="wb-stage-rail"
                            role="progressbar"
                            aria-label={t("stageRailLabel")}
                            aria-valuenow={progress}
                            aria-valuemin={0}
                            aria-valuemax={100}
                          >
                            {ticks.map((tick, tickIndex) => (
                              <span
                                key={`${tick.name}-${tickIndex}`}
                                className={`wb-stage-tick${
                                  tickIndex === currentTickIndex ? " wb-stage-tick--current" : ""
                                }`}
                              >
                                <span
                                  className="wb-stage-tick-fill"
                                  style={{ width: `${Math.round(tick.fraction * 100)}%` }}
                                />
                              </span>
                            ))}
                          </span>
                          <span className="wb-progress-text">{progress}%</span>
                          <span
                            className="wb-stage-cell-current"
                            title={`${t("stageCurrent")}: ${currentStage}`}
                          >
                            {currentStage}
                          </span>
                          <CaretRight
                            className="wb-stage-cell-caret"
                            size={12}
                            weight="bold"
                            aria-hidden="true"
                          />
                        </button>
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
                      <td className="wb-col-notes">
                        <NotesCell
                          value={paper.notes}
                          ariaLabel={`${t("notesEditHint")}: ${paper.title || displayID("research", index)}`}
                          onCommit={(notes) => props.onUpdate(paper.id, { notes })}
                        />
                      </td>
                      <td className="wb-col-actions">
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
                        <td colSpan={9}>
                          <div className="wb-detail-grid">
                            <StageTree
                              stages={stages}
                              virtual={virtualStages}
                              onChange={(next) => props.onUpdate(paper.id, { stages: next })}
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
                              <div className="wb-detail-notes">
                                <div className="wb-muted wb-abstract-label">{t("colNotes")}</div>
                                <div className="wb-abstract">
                                  <InlineText
                                    multiline
                                    value={paper.notes}
                                    placeholder={t("fillPlaceholder")}
                                    onCommit={(notes) => props.onUpdate(paper.id, { notes })}
                                  />
                                </div>
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
