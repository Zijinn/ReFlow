import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarBlank, Check, Lightning, Plus, Trash, WarningCircle } from "@phosphor-icons/react"
import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react"

import {
  createQuickTask,
  deleteQuickTask,
  listQuickTasks,
  updateQuickTask,
} from "../../api/client"
import type { QuickTask } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { useOnlineState } from "../../lib/online"
import { toast } from "../../store/toast"
import type { TaskFocus } from "../../store/reader"
import { ConfirmDialog } from "../ConfirmDialog"
import { GanttDays, GanttToolbar } from "./gantt"
import { ganttBar, useGanttWindow } from "./ganttUtils"
import { DatePickerCell, EmptyState, InlineText } from "./shared"
import { QUICK_TASK_QUERY_KEY } from "./taskQueryKeys"
import "./gantt.css"
import { daysUntil, parseDeadline, taskUrgency } from "./utils"

function isOverdue(task: QuickTask): boolean {
  if (task.completed) return false
  const due = parseDeadline(task.due_date)
  return due !== null && daysUntil(due) < 0
}

type QuickTaskGroup = "overdue" | "today" | "next7" | "later" | "unscheduled"

const quickTaskGroups: QuickTaskGroup[] = ["overdue", "today", "next7", "later", "unscheduled"]

function quickTaskGroup(task: QuickTask, now: Date): QuickTaskGroup {
  const due = parseDeadline(task.due_date)
  if (!due) return "unscheduled"
  const days = daysUntil(due, now)
  if (days < 0) return "overdue"
  if (days === 0) return "today"
  if (days <= 7) return "next7"
  return "later"
}

function compareQuickTasks(left: QuickTask, right: QuickTask): number {
  const leftDue = parseDeadline(left.due_date)
  const rightDue = parseDeadline(right.due_date)
  if (leftDue && rightDue && leftDue.getTime() !== rightDue.getTime()) {
    return leftDue.getTime() - rightDue.getTime()
  }
  if (leftDue && !rightDue) return -1
  if (!leftDue && rightDue) return 1
  const leftStart = parseDeadline(left.start_date)
  const rightStart = parseDeadline(right.start_date)
  if (leftStart && rightStart && leftStart.getTime() !== rightStart.getTime()) {
    return leftStart.getTime() - rightStart.getTime()
  }
  if (leftStart && !rightStart) return -1
  if (!leftStart && rightStart) return 1
  return left.position - right.position
}

