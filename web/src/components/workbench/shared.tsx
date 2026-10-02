import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type ReactNode,
} from "react"

import { useTranslation } from "../../lib/i18n"
import { useReaderStore } from "../../store/reader"
import { formatDeadline, parseDeadline } from "./utils"

// InlineText renders a value that becomes an input on double-click (or Enter
// when focused) and commits on blur/Enter (Escape cancels). It mirrors the
// original dashboard's inline editing without the global event-delegation hack.
export function InlineText(props: {
  value: string
  placeholder: string
  className?: string
  multiline?: boolean
  onCommit: (value: string) => void
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(props.value)
  const inputRef = useRef<HTMLInputElement | HTMLTextAreaElement | null>(null)

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [editing])

  const startEdit = () => {
    setDraft(props.value)
    setEditing(true)
  }
  const commit = () => {
    setEditing(false)
    // Only PATCH when the trimmed draft actually differs from the stored
    // value, so a no-op blur never writes to the server.
    const next = draft.trim()
    if (next !== props.value) props.onCommit(next)
  }

  if (editing) {
    const shared = {
      ref: (node: HTMLTextAreaElement | HTMLInputElement | null) => {
        inputRef.current = node
      },
      className: "wb-inline-input",
      value: draft,
      onChange: (e: { target: { value: string } }) => setDraft(e.target.value),
      onBlur: commit,
      onKeyDown: (e: React.KeyboardEvent) => {
        if (e.key === "Enter" && !props.multiline) {
          e.preventDefault()
          commit()
        }
        if (e.key === "Escape") {
          setDraft(props.value)
          setEditing(false)
        }
      },
    }
    return props.multiline ? <textarea {...shared} rows={4} /> : <input {...shared} type="text" />
  }

  return (
    <span
      className={`wb-editable ${props.className ?? ""}`}
      title={t("editHint")}
      role="button"
      tabIndex={0}
      onDoubleClick={startEdit}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault()
          startEdit()
        }
      }}
    >
      {props.value ? props.value : <span className="wb-ph">{props.placeholder}</span>}
    </span>
  )
}

// scrollHeight 只算内容 + padding，不含描边；border-box 下把高度写成它，框子就永远
// 短一圈描边，连空备注都会"差 2px 算溢出"而冒出一根滚动条。
function borderBoxGap(node: HTMLElement): number {
  const style = window.getComputedStyle(node)
  const px = (value: string) => Number.parseFloat(value) || 0
  return px(style.borderTopWidth) + px(style.borderBottomWidth)
}

// NotesCell 是备注列的栏内编辑器：一颗自动长高的 textarea，Enter 提交（走 blur
// 这一条路，避免 keydown 与 blur 各提交一次），Shift+Enter 换行，Escape 放弃草稿。
// 长高走 scrollHeight 套路，CSS 那边用 max-height 封顶（超长备注在格内滚动，
// 不把整行顶成半屏）；jsdom 不算布局，scrollHeight 恒为 0，测试只断言提交语义。
export function NotesCell(props: {
  value: string
  ariaLabel: string
  onCommit: (notes: string) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(props.value)
  const [synced, setSynced] = useState(props.value)
  const areaRef = useRef<HTMLTextAreaElement | null>(null)

  // 父级写回（同步/撤销/别处编辑）时草稿跟随存储值：渲染期校正（React 官方
  // "adjusting state when props change" 套路），其余时间不打扰输入。
  if (props.value !== synced) {
    setSynced(props.value)
    setDraft(props.value)
  }

  useEffect(() => {
    const node = areaRef.current
    if (!node) return
    node.style.height = "auto"
    node.style.height = `${node.scrollHeight + borderBoxGap(node)}px`
  }, [draft, props.value])

  const commit = () => {
    const next = draft.trim()
    if (next !== props.value) props.onCommit(next)
  }

  return (
    <textarea
      ref={areaRef}
      className="wb-notes-input"
      rows={1}
      value={draft}
      placeholder={t("notesEditHint")}
      aria-label={props.ariaLabel}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault()
          e.currentTarget.blur()
        }
        if (e.key === "Escape") {
          setDraft(props.value)
          e.currentTarget.blur()
        }
      }}
    />
  )
}

// 月历的日期算术：一律按"当天零点"重建再加减天数，避免夏令时那 23/25 小时的
// 一天把日期算歪（同一套写法见 utils 的 parseDeadline）。
function addDays(date: Date, days: number): Date {
  const next = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  next.setDate(next.getDate() + days)
  return next
}

