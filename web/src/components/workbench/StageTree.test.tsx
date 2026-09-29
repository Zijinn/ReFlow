import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchStage } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { StageTree } from "./StageTree"

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

afterEach(() => cleanup())

function stage(name: string, done = false, children: ResearchStage[] = []): ResearchStage {
  return { name, done, children }
}

describe("StageTree", () => {
  it("shows the done/total count and, for a template, why nothing is saved yet", () => {
    render(
      <StageTree stages={[stage("Drafting", true), stage("Submitting")]} onChange={() => {}} />,
    )
    expect(screen.getByText("1/2")).toBeInTheDocument()
    expect(screen.queryByText(/standard research pipeline/i)).not.toBeInTheDocument()

    cleanup()
    render(
      <StageTree
        stages={[stage("Drafting"), stage("Submitting")]}
        virtual
        onChange={() => {}}
      />,
    )
    expect(screen.getByText(/standard research pipeline/i)).toBeInTheDocument()
  })

  it("renders stage names and computed progress", () => {
    render(
      <StageTree stages={[stage("Drafting", true), stage("Submitting")]} onChange={() => {}} />,
    )
    expect(screen.getByText("Drafting")).toBeInTheDocument()
    expect(screen.getByText("Submitting")).toBeInTheDocument()
    expect(screen.getByText("50%")).toBeInTheDocument()
  })

  it("toggles completion via an aria-labelled button", () => {
    const onChange = vi.fn()
    render(<StageTree stages={[stage("Drafting")]} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Toggle completion: Drafting" }))
    expect(onChange).toHaveBeenCalledTimes(1)
    expect((onChange.mock.calls[0]![0] as ResearchStage[])[0]!.done).toBe(true)
  })

  it("adds a child stage through an inline input, not window.prompt", () => {
    const promptSpy = vi.spyOn(window, "prompt").mockImplementation(() => null)
    const stages = [stage("Drafting", false, [stage("Outline")])]
    const onChange = vi.fn()
    render(<StageTree stages={stages} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Add child stage: Drafting" }))
    const input = screen.getByPlaceholderText("Stage name, press Enter to add")
    fireEvent.change(input, { target: { value: "Regression" } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(promptSpy).not.toHaveBeenCalled()
    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0]![0] as ResearchStage[]
    expect(next[0]!.children.map((c) => c.name)).toEqual(["Outline", "Regression"])
  })

  it("adds a top-level stage via the root input", () => {
    const onChange = vi.fn()
    render(<StageTree stages={[stage("Drafting")]} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: /Add a top-level stage/ }))
    const input = screen.getByPlaceholderText("Stage name, press Enter to add")
    fireEvent.change(input, { target: { value: "Revision" } })
    fireEvent.keyDown(input, { key: "Enter" })
    const next = onChange.mock.calls[0]![0] as ResearchStage[]
    expect(next.map((s) => s.name)).toEqual(["Drafting", "Revision"])
  })

  it("renames a stage inline with double-click editing", () => {
    const onChange = vi.fn()
    render(<StageTree stages={[stage("Drafting")]} onChange={onChange} />)
    fireEvent.doubleClick(screen.getByText("Drafting"))
    const input = screen.getByDisplayValue("Drafting")
    fireEvent.change(input, { target: { value: "Draft v2" } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(onChange).toHaveBeenCalledTimes(1)
    expect((onChange.mock.calls[0]![0] as ResearchStage[])[0]!.name).toBe("Draft v2")
  })

  it("asks for confirmation via the shared dialog before deleting", async () => {
    const alertSpy = vi.spyOn(window, "alert").mockImplementation(() => {})
    const confirmSpy = vi.spyOn(window, "confirm").mockImplementation(() => true)
    const onChange = vi.fn()
    render(<StageTree stages={[stage("Drafting"), stage("Submitting")]} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Delete: Drafting" }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1))
    expect((onChange.mock.calls[0]![0] as ResearchStage[]).map((s) => s.name)).toEqual([
      "Submitting",
    ])
    expect(alertSpy).not.toHaveBeenCalled()
    expect(confirmSpy).not.toHaveBeenCalled()
  })

  it("does not delete when the dialog is dismissed", async () => {
    const onChange = vi.fn()
    render(<StageTree stages={[stage("Drafting")]} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Delete: Drafting" }))
    await screen.findByRole("dialog")
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    expect(onChange).not.toHaveBeenCalled()
  })
})