export function QuickTasks(props: {
  offline?: boolean
  onOpenTimeline?: () => void
  focus?: TaskFocus | null
  onFocusConsumed?: () => void
}) {
  const { t } = useTranslation()
  const { focus, onFocusConsumed } = props
  const queryClient = useQueryClient()
  const detected = useOnlineState()
  const online = props.offline === undefined ? detected : !props.offline
  const [title, setTitle] = useState("")
  const [startDate, setStartDate] = useState("")
  const [dueDate, setDueDate] = useState("")
  const [showCompleted, setShowCompleted] = useState(false)
  const [confirm, setConfirm] = useState<string | null>(null)
  const gantt = useGanttWindow()

  const tasksQuery = useQuery({
    queryKey: QUICK_TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listQuickTasks(signal),
  })
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: QUICK_TASK_QUERY_KEY })
  }
  const requireOnline = (): boolean => {
    if (online) return true
    toast(t("workbenchOfflineHint"))
    return false
  }

  const createMutation = useMutation({
    mutationFn: () =>
      createQuickTask({
        title: title.trim(),
        start_date: startDate,
        due_date: dueDate,
      }),
    onSuccess: () => {
      setTitle("")
      setStartDate("")
      setDueDate("")
      invalidate()
    },
    onError: () => toast(t("taskCreateFailed")),
  })
  const updateMutation = useMutation({
    mutationFn: (input: { id: string; patch: Parameters<typeof updateQuickTask>[1] }) =>
      updateQuickTask(input.id, input.patch),
    onSuccess: invalidate,
    onError: () => toast(t("taskUpdateFailed")),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteQuickTask(id),
    onSuccess: invalidate,
    onError: () => toast(t("taskDeleteFailed")),
  })

  const tasks = useMemo(() => tasksQuery.data?.items ?? [], [tasksQuery.data?.items])
  useEffect(() => {
    if (!focus || focus.kind !== "quick") return
    const row = document.querySelector(`[data-quick-task-id="${focus.id}"]`)
    if (!row) return
    row.scrollIntoView?.({ block: "center", behavior: "smooth" })
    row.classList.add("wb-row--flash")
    const timer = window.setTimeout(() => row.classList.remove("wb-row--flash"), 1600)
    onFocusConsumed?.()
    return () => window.clearTimeout(timer)
  }, [focus, onFocusConsumed, tasks])
  const open = tasks.filter((task) => !task.completed)
  const completed = tasks.filter((task) => task.completed)
  const overdueCount = open.filter(isOverdue).length
  const datedCount = tasks.filter((task) => task.start_date || task.due_date).length
  const now = new Date()
  const groupLabels: Record<QuickTaskGroup, string> = {
    overdue: t("taskQuickGroupOverdue"),
    today: t("taskQuickGroupToday"),
    next7: t("taskQuickGroupNext7"),
    later: t("taskQuickGroupLater"),
    unscheduled: t("taskQuickGroupUnscheduled"),
  }
  const groupedOpen = quickTaskGroups
    .map((key) => ({
      key,
      tasks: open.filter((task) => quickTaskGroup(task, now) === key).sort(compareQuickTasks),
    }))
    .filter((group) => group.tasks.length > 0)

  const add = () => {
    if (!title.trim() || !requireOnline()) return
    createMutation.mutate()
  }

  const update = (id: string, patch: Parameters<typeof updateQuickTask>[1]) => {
    if (!requireOnline()) return
    updateMutation.mutate({ id, patch })
  }

  let body
  if (tasksQuery.isError) {
    body = (
      <div className="wb-empty" role="alert">
        <p>{t("taskLoadFailed")}</p>
        <button type="button" className="wb-btn" onClick={() => void tasksQuery.refetch()}>
          {t("retry")}
        </button>
      </div>
    )
  } else if (tasksQuery.isPending) {
    body = <div className="wb-empty">{t("loading")}</div>
  } else if (tasks.length === 0) {
    body = (
      <EmptyState
        title={t("taskQuickEmptyTitle")}
        hint={t("taskQuickEmptyHint")}
        actionLabel={t("taskQuickAdd")}
        onAction={() => document.getElementById("wb-quick-title")?.focus()}
      />
    )
  } else {
    body = (
      <div className="wb-tm-quick-table-scroll">
        <div className="wb-tm-quick-table">
          <div className="wb-tm-quick-table-head">
            <span />
            <span>{t("taskTableTask")}</span>
            <span>{t("taskQuickTags")}</span>
            <span>{t("taskTableTime")}</span>
            <span>{t("taskQuickNotes")}</span>
            <div className="wb-tm-quick-axis" aria-label={t("taskTableAxis")}>
              <GanttDays window={gantt} />
            </div>
            <span />
          </div>
          {groupedOpen.map((group) => (
            <section
              key={group.key}
              className="wb-tm-quick-group"
              aria-labelledby={`wb-quick-group-${group.key}`}
            >
              <h3
                id={`wb-quick-group-${group.key}`}
                className={`wb-tm-quick-group-head ${
                  group.key === "overdue"
                    ? "wb-tm-quick-group-head--warn"
                    : group.key === "today" || group.key === "next7"
                      ? "wb-tm-quick-group-head--soon"
                      : group.key === "later"
                        ? "wb-tm-quick-group-head--future"
                        : ""
                }`}
              >
                <span>{groupLabels[group.key]}</span>
                <b>{group.tasks.length}</b>
              </h3>
              <ul className="wb-tm-quick-list">
                {group.tasks.map((task) => (
                  <QuickRow
                    key={task.id}
                    task={task}
                    window={gantt}
                    onUpdate={update}
                    onDelete={() => setConfirm(task.id)}
                  />
                ))}
              </ul>
            </section>
          ))}
          {completed.length > 0 && (
            <div className="wb-tm-quick-done">
              <button
                type="button"
                className="wb-tm-quick-done-toggle"
                aria-expanded={showCompleted}
                onClick={() => setShowCompleted((current) => !current)}
              >
                <Check size={13} weight="bold" aria-hidden="true" />
                {t("taskQuickCompleted")}
                <b>{completed.length}</b>
              </button>
              {showCompleted && (
                <ul className="wb-tm-quick-list">
                  {completed.map((task) => (
                    <QuickRow
                      key={task.id}
                      task={task}
                      window={gantt}
                      onUpdate={update}
                      onDelete={() => setConfirm(task.id)}
                    />
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="wb-tm-quick">
      <div className="wb-tm-quick-head">
        <div>
          <h2>{t("taskQuickTitle")}</h2>
          <p>{t("taskQuickSubtitle")}</p>
        </div>
        <div className="wb-tm-quick-meta">
          <span>{open.length} {t("taskQuickOpenUnit")}</span>
          {overdueCount > 0 && (
            <span className="wb-tm-overdue-text">
              <WarningCircle size={13} weight="fill" aria-hidden="true" />
              {overdueCount} {t("taskTimelineStatOverdue")}
            </span>
          )}
          {props.onOpenTimeline && (
            <button type="button" className="wb-btn" onClick={props.onOpenTimeline}>
              <CalendarBlank size={14} aria-hidden="true" />
              {t("taskViewTimeline")}
            </button>
          )}
        </div>
      </div>

      <form
        className="wb-tm-quick-add"
        onSubmit={(event) => {
          event.preventDefault()
          add()
        }}
      >
        <div className="wb-tm-quick-add-title">
          <Lightning size={16} weight="fill" aria-hidden="true" />
          <input
            id="wb-quick-title"
            className="text-input"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={t("taskQuickPlaceholder")}
            aria-label={t("taskQuickPlaceholder")}
          />
        </div>
        <DatePickerCell
          value={startDate}
          placeholder={t("taskStartDate")}
          ariaLabel={t("taskStartDate")}
          onCommit={setStartDate}
        />
        <DatePickerCell
          value={dueDate}
          placeholder={t("deadlineLabel")}
          ariaLabel={t("deadlineLabel")}
          onCommit={setDueDate}
        />
        <button
          type="submit"
          className="wb-btn wb-btn--primary"
          disabled={!title.trim() || createMutation.isPending}
        >
          <Plus size={14} weight="bold" aria-hidden="true" />
          {t("taskQuickAdd")}
        </button>
      </form>

      <div className="wb-tm-quick-summary">
        <span>{t("taskQuickDated").replace("{count}", String(datedCount))}</span>
        <span>{t("taskQuickHint")}</span>
      </div>
      <GanttToolbar window={gantt} />

      {body}
      <ConfirmDialog
        open={confirm !== null}
        message={t("taskQuickDeleteConfirm")}
        onOpenChange={(openState) => {
          if (!openState) setConfirm(null)
        }}
        onConfirm={() => {
          const id = confirm
          setConfirm(null)
          if (id && requireOnline()) deleteMutation.mutate(id)
        }}
      />
    </div>
  )
}

function QuickTagEditor(props: { tags: string[]; onChange: (tags: string[]) => void }) {
  const { t } = useTranslation()
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState("")
  const inputRef = useRef<HTMLInputElement | null>(null)

  useEffect(() => {
    if (adding) inputRef.current?.focus()
  }, [adding])

  const commit = () => {
    const next = [...props.tags]
    for (const raw of draft.split(/[,，]/)) {
      const tag = raw.trim()
      if (!tag || next.length >= 12) continue
      if (!next.some((entry) => entry.toLowerCase() === tag.toLowerCase())) next.push(tag)
    }
    setDraft("")
    setAdding(false)
    if (next.length !== props.tags.length) props.onChange(next)
  }

  return (
    <div className="wb-tm-quick-tags" aria-label={t("taskQuickTags")}>
      {props.tags.map((tag) => (
        <span className="wb-tm-tag" key={tag}>
          {tag}
          <button
            type="button"
            className="wb-tm-tag-remove"
            aria-label={`${t("taskQuickTagRemove")}: ${tag}`}
            title={t("taskQuickTagRemove")}
            onClick={() => props.onChange(props.tags.filter((entry) => entry !== tag))}
          >
            ×
          </button>
        </span>
      ))}
      {props.tags.length >= 12 ? null : adding ? (
        <input
          ref={inputRef}
          className="wb-tm-tag-input"
          value={draft}
          maxLength={200}
          placeholder={t("taskQuickTagPlaceholder")}
          aria-label={t("taskQuickTagPlaceholder")}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault()
              commit()
            } else if (event.key === "Escape") {
              setDraft("")
              setAdding(false)
            }
          }}
        />
      ) : (
        <button type="button" className="wb-tm-tag-add" onClick={() => setAdding(true)}>
          + {t("taskQuickTags")}
        </button>
      )}
    </div>
  )
}

function QuickRow(props: {
  task: QuickTask
  window: ReturnType<typeof useGanttWindow>
  onUpdate: (id: string, patch: Parameters<typeof updateQuickTask>[1]) => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const { task, window, onUpdate, onDelete } = props
  const urgency = task.completed ? null : taskUrgency(task.due_date)
  const bar = ganttBar(task.start_date, task.due_date, window.windowStart, window.dayCount)
  return (
    <li
      className={`wb-tm-quick-row ${task.completed ? "wb-tm-quick-row--done" : ""}`}
      data-quick-task-id={task.id}
    >
      <button
        type="button"
        className="wb-tm-check"
        aria-pressed={task.completed}
        aria-label={task.completed ? t("taskQuickReopen") : t("taskQuickComplete")}
        onClick={() => onUpdate(task.id, { completed: !task.completed })}
      >
        {task.completed ? <Check size={12} weight="bold" aria-hidden="true" /> : null}
      </button>
      <div className="wb-tm-quick-name">
        <InlineText
          value={task.title}
          placeholder={t("taskQuickPlaceholder")}
          onCommit={(value) => onUpdate(task.id, { title: value })}
        />
      </div>
      <QuickTagEditor tags={task.tags} onChange={(tags) => onUpdate(task.id, { tags })} />
      <div className="wb-tm-quick-dates" data-urgency={urgency ?? undefined}>
        <DatePickerCell
          value={task.start_date}
          placeholder={t("taskStartDate")}
          ariaLabel={`${t("taskStartDate")}: ${task.title}`}
          onCommit={(value) => onUpdate(task.id, { start_date: value })}
        />
        <span aria-hidden="true">–</span>
        <DatePickerCell
          value={task.due_date}
          placeholder={t("deadlineLabel")}
          ariaLabel={`${t("deadlineLabel")}: ${task.title}`}
          onCommit={(value) => onUpdate(task.id, { due_date: value })}
        />
      </div>
      <InlineText
        value={task.notes}
        placeholder={t("taskQuickNotes")}
        className="wb-tm-quick-notes"
        multiline
        onCommit={(value) => onUpdate(task.id, { notes: value })}
      />
      <div
        className="wb-gantt-track"
        style={{ "--gantt-days": window.dayCount } as CSSProperties}
      >
        {bar ? (
          <span
            className={`wb-gantt-bar wb-gantt-bar--${
              task.completed ? "closed" : urgency ?? "future"
            }`}
            style={{ left: `${bar.left}%`, width: `${bar.width}%` }}
            role="img"
            aria-label={`${task.start_date || "—"} – ${task.due_date || "—"}`}
          />
        ) : !task.start_date && !task.due_date ? (
          <span className="wb-gantt-empty">{t("taskTimelineUnscheduled")}</span>
        ) : null}
      </div>
      <button
        type="button"
        className="wb-icon-btn wb-icon-btn--danger"
        aria-label={t("delete")}
        title={t("delete")}
        onClick={onDelete}
      >
        <Trash size={14} aria-hidden="true" />
      </button>
    </li>
  )
}
