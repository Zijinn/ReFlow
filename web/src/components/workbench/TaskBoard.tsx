import "./tasks.css"

import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ArrowUUpLeft,
  Archive,
  CaretDown,
  CaretUp,
  Check,
  DotsThree,
  FileText,
  Link,
  LockSimple,
  LockSimpleOpen,
  Plus,
  Trash,
  XCircle,
} from "@phosphor-icons/react"
import {
  Fragment,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react"

import {
  closeTaskLayer,
  completeTaskAssignment,
  createTaskAssignment,
  createTaskItem,
  createTaskLayer,
  createTaskStep,
  createTaskThread,
  deleteTaskAssignment,
  deleteTaskItem,
  deleteTaskLayer,
  deleteTaskStep,
  deleteTaskThread,
  listResearchPapers,
  listTaskThreads,
  reorderTaskItems,
  updateTaskAssignment,
  updateTaskItem,
  updateTaskLayer,
  updateTaskStep,
  updateTaskThread,
} from "../../api/client"
import type {
  ListResponse,
  ResearchKind,
  ResearchPaper,
  TaskAssignment,
  TaskItem,
  TaskLayer,
  TaskStats,
  TaskStep,
  TaskThread,
} from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { useOnlineState } from "../../lib/online"
import { toast } from "../../store/toast"
import { ConfirmDialog } from "../ConfirmDialog"
import { DatePickerCell, EmptyState, ExpandToggle, InlineText } from "./shared"
import { parseDeadline, relativeTime } from "./utils"

// 整棵树只有一个 queryKey：`GET /tasks/threads` 把层、任务、指派、步和全部派生
// 字段（unlocked / progress / status / stats）一次带回来。写之后一律失效重取，
// 不在前端拼这棵树——派生口径只有服务端那一份，客户端补一份迟早和它漂开。
// 唯一的例外是"勾一步"：那一颗的本地结果毫无歧义（见 stepMutation），
// 所以它走快照-恢复那一套乐观更新，和 Workbench 的论文表同一个做法。
const TASK_QUERY_KEY = ["task-threads"]

// 「关联到论文」的候选来自三张论文表，用的就是 Workbench 那同一批缓存键，
// 所以在两个模块之间来回切并不会把论文表多打一遍。
const PAPER_KINDS: ResearchKind[] = ["research", "submitted", "published"]

type TaskTree = ListResponse<TaskThread>

/** 一次服务端写：跑什么 + 失败了念哪句话。派生字段交给失效重取。 */
interface TaskWrite {
  run: () => Promise<unknown>
  failKey: string
}

type CardPaneMode = "papers" | "layers"

/** 卡面 ••• 浮层当前停在二级面板上的哪一格；按任务 id 记，免得两张卡互相串。 */
interface CardPane {
  itemID: string
  mode: CardPaneMode
}

interface TaskActions {
  addThread: () => void
  renameThread: (threadID: string, name: string) => void
  archiveThread: (threadID: string, archived: boolean) => void
  deleteThread: (threadID: string) => void
  addLayer: (threadID: string) => void
  renameLayer: (layerID: string, name: string) => void
  closeLayer: (layerID: string) => void
  forceUnlock: (layerID: string, forced: boolean) => void
  deleteLayer: (layerID: string) => void
  addItem: (threadID: string, layerID?: string) => void
  renameItem: (itemID: string, title: string) => void
  saveNotes: (itemID: string, notes: string) => void
  linkPaper: (itemID: string, paperID: string | null) => void
  deleteItem: (itemID: string) => void
  moveItem: (thread: TaskThread, itemID: string, delta: number) => void
  addToLayer: (itemID: string, layerID: string) => void
  removeFromLayer: (assignment: TaskAssignment) => void
  setDates: (assignmentID: string, patch: { start_date?: string; due_date?: string }) => void
  complete: (assignment: TaskAssignment) => void
  toggleExpand: (assignmentID: string) => void
  expanded: (assignmentID: string) => boolean
  addStep: (assignmentID: string, name: string) => void
  renameStep: (stepID: string, name: string) => void
  toggleStep: (assignmentID: string, step: TaskStep) => void
  deleteStep: (stepID: string) => void
}

/** 一张卡（=一条指派）要读到的共享现场：论文表、动作、展开与浮层面板状态。 */
interface CardContext {
  paperTitles: Map<string, string>
  actions: TaskActions
  pane: CardPane | null
  onPane: (pane: CardPane | null) => void
  addingStep: string | null
  onAddingStep: (assignmentID: string | null) => void
}

// ── 纯派生小工具：只读服务端已经算好的字段，不重算任何口径 ──────────────

function byPosition(left: { position: number }, right: { position: number }): number {
  return left.position - right.position
}

function sortedLayers(layers: TaskLayer[]): TaskLayer[] {
  return [...layers].sort(byPosition)
}

// 层里的那一排卡＝这一层的指派。同一条任务挂在两层里就在两块层里各出现一次，
// 抬头那行 `3/13` 数的也是这些卡，不是任务条数。
function layerCards(
  layer: TaskLayer,
  items: TaskItem[],
): Array<{ item: TaskItem; assignment: TaskAssignment }> {
  const cards: Array<{ item: TaskItem; assignment: TaskAssignment }> = []
  for (const item of [...items].sort(byPosition)) {
    for (const assignment of item.assignments) {
      if (assignment.layer_id === layer.id) cards.push({ item, assignment })
    }
  }
  return cards.sort((left, right) => left.assignment.position - right.assignment.position)
}

function stepCounts(steps: TaskStep[]): { done: number; total: number } {
  let done = 0
  for (const step of steps) if (step.done) done += 1
  return { done, total: steps.length }
}

function layerFullyClosed(layer: TaskLayer): boolean {
  return layer.item_count > 0 && layer.closed_count >= layer.item_count
}

// M/D 一屏读得完的日子区间。服务端只发日历日；解析不动就原样露出来，
// 和 utils.ts 里那套"不猜用户写了什么"的规矩一致。
function shortDate(raw: string): string {
  if (!raw) return "—"
  const date = parseDeadline(raw)
  if (!date) return raw
  return `${date.getMonth() + 1}/${date.getDate()}`
}

function clampProgress(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, Math.round(value)))
}

