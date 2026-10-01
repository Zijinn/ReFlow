import {
  Fragment,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react"

import { CaretRight } from "@phosphor-icons/react"

import type { ResearchPaper, ResearchPaperPatch, ResearchTag } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { toast } from "../../store/toast"
import {
  buildStageTemplate,
  countStageLeaves,
  computeProgress,
  stageTicks,
} from "../../lib/research"
import { ChipEditor, DragHandle, EmptyState, InlineText, MenuSelect, NotesCell, Row } from "./shared"
import {
  displayID,
  matchesPaperQuery,
  reorderList,
  tagBadgeClass,
  tagDisplayName,
  tagDotClass,
} from "./utils"
import { StageTree } from "./StageTree"

// 工具栏排序档：manual 是手工顺序（拖拽把手可见），其余三种都是纯前端视图
// 排序——只改渲染顺序，不写回后端、不碰手工顺序，且把手隐藏。
type SortMode = "manual" | "updated_desc" | "updated_asc" | "tag"

// 标签格：药丸按钮 + 弹层菜单（无标签 / 调色板顺序的每个标签 / 新建标签）。
// 新建走行内输入（window.prompt 在桌面 WKWebView 里永远不渲染）：Enter 经
// onCreateTag POST 成功后就地指派给这篇论文，Esc 退回菜单。输入框自己处理
// 键盘并 stopPropagation，菜单的方向键/Enter 逻辑不会截走它，也不存在可提交
// 的外层表单。
function TagCell(props: {
  paper: ResearchPaper
  tags: ResearchTag[]
  offline?: boolean
  onAssign: (tagID: string) => void
  onCreateTag: (name: string) => Promise<ResearchTag | null>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState("")
  const [activeIndex, setActiveIndex] = useState(-1)
  const [placement, setPlacement] = useState({ up: false, right: false, maxHeight: 0 })
  const rootRef = useRef<HTMLDivElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const popRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])

  const tagIndex = props.tags.findIndex((tag) => tag.id === props.paper.tag_id)
  const currentTag = tagIndex >= 0 ? props.tags[tagIndex]! : undefined
  const label = currentTag ? tagDisplayName(currentTag.name, t) : t("untaggedLabel")
  // 菜单行：无标签(0) + 每个标签(1..n) + 新建标签(n+1)。
  const itemCount = props.tags.length + 2

  const close = () => {
    setOpen(false)
    setCreating(false)
    setDraft("")
    setActiveIndex(-1)
  }
  const openMenu = () => {
    setActiveIndex(Math.max(tagIndex + 1, 0))
    setCreating(false)
    setOpen(true)
  }

  // 与 MenuSelect 同一套锚定：格子弹层可能被视口下缘/右缘切掉，开一次量一次，
  // 空间不够就向上翻、向右收。
  useLayoutEffect(() => {
    if (!open) return
    const trigger = buttonRef.current
    const pop = popRef.current
    if (!trigger || !pop) return
    const rect = trigger.getBoundingClientRect()
    const gutter = 8
    const gap = 6
    const spaceBelow = window.innerHeight - rect.bottom - gap - gutter
    const spaceAbove = rect.top - gap - gutter
    const natural = pop.offsetHeight
    setPlacement({
      up: natural > spaceBelow && spaceAbove > spaceBelow,
      right: rect.left + pop.offsetWidth > window.innerWidth - gutter,
      maxHeight: Math.max(120, Math.max(spaceBelow, spaceAbove)),
    })
  }, [open, creating])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) close()
    }
    document.addEventListener("pointerdown", onPointerDown)
    return () => {
      document.removeEventListener("pointerdown", onPointerDown)
    }
  }, [open])

  useEffect(() => {
    if (!open || activeIndex < 0) return
    const node = itemRefs.current[activeIndex]
    const pop = popRef.current
    if (!node) return
    node.focus({ preventScroll: true })
    if (!pop) return
    if (node.offsetTop < pop.scrollTop) pop.scrollTop = node.offsetTop
    else if (node.offsetTop + node.offsetHeight > pop.scrollTop + pop.clientHeight) {
      pop.scrollTop = node.offsetTop + node.offsetHeight - pop.clientHeight
    }
  }, [open, activeIndex])

  useEffect(() => {
    if (open && creating) inputRef.current?.focus({ preventScroll: true })
  }, [open, creating])

  const assign = (tagID: string) => {
    close()
    buttonRef.current?.focus({ preventScroll: true })
    if (tagID !== props.paper.tag_id) props.onAssign(tagID)
  }

  const submitCreate = async () => {
    const name = draft.trim()
    if (!name || saving) return
    setSaving(true)
    const created = await props.onCreateTag(name)
    setSaving(false)
    // 失败的 toast 由创建方负责（409 重名 / 400 超长等），这里只保留输入现场。
    if (!created) return
    setDraft("")
    setCreating(false)
    setOpen(false)
    setActiveIndex(-1)
    toast(t("tagCreated"))
    buttonRef.current?.focus({ preventScroll: true })
    props.onAssign(created.id)
  }

  const commitIndex = (index: number) => {
    if (index === itemCount - 1) {
      setCreating(true)
      setActiveIndex(-1)
      return
    }
    if (index === 0) {
      assign("")
      return
    }
    const tag = props.tags[index - 1]
    if (tag) assign(tag.id)
  }

  const onMenuKeyDown = (e: React.KeyboardEvent) => {
    const last = itemCount - 1
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActiveIndex((i) => (i >= last ? 0 : i + 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActiveIndex((i) => (i <= 0 ? last : i - 1))
    } else if (e.key === "Home") {
      e.preventDefault()
      setActiveIndex(0)
    } else if (e.key === "End") {
      e.preventDefault()
      setActiveIndex(last)
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      if (activeIndex >= 0) commitIndex(activeIndex)
    } else if (e.key === "Escape") {
      e.preventDefault()
      close()
      buttonRef.current?.focus({ preventScroll: true })
    } else if (e.key === "Tab") {
      close()
    }
  }

  const itemClass = (index: number) =>
    `wb-menu-item ${index === activeIndex ? "wb-menu-item--focus" : ""}`
  const itemProps = (index: number) => ({
    tabIndex: -1,
    onMouseEnter: () => setActiveIndex(index),
  })

  return (
    <div ref={rootRef} className="wb-menu wb-tag-cell">
      <button
        ref={buttonRef}
        type="button"
        className={`wb-menu-btn wb-tag-pill ${currentTag ? tagBadgeClass(tagIndex) : "wb-badge--gray wb-tag-pill--empty"}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`${t("colTag")}: ${label}`}
        title={`${t("colTag")}: ${label}`}
        onClick={() => (open ? close() : openMenu())}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !open) {
            e.preventDefault()
            openMenu()
          }
        }}
      >
        {currentTag && (
          <i className={`wb-dot ${tagDotClass(tagIndex)}`} aria-hidden="true" />
        )}
        <span className="wb-menu-label">{label}</span>
        <span
          className={`wb-menu-chevron ${open ? "wb-menu-chevron--open" : ""}`}
          aria-hidden="true"
        >
          ▾
        </span>
      </button>
      {open && (
        <div
          ref={popRef}
          role="menu"
          aria-label={t("colTag")}
          className={`wb-menu-pop ${placement.up ? "wb-menu-pop--up" : ""} ${
            placement.right ? "wb-menu-pop--right" : ""
          } wb-tag-menu`}
          style={placement.maxHeight ? { maxHeight: placement.maxHeight } : undefined}
          onKeyDown={onMenuKeyDown}
        >
          <button
            {...itemProps(0)}
            ref={(node) => {
              itemRefs.current[0] = node
            }}
            type="button"
            role="menuitem"
            className={itemClass(0)}
            onClick={() => assign("")}
          >
            <span className="wb-menu-item-label wb-tag-none">{t("untaggedLabel")}</span>
            {!currentTag && (
              <span className="wb-menu-check" aria-hidden="true">
                ✓
              </span>
            )}
          </button>
          {props.tags.map((tag, index) => (
            <button
              {...itemProps(index + 1)}
              ref={(node) => {
                itemRefs.current[index + 1] = node
              }}
              key={tag.id}
              type="button"
              role="menuitem"
              className={itemClass(index + 1)}
              onClick={() => assign(tag.id)}
            >
              <i className={`wb-dot ${tagDotClass(index)}`} aria-hidden="true" />
              <span className="wb-menu-item-label">{tagDisplayName(tag.name, t)}</span>
              {tag.id === props.paper.tag_id && (
                <span className="wb-menu-check" aria-hidden="true">
                  ✓
                </span>
              )}
            </button>
          ))}
          {creating ? (
            <div className="wb-tag-create-row">
              <input
                ref={inputRef}
                className="wb-tag-create-input"
                type="text"
                value={draft}
                maxLength={40}
                disabled={saving}
                placeholder={t("newTagPlaceholder")}
                aria-label={t("newTag")}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  // 输入框自己吃掉键盘事件：外层菜单的方向键/Enter 逻辑不介入，
                  // Enter 也永远不会变成表单提交（这里根本没有 form）。
                  e.stopPropagation()
                  if (e.key === "Enter") {
                    e.preventDefault()
                    void submitCreate()
                  } else if (e.key === "Escape") {
                    e.preventDefault()
                    setDraft("")
                    setCreating(false)
                    setActiveIndex(itemCount - 1)
                  }
                }}
              />
            </div>
          ) : (
            <button
              {...itemProps(itemCount - 1)}
              ref={(node) => {
                itemRefs.current[itemCount - 1] = node
              }}
              type="button"
              role="menuitem"
              className={`${itemClass(itemCount - 1)} wb-tag-create`}
              disabled={props.offline}
              title={props.offline ? t("workbenchOfflineHint") : undefined}
              onClick={() => commitIndex(itemCount - 1)}
            >
              <span className="wb-tag-create-plus" aria-hidden="true">
                ＋
              </span>
              <span className="wb-menu-item-label">{t("newTag")}</span>
            </button>
          )}
        </div>
      )}
    </div>
  )
}

export function ResearchPage(props: {
  papers: ResearchPaper[]
  tags: ResearchTag[]
  offline?: boolean
  creating?: boolean
  onCreate: () => void
  onUpdate: (id: string, patch: ResearchPaperPatch) => void
  onDelete: (id: string) => void
  onReorder: (orderedIDs: string[]) => void
  onMove: (id: string) => void
  onCreateTag: (name: string) => Promise<ResearchTag | null>
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState("")
  const [tagFilter, setTagFilter] = useState("")
  const [expandedID, setExpandedID] = useState<string | null>(null)
  const [sortMode, setSortMode] = useState<SortMode>("manual")
  const sorting = sortMode !== "manual"

  // 调色板顺序即服务端 position 升序，筛选项与色阶都直接按下标走。
  const tagOptions = useMemo(
    () =>
      props.tags.map((tag, index) => ({
        value: tag.id,
        label: tagDisplayName(tag.name, t),
        dotClass: tagDotClass(index),
      })),
    [props.tags, t],
  )

  const tagRank = useMemo(
    () => new Map(props.tags.map((tag, index) => [tag.id, index])),
    [props.tags],
  )

  const filtered = useMemo(() => {
    const query = search.toLowerCase().trim()
    return props.papers.filter((paper) => {
      if (tagFilter && paper.tag_id !== tagFilter) return false
      if (!query) return true
      return matchesPaperQuery(paper, query)
    })
  }, [props.papers, search, tagFilter])

  // 视图排序：papers prop 的副本，同档按原（手工）顺序稳定排列。标签顺序按
  // 调色板下标升序，未挂标签（或 id 不在调色板里）恒排最后。
  const visible = useMemo(() => {
    if (sortMode === "manual") return filtered
    const untagged = Number.MAX_SAFE_INTEGER
    return filtered
      .map((paper, index) => ({ paper, index }))
      .sort((a, b) => {
        if (sortMode === "tag") {
          const rankA = a.paper.tag_id ? tagRank.get(a.paper.tag_id) ?? untagged : untagged
          const rankB = b.paper.tag_id ? tagRank.get(b.paper.tag_id) ?? untagged : untagged
          if (rankA !== rankB) return rankA - rankB
          return a.index - b.index
        }
        // last_updated 是 ISO 时间串，字典序即时间序；空串在"旧→新"里最靠前。
        const cmp = (a.paper.last_updated || "").localeCompare(b.paper.last_updated || "")
        if (cmp !== 0) return sortMode === "updated_desc" ? -cmp : cmp
        return a.index - b.index
      })
      .map((entry) => entry.paper)
  }, [filtered, sortMode, tagRank])

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
          value={tagFilter}
          ariaLabel={t("colTag")}
          className="wb-filter-menu"
          onChange={setTagFilter}
          options={[{ value: "", label: t("allTags") }, ...tagOptions]}
        />
        <MenuSelect
          value={sortMode}
          ariaLabel={t("sortLabel")}
          className="wb-filter-menu wb-sort-menu"
          onChange={(value) => setSortMode(value as SortMode)}
          options={[
            { value: "manual", label: t("sortManual") },
            { value: "updated_desc", label: t("sortUpdatedNewest") },
            { value: "updated_asc", label: t("sortUpdatedOldest") },
            { value: "tag", label: t("sortTagOrder") },
          ]}
        />
        {(search.trim() || tagFilter) && (
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
              {/* 列宽类沿用 wb-col-priority（styles.css 的固定列宽几何），
                  列头文案换成"标签"。 */}
              <th className="wb-col-priority">{t("colTag")}</th>
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
                  {search.trim() || tagFilter ? (
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
                        <TagCell
                          paper={paper}
                          tags={props.tags}
                          offline={props.offline}
                          onAssign={(tagID) => props.onUpdate(paper.id, { tag_id: tagID })}
                          onCreateTag={props.onCreateTag}
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