// 周一开头的星期序号，与 CalendarPage 的月历同一条起点。
function weekdayIndex(date: Date): number {
  return (date.getDay() + 6) % 7
}

function daysInMonth(year: number, month: number): number {
  return new Date(year, month + 1, 0).getDate()
}

// 浮层里的网格永远是 6 周：1 号前的空位用上月的尾巴补齐，尾行用下月的开头补齐。
// 行数固定，选日期时浮层才不会随月份忽高忽低。
function monthWindow(year: number, month: number): Date[][] {
  const first = new Date(year, month, 1)
  const start = addDays(first, -weekdayIndex(first))
  return Array.from({ length: 6 }, (_, week) =>
    Array.from({ length: 7 }, (_, day) => addDays(start, week * 7 + day)),
  )
}

function dayKey(date: Date): number {
  return date.getFullYear() * 10000 + (date.getMonth() + 1) * 100 + date.getDate()
}

// DatePickerCell：截止日期格。整颗格子是按钮，点开是一整张月历——把"在 98px 的
// 列宽里手打日期"换成点选，写回的仍是 utils 那一套 YYYY-MM-DD，清除写回空串。
// 浮层的测量翻转、外点关闭与焦点归还全部走 MenuSelect 那条路：格子埋在滚动的
// 表体里，普通 focus() 会让整张表跳一下。
export function DatePickerCell(props: {
  value: string
  placeholder: string
  ariaLabel: string
  onCommit: (value: string) => void
}) {
  const { t, locale } = useTranslation()
  const [open, setOpen] = useState(false)
  const [view, setView] = useState(() => {
    const base = new Date()
    return { year: base.getFullYear(), month: base.getMonth() }
  })
  // active 是"键盘此刻落在哪一天"，浮层里的焦点跟着它走；打开时由 openPicker 定位。
  const [active, setActive] = useState<Date | null>(null)
  const [placement, setPlacement] = useState({ up: false, right: false, maxHeight: 0 })
  const rootRef = useRef<HTMLDivElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const popRef = useRef<HTMLDivElement | null>(null)
  const dayRefs = useRef(new Map<number, HTMLButtonElement | null>())

  const today = new Date()
  const selected = parseDeadline(props.value)
  const weeks = monthWindow(view.year, view.month)
  // 箭头从"可见月份里的那一天"起步：翻过月之后 active 可能还留在上个月，这时取
  // 当月同一号数（月底越界就夹到该月末），键盘不会被悄悄扔回原月份。
  const origin =
    active && active.getFullYear() === view.year && active.getMonth() === view.month
      ? active
      : new Date(
          view.year,
          view.month,
          Math.min((active ?? today).getDate(), daysInMonth(view.year, view.month)),
        )

  const intlLocale = locale === "zh-CN" ? "zh-CN" : "en-US"
  const monthTitle = useMemo(
    () =>
      new Intl.DateTimeFormat(intlLocale, { year: "numeric", month: "long" }).format(
        new Date(view.year, view.month, 1),
      ),
    [intlLocale, view],
  )
  const weekdayNames = useMemo(() => {
    const formatter = new Intl.DateTimeFormat(intlLocale, { weekday: "short" })
    // 2024-01-01 是周一，所以这一串跟着网格一起周一开头。
    return Array.from({ length: 7 }, (_, index) => formatter.format(new Date(2024, 0, 1 + index)))
  }, [intlLocale])

  const reveal = (date: Date) => {
    setActive(date)
    setView({ year: date.getFullYear(), month: date.getMonth() })
  }
  const openPicker = () => {
    const base = selected ?? today
    setActive(base)
    setView({ year: base.getFullYear(), month: base.getMonth() })
    setOpen(true)
  }
  // refocus=false：点浮层外关闭时指针已经去了别处，不该把焦点抢回格子。
  const closePicker = (refocus = true) => {
    setOpen(false)
    if (refocus) buttonRef.current?.focus({ preventScroll: true })
  }
  const pick = (date: Date) => {
    const next = formatDeadline(date)
    closePicker()
    // 与 InlineText / MenuSelect 同一条规矩：值没变就不写服务器。
    if (next !== props.value) props.onCommit(next)
  }
  const clear = () => {
    closePicker()
    if (props.value) props.onCommit("")
  }
  // 翻页按钮只动月份：动了 active 就把焦点从按钮拽回顾格。
  const shiftView = (delta: number) => {
    const next = new Date(view.year, view.month + delta, 1)
    setView({ year: next.getFullYear(), month: next.getMonth() })
  }
  const shiftActive = (delta: number) => {
    const target = new Date(view.year, view.month + delta, 1)
    const year = target.getFullYear()
    const month = target.getMonth()
    reveal(new Date(year, month, Math.min(origin.getDate(), daysInMonth(year, month))))
  }

  // 格子可能被视口下缘/右缘切掉，量一次决定往上翻还是右对齐（同 MenuSelect）。
  useLayoutEffect(() => {
    if (!open) return
    const trigger = buttonRef.current
    const pop = popRef.current
    if (!trigger || !pop) return
    const rect = trigger.getBoundingClientRect()
    const gutter = 8
    const gap = 6
    const spaceBelow = window.innerHeight - rect.bottom - gap - gutter
    const spaceAbove = rect.top - gap - gutter
    const natural = pop.offsetHeight
    setPlacement({
      up: natural > spaceBelow && spaceAbove > spaceBelow,
      right: rect.left + pop.offsetWidth > window.innerWidth - gutter,
      maxHeight: Math.max(120, Math.max(spaceBelow, spaceAbove)),
    })
  }, [open])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) closePicker(false)
    }
    document.addEventListener("pointerdown", onPointerDown)
    return () => {
      document.removeEventListener("pointerdown", onPointerDown)
    }
  }, [open])

  // preventScroll 是这条交互能不能用的关键：焦点必须留在浮层里，但不能让浏览器
  // 为了露出回顾格把整张表滚走。
  useEffect(() => {
    if (!open || !active) return
    dayRefs.current.get(dayKey(active))?.focus({ preventScroll: true })
  }, [open, active])

  const onGridKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowLeft") {
      e.preventDefault()
      reveal(addDays(origin, -1))
    } else if (e.key === "ArrowRight") {
      e.preventDefault()
      reveal(addDays(origin, 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      reveal(addDays(origin, -7))
    } else if (e.key === "ArrowDown") {
      e.preventDefault()
      reveal(addDays(origin, 7))
    } else if (e.key === "Home") {
      e.preventDefault()
      reveal(addDays(origin, -weekdayIndex(origin)))
    } else if (e.key === "End") {
      e.preventDefault()
      reveal(addDays(origin, 6 - weekdayIndex(origin)))
    } else if (e.key === "PageUp") {
      e.preventDefault()
      shiftActive(-1)
    } else if (e.key === "PageDown") {
      e.preventDefault()
      shiftActive(1)
    } else if (e.key === "Enter" || e.key === " ") {
      // 原生 button 的 Enter/空格点击在 jsdom 里不会发生，这里显式选中；真实浏览器
      // 里 preventDefault 也把默认激活一起吃掉，只提交一次。
      e.preventDefault()
      pick(origin)
    }
  }

  return (
    <div ref={rootRef} className="wb-menu wb-date-cell">
      <button
        ref={buttonRef}
        type="button"
        className="wb-date-btn"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={props.ariaLabel}
        title={props.ariaLabel}
        onClick={() => (open ? closePicker(false) : openPicker())}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !open) {
            e.preventDefault()
            openPicker()
          }
        }}
      >
        <span className="wb-date-value">
          {props.value ? props.value : <span className="wb-ph">{props.placeholder}</span>}
        </span>
        <span
          className={`wb-menu-chevron wb-date-chevron ${open ? "wb-menu-chevron--open" : ""}`}
          aria-hidden="true"
        >
          ▾
        </span>
      </button>
      {open && (
        <div
          ref={popRef}
          role="dialog"
          aria-label={t("pickDateTitle")}
          className={`wb-menu-pop wb-date-pop ${placement.up ? "wb-menu-pop--up" : ""} ${
            placement.right ? "wb-menu-pop--right" : ""
          }`}
          style={placement.maxHeight ? { maxHeight: placement.maxHeight } : undefined}
          onKeyDown={(e) => {
            if (e.key === "Escape" || e.key === "Tab") {
              e.preventDefault()
              closePicker()
            }
          }}
        >
          <div className="wb-date-head">
            <button
              type="button"
              className="wb-icon-btn"
              aria-label={t("calendarPrevMonth")}
              title={t("calendarPrevMonth")}
              onClick={() => shiftView(-1)}
            >
              ‹
            </button>
            <span className="wb-date-month">{monthTitle}</span>
            <button
              type="button"
              className="wb-icon-btn"
              aria-label={t("calendarNextMonth")}
              title={t("calendarNextMonth")}
              onClick={() => shiftView(1)}
            >
              ›
            </button>
          </div>
          <div className="wb-date-grid" role="grid" onKeyDown={onGridKeyDown}>
            <div className="wb-date-weekdays" role="row">
              {weekdayNames.map((name, index) => (
                <span className="wb-date-weekday" role="columnheader" key={`${name}-${index}`}>
                  {name}
                </span>
              ))}
            </div>
            {weeks.map((week) => (
              <div className="wb-date-week" role="row" key={dayKey(week[0]!)}>
                {week.map((date) => {
                  const key = dayKey(date)
                  const isSelected = selected !== null && dayKey(selected) === key
                  return (
                    <button
                      type="button"
                      role="gridcell"
                      aria-label={formatDeadline(date)}
                      aria-current={dayKey(today) === key ? "date" : undefined}
                      aria-selected={isSelected}
                      ref={(node) => {
                        dayRefs.current.set(key, node)
                      }}
                      className={`wb-date-day ${
                        date.getMonth() === view.month ? "" : "wb-date-day--other"
                      } ${dayKey(today) === key ? "wb-date-day--today" : ""} ${
                        isSelected ? "wb-date-day--selected" : ""
                      }`.trim()}
                      onClick={() => pick(date)}
                      key={key}
                    >
                      {date.getDate()}
                    </button>
                  )
                })}
              </div>
            ))}
          </div>
          <div className="wb-date-foot">
            <button type="button" className="wb-btn" onClick={() => pick(today)}>
              {t("calendarToday")}
            </button>
            <button type="button" className="wb-btn" onClick={clear}>
              {t("clearDeadline")}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ChipEditor edits a list of strings (authors / keywords) as removable,
// reorderable pills. New chips are typed inline (window.prompt never works in
// the desktop WKWebView shell).
export function ChipEditor(props: {
  label: string
  items: string[]
  addPrompt: string
  onChange: (items: string[]) => void
}) {
  const { t } = useTranslation()
  const dragIndex = useRef<number | null>(null)
  const [adding, setAdding] = useState(false)
  const [chipDraft, setChipDraft] = useState("")

  const submitAdd = () => {
    setAdding(false)
    const value = chipDraft.trim()
    setChipDraft("")
    if (!value) return
    props.onChange([...props.items, value])
  }
  const remove = (index: number) => {
    const next = props.items.slice()
    next.splice(index, 1)
    props.onChange(next)
  }
  const onDrop = (event: DragEvent, targetIndex: number) => {
    event.preventDefault()
    event.stopPropagation()
    const from = dragIndex.current
    dragIndex.current = null
    if (from === null || from === targetIndex) return
    const next = props.items.slice()
    const rect = event.currentTarget.getBoundingClientRect()
    const before = event.clientX < rect.left + rect.width / 2
    const [node] = next.splice(from, 1)
    let to = targetIndex
    if (from < to) to -= 1
    next.splice(before ? to : to + 1, 0, node!)
    props.onChange(next)
  }

  return (
    <div className="wb-chip-editor">
      <span className="wb-chip-label">{props.label}</span>
      <span className="wb-chip-list">
        {props.items.map((item, index) => (
          <span
            key={`${item}-${index}`}
            className="wb-chip"
            draggable
            onDragStart={(e) => {
              dragIndex.current = index
              e.stopPropagation()
              e.dataTransfer.effectAllowed = "move"
            }}
            onDragOver={(e) => {
              e.preventDefault()
              e.stopPropagation()
            }}
            onDrop={(e) => onDrop(e, index)}
          >
            {item}
            <button
              type="button"
              className="wb-chip-del"
              title={t("delete")}
              aria-label={`${t("delete")}: ${item}`}
              onClick={() => remove(index)}
            >
              ✕
            </button>
          </span>
        ))}
        {adding && (
          <input
            className="wb-inline-input"
            autoFocus
            type="text"
            value={chipDraft}
            placeholder={props.addPrompt}
            aria-label={props.addPrompt}
            onChange={(e) => setChipDraft(e.target.value)}
            onBlur={submitAdd}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault()
                submitAdd()
              }
              if (e.key === "Escape") {
                setChipDraft("")
                setAdding(false)
              }
            }}
          />
        )}
      </span>
      <button
        type="button"
        className="wb-chip-add"
        title={t("add")}
        aria-label={t("add")}
        onClick={() => setAdding(true)}
      >
        ＋
      </button>
    </div>
  )
}

