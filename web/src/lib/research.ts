import type { ResearchPaper, ResearchStage, SubmissionRecord } from "../api/types"

// Mirrors the server rule: a paper is English when its journal name has no CJK
// characters (falling back to the language field when the journal is empty).
const CJK = /[\u4e00-\u9fff]/

export function isEnglishPaper(paper: ResearchPaper): boolean {
  const journal = paper.journal.trim()
  if (!journal) return paper.language.trim().toLowerCase() === "en"
  return !CJK.test(journal)
}

// Strip only real DOI resolvers (doi.org / dx.doi.org / www.doi.org) so that
// unrelated URLs never lose meaningful path segments; tolerate a trailing
// "doi:" label.
export function normalizeDoi(raw: string): string {
  let value = (raw ?? "").trim()
  value = value
    .replace(/^doi:\s*/i, "")
    .replace(/^https?:\/\/(?:www\.|dx\.)?doi\.org\/(?:doi\/)?/i, "")
    .trim()
  if (!value) return ""
  // Placeholders look like "10.xxxx/suffix": the registrant part has no
  // digits at all. Match only that shape so a legitimate DOI whose suffix
  // happens to contain "xxxx" (e.g. "10.1234/xxxx_data") survives.
  const [registrant] = value.split("/")
  if (/^10\.x+$/i.test((registrant ?? "").trim())) return ""
  return value
}

// Stable enum key written to the backend for manually entered citation
// counts. Legacy rows stored the literal Chinese label; display code maps
// both forms (see citationSourceKey), and no data migration is performed.
export const MANUAL_CITATION_SOURCE = "manual"

export function citationSourceKey(source: string): "manual" | "crossref" | "none" {
  const value = (source ?? "").trim().toLowerCase()
  if (value === "crossref") return "crossref"
  if (!value) return "none"
  // "manual" plus the legacy hard-coded label written by older builds.
  return "manual"
}

// --- Stage tree ---

export function countStageLeaves(stages: ResearchStage[]): { total: number; done: number } {
  let total = 0
  let done = 0
  const walk = (nodes: ResearchStage[]) => {
    for (const node of nodes) {
      if (node.children && node.children.length) walk(node.children)
      else {
        total += 1
        if (node.done) done += 1
      }
    }
  }
  walk(stages)
  return { total, done }
}

export function computeProgress(stages: ResearchStage[]): number {
  const { total, done } = countStageLeaves(stages)
  return total ? Math.round((done / total) * 100) : 0
}

// 标准研究流程模板。新建的论文 stages 是 []，此时表格与阶段面板都按这份模板
// 展示（虚拟状态，不落库）：用户点任一阶段才把"模板 + 这一次勾选"整体 PATCH 上去。
// 这样表格里不会出现一排 0% 的空行，也不会因为打开面板就静默改数据。
// 名称走 i18n key，落库时按当前语言解析成普通字符串（阶段名本来就是用户数据）。
export const STAGE_TEMPLATE_KEYS = [
  "stageFrame",
  "stageLiterature",
  "stageDataCollect",
  "stageDataClean",
  "stageDescriptive",
  "stageEmpirical",
  "stageRobustness",
  "stageConclusion",
  "stageDraft",
  "stagePolish",
  "stageSubmit",
] as const

export function buildStageTemplate(label: (key: string) => string): ResearchStage[] {
  return STAGE_TEMPLATE_KEYS.map((key) => ({ name: label(key), done: false, children: [] }))
}

export type StageTick = { name: string; fraction: number }

// 表格里那一格分段进度轨的每一段：一个一级阶段对应一段，段内填充比例取
// 该阶段下所有叶子阶段的完成度。父阶段自己不参与计数（和 computeProgress
// 同一套口径），所以有子阶段的父阶段永远不会因为"父勾了子没勾"而显示满格。
export function stageTicks(stages: ResearchStage[]): StageTick[] {
  return stages.map((stage) => {
    const children = stage.children ?? []
    if (!children.length) return { name: stage.name, fraction: stage.done ? 1 : 0 }
    const { total, done } = countStageLeaves(children)
    return { name: stage.name, fraction: total ? done / total : stage.done ? 1 : 0 }
  })
}

export const MAX_STAGE_LEVEL = 3

export function cloneStages(stages: ResearchStage[]): ResearchStage[] {
  return stages.map((stage) => ({
    name: stage.name,
    done: stage.done,
    children: cloneStages(stage.children ?? []),
  }))
}

function nodeAtPath(stages: ResearchStage[], path: number[]): ResearchStage | null {
  let nodes = stages
  let node: ResearchStage | null = null
  for (const index of path) {
    node = nodes[index] ?? null
    if (!node) return null
    nodes = node.children ?? []
  }
  return node
}

export function toggleStageAt(stages: ResearchStage[], path: number[]): ResearchStage[] {
  const next = cloneStages(stages)
  const node = nodeAtPath(next, path)
  if (!node) return stages
  node.done = !node.done
  const cascade = (n: ResearchStage) => {
    for (const child of n.children ?? []) {
      child.done = node.done
      cascade(child)
    }
  }
  cascade(node)
  return next
}