// 卡面左缘那一档颜色：这一颗卡的步全勾完才是绿（与本层是否收口同判据），
// 其余跟着任务状态走——超期是红相、推进中是主色、锁定/未开始是中性档。
function cardEdgeClass(item: TaskItem, assignment: TaskAssignment): string {
  const { done, total } = stepCounts(assignment.steps)
  if (assignment.closed || (total > 0 && done === total)) return "wb-task-card--closed"
  if (item.status === "overdue") return "wb-task-card--overdue"
  if (item.status === "locked" || item.status === "unstarted") return "wb-task-card--idle"
  return "wb-task-card--active"
}

// 乐观勾一步：只改这一颗步的 done，别的字段一个都不碰（progress/status 那些
// 派生值等失效重取回来覆盖）。
function patchStep(
  tree: TaskTree,
  vars: { assignmentID: string; stepID: string; done: boolean },
): TaskTree {
  return {
    ...tree,
    items: tree.items.map((thread) => ({
      ...thread,
      items: thread.items.map((item) => ({
        ...item,
        assignments: item.assignments.map((assignment) =>
          assignment.id !== vars.assignmentID
            ? assignment
            : {
                ...assignment,
                steps: assignment.steps.map((step) =>
                  step.id === vars.stepID ? { ...step, done: vars.done } : step,
                ),
              },
        ),
      })),
    })),
  }
}

// ── ••• 浮层 ───────────────────────────────────────────────────────────
// 触发键与几何都借表格里那一族（.wb-icon-btn + .wb-menu-pop），这里只加一条
// "点别处/按 Esc 收起"和一行可选的小标题。菜单项由调用方拼，二级面板（挑论文、
// 挑层）靠 onSelect 换 pane，带 keepOpen 的项选中后不收起。
interface MenuEntry {
  key: string
  label: string
  Icon?: typeof Check
  onSelect?: () => void
  keepOpen?: boolean
  danger?: boolean
  disabled?: boolean
}

