import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { fillMetadataWithAI } from "../../api/client"
import type { AIProfile, ResearchPaper } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { useToastStore } from "../../store/toast"
import { PublishedPage } from "./PublishedPage"

// PublishedPage only pulls fillMetadataWithAI from the API client; the mock
// keeps the paste-dialog tests free of network access.
vi.mock("../../api/client", () => ({
  fillMetadataWithAI: vi.fn(),
}))

const mockFill = vi.mocked(fillMetadataWithAI)

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
  useToastStore.setState({ toasts: [] })
  mockFill.mockReset()
})

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "p-1",
    kind: "published",
    position: 0,
    title: "Data and Growth",
    authors: ["Smith J"],
    keywords: [],
    file_path: "",
    next_action: "",
    notes: "",
    research_area: "",
    status: "",
    priority: "",
    target_journal: "",
    stages: [],
    current_journal: "",
    submission_date: "",
    manuscript_id: "",
    submission_count: 0,
    target_level: "",
    editor: "",
    deadline: "",
    history: [],
    abstract: "",
    journal: "American Economic Review",
    language: "",
    year: "2024",
    volume: "",
    issue: "",
    pages: "",
    doi: "",
    citations: null,
    citation_source: "",
    citation_updated_at: "",
    last_updated: "",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

function noopProps() {
  return {
    citationPendingID: null,
    onCreate: vi.fn(),
    onUpdate: vi.fn(),
    onDelete: vi.fn(),
    onReorder: vi.fn(),
    onFetchCitation: vi.fn(),
    onFetchAllCitations: vi.fn(),
    onCrossrefEmailChange: vi.fn(),
  }
}

describe("PublishedPage crossref email input", () => {
  it("backfills the input when the email arrives asynchronously", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    const view = render(<PublishedPage papers={[paper()]} {...props} />)
    const input = screen.getByPlaceholderText(/your-email|example\.com/i)
    expect(input).toHaveValue("")
    // Preferences query resolves later: the old defaultValue version kept
    // showing an empty box forever.
    view.rerender(<PublishedPage papers={[paper()]} {...props} crossrefEmail="me@lab.org" />)
    expect(screen.getByPlaceholderText(/your-email|example\.com/i)).toHaveValue("me@lab.org")
  })

  it("never submits an empty string and blurs away", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    const input = screen.getByPlaceholderText(/your-email|example\.com/i)
    fireEvent.change(input, { target: { value: "   " } })
    fireEvent.blur(input)
    expect(props.onCrossrefEmailChange).not.toHaveBeenCalled()
  })

  it("does not re-PUT the unchanged stored value", () => {
    const props = { ...noopProps(), crossrefEmail: "me@lab.org" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    const input = screen.getByPlaceholderText(/your-email|example\.com/i)
    fireEvent.blur(input)
    expect(props.onCrossrefEmailChange).not.toHaveBeenCalled()
  })

  it("commits the trimmed value on blur", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    const input = screen.getByPlaceholderText(/your-email|example\.com/i)
    fireEvent.change(input, { target: { value: " new@lab.org " } })
    fireEvent.blur(input)
    expect(props.onCrossrefEmailChange).toHaveBeenCalledWith("new@lab.org")
  })
})

describe("PublishedPage citations", () => {
  it("stores the stable manual enum key instead of a Chinese label", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(<PublishedPage papers={[paper({ citations: 3 })]} {...props} />)
    fireEvent.doubleClick(screen.getByText("3"))
    const input = screen.getByDisplayValue("3")
    fireEvent.change(input, { target: { value: "12" } })
    fireEvent.keyDown(input, { key: "Enter" })
    expect(props.onUpdate).toHaveBeenCalledWith("p-1", {
      citations: 12,
      citation_source: "manual",
    })
  })

  it("recognises Crossref sources (including the legacy Chinese label)", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(
      <PublishedPage
        papers={[
          paper({
            id: "p-cross",
            citations: 8,
            citation_source: "Crossref",
            citation_updated_at: "2026-01-02T00:00:00Z",
          }),
          paper({ id: "p-legacy", citations: 4, citation_source: "手工录入" }),
        ]}
        {...props}
      />,
    )
    expect(screen.getByText(/Crossref \(2026-01-02\)/)).toBeInTheDocument()
    // The legacy label must not be displayed raw nor treated as Crossref.
    expect(screen.queryByText("手工录入")).not.toBeInTheDocument()
    expect(
      screen.getAllByText(/Double-click to edit, or fetch|fetch from Crossref/i).length,
    ).toBeGreaterThan(0)
  })

  it("fetch guard: a missing DOI surfaces a toast without calling the API", () => {
    const props = { ...noopProps(), crossrefEmail: "me@lab.org" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    fireEvent.click(screen.getByRole("button", { name: /Crossref/ }))
    expect(props.onFetchCitation).not.toHaveBeenCalled()
    const messages = useToastStore.getState().toasts.map((item) => item.message)
    expect(messages.some((message) => /DOI/i.test(message))).toBe(true)
  })
})

