import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { PromptDialog } from "./PromptDialog"

afterEach(() => cleanup())

function renderDialog(overrides: Partial<Parameters<typeof PromptDialog>[0]> = {}) {
  const handlers = {
    onSubmit: vi.fn(),
    onCancel: vi.fn(),
    ...overrides,
  }
  render(
    <PromptDialog
      open={true}
      title="Rename"
      label="Folder name"
      submitLabel="Save"
      cancelLabel="Cancel"
      {...handlers}
    />,
  )
  return handlers
}

it("submits the typed value", () => {
  const handlers = renderDialog()
  const input = screen.getByLabelText<HTMLInputElement>("Folder name")
  fireEvent.change(input, { target: { value: "Papers" } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(handlers.onSubmit).toHaveBeenCalledWith("Papers")
  expect(handlers.onCancel).not.toHaveBeenCalled()
})

it("trims the submitted value", () => {
  const handlers = renderDialog()
  fireEvent.change(screen.getByLabelText<HTMLInputElement>("Folder name"), { target: { value: "  Papers  " } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(handlers.onSubmit).toHaveBeenCalledWith("Papers")
})

it("keeps submit disabled while the input is empty", () => {
  const handlers = renderDialog({ initialValue: "Research" })
  const submit = screen.getByRole("button", { name: "Save" })
  expect(submit).not.toBeDisabled()
  fireEvent.change(screen.getByLabelText<HTMLInputElement>("Folder name"), { target: { value: "   " } })
  expect(submit).toBeDisabled()
  fireEvent.click(submit)
  expect(handlers.onSubmit).not.toHaveBeenCalled()
})

it("seeds and selects the initial value on open", () => {
  renderDialog({ initialValue: "Research" })
  const input = screen.getByLabelText<HTMLInputElement>("Folder name")
  expect(input.value).toBe("Research")
  expect(input.selectionStart).toBe(0)
  expect(input.selectionEnd).toBe("Research".length)
})

it("submits on Enter and cancels on Escape", () => {
  const handlers = renderDialog({ initialValue: "Papers" })
  const input = screen.getByLabelText<HTMLInputElement>("Folder name")
  fireEvent.keyDown(input, { key: "Escape" })
  expect(handlers.onCancel).toHaveBeenCalledTimes(1)
  expect(handlers.onSubmit).not.toHaveBeenCalled()
  fireEvent.keyDown(input, { key: "Enter" })
  expect(handlers.onSubmit).toHaveBeenCalledWith("Papers")
})

it("cancels through the cancel button without submitting", () => {
  const handlers = renderDialog({ initialValue: "Papers" })
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
  expect(handlers.onCancel).toHaveBeenCalledTimes(1)
  expect(handlers.onSubmit).not.toHaveBeenCalled()
})

it("renders nothing while closed", () => {
  render(
    <PromptDialog
      open={false}
      title="Rename"
      label="Folder name"
      submitLabel="Save"
      cancelLabel="Cancel"
      onSubmit={vi.fn()}
      onCancel={vi.fn()}
    />,
  )
  expect(screen.queryByLabelText("Folder name")).not.toBeInTheDocument()
})
