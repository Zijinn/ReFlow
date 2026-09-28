import { useRef, useState } from "react"

import type { ResearchStage } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import {
  addStageAt,
  computeProgress,
  deleteStageAt,
  MAX_STAGE_LEVEL,
  moveStageWithinParent,
  renameStageAt,
  toggleStageAt,
} from "../../lib/research"
import { toast } from "../../store/toast"
import { ConfirmDialog } from "../ConfirmDialog"
import { InlineText } from "./shared"

// pathKey identifies a node (or the root when empty) across renders.
function pathKey(path: number[]): string {
  return path.join("/")
}

function StageAddInput(props: {
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
      onChange={(e) => setValue(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault()
          commit()
        }
        if (e.key === "Escape") {
          e.preventDefault()
          props.onCancel()
        }
      }}
    />
  )
}

export function StageTree(props: {
  stages: ResearchStage[]
  onChange: (stages: ResearchStage[]) => void
}) {
  const { t } = useTranslation()
  const progress = computeProgress(props.stages)
  const dragPath = useRef<number[] | null>(null)
  const [addingPath, setAddingPath] = useState<string | null>(null)
  const [confirmPath, setConfirmPath] = useState<number[] | null>(null)
  const [dropTarget, setDropTarget] = useState<{ key: string; before: boolean } | null>(null)
  const [dragParentKey, setDragParentKey] = useState<string | null>(null)

  const requestAdd = (path: number[]) => {
    if (path.length >= MAX_STAGE_LEVEL) {
      toast(t("stageMaxLevel"))
      return
    }
    setAddingPath(pathKey(path))
  }
  const commitAdd = (path: number[], name: string) => {
    setAddingPath(null)
    props.onChange(addStageAt(props.stages, path, name))
  }
  const rename = (path: number[], name: string) => {
    if (!name) return
    props.onChange(renameStageAt(props.stages, path, name))
  }

  const renderNode = (node: ResearchStage, path: number[], level: number) => (
    <div className={`wb-stage-node wb-stage-level-${Math.min(level, MAX_STAGE_LEVEL)}`} key={path.join("-")}>
      <div
        className={[
          "wb-stage-row",
          node.done ? "wb-stage-row--done" : "",
          dropTarget &&
          dropTarget.key === pathKey(path) &&
          dragParentKey !== null &&
          dragParentKey === path.slice(0, -1).join("/")
            ? dropTarget.before
              ? "wb-stage-row--drop-before"
              : "wb-stage-row--drop-after"
            : "",
        ]
          .filter(Boolean)
          .join(" ")}
        draggable
        onDragStart={(e) => {
          dragPath.current = path
          setDragParentKey(path.slice(0, -1).join("/"))
          e.stopPropagation()
          e.dataTransfer.effectAllowed = "move"
          e.dataTransfer.setData("text/wb-stage", "1")
        }}
        onDragEnd={() => {
          dragPath.current = null
          setDragParentKey(null)
          setDropTarget(null)
        }}
        onDragOver={(e) => {
          if (!dragPath.current) return
          e.preventDefault()
          e.stopPropagation()
          const rect = e.currentTarget.getBoundingClientRect()
          const before = e.clientY < rect.top + rect.height / 2
          const key = pathKey(path)
          setDropTarget((prev) =>
            prev && prev.key === key && prev.before === before ? prev : { key, before },
          )
        }}
        onDragLeave={(e) => {
          e.stopPropagation()
          const key = pathKey(path)
          setDropTarget((prev) => (prev && prev.key === key ? null : prev))
        }}
        onDrop={(e) => {
          const from = dragPath.current
          dragPath.current = null
          setDropTarget(null)
          setDragParentKey(null)
          if (!from) return
          e.preventDefault()
          e.stopPropagation()
          const rect = e.currentTarget.getBoundingClientRect()
          const before = e.clientY < rect.top + rect.height / 2
          props.onChange(moveStageWithinParent(props.stages, from, path, before))
        }}
      >
        <span className="wb-stage-handle" title={t("stageDragHint")} aria-hidden="true">
          ≣
        </span>
        <button
          type="button"
          className="wb-stage-toggle"
          title={node.done ? t("done") : t("todo")}
          aria-label={`${t("toggleStage")}: ${node.name}`}
          aria-pressed={node.done}
          onClick={() => props.onChange(toggleStageAt(props.stages, path))}
        >
          {node.done ? "✓" : ""}
        </button>
        <span className="wb-stage-name">
          <InlineText
            value={node.name}
            placeholder={t("stageNamePlaceholder")}
            onCommit={(name) => rename(path, name)}
          />
        </span>
        <span className="wb-badge wb-stage-state">{node.done ? t("done") : t("todo")}</span>
        <span className="wb-stage-tools">
          {level < MAX_STAGE_LEVEL && (
            <button
              type="button"
              className="wb-icon-btn"
              title={t("stageAddChild")}
              aria-label={`${t("stageAddChild")}: ${node.name}`}
              onClick={() => requestAdd(path)}
            >
              ＋
            </button>
          )}
          <button
            type="button"
            className="wb-icon-btn wb-icon-btn--danger"
            title={t("delete")}
            aria-label={`${t("delete")}: ${node.name}`}
            onClick={() => setConfirmPath(path)}
          >
            ✕
          </button>
        </span>
      </div>
      {addingPath === pathKey(path) && (
        <StageAddInput
          placeholder={t("stageNamePlaceholder")}
          onCommit={(name) => commitAdd(path, name)}
          onCancel={() => setAddingPath(null)}
        />
      )}
      {node.children && node.children.length > 0 && (
        <div className="wb-stage-children">
          {node.children.map((child, index) => renderNode(child, path.concat(index), level + 1))}
        </div>
      )}
    </div>
  )

  return (
    <div>
      <div className="wb-progress-row">
        <span className="wb-muted">{t("stageProgress")}</span>
        <b>{progress}%</b>
      </div>
      <div className="wb-progress-track">
        <div className="wb-progress-fill" style={{ width: `${progress}%` }} />
      </div>
      <div className="wb-stage-tree">
        {props.stages.map((stage, index) => renderNode(stage, [index], 1))}
        {addingPath === pathKey([]) && (
          <StageAddInput
            placeholder={t("stageNamePlaceholder")}
            onCommit={(name) => commitAdd([], name)}
            onCancel={() => setAddingPath(null)}
          />
        )}
        <button type="button" className="wb-stage-add-btn" onClick={() => requestAdd([])}>
          {t("addTopStage")}
        </button>
      </div>
      <ConfirmDialog
        open={confirmPath !== null}
        message={t("deleteStageConfirm")}
        onOpenChange={(open) => {
          if (!open) setConfirmPath(null)
        }}
        onConfirm={() => {
          const path = confirmPath
          setConfirmPath(null)
          if (path) props.onChange(deleteStageAt(props.stages, path))
        }}
      />
    </div>
  )
}
