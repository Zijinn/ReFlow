export interface OutlineStep {
  name: string
  done: boolean
}

export interface OutlineTask {
  title: string
  steps: OutlineStep[]
}

export interface OutlineLayer {
  title: string
  tasks: OutlineTask[]
}

export interface TaskOutline {
  thread: string
  layers: OutlineLayer[]
}

export type TaskOutlineResult =
  | { ok: true; outline: TaskOutline }
  | { ok: false; error: string }

const LIST_ITEM = /^(\s*)(?:[-*+]|\d+\.)\s+(.*)$/
const CHECKBOX = /^\[([ xX])\]\s*(.*)$/

/**
 * Parses the mind-map's Markdown subset:
 *
 *   # 主线
 *   ## 层
 *   - 任务
 *     - 步
 *     - [x] 已完成的步
 *
 * `#` is the thread, `##` is a sequential layer, a top-level list item is a
 * task, and an indented list item under a task is a step. Deeper nesting still
 * becomes steps of the same task; the board only has one checklist level.
 */
export function parseTaskOutline(markdown: string): TaskOutlineResult {
  const outline: TaskOutline = { thread: "未命名长期任务", layers: [] }
  let threadSeen = false
  let currentLayer: OutlineLayer | null = null
  let currentTask: OutlineTask | null = null
  let sawTask = false

  for (const rawLine of markdown.replace(/\r\n?/g, "\n").split("\n")) {
    const line = rawLine.replace(/\s+$/, "")
    if (line.trim() === "") continue

    if (line.startsWith("## ")) {
      const title = line.slice(3).trim()
      if (!title) continue
      currentLayer = { title, tasks: [] }
      outline.layers.push(currentLayer)
      currentTask = null
      continue
    }
    if (line.startsWith("# ")) {
      // Only the first thread heading names the outline; deeper headings are
      // treated as prose rather than silently creating a second root.
      if (!threadSeen) {
        const title = line.slice(2).trim()
        if (title) outline.thread = title
        threadSeen = true
      }
      currentTask = null
      continue
    }
    if (line.startsWith("###")) continue

    const match = LIST_ITEM.exec(line)
    if (!match) continue
    const indent = (match[1] ?? "").replace(/\t/g, "  ").length
    let text = (match[2] ?? "").trim()
    let done = false
    const checkbox = CHECKBOX.exec(text)
    if (checkbox) {
      done = (checkbox[1] ?? "").toLowerCase() === "x"
      text = (checkbox[2] ?? "").trim()
    }
    if (!text) continue

    if (indent >= 2 && currentTask && currentLayer) {
      currentTask.steps.push({ name: text, done })
      continue
    }

    if (!currentLayer) {
      currentLayer = { title: "第一层", tasks: [] }
      outline.layers.push(currentLayer)
    }
    currentTask = { title: text, steps: [] }
    currentLayer.tasks.push(currentTask)
    sawTask = true
  }

  if (!sawTask) return { ok: false, error: "empty" }
  return { ok: true, outline }
}

/** Builds a Markdown source from the authoritative long-task tree. */
export function taskThreadToMarkdown(thread: {
  name: string
  map_markdown?: string
  layers: Array<{
    name: string
    position: number
    item_count: number
  }>
  items: Array<{
    title: string
    position: number
    assignments: Array<{ layer_position: number; steps: Array<{ name: string; done: boolean }> }>
  }>
}): string {
  if (thread.map_markdown?.trim()) return thread.map_markdown
  const lines = [`# ${thread.name}`]
  const layers = [...thread.layers].sort((left, right) => left.position - right.position)
  const items = [...thread.items].sort((left, right) => left.position - right.position)
  for (const layer of layers) {
    lines.push(`## ${layer.name}`)
    for (const item of items) {
      const assignment = item.assignments.find((entry) => entry.layer_position === layer.position)
      if (!assignment) continue
      lines.push(`- ${item.title}`)
      for (const step of assignment.steps) {
        lines.push(`  - ${step.done ? "[x] " : ""}${step.name}`)
      }
    }
  }
  return lines.join("\n")
}