// Card is a draggable paper card with reorder support shared across all three
// workbench pages.
export function Card(props: {
  id: string
  onReorder: (fromID: string, toID: string, before: boolean) => void
  children: ReactNode
}) {
  const [dropHint, setDropHint] = useState<"top" | "bottom" | null>(null)
  return (
    <article
      className={`wb-card ${dropHint ? `wb-card--drop-${dropHint}` : ""}`}
      draggable
      onDragStart={(e) => {
        const target = e.target as HTMLElement
        if (target.closest("button, select, input, textarea, .wb-editable, .wb-chip, a")) {
          e.preventDefault()
          return
        }
        e.dataTransfer.effectAllowed = "move"
        e.dataTransfer.setData("text/wb-card", props.id)
      }}
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes("text/wb-card")) return
        e.preventDefault()
        const rect = e.currentTarget.getBoundingClientRect()
        setDropHint(e.clientY < rect.top + rect.height / 2 ? "top" : "bottom")
      }}
      onDragLeave={() => setDropHint(null)}
      onDrop={(e) => {
        const fromID = e.dataTransfer.getData("text/wb-card")
        setDropHint(null)
        if (!fromID || fromID === props.id) return
        e.preventDefault()
        const rect = e.currentTarget.getBoundingClientRect()
        props.onReorder(fromID, props.id, e.clientY < rect.top + rect.height / 2)
      }}
    >
      {props.children}
    </article>
  )
}