function TaskMenu(props: {
  label: string
  heading?: string
  entries: MenuEntry[]
  onClose?: () => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("pointerdown", onPointerDown)
    document.addEventListener("keydown", onKeyDown)
    return () => {
      document.removeEventListener("pointerdown", onPointerDown)
      document.removeEventListener("keydown", onKeyDown)
    }
  }, [open])

  // 收起之后二级面板就作废：下一次打开永远从动作格开始，不会停在上次挑论文的现场。
  const close = () => {
    setOpen(false)
    props.onClose?.()
  }

  return (
    <div className="wb-menu" ref={rootRef}>
      <button
        type="button"
        className="wb-icon-btn"
        aria-label={props.label}
        title={props.label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <DotsThree size={15} weight="bold" />
      </button>
      {open && (
        <div className="wb-menu-pop wb-task-menu-pop" role="menu" aria-label={props.label}>
          {props.heading && <div className="wb-task-menu-heading">{props.heading}</div>}
          {props.entries.length === 0 && (
            <div className="wb-task-menu-empty">{props.label}</div>
          )}
          {props.entries.map((entry) => {
            const Icon = entry.Icon
            return (
              <button
                key={entry.key}
                type="button"
                role="menuitem"
                className={`wb-menu-item wb-task-menu-item ${entry.danger ? "wb-task-menu-item--danger" : ""}`}
                disabled={entry.disabled}
                onClick={() => {
                  if (!entry.onSelect) return
                  entry.onSelect()
                  if (!entry.keepOpen) close()
                }}
              >
                {Icon && <Icon size={14} weight="regular" aria-hidden="true" />}
                <span className="wb-menu-item-label">{entry.label}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

// 新建一颗步：与阶段树那颗输入框同一套语义（Enter/失焦提交，Esc 放弃现场），
// 空字符串不写服务端。
function StepAddInput(props: {
  placeholder: string
  onCommit: (name: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState("")
  const commit = () => {
    const name = value.trim()
    if (name) props.onCommit(name)
    else props.onCancel()
  }
  return (
    <input
      className="wb-inline-input"
      autoFocus
      type="text"
      value={value}
      placeholder={props.placeholder}
      aria-label={props.placeholder}
      onChange={(event) => setValue(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault()
          commit()
        }
        if (event.key === "Escape") {
          event.preventDefault()
          props.onCancel()
        }
      }}
    />
  )
}

// ── 卡面 •••：动作格 ⇄ 挑论文 / 挑层两块二级面板 ────────────────────────
// MenuSelect 是"一个值一条菜单"的单层控件，这里要的是选完还留在菜单里的两层，
// 所以用同一套 .wb-menu-item 几何自己拼，pane 由上层按任务 id 拿着。
function CardMenu(props: {
  thread: TaskThread
  item: TaskItem
  assignment: TaskAssignment
  ctx: CardContext
}) {
  const { t } = useTranslation()
  const { item, assignment, thread, ctx } = props
  const pane = ctx.pane?.itemID === item.id ? ctx.pane.mode : null

  const ordered = useMemo(() => [...thread.items].sort(byPosition), [thread.items])
  const index = ordered.findIndex((entry) => entry.id === item.id)

  const paperEntries: MenuEntry[] = []
  ctx.paperTitles.forEach((title, paperID) => {
    paperEntries.push({
      key: `paper-${paperID}`,
      label: title,
      Icon: paperID === item.paper_id ? Check : FileText,
      onSelect: () => {
        ctx.actions.linkPaper(item.id, paperID)
        ctx.onPane(null)
      },
    })
  })

  const otherLayers = sortedLayers(thread.layers).filter((layer) => layer.id !== assignment.layer_id)
  const layerEntries: MenuEntry[] = otherLayers.map((layer) => ({
    key: `layer-${layer.id}`,
    label: layerMenuLabel(layer, t("taskUntitledLayer")),
    onSelect: () => {
      ctx.actions.addToLayer(item.id, layer.id)
      ctx.onPane(null)
    },
  }))

  const back: MenuEntry = {
    key: "back",
    label: t("taskBack"),
    Icon: ArrowUUpLeft,
    onSelect: () => ctx.onPane(null),
    keepOpen: true,
  }

  let entries: MenuEntry[]
  let heading: string | undefined
  if (pane === "papers") {
    heading = t("taskLinkPaper")
    entries = [back, ...paperEntries]
    if (paperEntries.length === 0) {
      entries.push({ key: "no-paper", label: t("taskNoPapersToLink"), disabled: true })
    }
  } else if (pane === "layers") {
    heading = t("taskAddToLayer")
    entries = [back, ...layerEntries]
    if (layerEntries.length === 0) {
      entries.push({ key: "no-layer", label: t("taskNoOtherLayers"), disabled: true })
    }
  } else {
    entries = [
      {
        key: "link",
        label: t("taskLinkPaper"),
        Icon: Link,
        onSelect: () => ctx.onPane({ itemID: item.id, mode: "papers" }),
        keepOpen: true,
      },
      ...(item.paper_id
        ? [
            {
              key: "unlink",
              label: t("taskUnlinkPaper"),
              Icon: XCircle,
              onSelect: () => ctx.actions.linkPaper(item.id, null),
            },
          ]
        : []),
      {
        key: "layers",
        label: t("taskAddToLayer"),
        Icon: Plus,
        onSelect: () => ctx.onPane({ itemID: item.id, mode: "layers" }),
        keepOpen: true,
      },
      ...(item.layer_count > 1
        ? [
            {
              key: "remove",
              label: t("taskRemoveFromLayer"),
              Icon: Trash,
              danger: true,
              onSelect: () => ctx.actions.removeFromLayer(assignment),
            },
          ]
        : []),
      {
        key: "up",
        label: t("taskMoveUp"),
        Icon: CaretUp,
        disabled: index <= 0,
        onSelect: () => ctx.actions.moveItem(thread, item.id, -1),
      },
      {
        key: "down",
        label: t("taskMoveDown"),
        Icon: CaretDown,
        disabled: index < 0 || index >= ordered.length - 1,
        onSelect: () => ctx.actions.moveItem(thread, item.id, 1),
      },
      {
        key: "delete",
        label: t("delete"),
        Icon: Trash,
        danger: true,
        onSelect: () => ctx.actions.deleteItem(item.id),
      },
    ]
  }

  return (
    <TaskMenu
      label={t("taskCardMenu")}
      heading={heading}
      entries={entries}
      onClose={() => ctx.onPane(null)}
    />
  )
}

// 菜单/药丸里的层名：没起名时读"第几层"，起名叫 L1/L2 这一类太长的一律裁在名字里。
function layerMenuLabel(layer: TaskLayer, fallback: string): string {
  const name = layer.name.trim() || fallback
  return `L${layer.position + 1} · ${name}`
}

// ── 一颗卡＝一条指派 ───────────────────────────────────────────────────
function TaskCard(props: {
  thread: TaskThread
  layer: TaskLayer
  item: TaskItem
  assignment: TaskAssignment
  ctx: CardContext
}) {
  const { item, assignment, layer, ctx, thread } = props
  const { t, locale } = useTranslation()
  const actions = ctx.actions
  const expanded = actions.expanded(assignment.id)
  const { done, total } = stepCounts(assignment.steps)
  const progress = clampProgress(item.progress)
  // 环里的数字是服务端跨全部层算的那一个：同一条任务的两颗卡读到同一个 40，
  // 这里绝不按本层的 done/total 另算一份。
  const ringStyle = { "--wb-task-ring-value": `${progress}%` } as CSSProperties

  const nextStep = assignment.steps.find((step) => !step.done)
  const allStepsDone = total > 0 && done === total
  let summary: string
  if (assignment.closed) {
    summary = `${total} ${t("taskAllStepsUnit")} · ${t("taskClosedAt")} ${relativeTime(assignment.closed_at, locale)}`
  } else if (allStepsDone) {
    summary = t("taskLayerStageDone")
  } else if (nextStep) {
    summary = nextStep.name
  } else {
    summary = t("taskNoSteps")
  }

  const adding = ctx.addingStep === assignment.id
  const canComplete = !assignment.closed && layer.unlocked

  return (
    <article className={`wb-task-card ${cardEdgeClass(item, assignment)}`}>
      <div className="wb-task-card-head">
        <ExpandToggle
          expanded={expanded}
          onToggle={() => actions.toggleExpand(assignment.id)}
          label={expanded ? t("taskHideSteps") : t("taskShowSteps")}
        />
        <span
          className={`wb-task-ring ${progress >= 100 ? "wb-task-ring--full" : ""}`}
          style={ringStyle}
          title={t("taskProgressHint")}
          aria-label={`${t("taskProgressHint")}: ${progress}%`}
        >
          <b className="wb-task-ring-value">{progress}</b>
        </span>
        <span className="wb-task-card-title">
          <InlineText
            value={item.title}
            placeholder={t("taskUntitledItem")}
            onCommit={(title) => actions.renameItem(item.id, title)}
          />
        </span>
        <CardMenu thread={thread} item={item} assignment={assignment} ctx={ctx} />
      </div>
      {(item.layer_count > 1 || assignment.closed || item.paper_id) && (
        <div className="wb-task-chips">
          {item.layer_count > 1 && (
            <span className="wb-badge wb-badge--gray wb-task-chip">
              {t("taskSpanLayers").replace("{count}", String(item.layer_count))}
            </span>
          )}
          {assignment.closed && (
            <span className="wb-badge wb-badge--green wb-task-chip">
              <Check size={12} weight="bold" aria-hidden="true" />
              {t("taskClosedHere")}
            </span>
          )}
          {item.paper_id && (
            <span className="wb-badge wb-badge--blue wb-task-chip">
              <Link size={12} weight="bold" aria-hidden="true" />
              <span className="wb-task-chip-label">
                {ctx.paperTitles.get(item.paper_id) ?? t("taskUntitledPaperLink")}
              </span>
            </span>
          )}
        </div>
      )}
      <div className="wb-task-card-meta">
        {shortDate(assignment.start_date)} – {shortDate(assignment.due_date)} · {done}/{total}{" "}
        {t("taskStepUnit")}
      </div>
      {expanded ? (
        <div className="wb-task-card-body">
          <div className="wb-task-dates">
            <span className="wb-task-date-label">{t("taskStartDate")}</span>
            <DatePickerCell
              value={assignment.start_date}
              placeholder={t("pickDateTitle")}
              ariaLabel={`${t("taskStartDate")}: ${item.title}`}
              onCommit={(start_date) => actions.setDates(assignment.id, { start_date })}
            />
            <span className="wb-task-date-label">{t("deadlineLabel")}</span>
            <DatePickerCell
              value={assignment.due_date}
              placeholder={t("pickDateTitle")}
              ariaLabel={`${t("deadlineLabel")}: ${item.title}`}
              onCommit={(due_date) => actions.setDates(assignment.id, { due_date })}
            />
          </div>
          <ul className="wb-task-steps">
            {assignment.steps.map((step) => (
              <li
                key={step.id}
                className={`wb-task-step wb-stage-row ${step.done ? "wb-stage-row--done" : ""}`}
              >
                <button
                  type="button"
                  className="wb-stage-toggle"
                  title={step.done ? t("done") : t("todo")}
                  aria-label={`${t("toggleStage")}: ${step.name}`}
                  aria-pressed={step.done}
                  onClick={() => actions.toggleStep(assignment.id, step)}
                >
                  {step.done ? "✓" : ""}
                </button>
                <span className="wb-stage-name wb-task-step-name">
                  <InlineText
                    value={step.name}
                    placeholder={t("taskStepPlaceholder")}
                    onCommit={(name) => actions.renameStep(step.id, name)}
                  />
                </span>
                <span className="wb-stage-tools">
                  <button
                    type="button"
                    className="wb-icon-btn wb-icon-btn--danger"
                    title={t("delete")}
                    aria-label={`${t("delete")}: ${step.name}`}
                    onClick={() => actions.deleteStep(step.id)}
                  >
                    ✕
                  </button>
                </span>
              </li>
            ))}
            {assignment.steps.length === 0 && !adding && (
              <li className="wb-task-empty-steps">{t("taskNoSteps")}</li>
            )}
            {adding && (
              <li className="wb-task-step">
                <StepAddInput
                  placeholder={t("taskStepPlaceholder")}
                  onCommit={(name) => {
                    ctx.onAddingStep(null)
                    actions.addStep(assignment.id, name)
                  }}
                  onCancel={() => ctx.onAddingStep(null)}
                />
              </li>
            )}
          </ul>
          <div className="wb-task-card-foot">
            <button
              type="button"
              className="wb-stage-add-btn"
              onClick={() => ctx.onAddingStep(assignment.id)}
            >
              {t("taskAddStep")}
            </button>
            {canComplete && (
              <button type="button" className="wb-task-done" onClick={() => actions.complete(assignment)}>
                <Check size={13} weight="bold" aria-hidden="true" />
                {t("taskComplete")}
              </button>
            )}
          </div>
          <InlineText
            className="wb-task-notes"
            value={item.notes}
            placeholder={t("colNotes")}
            multiline
            onCommit={(notes) => actions.saveNotes(item.id, notes)}
          />
        </div>
      ) : (
        <div className="wb-task-card-foot">
          <div
            className={`wb-task-summary ${nextStep && !assignment.closed && !allStepsDone ? "wb-task-next" : ""}`}
          >
            {nextStep && !assignment.closed && !allStepsDone ? `→ ${summary}` : summary}
          </div>
          {canComplete && (
            <button type="button" className="wb-task-done" onClick={() => actions.complete(assignment)}>
              <Check size={13} weight="bold" aria-hidden="true" />
              {t("taskComplete")}
            </button>
          )}
        </div>
      )}
    </article>
  )
}

// ── 一块层 ─────────────────────────────────────────────────────────────
function LayerBlock(props: {
  thread: TaskThread
  layer: TaskLayer
  index: number
  ctx: CardContext
}) {
  const { t } = useTranslation()
  const { layer, thread, index, ctx } = props
  const cards = layerCards(layer, thread.items)
  const total = layer.item_count
  const closed = layer.closed_count
  // 进度条那根的长度只是把服务端给的两个计数画出来，不产出任何新口径。
  const percent = total > 0 ? Math.round((closed / total) * 100) : 0
  const fullyClosed = layerFullyClosed(layer)

  const stateClass = fullyClosed
    ? "wb-badge--green"
    : layer.unlocked
      ? "wb-badge--blue"
      : "wb-badge--gray"
  const stateLabel = fullyClosed
    ? t("taskLayerStateClosed")
    : layer.unlocked
      ? t("taskLayerStateActive")
      : t("taskLayerStateLocked")

  const entries: MenuEntry[] = [
    {
      key: "close",
      label: t("taskCloseLayer"),
      Icon: Check,
      disabled: fullyClosed,
      onSelect: () => ctx.actions.closeLayer(layer.id),
    },
    {
      key: "add",
      label: t("taskNewInLayer"),
      Icon: Plus,
      onSelect: () => ctx.actions.addItem(thread.id, layer.id),
    },
  ]
  if (layer.forced_unlock) {
    entries.push({
      key: "unforce",
      label: t("taskCancelForceUnlock"),
      Icon: LockSimple,
      onSelect: () => ctx.actions.forceUnlock(layer.id, false),
    })
  } else if (!layer.unlocked) {
    entries.push({
      key: "force",
      label: t("taskForceUnlock"),
      Icon: LockSimpleOpen,
      onSelect: () => ctx.actions.forceUnlock(layer.id, true),
    })
  }
  entries.push({
    key: "delete",
    label: t("delete"),
    Icon: Trash,
    danger: true,
    onSelect: () => ctx.actions.deleteLayer(layer.id),
  })

  return (
    <div className="wb-task-layer">
      <div className="wb-task-layer-head">
        <span className="wb-task-layer-badge" aria-hidden="true">
          {index + 1}
        </span>
        <span className="wb-task-layer-name">
          <InlineText
            value={layer.name}
            placeholder={t("taskUntitledLayer")}
            onCommit={(name) => ctx.actions.renameLayer(layer.id, name)}
          />
        </span>
        <span className={`wb-badge wb-task-layer-state ${stateClass}`}>
          {fullyClosed ? (
            <Check size={12} weight="bold" aria-hidden="true" />
          ) : layer.unlocked ? null : (
            <LockSimple size={12} aria-hidden="true" />
          )}
          {stateLabel}
        </span>
        <div className="wb-task-layer-progress">
          <div className="wb-progress-track" aria-hidden="true">
            <div className="wb-progress-fill" style={{ width: `${percent}%` }} />
          </div>
          <span className="wb-task-layer-count">
            {closed}/{total}
          </span>
        </div>
        <TaskMenu label={t("taskLayerMenu")} entries={entries} />
      </div>
      <div className="wb-task-cards">
        {cards.map(({ item, assignment }) => (
          <TaskCard
            key={assignment.id}
            thread={thread}
            layer={layer}
            item={item}
            assignment={assignment}
            ctx={ctx}
          />
        ))}
        <button
          type="button"
          className="wb-task-add-slot"
          onClick={() => ctx.actions.addItem(thread.id, layer.id)}
        >
          <Plus size={14} weight="bold" aria-hidden="true" />
          {t("taskNewInLayer")}
        </button>
      </div>
    </div>
  )
}

// ── 一条主线：统计 → 抬头 → 逐层块（中间夹解锁泡） ──────────────────────
function StatStrip(props: { stats: TaskStats }) {
  const { t } = useTranslation()
  const cells: Array<{ key: string; label: string; value: number; tone?: "green" | "warning" }> = [
    { key: "active", label: t("taskStatActive"), value: props.stats.active },
    { key: "locked", label: t("taskStatLocked"), value: props.stats.locked },
    { key: "unstarted", label: t("taskStatUnstarted"), value: props.stats.unstarted },
    { key: "overdue", label: t("taskStatOverdue"), value: props.stats.overdue, tone: "warning" },
    { key: "closed", label: t("taskStatClosed"), value: props.stats.closed, tone: "green" },
  ]
  return (
    <div className="wb-task-stats">
      {cells.map((cell) => (
        <div
          key={cell.key}
          className={`wb-task-stat ${cell.value > 0 && cell.tone ? `wb-task-stat--${cell.tone}` : ""}`}
        >
          <b className="wb-task-stat-number">{cell.value}</b>
          <span className="wb-task-stat-label">{cell.label}</span>
        </div>
      ))}
    </div>
  )
}

function ThreadSection(props: { thread: TaskThread; ctx: CardContext }) {
  const { t } = useTranslation()
  const { thread, ctx } = props
  const layers = sortedLayers(thread.layers)

  const entries: MenuEntry[] = [
    { key: "layer", label: t("taskAddLayer"), Icon: Plus, onSelect: () => ctx.actions.addLayer(thread.id) },
    {
      key: "archive",
      label: thread.archived ? t("taskUnarchive") : t("taskArchive"),
      Icon: Archive,
      onSelect: () => ctx.actions.archiveThread(thread.id, !thread.archived),
    },
    {
      key: "delete",
      label: t("delete"),
      Icon: Trash,
      danger: true,
      onSelect: () => ctx.actions.deleteThread(thread.id),
    },
  ]

  return (
    <section className="wb-task-thread">
      <StatStrip stats={thread.stats} />
      <div className="wb-task-thread-head">
        <span className="wb-task-thread-dot" aria-hidden="true" />
        <span className="wb-task-thread-name">
          <InlineText
            value={thread.name}
            placeholder={t("taskUntitledThread")}
            onCommit={(name) => ctx.actions.renameThread(thread.id, name)}
          />
        </span>
        <span className="wb-task-slots" title={t("taskSlotsHint")}>
          {thread.stats.slots_closed}/{thread.stats.slots}
        </span>
        {thread.archived && (
          <span className="wb-badge wb-badge--gray wb-task-chip">{t("taskArchivedChip")}</span>
        )}
        <div className="wb-task-layer-pills">
          {layers.map((layer, index) => {
            const fullyClosed = layerFullyClosed(layer)
            const pillClass = fullyClosed
              ? "wb-badge--green"
              : layer.unlocked
                ? "wb-badge--blue"
                : "wb-badge--gray"
            return (
              <span key={layer.id} className={`wb-badge wb-task-pill ${pillClass}`}>
                {fullyClosed ? (
                  <Check size={12} weight="bold" aria-hidden="true" />
                ) : layer.unlocked ? (
                  <b className="wb-task-pill-count">L{index + 1}</b>
                ) : (
                  <LockSimple size={12} aria-hidden="true" />
                )}
                {layer.name.trim() || t("taskUntitledLayer")}{" "}
                <b className="wb-task-pill-count">
                  {layer.closed_count}/{layer.item_count}
                </b>
              </span>
            )
          })}
        </div>
        <div className="wb-task-head-actions">
          <button type="button" className="wb-btn" onClick={() => ctx.actions.addItem(thread.id)}>
            <Plus size={14} weight="bold" aria-hidden="true" />
            {t("taskAddTask")}
          </button>
          <TaskMenu label={t("taskThreadMenu")} entries={entries} />
        </div>
      </div>
      {layers.map((layer, index) => {
        // 泡只在"上面那一层是靠收口解开这一层"的时候出现：这一层 unlocked 且上一层
        // 真的全部收口了。强制解锁不涂绿泡——那是绕过闸，不是闸自己开的。
        const previous = index > 0 ? layers[index - 1] : undefined
        const unlockedByPrevious =
          previous !== undefined && layer.unlocked && layerFullyClosed(previous)
        return (
          <Fragment key={layer.id}>
            {unlockedByPrevious && (
              <div className="wb-task-unlock">
                <span className="wb-badge wb-badge--green wb-task-unlock-pill">
                  <LockSimpleOpen size={12} aria-hidden="true" />
                  {t("taskUnlockPill")}
                </span>
              </div>
            )}
            <LayerBlock thread={thread} layer={layer} index={index} ctx={ctx} />
          </Fragment>
        )
      })}
      <button type="button" className="wb-task-add-slot" onClick={() => ctx.actions.addLayer(thread.id)}>
        <Plus size={14} weight="bold" aria-hidden="true" />
        {t("taskAddLayer")}
      </button>
    </section>
  )
}

// ── 长任务板本体 ───────────────────────────────────────────────────────
export function TaskBoard(props: { offline?: boolean }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const detected = useOnlineState()
  // Workbench 已经把在线状态算好传下来；单独挂这块板子（测试、以后的别的入口）
  // 时用它自己的探测。
  const online = props.offline === undefined ? detected : !props.offline

  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [addingStep, setAddingStep] = useState<string | null>(null)
  const [pane, setPane] = useState<CardPane | null>(null)
  const [showArchived, setShowArchived] = useState(false)
  const [confirm, setConfirm] = useState<{ message: string; action: () => void } | null>(null)

  const treeQuery = useQuery({
    queryKey: TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listTaskThreads(signal),
  })

  const paperResults = useQueries({
    queries: PAPER_KINDS.map((kind) => ({
      queryKey: ["research", kind],
      queryFn: ({ signal }: { signal: AbortSignal }) => listResearchPapers(kind, signal),
    })),
  })
  // 与 Workbench 同一套"逐条 memo"写法：`?? []` 每次渲染都是新数组，
  // 会让下面那张 Map 每帧重建。
  const researchItems = paperResults[0]?.data?.items
  const submittedItems = paperResults[1]?.data?.items
  const publishedItems = paperResults[2]?.data?.items
  const allPapers = useMemo<ResearchPaper[]>(
    () => [...(researchItems ?? []), ...(submittedItems ?? []), ...(publishedItems ?? [])],
    [researchItems, submittedItems, publishedItems],
  )
  const paperTitles = useMemo(() => {
    const map = new Map<string, string>()
    for (const paper of allPapers) {
      if (!map.has(paper.id)) map.set(paper.id, paper.title)
    }
    return map
  }, [allPapers])

  const invalidateTree = () =>
    void queryClient.invalidateQueries({ queryKey: TASK_QUERY_KEY })

  // 和 Workbench 同一条规矩：这里没有离线发件箱，写必须立刻到服务端。
  const requireOnline = (): boolean => {
    if (online) return true
    toast(t("workbenchOfflineHint"))
    return false
  }

  const writeMutation = useMutation({
    mutationFn: (input: TaskWrite) => input.run(),
    onSuccess: invalidateTree,
    onError: (_error, input) => toast(t(input.failKey)),
  })
  const write = (run: () => Promise<unknown>, failKey: string) => {
    if (!requireOnline()) return
    writeMutation.mutate({ run, failKey })
  }

  // 唯一的乐观路径：勾一步。本地先翻这一颗的 done，失败就整棵树快照恢复。
  const stepMutation = useMutation({
    mutationFn: (vars: { assignmentID: string; stepID: string; done: boolean }) =>
      updateTaskStep(vars.stepID, { done: vars.done }),
    onMutate: async (vars) => {
      await queryClient.cancelQueries({ queryKey: TASK_QUERY_KEY })
      const prev = queryClient.getQueryData<TaskTree>(TASK_QUERY_KEY)
      queryClient.setQueryData<TaskTree>(TASK_QUERY_KEY, (old) =>
        old ? patchStep(old, vars) : old,
      )
      return { prev }
    },
    onError: (_error, _vars, context) => {
      if (context) queryClient.setQueryData(TASK_QUERY_KEY, context.prev)
      toast(t("taskStepFailed"))
    },
    onSuccess: invalidateTree,
  })

  const requestConfirm = (message: string, action: () => void) => setConfirm({ message, action })

  const actions: TaskActions = {
    addThread: () => write(() => createTaskThread({ name: t("taskUntitledThread") }), "taskCreateFailed"),
    renameThread: (threadID, name) =>
      write(() => updateTaskThread(threadID, { name }), "taskUpdateFailed"),
    archiveThread: (threadID, archived) =>
      write(() => updateTaskThread(threadID, { archived }), "taskUpdateFailed"),
    deleteThread: (threadID) =>
      requestConfirm(t("taskDeleteThreadConfirm"), () =>
        write(() => deleteTaskThread(threadID), "taskDeleteFailed"),
      ),
    addLayer: (threadID) => write(() => createTaskLayer(threadID), "taskCreateFailed"),
    renameLayer: (layerID, name) =>
      write(() => updateTaskLayer(layerID, { name }), "taskUpdateFailed"),
    closeLayer: (layerID) => write(() => closeTaskLayer(layerID), "taskGateFailed"),
    forceUnlock: (layerID, forced) =>
      write(() => updateTaskLayer(layerID, { forced_unlock: forced }), "taskGateFailed"),
    deleteLayer: (layerID) =>
      requestConfirm(t("taskDeleteLayerConfirm"), () =>
        write(() => deleteTaskLayer(layerID), "taskDeleteFailed"),
      ),
    addItem: (threadID, layerID) =>
      write(
        () => createTaskItem(threadID, layerID === undefined ? {} : { layer_id: layerID }),
        "taskCreateFailed",
      ),
    renameItem: (itemID, title) => write(() => updateTaskItem(itemID, { title }), "taskUpdateFailed"),
    saveNotes: (itemID, notes) => write(() => updateTaskItem(itemID, { notes }), "taskUpdateFailed"),
    // 取消关联走显式 null：省略字段是"别碰它"，只有 null 才是"解开"。
    linkPaper: (itemID, paperID) =>
      write(() => updateTaskItem(itemID, { paper_id: paperID }), "taskUpdateFailed"),
    deleteItem: (itemID) =>
      requestConfirm(t("taskDeleteTaskConfirm"), () =>
        write(() => deleteTaskItem(itemID), "taskDeleteFailed"),
      ),
    moveItem: (thread, itemID, delta) => {
      const ids = [...thread.items].sort(byPosition).map((item) => item.id)
      const from = ids.indexOf(itemID)
      const to = from + delta
      if (from < 0 || to < 0 || to >= ids.length) return
      const next = [...ids]
      const [moved] = next.splice(from, 1)
      if (moved === undefined) return
      next.splice(to, 0, moved)
      write(() => reorderTaskItems(thread.id, next), "taskUpdateFailed")
    },
    addToLayer: (itemID, layerID) =>
      write(() => createTaskAssignment(itemID, { layer_id: layerID }), "taskCreateFailed"),
    removeFromLayer: (assignment) =>
      write(() => deleteTaskAssignment(assignment.id), "taskDeleteFailed"),
    setDates: (assignmentID, patch) =>
      write(() => updateTaskAssignment(assignmentID, patch), "taskUpdateFailed"),
    complete: (assignment) =>
      write(() => completeTaskAssignment(assignment.id), "taskGateFailed"),
    toggleExpand: (assignmentID) =>
      setExpanded((current) => {
        const next = new Set(current)
        if (next.has(assignmentID)) next.delete(assignmentID)
        else next.add(assignmentID)
        return next
      }),
    expanded: (assignmentID) => expanded.has(assignmentID),
    addStep: (assignmentID, name) =>
      write(() => createTaskStep(assignmentID, name), "taskCreateFailed"),
    renameStep: (stepID, name) =>
      write(() => updateTaskStep(stepID, { name }), "taskUpdateFailed"),
    toggleStep: (assignmentID, step) => {
      if (!requireOnline()) return
      stepMutation.mutate({ assignmentID, stepID: step.id, done: !step.done })
    },
    deleteStep: (stepID) =>
      requestConfirm(t("taskDeleteStepConfirm"), () =>
        write(() => deleteTaskStep(stepID), "taskDeleteFailed"),
      ),
  }

  const ctx: CardContext = {
    paperTitles,
    actions,
    pane,
    onPane: setPane,
    addingStep,
    onAddingStep: setAddingStep,
  }

  // 逐条 memo，与 Workbench 那三张论文表同一写法：`?? []` 每次渲染都是新数组，
  // 会让下面两个 filter 每帧重跑（exhaustive-deps 也会就此报警）。
  const treeItems = treeQuery.data?.items
  const threads = useMemo(() => treeItems ?? [], [treeItems])
  const visible = useMemo(
    () => threads.filter((thread) => !thread.archived).sort(byPosition),
    [threads],
  )
  const archived = useMemo(
    () => threads.filter((thread) => thread.archived).sort(byPosition),
    [threads],
  )

  let body: ReactNode
  if (treeQuery.isError) {
    body = (
      <div className="wb-empty" role="alert">
        <p>{t("taskLoadFailed")}</p>
        <button type="button" className="wb-btn" onClick={() => void treeQuery.refetch()}>
          {t("retry")}
        </button>
      </div>
    )
  } else if (treeQuery.isPending) {
    body = <div className="wb-empty">{t("loading")}</div>
  } else if (visible.length === 0 && archived.length === 0) {
    body = (
      <EmptyState
        title={t("taskEmptyTitle")}
        hint={t("taskEmptyHint")}
        actionLabel={t("taskNewThread")}
        onAction={actions.addThread}
      />
    )
  } else {
    body = (
      <>
        {visible.map((thread) => (
          <ThreadSection key={thread.id} thread={thread} ctx={ctx} />
        ))}
        {showArchived &&
          archived.map((thread) => <ThreadSection key={thread.id} thread={thread} ctx={ctx} />)}
      </>
    )
  }

  return (
    <div className="wb-task-board">
      {body}
      {(visible.length > 0 || (showArchived && archived.length > 0)) && (
        <footer className="wb-task-foot">
          <span className="wb-task-gate">
            <LockSimple size={13} aria-hidden="true" />
            {t("taskGateFooter")}
          </span>
          <button type="button" className="wb-btn" onClick={actions.addThread}>
            <Plus size={14} weight="bold" aria-hidden="true" />
            {t("taskNewThread")}
          </button>
          {archived.length > 0 && (
            <button
              type="button"
              className="wb-task-archived-toggle"
              aria-expanded={showArchived}
              onClick={() => setShowArchived((current) => !current)}
            >
              {t("taskArchivedToggle")}{" "}
              <b className="wb-task-archived-count">{archived.length}</b>
            </button>
          )}
        </footer>
      )}
      <ConfirmDialog
        open={confirm !== null}
        message={confirm?.message ?? ""}
        onOpenChange={(open) => {
          if (!open) setConfirm(null)
        }}
        onConfirm={() => {
          const action = confirm?.action
          setConfirm(null)
          action?.()
        }}
      />
    </div>
  )
}
