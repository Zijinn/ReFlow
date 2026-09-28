import { useEffect, useLayoutEffect, useRef, useState, type DragEvent, type ReactNode } from "react"

import { useTranslation } from "../../lib/i18n"

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
  return (
    <tr
      className={`wb-row ${dropHint ? `wb-row--drop-${dropHint}` : ""} ${props.className ?? ""}`}
      data-paper-id={props.dataPaperID}
      draggable
      onDragStart={(e) => {
        const target = e.target as HTMLElement
        if (target.closest("button, select, input, textarea, .wb-editable, .wb-chip, a")) {
          e.preventDefault()
          return
        }
        e.dataTransfer.effectAllowed = "move"
        e.dataTransfer.setData("text/wb-row", props.id)
      }}
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
