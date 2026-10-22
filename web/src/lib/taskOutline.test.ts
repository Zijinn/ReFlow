import { describe, expect, it } from "vitest"

import { parseTaskOutline, taskThreadToMarkdown } from "./taskOutline"

describe("parseTaskOutline", () => {
  it("maps headings, tasks, steps and checkboxes", () => {
    const result = parseTaskOutline(`# 论文项目
## 第一层
- 修改正文
  - 改摘要
  - [x] 补图
- 重跑回归
## 第二层
- 投稿
`)
    expect(result.ok).toBe(true)
    if (!result.ok) return
    expect(result.outline.thread).toBe("论文项目")
    expect(result.outline.layers).toHaveLength(2)
    expect(result.outline.layers[0]?.tasks[0]).toEqual({
      title: "修改正文",
      steps: [
        { name: "改摘要", done: false },
        { name: "补图", done: true },
      ],
    })
    expect(result.outline.layers[1]?.tasks[0]?.title).toBe("投稿")
  })

  it("falls back to a default layer when tasks precede any heading", () => {
    const result = parseTaskOutline("- 一条任务")
    expect(result.ok).toBe(true)
    if (!result.ok) return
    expect(result.outline.layers[0]?.title).toBe("第一层")
    expect(result.outline.layers[0]?.tasks[0]?.title).toBe("一条任务")
  })

  it("rejects an outline without tasks", () => {
    expect(parseTaskOutline("# 只有标题").ok).toBe(false)
  })
})

describe("taskThreadToMarkdown", () => {
  it("serializes the authoritative tree when no source is stored", () => {
    const markdown = taskThreadToMarkdown({
      name: "论文项目",
      layers: [{ name: "第一层", position: 0, item_count: 1 }],
      items: [
        {
          title: "修改正文",
          position: 0,
          assignments: [{ layer_position: 0, steps: [{ name: "改摘要", done: true }] }],
        },
      ],
    })
    expect(markdown).toContain("# 论文项目")
    expect(markdown).toContain("## 第一层")
    expect(markdown).toContain("- 修改正文")
    expect(markdown).toContain("  - [x] 改摘要")
  })
})