// Row is the table counterpart of Card: same reorder contract, but the drop
// hint renders as a top/bottom edge on the <tr> instead of a card outline.
export function Row(props: {
  id: string
  onReorder: (fromID: string, toID: string, before: boolean) => void
  className?: string
  dataPaperID?: string
  children: ReactNode
}) {
  const [dropHint, setDropHint] = useState<"top" | "bottom" | null>(null)
  // dragstart 的 target 永远是 draggable 的那一行本身，所以"从按键上不发起拖拽"
  // 这一层判断在 dragstart 里做不到；改成只有按住把手才给行上 draggable。
  // 否则 WKWebView 会把整行的 mousedown 当成拖拽，吞掉进度按键的点击。
  const [armed, setArmed] = useState(false)
  return (
    <tr
      className={`wb-row ${dropHint ? `wb-row--drop-${dropHint}` : ""} ${props.className ?? ""}`}
      data-paper-id={props.dataPaperID}
      draggable={armed}
      onPointerDown={(e) => {
        const target = e.target as HTMLElement
        setArmed(Boolean(target.closest(".wb-drag-handle")))
      }}
      onDragStart={(e) => {
        if (!armed) {
          e.preventDefault()
          return
        }
        e.dataTransfer.effectAllowed = "move"
        e.dataTransfer.setData("text/wb-row", props.id)
      }}
      onDragEnd={() => setArmed(false)}
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes("text/wb-row")) return
        e.preventDefault()
        const rect = e.currentTarget.getBoundingClientRect()
        setDropHint(e.clientY < rect.top + rect.height / 2 ? "top" : "bottom")
      }}
      onDragLeave={() => setDropHint(null)}
      onDrop={(e) => {
        const fromID = e.dataTransfer.getData("text/wb-row")
        setDropHint(null)
        if (!fromID || fromID === props.id) return
        e.preventDefault()
        const rect = e.currentTarget.getBoundingClientRect()
        props.onReorder(fromID, props.id, e.clientY < rect.top + rect.height / 2)
      }}
    >
      {props.children}
    </tr>
  )
}

