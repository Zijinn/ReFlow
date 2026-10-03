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

// 距离截止还有几天：没有日历日期（空串，或"下周三是死线"这类自由文本）恒排最后，
// 所以排序拿它当键位时未设值不会挤掉已设值。
export function deadlineDays(paper: ResearchPaper): number {
  const date = parseDeadline(paper.deadline)
  return date ? daysUntil(date) : Number.MAX_SAFE_INTEGER
}

// 紧急度三档（过期 / 今天 / 一周内）：类名给 .wb-deadline--* 上色，文案走 i18n 键。
// 在研与在投两张表共用同一份判定，两份日历列的"要不要提醒"必须说得一样。
export function deadlineUrgency(days: number): {
  className: string
  key: string
  count?: number
} {
  if (days < 0) return { className: "wb-deadline--overdue", key: "overdueUnit", count: -days }
  if (days === 0) return { className: "wb-deadline--today", key: "dueToday" }
  if (days <= 7) return { className: "wb-deadline--soon", key: "daysLeftUnit", count: days }
  return { className: "", key: "" }
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

// 标签色阶：前三档沿用旧优先级的红/琥珀/灰（迁移把 High/Medium/Average/Low 播种为
// position 0/1/2，旧行的含义不变），第四档起循环 wb-badge 家族其余既有淡底。
// 顺序就是后端允许的那八个名字（`research_tags.color` 只收这些值），
// 不新增任何色板令牌：色相永远从 wb-badge / wb-dot / --wb-hue-* 既有三件套里取。
export const TAG_COLOR_NAMES = [
  "red",
  "amber",
  "gray",
  "green",
  "teal",
  "orange",
  "violet",
  "blue",
] as const

export type TagColorName = (typeof TAG_COLOR_NAMES)[number]

// 存进来的颜色名要挡一道未知值：后端只认这八个，但旧缓存/别处写入的脏值不该把
// 整颗药丸渲染成 `wb-badge--<undefined>` 这种没有样式的类名。
export function isTagColorName(value: string | undefined | null): value is TagColorName {
  return typeof value === "string" && (TAG_COLOR_NAMES as readonly string[]).includes(value)
}

// 调色板下标推出的那一档：越靠前的标签越"重"，红 → 琥珀 → 灰，之后循环其余淡底。
export function tagColorName(index: number, color?: string): TagColorName {
  if (isTagColorName(color)) return color
  if (index < 0) return "gray"
  return TAG_COLOR_NAMES[index % TAG_COLOR_NAMES.length]!
}

// `painted` 是用户给这枚标签的「颜色是否启用」开关。关掉时这里三个函数都返回
// 中性：药丸不带色相修饰符（.wb-badge / .wb-tag-chip 本来就在中性墨上）、点回落到
// .wb-dot 的底色的、行染色交出 null 干脆不涂。自选的那一档颜色仍留在 tag.color 上，
// 所以开关再打开回到原样，而不是被悄悄重置成调色板默认。
export function tagBadgeClass(index: number, color?: string, painted = true): string {
  if (!painted) return ""
  return `wb-badge--${tagColorName(index, color)}`
}

export function tagDotClass(index: number, color?: string, painted = true): string {
  if (!painted) return ""
  return `wb-dot--${tagColorName(index, color)}`
}

// 行染色的色相：把色相名写成一条 var() 引用交给 CSS 自定义属性，浅深两档由
// styles.css 里那批 --wb-hue-* 令牌自己分档，所以这里不新造任何颜色。
// 返回 null 表示这一行不该被染色（开关关掉了，或者没有可染的标签）。
export function tagHueVar(index: number, color?: string, painted = true): string | null {
  if (!painted) return null
  return `var(--wb-hue-${tagColorName(index, color)})`
}

// 迁移播种的旧优先级标签按既有 i18n 键显示本地化名；其余名字原样渲染。
// 后端现在播种的是 Low（不再是 Average），两代名字都指向同一档"低优先级"。
export function tagDisplayName(name: string, t: (key: string) => string): string {
  if (name === "High") return t("priorityHigh")
  if (name === "Medium") return t("priorityMedium")
  if (name === "Average" || name === "Low") return t("priorityAverage")
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
