// 月历浮层（DatePickerCell）的样式跟着在投页那份引入：ESM 里同一个模块只会
// 进一次产物，所以这里再 import 一次不会把 submitted-picker.css 拷第二份进 chunk；
// 求值顺序仍旧排在 main.tsx 那一串 styles.css / phase*.css 之后（见该文件开头）。
import "./submitted-picker.css"

import {
  Fragment,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
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
import {
  ChipEditor,
  ColumnHead,
  DatePickerCell,
  DragHandle,
  EmptyState,
  InlineText,
  MenuSelect,
  NotesCell,
  Row,
} from "./shared"
import {
  daysUntil,
  deadlineDays,
  deadlineUrgency,
  displayID,
  matchesPaperQuery,
  parseDeadline,
  reorderList,
  tagBadgeClass,
  tagDisplayName,
  tagDotClass,
  tagHueVar,
} from "./utils"
import { StageTree } from "./StageTree"

// 工具栏排序档：manual 是手工顺序（拖拽把手可见），其余几种都是纯前端视图
// 排序——只改渲染顺序，不写回后端、不碰手工顺序，且把手隐藏。
type SortMode = "manual" | "updated_desc" | "updated_asc" | "deadline_asc" | "tag"

// 行染色取的是"这一行最高的那一档"：调色板里下标最小的那枚标签（和 tag 排序
// 同一个口径），它挂上的色相写进 --wb-row-hue 交给 phase3-research.css 去画。
// 没挂标签、或只剩已被删除的残留 id，就返回 null 不染色。
// 把颜色关掉的标签也不参加这场比较：它是用来归类的，不是用来说"这行很急"的。
// 所以一行上若还有别的在染的标签，色相来自那些；全关掉了就跟没挂一样不涂色。
function rowHue(tagIDs: string[], tags: ResearchTag[], rank: Map<string, number>): string | null {
  let best = -1
  for (const id of tagIDs) {
    const index = rank.get(id)
    if (index === undefined) continue
    if (!tags[index]!.color_enabled) continue
    if (best < 0 || index < best) best = index
  }
  if (best < 0) return null
  return tagHueVar(best, tags[best]!.color, tags[best]!.color_enabled)
}

// 标签格：一篇论文可以同时挂多个标签，挂上的都平铺成小药丸（与在投页的作者/关键词
// 同一套语汇：药丸 + 每颗自带的 ✕ + 末尾一颗 ＋），列宽不够就换行、行高自己长，
// 不再做「+N」折叠——那一列本来就能拖宽，折叠只是把用户挂上去的东西藏起来。
// ✕ 就地摘掉这一个标签；＋ 打开弹层菜单，每个标签是一行可勾选的项
// （menuitemcheckbox + aria-checked），点一下只切换它、不关浮层，多选才连得下去，
// 只有 Esc 或点到格子弹层之外才收。菜单里保留「清空全部」这一整串清空。
// 新建走行内输入（window.prompt 在桌面 WKWebView 里永远不渲染）：Enter 经
// onCreateTag POST 成功后追加到这篇论文的 tag_ids，Esc 退回菜单。输入框自己处理
// 键盘并 stopPropagation，菜单的方向键/Enter 逻辑不会截走它，也不存在可提交
// 的外层表单。
function TagCell(props: {
  paper: ResearchPaper
  tags: ResearchTag[]
  offline?: boolean
  onChangeTags: (tagIDs: string[]) => void
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

  const tagIDs = props.paper.tag_ids
  const selected = useMemo(() => new Set(tagIDs), [tagIDs])
  // 调色板下标同时是"没有自选色时"的色阶下标；不在调色板里的 id（标签已被删除）
  // 取 -1，tagBadgeClass/tagDotClass 对负下标本就回中性灰，名字换成占位文案。
  const paletteIndex = useMemo(
    () => new Map(props.tags.map((tag, index) => [tag.id, index] as const)),
    [props.tags],
  )
  const entries = useMemo(
    () =>
      tagIDs.map((id, order) => {
        const index = paletteIndex.get(id) ?? -1
        const tag = index >= 0 ? props.tags[index] : undefined
        return {
          id,
          key: `${id}-${order}`,
          index,
          color: tag?.color ?? "",
          // 关掉颜色的标签，这颗药丸整体回到中性：不涂色相、点不显色。名字仍挂得上、
          // 仍能摘，只是不再替这行说话。
          painted: tag?.color_enabled ?? true,
          label: tag ? tagDisplayName(tag.name, t) : t("tagRemovedLabel"),
        }
      }),
    [paletteIndex, props.tags, tagIDs, t],
  )
  // 菜单行：清空全部(0) + 调色板顺序的每个标签(1..n) + 新建标签(n+1)。
  const itemCount = props.tags.length + 2

  const close = () => {
    setOpen(false)
    setCreating(false)
    setDraft("")
    setActiveIndex(-1)
  }
  const openMenu = () => {
    const firstChecked = props.tags.findIndex((tag) => selected.has(tag.id))
    setActiveIndex(firstChecked >= 0 ? firstChecked + 1 : 0)
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

  // 勾上追加到末尾、取消就地摘掉，其余顺序不动：tag_ids 的顺序就是用户的指派顺序。
  // 不关浮层也不动焦点，多选要能连着点。
  const toggle = (tagID: string) => {
    props.onChangeTags(
      selected.has(tagID) ? tagIDs.filter((id) => id !== tagID) : [...tagIDs, tagID],
    )
  }
  const clearAll = () => {
    const hadTags = tagIDs.length > 0
    close()
    buttonRef.current?.focus({ preventScroll: true })
    if (hadTags) props.onChangeTags([])
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
    props.onChangeTags([...tagIDs, created.id])
  }

  const commitIndex = (index: number) => {
    if (index === itemCount - 1) {
      setCreating(true)
      setActiveIndex(-1)
      return
    }
    if (index === 0) {
      clearAll()
      return
    }
    const tag = props.tags[index - 1]
    if (tag) toggle(tag.id)
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
      <span className="wb-tag-list">
        {entries.map((entry) => (
          <span
            key={entry.key}
            className={`wb-tag-chip ${tagBadgeClass(entry.index, entry.color, entry.painted)}`}
          >
            <i
              className={`wb-dot ${tagDotClass(entry.index, entry.color, entry.painted)}`}
              aria-hidden="true"
            />
            <span className="wb-tag-chip-label">{entry.label}</span>
            <button
              type="button"
              className="wb-chip-del wb-tag-chip-del"
              title={t("detachTag")}
              aria-label={`${t("detachTag")}: ${entry.label}`}
              onClick={() => toggle(entry.id)}
            >
              ✕
            </button>
          </span>
        ))}
        {entries.length === 0 && <span className="wb-tag-blank">{t("untaggedLabel")}</span>}
      </span>
      <button
        ref={buttonRef}
        type="button"
        className="wb-chip-add wb-tag-add"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("assignTag")}
        title={t("assignTag")}
        onClick={() => (open ? close() : openMenu())}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !open) {
            e.preventDefault()
            openMenu()
          }
        }}
      >
        ＋
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
            onClick={clearAll}
          >
            <span className="wb-menu-item-label wb-tag-none">
              {tagIDs.length ? t("clearAllTags") : t("untaggedLabel")}
            </span>
            {tagIDs.length === 0 && (
              <span className="wb-menu-check" aria-hidden="true">
                ✓
              </span>
            )}
          </button>
          {props.tags.map((tag, index) => {
            const checked = selected.has(tag.id)
            return (
              <button
                {...itemProps(index + 1)}
                ref={(node) => {
                  itemRefs.current[index + 1] = node
                }}
                key={tag.id}
                type="button"
                role="menuitemcheckbox"
                aria-checked={checked}
                className={itemClass(index + 1)}
                onClick={() => toggle(tag.id)}
              >
                <i
                  className={`wb-dot ${tagDotClass(index, tag.color, tag.color_enabled)}`}
                  aria-hidden="true"
                />
                <span className="wb-menu-item-label">{tagDisplayName(tag.name, t)}</span>
                {/* 勾选框永远占位：切换时整行不跳，读屏也拿到 aria-checked。 */}
                <span
                  className={`wb-tag-check ${checked ? "wb-tag-check--on" : ""}`}
                  aria-hidden="true"
                >
                  {checked ? "✓" : ""}
                </span>
              </button>
            )
          })}
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

  // 调色板顺序即服务端 position 升序，筛选项按它排；圆点取的是标签自己存的颜色，
  // 没有自选色才回落到下标推出来的那一档；颜色被关掉的标签点是中性的——筛选器本身
  // 不受开关影响，仍然按名字过滤。
  const tagOptions = useMemo(
    () =>
      props.tags.map((tag, index) => ({
        value: tag.id,
        label: tagDisplayName(tag.name, t),
        dotClass: tagDotClass(index, tag.color, tag.color_enabled),
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
      if (tagFilter && !paper.tag_ids.includes(tagFilter)) return false
      if (!query) return true
      return matchesPaperQuery(paper, query)
    })
  }, [props.papers, search, tagFilter])

  // 视图排序：papers prop 的副本，同档按原（手工）顺序稳定排列。标签顺序按
  // 调色板下标升序，多个标签取其中最前的那一档（挂上就等于声称它属于那个优先级
  // 区间）；未挂标签（或 id 全都不在调色板里）恒排最后。截止档同一条规矩：
  // 没有日历日期（空串或"下周三是死线"这类自由文本）恒排最后。
  const visible = useMemo(() => {
    if (sortMode === "manual") return filtered
    const untagged = Number.MAX_SAFE_INTEGER
    const rank = (paper: ResearchPaper) =>
      paper.tag_ids.reduce((best, id) => Math.min(best, tagRank.get(id) ?? untagged), untagged)
    return filtered
      .map((paper, index) => ({ paper, index }))
      .sort((a, b) => {
        if (sortMode === "tag") {
          const rankA = rank(a.paper)
          const rankB = rank(b.paper)
          if (rankA !== rankB) return rankA - rankB
          return a.index - b.index
        }
        if (sortMode === "deadline_asc") {
          const days = deadlineDays(a.paper) - deadlineDays(b.paper)
          if (days !== 0) return days
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
            { value: "deadline_asc", label: t("sortDeadlineNearest") },
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
              <ColumnHead table="research" column="title" className="wb-col-title">
                {t("colTitle")}
              </ColumnHead>
              <ColumnHead table="research" column="stage" className="wb-col-stage">
                {t("colStage")}
              </ColumnHead>
              {/* 列宽类沿用 wb-col-priority（styles.css 的固定列宽几何），
                  列头文案换成"标签"。 */}
              <ColumnHead table="research" column="tag" className="wb-col-priority">
                {t("colTag")}
              </ColumnHead>
              <ColumnHead table="research" column="journal" className="wb-col-text">
                {t("targetJournal")}
              </ColumnHead>
              <ColumnHead table="research" column="note" className="wb-col-text wb-col-note">
                {t("nextAction")}
              </ColumnHead>
              {/* 截止日期挨着更新日期放（两张日期列归在一起）。列头键用 "deadline"
                  而不是 "date"：--wb-col-w 的持久化是按 column 键存的，"date" 已经被
                  更新日期占了，共用会让两列同宽同变。
                  类名带两份：wb-col-date 借等宽数字与"日期列"那一套几何，
                  wb-col-deadline 是这一列自己的钩子（宽度与收列档在
                  phase3-research.css 里）。 */}
              <ColumnHead
                table="research"
                column="deadline"
                className="wb-col-date wb-col-deadline"
              >
                {t("deadlineLabel")}
              </ColumnHead>
              <ColumnHead table="research" column="date" className="wb-col-date">
                {t("lastUpdatedLabel")}
              </ColumnHead>
              <ColumnHead table="research" column="notes" className="wb-col-notes">
                {t("colNotes")}
              </ColumnHead>
              <ColumnHead table="research" column="actions" className="wb-col-actions">
                {t("colActions")}
              </ColumnHead>
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 ? (
              <tr>
                <td colSpan={10} className="wb-empty">
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
                // 行染色的色相（最高优先级那一枚标签的颜色）与截止紧急度。
                const hue = rowHue(paper.tag_ids, props.tags, tagRank)
                const deadlineDate = parseDeadline(paper.deadline)
                const urgency = deadlineDate
                  ? deadlineUrgency(daysUntil(deadlineDate))
                  : null
                return (
                  <Fragment key={paper.id}>
                    <Row
                      id={paper.id}
                      onReorder={reorder}
                      dataPaperID={paper.id}
                      className={hue ? "wb-row--tinted" : undefined}
                      // 自定义属性里放的是 var(--wb-hue-*) 这条引用本身：CSS 变量
                      // 允许持有 var()，于是浅深两档跟着令牌走，一行代码都不用在
                      // 样式表里挑颜色（也不新增任何色板令牌）。
                      style={hue ? ({ "--wb-row-hue": hue } as CSSProperties) : undefined}
                    >
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
                          onChangeTags={(tagIDs) => props.onUpdate(paper.id, { tag_ids: tagIDs })}
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
                      <td className="wb-col-date wb-col-deadline">
                        {/* 空格子也要能点：占位那枚"—"是按钮里的内容，不是禁用的标签。 */}
                        <DatePickerCell
                          value={paper.deadline}
                          placeholder="—"
                          ariaLabel={t("deadlineLabel")}
                          onCommit={(deadline) => props.onUpdate(paper.id, { deadline })}
                        />
                        {urgency && urgency.key && (
                          <span className={`wb-deadline-hint ${urgency.className}`}>
                            {urgency.count === undefined
                              ? t(urgency.key)
                              : `${urgency.count} ${t(urgency.key)}`}
                          </span>
                        )}
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
                        <td colSpan={10}>
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
