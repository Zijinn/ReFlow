import { useId, useRef, useState } from "react"
import * as Dialog from "@radix-ui/react-dialog"

interface PromptDialogProps {
  open: boolean
  title: string
  label: string
  initialValue?: string
  submitLabel: string
  cancelLabel: string
  onSubmit: (value: string) => void
  onCancel: () => void
}

// Replaces window.prompt: the desktop WKWebView/WebView2 shells never show
// native JS dialogs, so every prompt-backed flow silently died there. This is
// an in-app dialog with a single text input — submit is disabled while the
// trimmed value is empty, the initial value is selected on open, Enter
// submits and Escape cancels (focus handling is Radix's job).
export function PromptDialog(props: PromptDialogProps) {
  // Keying on `open` remounts the implementation every time the dialog
  // opens, so the input always seeds from `initialValue` without a reset
  // effect — back-to-back opens for different rows never leak a stale draft.
  return <PromptDialogImpl key={props.open ? "open" : "closed"} {...props} />
}

function PromptDialogImpl(props: PromptDialogProps) {
  const inputID = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const [value, setValue] = useState(props.initialValue ?? "")

  const submit = () => {
    const next = value.trim()
    if (!next) return
    props.onSubmit(next)
  }

  return (
    <Dialog.Root
      open={props.open}
      onOpenChange={(open) => {
        if (!open) props.onCancel()
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="dialog-content dialog-content--confirm"
          aria-describedby={undefined}
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            inputRef.current?.focus()
            inputRef.current?.select()
          }}
        >
          <div className="dialog-header">
            <Dialog.Title>{props.title}</Dialog.Title>
          </div>
          <form
            className="dialog-form"
            onSubmit={(event) => {
              event.preventDefault()
              submit()
            }}
          >
            <label className="field-label" htmlFor={inputID}>
              {props.label}
            </label>
            <input
              ref={inputRef}
              id={inputID}
              className="text-input"
              type="text"
              value={value}
              onChange={(event) => setValue(event.target.value)}
              onKeyDown={(event) => {
                // Explicit Enter handling: preventDefault keeps the browser's
                // implicit form submission from double-firing onSubmit.
                if (event.key === "Enter") {
                  event.preventDefault()
                  submit()
                }
              }}
            />
            <div className="dialog-actions dialog-actions--end">
              <button
                className="button button--secondary"
                type="button"
                onClick={props.onCancel}
              >
                {props.cancelLabel}
              </button>
              <button
                className="button button--primary"
                type="submit"
                disabled={!value.trim()}
              >
                {props.submitLabel}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
