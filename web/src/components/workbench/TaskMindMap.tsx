import { useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowRight, Plus, TreeStructure } from "@phosphor-icons/react"
import { useMemo, useState } from "react"

import {
  createTaskAssignment,
  createTaskItem,
  createTaskLayer,
  createTaskStep,
  createTaskThread,
  listTaskThreads,
  updateTaskStep,
  updateTaskThread,
} from "../../api/client"
import { useTranslation } from "../../lib/i18n"
import { useOnlineState } from "../../lib/online"
import { toast } from "../../store/toast"
import { TASK_QUERY_KEY } from "./taskQueryKeys"
import {
  parseTaskOutline,
  taskThreadToMarkdown,
  type TaskOutline,
} from "../../lib/taskOutline"

const DEFAULT_MARKDOWN = `# 新的长期主线
## 第一层
- 先写下一个任务
  - 再写它的第一步
## 第二层
- 这一层会在上一层收口后解锁
`

export function TaskMindMap(props: { offline?: boolean; onImported: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const detected = useOnlineState()
  const online = props.offline === undefined ? detected : !props.offline
  const [markdown, setMarkdown] = useState(DEFAULT_MARKDOWN)
  const [selectedThread, setSelectedThread] = useState("")
  const [importing, setImporting] = useState(false)

  const threadsQuery = useQuery({
    queryKey: TASK_QUERY_KEY,
    queryFn: ({ signal }: { signal: AbortSignal }) => listTaskThreads(signal),
  })
  const threads = useMemo(() => threadsQuery.data?.items ?? [], [threadsQuery.data?.items])
  const parsed = useMemo(() => parseTaskOutline(markdown), [markdown])

  const loadThread = (threadID: string) => {
    setSelectedThread(threadID)
    if (!threadID) {
      setMarkdown(DEFAULT_MARKDOWN)
      return
    }
    const thread = threads.find((entry) => entry.id === threadID)
    if (thread) setMarkdown(taskThreadToMarkdown(thread))
  }

  const importOutline = async () => {
    if (!online) {
      toast(t("workbenchOfflineHint"))
      return
    }
    if (!parsed.ok) {
      toast(t("taskMindInvalid"))
      return
    }
    setImporting(true)
    try {
      const outline = parsed.outline
      const thread = await createTaskThread({ name: outline.thread })
      await updateTaskThread(thread.id, { map_markdown: markdown.trim() })
      for (const layer of outline.layers) {
        const createdLayer = await createTaskLayer(thread.id, layer.title)
        for (const task of layer.tasks) {
          const item = await createTaskItem(thread.id, { title: task.title })
          const assignment = await createTaskAssignment(item.id, { layer_id: createdLayer.id })
          for (const step of task.steps) {
            const createdStep = await createTaskStep(assignment.id, step.name)
            if (step.done) await updateTaskStep(createdStep.id, { done: true })
          }
        }
      }
      await queryClient.invalidateQueries({ queryKey: TASK_QUERY_KEY })
      toast(t("taskMindImported"))
      setSelectedThread(thread.id)
      props.onImported()
    } catch {
      toast(t("taskMindImportFailed"))
    } finally {
      setImporting(false)
    }
  }

  return (
    <div className="wb-tm-mind">
      <div className="wb-tm-mind-top">
        <div>
          <h2>{t("taskMindTitle")}</h2>
          <p>{t("taskMindHint")}</p>
        </div>
        <div className="wb-tm-mind-actions">
          <select
            className="select-input"
            value={selectedThread}
            aria-label={t("taskMindLoad")}
            onChange={(event) => loadThread(event.target.value)}
          >
            <option value="">{t("taskMindNew")}</option>
            {threads.map((thread) => (
              <option key={thread.id} value={thread.id}>
                {thread.name}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="wb-btn wb-btn--primary"
            disabled={!parsed.ok || importing}
            onClick={() => void importOutline()}
          >
            {importing ? (
              t("taskMindImporting")
            ) : (
              <>
                <Plus size={14} weight="bold" aria-hidden="true" />
                {t("taskMindImport")}
              </>
            )}
          </button>
        </div>
      </div>

      <div className="wb-tm-mind-split">
        <section className="wb-tm-mind-editor" aria-label={t("taskMindSource")}>
          <div className="wb-tm-mind-pane-head">
            <span>{t("taskMindSource")}</span>
            <small>Markdown</small>
          </div>
          <textarea
            className="wb-tm-mind-textarea"
            value={markdown}
            spellCheck={false}
            onChange={(event) => setMarkdown(event.target.value)}
            aria-label={t("taskMindSource")}
          />
          <p className="wb-tm-mind-foot">{t("taskMindExistingHint")}</p>
        </section>

        <section className="wb-tm-mind-preview" aria-label={t("taskMindPreview")}>
          <div className="wb-tm-mind-pane-head">
            <span>
              <TreeStructure size={14} aria-hidden="true" />
              {t("taskMindPreview")}
            </span>
            {parsed.ok && <small>{t("taskMindLayerCount").replace("{count}", String(parsed.outline.layers.length))}</small>}
          </div>
          {parsed.ok ? (
            <MindTree outline={parsed.outline} />
          ) : (
            <div className="wb-tm-mind-empty">
              <TreeStructure size={22} aria-hidden="true" />
              <p>{t("taskMindInvalid")}</p>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}

function MindTree({ outline }: { outline: TaskOutline }) {
  const { t } = useTranslation()
  return (
    <div className="wb-tm-mind-map">
      <div className="wb-tm-mind-root">
        <small>{t("taskMindRoot")}</small>
        <b>{outline.thread}</b>
        <span>{t("taskMindLayerCount").replace("{count}", String(outline.layers.length))}</span>
      </div>
      <ul className="wb-tm-mind-layers">
        {outline.layers.map((layer, layerIndex) => (
          <li
            key={`${layer.title}-${layerIndex}`}
            className={`wb-tm-mind-layer wb-tm-mind-layer--${layerIndex % 4}`}
          >
            <div className="wb-tm-mind-node wb-tm-mind-layer-node">
              <span>{layer.title}</span>
              <b>{layer.tasks.length}</b>
            </div>
            <ul className="wb-tm-mind-tasks">
              {layer.tasks.map((task, taskIndex) => (
                <li key={`${task.title}-${taskIndex}`}>
                  <div className="wb-tm-mind-node wb-tm-mind-task-node">
                    <ArrowRight size={11} weight="bold" aria-hidden="true" />
                    <span>{task.title}</span>
                  </div>
                  {task.steps.length > 0 && (
                    <div className="wb-tm-mind-steps">
                      {task.steps.map((step, stepIndex) => (
                        <span
                          key={`${step.name}-${stepIndex}`}
                          className={step.done ? "is-done" : ""}
                        >
                          {step.name}
                        </span>
                      ))}
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </div>
  )
}