export function DragHandle() {
  return (
    <span className="wb-drag-handle" aria-hidden="true">
      ⠿
    </span>
  )
}

// 拖拽步长与侧栏/时间线的 PaneDivider 同档，键盘用户按一次拿到可感知的一跳。
const COLUMN_STEP = 16
// CSS 没给 --wb-col-min 时的兜底下限：再窄就只剩一个汉字宽，等于把列藏了。
const COLUMN_MIN = 64
// 上限是给"把整张表拽到容器外"留的闸；容器内的真实上限由其余各列还能让出的余量算出来。
const COLUMN_MAX = 640

function parsePx(value: string | null | undefined) {
  const px = value ? Number.parseFloat(value) : Number.NaN
  return Number.isFinite(px) ? px : null
}

// ColumnHead 是三张论文表唯一的可拖表头格。
//
// 宽度写成 `--wb-col-w` 这个自定义属性、由 styles.css 的
// `width: var(--wb-col-w, 默认)` 去读，而不是直接内联 width：收列的容器档
// （`.wb-table .wb-col-note { width: 0 }`）特异度只有 (0,2,0)，顶不开内联样式。
// 直接内联会让侧栏一开、窄档收不动列，表格横向溢出。
export function ColumnHead(props: {
  table: string
  column: string
  className: string
  children: ReactNode
}) {
  const { t } = useTranslation()
  const stored = useReaderStore(
    (state) => state.workbenchColumnWidths[props.table]?.[props.column],
  )
  const setWidth = useReaderStore((state) => state.setWorkbenchColumnWidth)
  const clearWidth = useReaderStore((state) => state.clearWorkbenchColumnWidth)
  // live 只在拖拽期间有值：它让这一格自己重渲染，指针移动不惊动整张表。
  const [live, setLive] = useState<number | null>(null)
  const cellRef = useRef<HTMLTableCellElement | null>(null)
  const drag = useRef<{ x: number; from: number; min: number; max: number; last: number | null } | null>(
    null,
  )

  // 列宽下限读 CSS 的 --wb-col-min：那些数字本来就按格子里的控件实测出来的
  // （药丸 108、阶段轨 116、操作列的按钮串 122），写在样式表里才不会和默认宽度各说各话。
  // 往宽能拖多少，看的是同一张表里其余内容列各自还能让出多少（现宽 − 自己的下限）：
  // 表是 width:100% 的 fixed 布局，各列下限之和永远塞得下容器，所以这条上限同时保证
  // 拖不出横向滚动条——滚动条会把格子里的弹层菜单裁掉。
  // 标题列是这张表唯一的 width:auto 列（它吸收整张表的余量），所以只要它没被拖过，
  // 看到的都是标题在让位；一旦标题也落下确定宽度，余量就按各列现宽比例摊开。
  const measure = () => {
    const cell = cellRef.current
    const from = cell?.getBoundingClientRect().width ?? 0
    // 往窄只能收到本列控件自己的实测下限（Math.min 兜住"当前已经比下限窄"的情形：
    // 窗口收窄后下限可能大于现宽，这时不该被一把顶宽）。
    const declared = cell ? getComputedStyle(cell).getPropertyValue("--wb-col-min") : null
    const min = Math.min(from, parsePx(declared) ?? COLUMN_MIN)
    let slack = 0
    const row = cell?.closest("tr")
    for (const th of Array.from(row?.children ?? [])) {
      if (th === cell || th.classList.contains("wb-col-grip")) continue
      const width = th.getBoundingClientRect().width
      const floor = parsePx(getComputedStyle(th).getPropertyValue("--wb-col-min")) ?? COLUMN_MIN
      slack += Math.max(0, width - floor)
    }
    // 标题没被拖过时它的现宽就是吸收完余量的结果，可以远大于 COLUMN_MAX；闸不能
    // 低于现宽，否则第一下拖拽就把一列 900px 的标题拽回 640。
    return { from, min, max: Math.min(Math.max(COLUMN_MAX, from), from + slack) }
  }

  const clamp = (value: number, bounds: { min: number; max: number }) =>
    Math.round(Math.min(bounds.max, Math.max(bounds.min, value)))

  const begin = (event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault()
    const { from, min, max } = measure()
    drag.current = { x: event.clientX, from, min, max, last: null }
    event.currentTarget.setPointerCapture(event.pointerId)
  }
  const move = (event: React.PointerEvent<HTMLButtonElement>) => {
    const session = drag.current
    if (!session) return
    const next = clamp(session.from + (event.clientX - session.x), session)
    // last 记在 ref 上：松手那一下要写的值不能依赖"最后一次 render 有没有跑完"。
    session.last = next
    setLive(next)
  }
  const end = (event: React.PointerEvent<HTMLButtonElement>) => {
    const session = drag.current
    if (!session) return
    drag.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    if (session.last !== null) setWidth(props.table, props.column, session.last)
    setLive(null)
  }
  const nudge = (event: React.KeyboardEvent<HTMLButtonElement>, direction: number) => {
    event.preventDefault()
    const { from, min, max } = measure()
    setWidth(props.table, props.column, clamp(from + direction * COLUMN_STEP, { min, max }))
  }
  const width = live ?? stored
  // 表头格的可见文字同时写进 aria-label：columnheader 的默认命名方式是"由内容算"，
  // 把手那颗按钮的名字会被拼进去，读屏每读到一格就念一遍"拖动调整列宽…"。
  // 显式 label 把它钉回列名本身，把手自己的名字留在按钮上。
  const name = typeof props.children === "string" ? props.children : null
  const label = name ? `${t("resizeColumn")}: ${name}` : t("resizeColumn")

  return (
    <th
      ref={cellRef}
      className={props.className}
      aria-label={name ?? undefined}
      style={width ? ({ "--wb-col-w": `${width}px` } as CSSProperties) : undefined}
    >
      {props.children}
      <button
        type="button"
        className={`wb-col-resizer ${live !== null ? "wb-col-resizer--active" : ""}`}
        role="separator"
        aria-orientation="vertical"
        aria-label={label}
        title={label}
        aria-valuemin={COLUMN_MIN}
        aria-valuemax={COLUMN_MAX}
        aria-valuenow={width}
        onPointerDown={begin}
        onPointerMove={move}
        onPointerUp={end}
        onPointerCancel={end}
        onKeyDown={(event) => {
          if (event.key === "ArrowRight") nudge(event, 1)
          else if (event.key === "ArrowLeft") nudge(event, -1)
        }}
        onDoubleClick={() => clearWidth(props.table, props.column)}
      />
    </th>
  )
}