describe("PublishedPage search", () => {
  it("filters on whitelisted fields only", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(
      <PublishedPage
        papers={[
          paper({ title: "Machine Learning Review", notes: "internal-note-42" }),
          paper({ id: "p-2", title: "Other Paper" }),
        ]}
        {...props}
      />,
    )
    const search = screen.getByPlaceholderText(/Search title/i)
    fireEvent.change(search, { target: { value: "machine learning" } })
    expect(screen.getByText("Machine Learning Review")).toBeInTheDocument()
    expect(screen.queryByText("Other Paper")).not.toBeInTheDocument()
    // JSON.stringify matching would have hit internal keys like "notes" too;
    // the value itself only matches through the whitelisted notes field.
    fireEvent.change(search, { target: { value: "internal-note-42" } })
    expect(screen.getByText("Machine Learning Review")).toBeInTheDocument()
    fireEvent.change(search, { target: { value: "citation_updated_at" } })
    expect(screen.queryByText("Machine Learning Review")).not.toBeInTheDocument()
  })
})

describe("PublishedPage batch citations", () => {
  it("fires onFetchAllCitations from the toolbar button", () => {
    const props = { ...noopProps(), crossrefEmail: "me@lab.org" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    fireEvent.click(screen.getByRole("button", { name: "Update all citations" }))
    expect(props.onFetchAllCitations).toHaveBeenCalledTimes(1)
  })

  it("disables the batch button and swaps its label while pending", () => {
    const props = { ...noopProps(), crossrefEmail: "me@lab.org", batchCitationPending: true }
    render(<PublishedPage papers={[paper()]} {...props} />)
    const button = screen.getByRole("button", { name: "Updating all citations…" })
    expect(button).toBeDisabled()
    expect(screen.queryByRole("button", { name: "Update all citations" })).not.toBeInTheDocument()
  })
})

describe("PublishedPage offline", () => {
  it("disables write affordances when offline", () => {
    const props = { ...noopProps(), crossrefEmail: "", offline: true }
    render(<PublishedPage papers={[paper()]} {...props} />)
    expect(screen.getByRole("button", { name: /＋ Add paper|Add paper/ })).toBeDisabled()
  })
})

function aiProfile(overrides: Partial<AIProfile> = {}): AIProfile {
  return {
    id: "ai-1",
    provider: "openai_compatible",
    name: "Fixture AI",
    endpoint: "http://127.0.0.1:11434/v1",
    model: "fixture",
    enabled: true,
    allow_private_network: true,
    remote_content_approved: true,
    is_default: true,
    last_used_at: null,
    last_error_code: null,
    last_error_message: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

const ZH_REFERENCE =
  "[1] 赵金阳. 非洲数字贸易规则的构建动因[J]. 国际经贸探索, 2026, 42(3): 12-25."

function openPasteDialog() {
  fireEvent.click(screen.getByRole("button", { name: /Paste a GB\/T 7714 reference/ }))
  return screen.getByPlaceholderText(/Smith J\. The dynamics of digital trade rules/)
}

describe("PublishedPage GB/T 7714 paste", () => {
  it("creates an entry from a parsed reference through the existing mutation props", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    const view = render(<PublishedPage papers={[paper()]} {...props} />)
    const textarea = openPasteDialog()
    fireEvent.change(textarea, { target: { value: ZH_REFERENCE } })
    fireEvent.click(screen.getByRole("button", { name: "Parse and create entry" }))

    // Step 1: the create mutation fires and the dialog reports success.
    expect(props.onCreate).toHaveBeenCalledTimes(1)
    expect(
      useToastStore.getState().toasts.some((item) => /parsed into a new entry/i.test(item.message)),
    ).toBe(true)

    // Step 2: once the created paper arrives in the list, the remaining
    // fields are PATCHed through onUpdate — no direct fetch anywhere.
    view.rerender(
      <PublishedPage papers={[paper(), paper({ id: "p-2", title: "" })]} {...props} />,
    )
    expect(props.onUpdate).toHaveBeenCalledWith("p-2", {
      title: "非洲数字贸易规则的构建动因",
      authors: ["赵金阳"],
      journal: "国际经贸探索",
      year: "2026",
      volume: "42",
      issue: "3",
      pages: "12-25",
    })
  })

  it("toasts pasteGbInvalid and creates nothing for unparseable text", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    const textarea = openPasteDialog()
    fireEvent.change(textarea, { target: { value: "lorem ipsum dolor" } })
    fireEvent.click(screen.getByRole("button", { name: "Parse and create entry" }))
    expect(props.onCreate).not.toHaveBeenCalled()
    expect(
      useToastStore
        .getState()
        .toasts.some((item) => /Could not read reference fields/i.test(item.message)),
    ).toBe(true)
    // The dialog stays open so the text can be fixed.
    expect(screen.getByPlaceholderText(/Smith J\. The dynamics/)).toBeInTheDocument()
  })
})

describe("PublishedPage AI metadata fill", () => {
  it("disables the AI button with the no-profile hint when no profile exists", () => {
    const props = { ...noopProps(), crossrefEmail: "" }
    render(<PublishedPage papers={[paper()]} {...props} />)
    openPasteDialog()
    const button = screen.getByRole("button", { name: "Fill with AI" })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute("title", "Add an AI profile in preferences first.")
  })

  it("merges the AI answer into the local parse, filling empty fields only", async () => {
    mockFill.mockResolvedValue({
      title: "AI Title That Must Lose",
      authors: ["AI Author"],
      year: "2021",
      doi: "10.9/xyz",
    })
    const props = { ...noopProps(), crossrefEmail: "", aiProfiles: [aiProfile()] }
    const view = render(<PublishedPage papers={[paper()]} {...props} />)
    const textarea = openPasteDialog()
    const raw = "Smith J. Digital trade[J]. Journal of Trade."
    fireEvent.change(textarea, { target: { value: raw } })
    fireEvent.click(screen.getByRole("button", { name: "Fill with AI" }))

    await waitFor(() => expect(props.onCreate).toHaveBeenCalledTimes(1))
    expect(mockFill).toHaveBeenCalledWith("ai-1", raw)
    expect(
      useToastStore.getState().toasts.some((item) => /AI filled the entry/i.test(item.message)),
    ).toBe(true)

    view.rerender(
      <PublishedPage papers={[paper(), paper({ id: "p-2", title: "" })]} {...props} />,
    )
    // Local parse wins for title/authors/journal; AI fills the empty year and
    // contributes the DOI the local text did not carry.
    expect(props.onUpdate).toHaveBeenCalledWith("p-2", {
      title: "Digital trade",
      authors: ["Smith J"],
      journal: "Journal of Trade",
      year: "2021",
      doi: "10.9/xyz",
    })
  })

  it("toasts aiFillFailed and creates nothing when the AI call fails", async () => {
    mockFill.mockRejectedValue(new Error("boom"))
    const props = { ...noopProps(), crossrefEmail: "", aiProfiles: [aiProfile()] }
    render(<PublishedPage papers={[paper()]} {...props} />)
    const textarea = openPasteDialog()
    fireEvent.change(textarea, { target: { value: "Smith J. Digital trade[J]. Journal." } })
    fireEvent.click(screen.getByRole("button", { name: "Fill with AI" }))

    await waitFor(() =>
      expect(
        useToastStore.getState().toasts.some((item) => /AI fill failed/i.test(item.message)),
      ).toBe(true),
    )
    expect(props.onCreate).not.toHaveBeenCalled()
  })
})

afterEach(() => cleanup())
