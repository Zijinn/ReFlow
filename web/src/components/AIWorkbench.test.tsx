import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { ComponentProps } from "react"

import type { AIChatMessage, AIChatSession, AIProfile, Job } from "../api/types"
import { useReaderStore } from "../store/reader"
import { AIWorkbench } from "./AIWorkbench"

// Spread the real module so `APIError` keeps its identity and every network
// entry point is replaced: a panel test must never reach fetch.
vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>()
  return {
    ...actual,
    cancelJob: vi.fn(),
    getAIChat: vi.fn(),
    getJob: vi.fn(),
    listAIResults: vi.fn(),
    runAIOperation: vi.fn(),
    startAIChat: vi.fn(),
    startAILibraryChat: vi.fn(),
    startAIPaperChat: vi.fn(),
  }
})

import * as api from "../api/client"

const QUESTION = "which papers are stuck?"
const CONTEXT_LABEL = "All papers in the workspace · 2"
const ASK = "Ask about the papers in this tab"

function profile(overrides: Partial<AIProfile> = {}): AIProfile {
  return {
    id: "ai-1",
    provider: "openai_compatible",
    name: "Lab endpoint",
    endpoint: "https://example.test/v1",
    model: "gpt-test",
    enabled: true,
    allow_private_network: false,
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

function job(state: Job["state"]): Job {
  return {
    id: "job-1",
    kind: "ai.research",
    state,
    progress_current: 0,
    progress_total: 0,
    scheduled_at: "2026-10-01T09:00:00Z",
    started_at: null,
    finished_at: null,
    error_code: state === "failed" ? "ai_upstream" : null,
    error_message: state === "failed" ? "upstream 502: bad gateway" : null,
    created_at: "2026-10-01T09:00:00Z",
    updated_at: "2026-10-01T09:00:00Z",
  }
}

// `metadata` is what the Go handler actually stores the model in; the fixture
// carries it as the server sends it, since the client type has no field for it.
function message(overrides: Record<string, unknown> = {}): AIChatMessage {
  return {
    id: "msg-1",
    role: "user",
    content: QUESTION,
    status: "completed",
    usage: {},
    created_at: "2026-10-01T09:00:00Z",
    ...overrides,
  }
}

function session(messages: AIChatMessage[]): AIChatSession {
  return {
    id: "sess-1",
    ai_profile_id: "ai-1",
    entry_id: null,
    title: "",
    messages,
    created_at: "2026-10-01T09:00:00Z",
    updated_at: "2026-10-01T09:00:00Z",
  }
}

const ANSWER = message({
  id: "msg-2",
  role: "assistant",
  content: "Two papers have no next action recorded.",
  usage: { total_tokens: 1234 },
  created_at: "2026-10-01T09:00:42Z",
  metadata: { provider: "openai_compatible", model: "gpt-test" },
})

function renderPanel(props: Partial<ComponentProps<typeof AIWorkbench>> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <AIWorkbench
        paperIDs={["p-1", "p-2"]}
        profiles={[profile()]}
        width={380}
        contextLabel={CONTEXT_LABEL}
        initialMode="chat"
        onWidthChange={() => {}}
        onClose={() => {}}
        onConfigure={() => {}}
        {...props}
      />
    </QueryClientProvider>,
  )
  return { ...result, queryClient }
}

// Handed to the queue: the context count is a prop, so the send control is
// enabled the moment the panel paints and no query has to settle first.
function submit() {
  const box = screen.getByLabelText(ASK)
  fireEvent.change(box, { target: { value: QUESTION } })
  const send = screen.getByRole("button", { name: "Ask" })
  expect(send).not.toBeDisabled()
  fireEvent.click(send)
}

async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

function chat(container: HTMLElement) {
  return container.querySelector(".ai-chat") as HTMLElement
}

beforeEach(() => {
  vi.clearAllMocks()
  useReaderStore.setState({ locale: "en-US" })
  vi.mocked(api.startAIPaperChat).mockResolvedValue({
    job: job("queued"),
    session: session([message()]),
  })
  vi.mocked(api.getJob).mockResolvedValue(job("succeeded"))
  vi.mocked(api.getAIChat).mockResolvedValue(session([message(), ANSWER]))
  vi.mocked(api.cancelJob).mockResolvedValue(job("cancelled"))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe("AIWorkbench empty state", () => {
  it("says what the model can read instead of printing the context line twice", async () => {
    const { container } = renderPanel()
    const panel = await screen.findByRole("complementary")
    expect(within(panel).getAllByText(CONTEXT_LABEL)).toHaveLength(1)
    expect(container.querySelector(".ai-chat__context-note")).toBeNull()
    expect(
      within(panel).getByText(
        "The model reads what these papers record in the workspace — title, authors, " +
          "keywords, stages, next action, deadline and notes. The PDF text is not part of it.",
      ),
    ).toBeInTheDocument()
    expect(within(panel).getByText("2")).toBeInTheDocument()
    expect(within(panel).getByText("papers in context")).toBeInTheDocument()
  })

  it("offers three openers that fill the composer without sending", async () => {
    renderPanel()
    const openers = await screen.findAllByRole("button", {
      name: /What to push this week|Deadlines and submission status|Which papers have stalled/,
    })
    expect(openers).toHaveLength(3)
    fireEvent.click(openers[0]!)
    expect(screen.getByRole("textbox", { name: ASK })).toHaveValue(
      "By deadline and unfinished stages, name the papers that most need pushing this week and say why.",
    )
    expect(api.startAIPaperChat).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Ask" }))
    await waitFor(() => expect(api.startAIPaperChat).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.startAIPaperChat).mock.calls[0]![0].message).toBe(
      "By deadline and unfinished stages, name the papers that most need pushing this week and say why.",
    )
  })

  it("steps out of the way once a turn is in flight", async () => {
    const { container } = renderPanel()
    submit()
    await flush()
    expect(container.querySelector(".ai-chat__empty")).toBeNull()
    expect(screen.queryByText("Try asking")).toBeNull()
  })
})

describe("AIWorkbench stop control", () => {
  it("is absent between runs instead of sitting there disabled", async () => {
    renderPanel()
    submit()
    await flush()
    cleanup()
    renderPanel()
    // 原来这颗常驻且永远 disabled——"可点但永远不可点"挂在输入区下方读起来像界面坏了。
    expect(screen.queryByRole("button", { name: "Stop generating" })).toBeNull()
  })

  it("appears in the composer while a job runs and cancels exactly that job", async () => {
    vi.mocked(api.getJob).mockResolvedValue(job("running"))
    const { container } = renderPanel()
    submit()
    await flush()
    const stop = await screen.findByRole("button", { name: "Stop generating" })
    // 控件在 composer 里，而那一排是 row-reverse：增删这一颗不会推动发送键。
    expect(stop.closest("form")).toBe(container.querySelector(".ai-chat__composer"))
    await waitFor(() => expect(stop).not.toBeDisabled())
    fireEvent.click(stop)
    // React Query appends its mutation context to the variables, so `cancelJob`
    // arrives as ("job-1", context); the id is what has to be exact.
    await waitFor(() => expect(vi.mocked(api.cancelJob).mock.calls[0]?.[0]).toBe("job-1"))
    expect(vi.mocked(api.cancelJob)).toHaveBeenCalledTimes(1)
  })

  it("renders the in-flight line inside the thread, above a composer that stays last", async () => {
    vi.mocked(api.getJob).mockResolvedValue(job("running"))
    const { container } = renderPanel()
    submit()
    await flush()
    const panel = chat(container)
    const pending = panel.querySelector(".ai-chat__message--pending")
    expect(pending).not.toBeNull()
    expect(panel.querySelector(".ai-chat__messages")?.contains(pending as Node)).toBe(true)
    expect(panel.querySelector(".ai-chat__messages")).toHaveAttribute("aria-live", "polite")
    expect(panel.lastElementChild).toBe(panel.querySelector(".ai-chat__composer"))
  })
})

describe("AIWorkbench liveness", () => {
  it("polls the session only while the job is active", async () => {
    vi.useFakeTimers()
    vi.mocked(api.getJob).mockResolvedValue(job("running"))
    renderPanel()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    submit()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    const started = vi.mocked(api.getAIChat).mock.calls.length
    expect(started).toBeGreaterThan(0)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_600)
    })
    expect(vi.mocked(api.getAIChat).mock.calls.length).toBeGreaterThan(started)
    // The job settles, so the thread settles with it: no idle polling.
    vi.mocked(api.getJob).mockResolvedValue(job("succeeded"))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(900)
    })
    const settled = vi.mocked(api.getAIChat).mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000)
    })
    expect(vi.mocked(api.getAIChat).mock.calls.length).toBe(settled)
  })

  it("shows one pulsing dot and a seconds counter, and no second spinner", async () => {
    vi.useFakeTimers()
    vi.mocked(api.getJob).mockResolvedValue(job("running"))
    const { container } = renderPanel()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    submit()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(container.querySelectorAll(".ai-chat__pulse")).toHaveLength(1)
    expect(container.querySelector(".spin")).toBeNull()
    const live = container.querySelector(".ai-chat__live")
    expect(live).toHaveAttribute("aria-hidden", "true")
    expect(container.querySelector(".ai-chat__elapsed")?.textContent).toBe("0 s")
    expect(screen.getByText("Generating an answer")).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_100)
    })
    expect(container.querySelector(".ai-chat__elapsed")?.textContent).toBe("2 s")
  })
})