export function ExpandToggle(props: { expanded: boolean; onToggle: () => void; label: string }) {
  return (
    <button
      type="button"
      className={`wb-expand ${props.expanded ? "wb-expand--open" : ""}`}
      aria-expanded={props.expanded}
      aria-label={props.label}
      title={props.label}
      onClick={props.onToggle}
    >
      ▸
    </button>
  )
}

export interface MenuOption {
  value: string
  label: string
  dotClass?: string
}

// 自定义下拉：替代原生 <select>，解决系统下拉与表格药丸样式割裂的问题。
// 按钮显示圆点 + 文案 + 箭头，弹层为圆角菜单，选中项带对勾。
// 键盘：按钮上 ↓ 打开；菜单内 ↑↓/Home/End 移动、Enter 确认、Esc 关闭。
export function MenuSelect(props: {
  value: string
  options: MenuOption[]
  onChange: (value: string) => void
  ariaLabel: string
  className?: string
  menuClassName?: string
}) {
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const [placement, setPlacement] = useState({ up: false, right: false, maxHeight: 0 })
  const rootRef = useRef<HTMLDivElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const popRef = useRef<HTMLDivElement | null>(null)
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([])
  const current = props.options.find((o) => o.value === props.value)
  const currentIndex = props.options.findIndex((o) => o.value === props.value)

  const openMenu = (index: number) => {
    setActiveIndex(index >= 0 ? index : Math.max(currentIndex, 0))
    setOpen(true)
  }
  const closeMenu = () => {
    setOpen(false)
    setActiveIndex(-1)
  }

  // The menu is anchored inside a table cell, so a fixed drop-down can end up
  // under the viewport edge or past the right gutter. Measure once per open and
  // flip the anchor instead of letting the popover hide the rows it gates.
  useLayoutEffect(() => {
    if (!open) return
    const trigger = buttonRef.current
    const pop = popRef.current
    if (!trigger || !pop) return
    const rect = trigger.getBoundingClientRect()
    const gutter = 8
    const gap = 6
    const spaceBelow = window.innerHeight - rect.bottom - gap - gutter
    const spaceAbove = rect.top - gap - gutter
    const natural = pop.offsetHeight
    setPlacement({
      up: natural > spaceBelow && spaceAbove > spaceBelow,
      right: rect.left + pop.offsetWidth > window.innerWidth - gutter,
      maxHeight: Math.max(120, Math.max(spaceBelow, spaceAbove)),
    })
  }, [open])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) closeMenu()
    }
    document.addEventListener("pointerdown", onPointerDown)
    return () => {
      document.removeEventListener("pointerdown", onPointerDown)
    }
  }, [open])

  // preventScroll matters: the menu lives inside the scrolling workbench, and a
  // plain focus() would scroll the page to reveal the option, which reads as the
  // table jumping. Only the popover itself is allowed to scroll.
  useEffect(() => {
    if (!open || activeIndex < 0) return
    const node = optionRefs.current[activeIndex]
    const pop = popRef.current
    if (!node) return
    node.focus({ preventScroll: true })
    if (!pop) return
    if (node.offsetTop < pop.scrollTop) pop.scrollTop = node.offsetTop
    else if (node.offsetTop + node.offsetHeight > pop.scrollTop + pop.clientHeight) {
      pop.scrollTop = node.offsetTop + node.offsetHeight - pop.clientHeight
    }
  }, [open, activeIndex])

  const commitIndex = (index: number) => {
    const option = props.options[index]
    closeMenu()
    buttonRef.current?.focus({ preventScroll: true })
    if (option && option.value !== props.value) props.onChange(option.value)
  }

  const onMenuKeyDown = (e: React.KeyboardEvent) => {
    const last = props.options.length - 1
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActiveIndex((i) => (i >= last ? 0 : i + 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActiveIndex((i) => (i <= 0 ? last : i - 1))
    } else if (e.key === "Home") {
      e.preventDefault()
      setActiveIndex(0)
    } else if (e.key === "End") {
      e.preventDefault()
      setActiveIndex(last)
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      if (activeIndex >= 0) commitIndex(activeIndex)
    } else if (e.key === "Escape") {
      e.preventDefault()
      closeMenu()
      buttonRef.current?.focus({ preventScroll: true })
    } else if (e.key === "Tab") {
      closeMenu()
    }
  }

  return (
    <div ref={rootRef} className={`wb-menu ${props.className ?? ""}`}>
      <button
        ref={buttonRef}
        type="button"
        className="wb-menu-btn"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={props.ariaLabel}
        title={props.ariaLabel}
        onClick={() => (open ? setOpen(false) : openMenu(-1))}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !open) {
            e.preventDefault()
            openMenu(-1)
          }
        }}
      >
        {current?.dotClass && <i className={`wb-dot ${current.dotClass}`} aria-hidden="true" />}
        <span className="wb-menu-label">{current?.label ?? props.value}</span>
        <span
          className={`wb-menu-chevron ${open ? "wb-menu-chevron--open" : ""}`}
          aria-hidden="true"
        >
          ▾
        </span>
      </button>
      {open && (
        <div
          ref={popRef}
          role="listbox"
          aria-label={props.ariaLabel}
          className={`wb-menu-pop ${placement.up ? "wb-menu-pop--up" : ""} ${
            placement.right ? "wb-menu-pop--right" : ""
          } ${props.menuClassName ?? ""}`}
          style={placement.maxHeight ? { maxHeight: placement.maxHeight } : undefined}
          onKeyDown={onMenuKeyDown}
        >
          {props.options.map((option, index) => {
            const selected = option.value === props.value
            return (
              <button
                key={option.value || "__all__"}
                ref={(node) => {
                  optionRefs.current[index] = node
                }}
                type="button"
                role="option"
                aria-selected={selected}
                tabIndex={-1}
                className={`wb-menu-item ${selected ? "wb-menu-item--active" : ""} ${index === activeIndex ? "wb-menu-item--focus" : ""}`}
                onClick={() => commitIndex(index)}
                onMouseEnter={() => setActiveIndex(index)}
              >
                {option.dotClass && (
                  <i className={`wb-dot ${option.dotClass}`} aria-hidden="true" />
                )}
                <span className="wb-menu-item-label">{option.label}</span>
                {selected && (
                  <span className="wb-menu-check" aria-hidden="true">
                    ✓
                  </span>
                )}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

// 空状态：插画圆点 + 标题 + 提示 + 可选 CTA，区分“搜不到”与“零数据”。
export function EmptyState(props: {
  title: string
  hint?: string
  actionLabel?: string
  onAction?: () => void
  actionDisabled?: boolean
}) {
  return (
    <div className="wb-empty-state">
      <span className="wb-empty-orb" aria-hidden="true" />
      <div className="wb-empty-title">{props.title}</div>
      {props.hint && <div className="wb-empty-hint">{props.hint}</div>}
      {props.actionLabel && props.onAction && (
        <button
          type="button"
          className="wb-btn wb-btn--primary wb-empty-action"
          disabled={props.actionDisabled}
          onClick={props.onAction}
        >
          {props.actionLabel}
        </button>
      )}
    </div>
  )
}

/* 状态 / 优先级映射已移至 ./utils，避免组件文件导出非组件函数。 */