export function addStageAt(stages: ResearchStage[], path: number[], name: string): ResearchStage[] {
  const next = cloneStages(stages)
  const stage: ResearchStage = { name: name.trim(), done: false, children: [] }
  if (path.length === 0) {
    next.push(stage)
    return next
  }
  const parent = nodeAtPath(next, path)
  if (!parent) return stages
  parent.children = parent.children ?? []
  parent.children.push(stage)
  return next
}

export function renameStageAt(stages: ResearchStage[], path: number[], name: string): ResearchStage[] {
  const next = cloneStages(stages)
  const node = nodeAtPath(next, path)
  if (!node) return stages
  node.name = name.trim()
  return next
}

export function deleteStageAt(stages: ResearchStage[], path: number[]): ResearchStage[] {
  const next = cloneStages(stages)
  const parentPath = path.slice(0, -1)
  const index = path[path.length - 1]!
  const siblings =
    parentPath.length === 0 ? next : (nodeAtPath(next, parentPath)?.children ?? [])
  siblings.splice(index, 1)
  return next
}

export function moveStageWithinParent(
  stages: ResearchStage[],
  fromPath: number[],
  toPath: number[],
  insertBefore: boolean,
): ResearchStage[] {
  if (fromPath.length !== toPath.length) return stages
  const fromParent = fromPath.slice(0, -1)
  const toParent = toPath.slice(0, -1)
  if (JSON.stringify(fromParent) !== JSON.stringify(toParent)) return stages
  const fromIndex = fromPath[fromPath.length - 1]!
  let toIndex = toPath[toPath.length - 1]!
  if (fromIndex === toIndex) return stages
  const next = cloneStages(stages)
  const siblings = fromParent.length === 0 ? next : (nodeAtPath(next, fromParent)?.children ?? [])
  const [node] = siblings.splice(fromIndex, 1)
  if (!node) return stages
  if (fromIndex < toIndex) toIndex -= 1
  siblings.splice(insertBefore ? toIndex : toIndex + 1, 0, node)
  return next
}

// --- GB/T 7714-2025 references ---

function formatEnglishAuthor(name: string): string {
  const raw = name.trim()
  if (!raw) return ""
  if (/^[^\s,]+\s+[A-Z](?:\.?\s*[A-Z]\.?)*$/.test(raw)) return raw
  const parts = raw.replace(/\./g, "").split(/\s+/).filter(Boolean)
  if (parts.length === 1) return parts[0]!
  const surname = parts[parts.length - 1]!
  const initials = parts
    .slice(0, -1)
    .map((part) => part[0]!.toUpperCase())
    .join(" ")
  return `${surname} ${initials}`
}

export function buildGb7714Reference(paper: ResearchPaper): string {
  const isEn = isEnglishPaper(paper)
  const authors = isEn
    ? paper.authors.map(formatEnglishAuthor).filter(Boolean).join(", ")
    : paper.authors.join("，")
  const title = paper.title.trim() || "未填写题名"
  const journal = paper.journal.trim() || "未填写期刊"
  const year = paper.year.trim() || "未填写年份"
  let yearVolumeIssue = year
  if (paper.volume && paper.issue) yearVolumeIssue += `, ${paper.volume}(${paper.issue})`
  else if (paper.volume) yearVolumeIssue += `, ${paper.volume}`
  else if (paper.issue) yearVolumeIssue += ` (${paper.issue})`
  let ref = `${authors}. ${title}[J]. ${journal}, ${yearVolumeIssue}`
  if (paper.pages) ref += `: ${paper.pages}`
  ref += "."
  const doi = normalizeDoi(paper.doi)
  if (doi && isEn) ref += ` DOI:${doi}.`
  return ref
}

export type ReferenceSort =
  | "year-desc"
  | "year-asc"
  | "solo-first"
  | "coauthor-first"
  | "author"

export function sortReferences(list: ResearchPaper[], mode: ReferenceSort): ResearchPaper[] {
  return [...list].sort((a, b) => {
    const yearA = Number(a.year) || 0
    const yearB = Number(b.year) || 0
    const soloA = a.authors.length === 1
    const soloB = b.authors.length === 1
    const authorA = (a.authors[0] ?? "").toLowerCase()
    const authorB = (b.authors[0] ?? "").toLowerCase()
    if (mode === "year-asc") return yearA - yearB || authorA.localeCompare(authorB, "zh")
    if (mode === "solo-first")
      return Number(!soloA) - Number(!soloB) || yearB - yearA || authorA.localeCompare(authorB, "zh")
    if (mode === "coauthor-first")
      return Number(soloA) - Number(soloB) || yearB - yearA || authorA.localeCompare(authorB, "zh")
    if (mode === "author") return authorA.localeCompare(authorB, "zh") || yearB - yearA
    return yearB - yearA || authorA.localeCompare(authorB, "zh")
  })
}

export const SUBMISSION_STATUS_OPTIONS: Array<{ value: string; key: string }> = [
  { value: "submitted", key: "statusSubmitted" },
  { value: "with_editor", key: "statusWithEditor" },
  { value: "under_review", key: "statusUnderReview" },
  { value: "minor_revision", key: "statusMinorRevision" },
  { value: "major_revision", key: "statusMajorRevision" },
  { value: "accepted", key: "statusAccepted" },
  { value: "rejected", key: "statusRejected" },
]

export function emptySubmissionRecord(): SubmissionRecord {
  return { journal: "", date: "", status: "submitted" }
}
