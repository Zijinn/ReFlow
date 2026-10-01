import type { ResearchPaper } from "../../api/types"

// Whitelisted free-text fields for the workbench search boxes. JSON.stringify
// over the whole paper also matched ids, timestamps and booleans, which made
// results unpredictable (and leaked stage/history internals).
export function matchesPaperQuery(paper: ResearchPaper, query: string): boolean {
  const haystack = [
    paper.title,
    paper.authors.join(" "),
    paper.keywords.join(" "),
    paper.notes,
    paper.research_area,
    paper.target_journal,
    paper.current_journal,
    paper.journal,
    paper.manuscript_id,
    paper.next_action,
    paper.doi,
  ]
    .join("\n")
    .toLowerCase()
  return haystack.includes(query)
}

export function reorderList<T extends { id: string }>(
  list: T[],
  fromID: string,
  toID: string,
  before: boolean,
): T[] {
  const from = list.findIndex((item) => item.id === fromID)
  let to = list.findIndex((item) => item.id === toID)
  if (from < 0 || to < 0 || from === to) return list
  const next = list.slice()
  const [node] = next.splice(from, 1)
  if (from < to) to -= 1
  next.splice(before ? to : to + 1, 0, node!)
  return next
}

export function displayID(kind: string, index: number): string {
  const prefix = kind === "research" ? "R" : kind === "submitted" ? "S" : "P"
  return prefix + String(index + 1).padStart(3, "0")
}

// Deadlines are free text like submission_date; the calendar only trusts an
// explicit calendar date and ignores anything else (e.g. "下周三是死线").
export function parseDeadline(raw: string): Date | null {
  const match = /^\s*(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})\s*$/.exec(raw ?? "")
  if (!match) return null
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(year, month - 1, day)
  if (date.getFullYear() !== year || date.getMonth() !== month - 1 || date.getDate() !== day) {
    return null
  }
  return date
}

export function formatDeadline(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

// Keeps a typed deadline storable and calendar-readable; unparseable input is
// returned trimmed so the user sees what they wrote instead of a silent wipe.
export function normalizeDeadlineInput(raw: string): string {
  const trimmed = (raw ?? "").trim()
  const date = parseDeadline(trimmed)
  return date ? formatDeadline(date) : trimmed
}

export function daysUntil(date: Date, now: Date = new Date()): number {
  const startOf = (value: Date) =>
    new Date(value.getFullYear(), value.getMonth(), value.getDate()).getTime()
  return Math.round((startOf(date) - startOf(now)) / 86_400_000)
}

// 状态 / 优先级圆点与药丸映射：与 shared.tsx 的 MenuSelect 配合使用，
/// 避免把非组件函数放在组件文件里触发 react-refresh 告警。
export function statusDotClass(status: string): string {
  switch (status) {
    case "accepted":
      return "wb-dot--green"
    case "rejected":
      return "wb-dot--red"
    case "minor_revision":
      return "wb-dot--teal"
    case "major_revision":
      return "wb-dot--orange"
    case "under_review":
      return "wb-dot--amber"
    case "with_editor":
      return "wb-dot--violet"
    default:
      return "wb-dot--blue"
  }
}

export function statusBadgeClass(status: string): string {
  switch (status) {
    case "accepted":
      return "wb-badge--green"
    case "rejected":
      return "wb-badge--red"
    case "minor_revision":
      return "wb-badge--teal"
    case "major_revision":
      return "wb-badge--orange"
    case "under_review":
      return "wb-badge--amber"
    case "with_editor":
      return "wb-badge--violet"
    default:
      return "wb-badge--blue"
  }
}

// 标签色阶：前三档沿用旧优先级的红/琥珀/灰（迁移把 High/Medium/Average 播种为
// position 0/1/2，旧行的含义不变），第四档起循环 wb-badge 家族其余既有淡底。
// 只按调色板下标取色，不新增任何色板令牌。
const TAG_BADGE_TINTS = [
  "wb-badge--red",
  "wb-badge--amber",
  "wb-badge--gray",
  "wb-badge--green",
  "wb-badge--teal",
  "wb-badge--orange",
  "wb-badge--violet",
  "wb-badge--blue",
]

const TAG_DOT_TINTS = [
  "wb-dot--red",
  "wb-dot--amber",
  "wb-dot--gray",
  "wb-dot--green",
  "wb-dot--teal",
  "wb-dot--orange",
  "wb-dot--violet",
  "wb-dot--blue",
]

export function tagBadgeClass(index: number): string {
  if (index < 0) return "wb-badge--gray"
  return TAG_BADGE_TINTS[index % TAG_BADGE_TINTS.length]!
}

export function tagDotClass(index: number): string {
  if (index < 0) return "wb-dot--gray"
  return TAG_DOT_TINTS[index % TAG_DOT_TINTS.length]!
}

// 迁移播种的三个旧优先级标签按既有 i18n 键显示本地化名；其余名字原样渲染。
export function tagDisplayName(name: string, t: (key: string) => string): string {
  if (name === "High") return t("priorityHigh")
  if (name === "Medium") return t("priorityMedium")
  if (name === "Average") return t("priorityAverage")
  return name
}

// 相对时间：总览最近动态用；解析失败回退原文。
export function relativeTime(raw: string, locale: string, now: Date = new Date()): string {
  const date = new Date((raw ?? "").trim())
  if (Number.isNaN(date.getTime())) return raw || "—"
  const diffMs = now.getTime() - date.getTime()
  if (diffMs < 0) return (raw || "").slice(0, 10) || raw
  const zh = locale === "zh-CN"
  const minute = 60_000
  const hour = 3_600_000
  const day = 86_400_000
  if (diffMs < hour) {
    const minutes = Math.floor(diffMs / minute)
    if (minutes <= 0) return zh ? "刚刚" : "just now"
    return zh ? `${minutes} 分钟前` : `${minutes}m ago`
  }
  if (diffMs < day) {
    const hours = Math.floor(diffMs / hour)
    return zh ? `${hours} 小时前` : `${hours}h ago`
  }
  const days = Math.floor(diffMs / day)
  if (days < 30) return zh ? `${days} 天前` : `${days}d ago`
  return (raw || "").slice(0, 10) || raw
}
