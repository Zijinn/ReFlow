import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarBlank, Check, Lightning, Plus, Trash, WarningCircle } from "@phosphor-icons/react"
import { useMemo, useState } from "react"

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
import { ConfirmDialog } from "../ConfirmDialog"
import { DatePickerCell, EmptyState, InlineText } from "./shared"
import { QUICK_TASK_QUERY_KEY } from "./taskQueryKeys"
import { daysUntil, parseDeadline } from "./utils"

function isOverdue(task: QuickTask): boolean {
  if (task.completed) return false
  const due = parseDeadline(task.due_date)
  return due !== null && daysUntil(due) < 0
}

export function QuickTasks(props: { offline?: boolean; onOpenTimeline?: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const detected = useOnlineState()
  const online = props.offline === undefined ? detected : !props.offline
  const [title, setTitle] = useState("")
  const [startDate, setStartDate] = useState("")
  const [dueDate, setDueDate] = useState("")
  const [showCompleted, setShowCompleted] = useState(false)
  const [confirm, setConfirm] = useState<string | null>(null)

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
  const open = tasks.filter((task) => !task.completed)
  const completed = tasks.filter((task) => task.completed)
  const overdueCount = open.filter(isOverdue).length
  const datedCount = tasks.filter((task) => task.start_date || task.due_date).length

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
      <>
        <ul className="wb-tm-quick-list">
          {open.map((task) => (
            <QuickRow
              key={task.id}
              task={task}
              onUpdate={update}
              onDelete={() => setConfirm(task.id)}
            />
          ))}
        </ul>
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
                    onUpdate={update}
                    onDelete={() => setConfirm(task.id)}
                  />
                ))}
              </ul>
            )}
          </div>
        )}
      </>
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

function QuickRow(props: {
  task: QuickTask
  onUpdate: (id: string, patch: Parameters<typeof updateQuickTask>[1]) => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const { task, onUpdate, onDelete } = props
  const overdue = isOverdue(task)
  return (
    <li className={`wb-tm-quick-row ${task.completed ? "wb-tm-quick-row--done" : ""}`}>
      <button
        type="button"
        className="wb-tm-check"
        aria-pressed={task.completed}
        aria-label={task.completed ? t("taskQuickReopen") : t("taskQuickComplete")}
        onClick={() => onUpdate(task.id, { completed: !task.completed })}
      >
        {task.completed ? <Check size={12} weight="bold" aria-hidden="true" /> : null}
      </button>
      <div className="wb-tm-quick-main">
        <InlineText
          value={task.title}
          placeholder={t("taskQuickPlaceholder")}
          onCommit={(value) => onUpdate(task.id, { title: value })}
        />
        <div className="wb-tm-quick-dates">
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
          {overdue && (
            <span className="wb-badge wb-badge--red wb-tm-quick-overdue">
              {t("taskTimelineStatOverdue")}
            </span>
          )}
        </div>
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