describe("AIWorkbench answer receipt", () => {
  it("prints tokens · seconds · model for an answer that carries them", async () => {
    const { container } = renderPanel()
    submit()
    await flush()
    const receipt = await screen.findByText("1,234 tokens · 42 s · gpt-test")
    expect(receipt.className).toBe("ai-chat__receipt")
    expect(chat(container).querySelector(".ai-chat__message--user .ai-chat__receipt")).toBeNull()
  })

  it("prints nothing it cannot derive", async () => {
    // No usage, no metadata model, and a server that left the timestamp empty:
    // every segment of the receipt is absent, so the line itself must be too.
    vi.mocked(api.getAIChat).mockResolvedValue(
      session([
        message(),
        message({ id: "msg-2", role: "assistant", content: "No usage.", created_at: "" }),
      ]),
    )
    const { container } = renderPanel()
    submit()
    await flush()
    await screen.findByText("No usage.")
    expect(container.querySelector(".ai-chat__receipt")).toBeNull()
  })

  it("drops the seconds of a one-shot answer with no question before it", async () => {
    vi.mocked(api.getAIChat).mockResolvedValue(
      session([
        message({
          role: "assistant",
          content: "Digest.",
          usage: { total_tokens: 80 },
          created_at: "2026-10-01T09:00:42Z",
        }),
      ]),
    )
    const { container } = renderPanel()
    submit()
    await flush()
    await screen.findByText("Digest.")
    expect(container.querySelector(".ai-chat__receipt")?.textContent).toBe("80 tokens")
  })
})

describe("AIWorkbench failed answer", () => {
  it("renders the job's own error verbatim inside the thread", async () => {
    vi.mocked(api.getJob).mockResolvedValue(job("failed"))
    vi.mocked(api.getAIChat).mockResolvedValue(session([message()]))
    const { container } = renderPanel()
    submit()
    await flush()
    const failed = await waitFor(() => {
      const node = chat(container).querySelector(".ai-chat__message--failed")
      expect(node).not.toBeNull()
      return node
    })
    expect(failed).toHaveTextContent("upstream 502: bad gateway")
    expect(failed).toHaveAttribute("role", "alert")
    // One alert for one failure: the error is the answer, not a footnote as well.
    expect(container.querySelectorAll("[role='alert']")).toHaveLength(1)
    expect(chat(container).querySelector(".ai-chat__composer")).not.toBeNull()
  })

  it("keeps a turn that never reached the queue as an inline alert", async () => {
    vi.mocked(api.startAIPaperChat).mockRejectedValue(new api.APIError(503, "AI is not configured"))
    const { container } = renderPanel()
    submit()
    await flush()
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("AI is not configured")
    expect(container.querySelector(".ai-chat__message--pending")).toBeNull()
  })
})
