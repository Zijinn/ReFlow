import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { AIChatMessage, AIChatSession, AIProfile, Job, ResearchPaper } from "../../api/types"
import { useReaderStore } from "../../store/reader"
import { useToastStore } from "../../store/toast"
import { DailyDigestCard } from "./DailyDigest"
import { formatDeadline } from "./utils"

// Spread the real module so `APIError` keeps its identity: the card maps HTTP
// statuses to its quiet lines through `instanceof`, which a plain stub would
// silently defeat.
vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>()
  return {
    ...actual,
    getAIChat: vi.fn(),
    getJob: vi.fn(),
    startAIDailyDigest: vi.fn(),
  }
})

import * as api from "../../api/client"

const CACHE_KEY = "reflow-daily-digest"

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

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "p-1",
    kind: "research",
    position: 0,
    title: "A Working Paper",
    authors: [],
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
    journal: "",
    language: "",
    year: "",
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

function job(state: Job["state"]): Job {
  return {
    id: "job-1",
    kind: "ai.research",
    state,
    progress_current: 0,
    progress_total: 0,
    scheduled_at: "",
    started_at: null,
    finished_at: null,
    error_code: null,
    error_message: state === "failed" ? "upstream 500" : null,
    created_at: "",
    updated_at: "",
  }
}

function session(messages: AIChatMessage[]): AIChatSession {
  return {
    id: "sess-1",
    ai_profile_id: "ai-1",
    entry_id: null,
    title: "",
    messages,
    created_at: "",
    updated_at: "",
  }
}

function assistant(content: string): AIChatMessage {
  return {
    id: "msg-1",
    role: "assistant",
    content,
    status: "completed",
    usage: {},
    created_at: "",
  }
}

function dayOffset(days: number): string {
  return formatDeadline(new Date(Date.now() + days * 86_400_000))
}

function renderCard(
  overrides: Partial<{
    papers: ResearchPaper[]
    profiles: AIProfile[]
    onAskAI: () => void
    onOpenPaper: (paperID: string) => void
  }> = {},
) {
  return render(
    <DailyDigestCard
      papers={overrides.papers ?? [paper()]}
      profiles={overrides.profiles ?? [profile()]}
      onConfigure={onConfigure}
      onAskAI={overrides.onAskAI}
      onOpenPaper={overrides.onOpenPaper}
    />,
  )
}

const BRIEFING = [
  "今天先处理两份稿子。",
  "1. 完成稳健性表格",
  "- 给审稿人 2 写回复",
  "* 补一个工具变量",
  "4) 校对参考文献",
  "· 提交前重读一遍",
  "第六条被截断，卡片只留五行",
].join("\n")

let onConfigure = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  onConfigure = vi.fn()
  localStorage.clear()
  useReaderStore.setState({ locale: "zh-CN" })
  useToastStore.setState({ toasts: [] })
  vi.mocked(api.startAIDailyDigest).mockResolvedValue({
    job: job("succeeded"),
    session: session([]),
  })
  vi.mocked(api.getJob).mockResolvedValue(job("succeeded"))
  vi.mocked(api.getAIChat).mockResolvedValue(session([assistant(BRIEFING)]))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  localStorage.clear()
})

describe("DailyDigestCard without AI", () => {
  it("degrades to one quiet line and offers configuration instead of firing", async () => {
    renderCard({ profiles: [] })
    expect(screen.getByText("配置 AI 后，这里会给出每天的推进建议。")).toBeInTheDocument()
    expect(screen.queryByRole("status")).toBeNull()
    const configure = screen.getByRole("button", { name: "配置 AI" })
    fireEvent.click(configure)
    expect(onConfigure).toHaveBeenCalled()
    // No provider means no request: an app open must not spend a round trip.
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })
    expect(api.startAIDailyDigest).not.toHaveBeenCalled()
    expect(useToastStore.getState().toasts).toEqual([])
  })

  it("counts deadlines locally, with the stats block outside the component", () => {
    renderCard({
      profiles: [],
      papers: [
        paper({ id: "a", deadline: dayOffset(-3) }),
        paper({ id: "b", deadline: dayOffset(0) }),
        paper({ id: "c", deadline: dayOffset(5) }),
        paper({ id: "d", deadline: dayOffset(40) }),
        paper({ id: "e", deadline: "下周三是死线" }),
        paper({ id: "f" }),
      ],
    })
    expect(screen.getByRole("region", { name: "今日推进建议" })).toBeInTheDocument()
    const values = Array.from(document.querySelectorAll(".wb-daily dl dd")).map(
      (node) => node.textContent,
    )
    expect(values).toEqual(["6", "2", "1"])
  })
})

describe("DailyDigestCard request shape", () => {
  it("asks for the briefing in the app locale and reads the digest session once", async () => {
    renderCard()
    await screen.findAllByRole("listitem")
    expect(api.startAIDailyDigest).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.startAIDailyDigest).mock.calls[0]![0]).toEqual({
      profileID: "ai-1",
      language: "Simplified Chinese",
    })
    expect(api.getAIChat).toHaveBeenCalledWith("sess-1")
  })

  it("sends English when the app locale is English", async () => {
    useReaderStore.setState({ locale: "en-US" })
    renderCard()
    await screen.findAllByRole("listitem")
    expect(vi.mocked(api.startAIDailyDigest).mock.calls[0]![0]).toEqual({
      profileID: "ai-1",
      language: "English",
    })
  })

  it("waits for a running job before reading the session", async () => {
    vi.useFakeTimers()
    vi.mocked(api.startAIDailyDigest).mockResolvedValue({
      job: job("queued"),
      session: session([]),
    })
    vi.mocked(api.getJob)
      .mockResolvedValueOnce(job("running"))
      .mockResolvedValueOnce(job("running"))
      .mockResolvedValue(job("succeeded"))
    renderCard()
    // The auto-start is scheduled past the paint; the poll sleeps 800ms per hop.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(api.getAIChat).not.toHaveBeenCalled()
    expect(screen.getByText("正在汇总今日推进建议…")).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })
    expect(vi.mocked(api.getJob).mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(api.getAIChat).toHaveBeenCalledWith("sess-1")
    expect(screen.getAllByRole("listitem")).toHaveLength(5)
  })
})

describe("DailyDigestCard answer rendering", () => {
  it("keeps the lead line and at most five one-line items", async () => {
    renderCard()
    await screen.findAllByRole("listitem")
    expect(screen.getByRole("heading", { name: "今天先处理两份稿子。" })).toBeInTheDocument()
    const items = screen.getAllByRole("listitem").map((node) => node.textContent)
    expect(items).toEqual([
      "1完成稳健性表格",
      "2给审稿人 2 写回复",
      "3补一个工具变量",
      "4校对参考文献",
      "5提交前重读一遍",
    ])
    expect(screen.queryByText(/第六条被截断/)).toBeNull()
    expect(screen.getByText("今日已生成")).toBeInTheDocument()
  })

  it("caches the briefing for the day and does not re-request it", async () => {
    renderCard()
    await screen.findAllByRole("listitem")
    const cached = JSON.parse(localStorage.getItem(CACHE_KEY) ?? "{}") as Record<string, string>
    expect(cached.text).toBe(BRIEFING)
    cleanup()
    vi.mocked(api.startAIDailyDigest).mockClear()
    renderCard()
    await screen.findAllByRole("listitem")
    expect(screen.getByText("今天先处理两份稿子。")).toBeInTheDocument()
    expect(api.startAIDailyDigest).not.toHaveBeenCalled()
  })

  it("reports an empty briefing without inventing items", async () => {
    vi.mocked(api.getAIChat).mockResolvedValue(session([assistant("   ")]))
    renderCard()
    expect(await screen.findByText("AI 没有返回内容，请稍后重试。")).toBeInTheDocument()
    expect(screen.queryAllByRole("listitem")).toHaveLength(0)
    expect(localStorage.getItem(CACHE_KEY)).toBeNull()
  })
})

// The digest prompt asks for one JSON object; these fixtures mirror the
// contract the Go prompt spells out (kicker / headline / lead / typed blocks
// / closing / action). Providers that answer in prose or half-JSON hit the
// fallback cases at the end of this block.
const DIGEST_DOC = {
  sections: [
    {
      kicker: "我读到的你",
      headline: "最靠前的那件事是预答辩申请表，不是日程。",
      lead: "在研稿件里只有一篇的下一步还是空的。",
      blocks: [
        {
          type: "timeline",
          items: [
            { time: "10-15", label: "《数字人民币》投稿截止" },
            { time: "idle 12 天", label: "《Kiva》最近一次改动" },
          ],
        },
        {
          type: "grid",
          items: [
            { title: "先搭结构，再填内容", text: "Paper 用「日期+主题」编号" },
            { title: "第二格", text: "说明二" },
          ],
        },
        { type: "chips", items: ["论文 3 篇 · 按状态分层", "Stata 19 · R · MATLAB"] },
      ],
      closing: "先看唯一空着的那条线。",
      action: { label: "整理成一张推进视图", kind: "ask" },
    },
    {
      kicker: "稿件 · 推进状态",
      headline: "三篇稿子各停在不同地方。",
      blocks: [
        {
          type: "status-rows",
          items: [
            {
              status: "卡在数据",
              title: "A Working Paper",
              meta: "逾期 3 天 · 阶段 3/8",
              next: "把稳健性表跑完",
            },
            { title: "没有状态药丸的一行", next: "润色到位后定投" },
          ],
        },
        { type: "quote", text: "最近一次改动：6 天前补了识别部分。" },
        { type: "note", text: "预答辩表是否已交，数据里没有记录。" },
      ],
      action: { label: "一起打开逐条对", kind: "paper", paper_id: "p-1" },
    },
  ],
}
const DIGEST_TEXT = JSON.stringify(DIGEST_DOC)

function renderStructured(
  overrides: Parameters<typeof renderCard>[0] = {},
  content = DIGEST_TEXT,
) {
  vi.mocked(api.getAIChat).mockResolvedValue(session([assistant(content)]))
  return renderCard(overrides)
}

describe("DailyDigestCard structured digest document", () => {
  it("renders stacked section cards for every block type", async () => {
    renderStructured()
    await screen.findByText("最靠前的那件事是预答辩申请表，不是日程。")
    expect(document.querySelectorAll(".wb-daily--structured")).toHaveLength(1)
    expect(document.querySelectorAll(".wb-digest__section")).toHaveLength(2)
    expect(document.querySelector(".wb-digest__kicker")).toHaveTextContent("我读到的你")
    expect(
      screen.getByRole("heading", { name: "最靠前的那件事是预答辩申请表，不是日程。" }),
    ).toBeInTheDocument()
    expect(screen.getByText("在研稿件里只有一篇的下一步还是空的。")).toBeInTheDocument()
    // timeline / grid / chips / status-rows / quote / note
    expect(document.querySelectorAll(".wb-digest__timeline-item")).toHaveLength(2)
    expect(screen.getByText("10-15")).toBeInTheDocument()
    expect(document.querySelectorAll(".wb-digest__cell")).toHaveLength(2)
    expect(screen.getByText("Stata 19 · R · MATLAB")).toBeInTheDocument()
    expect(document.querySelectorAll(".wb-digest__row")).toHaveLength(2)
    expect(screen.getByText("卡在数据")).toBeInTheDocument()
    expect(screen.getAllByText("下一步：")).toHaveLength(2)
    expect(screen.getByText("把稳健性表跑完")).toBeInTheDocument()
    expect(document.querySelector(".wb-digest__quote")).toHaveTextContent("最近一次改动")
    expect(document.querySelector(".wb-digest__note")).toHaveTextContent("预答辩表是否已交")
    expect(screen.getByText("先看唯一空着的那条线。")).toBeInTheDocument()
    // The legacy numbered list is gone once the document renders.
    expect(document.querySelector(".wb-daily__items")).toBeNull()
  })

  it("parses fenced and prose-prefixed JSON the same way", async () => {
    renderStructured({}, "```json\n" + DIGEST_TEXT + "\n```")
    await screen.findByText("三篇稿子各停在不同地方。")
    expect(document.querySelectorAll(".wb-digest__section")).toHaveLength(2)
    cleanup()
    renderStructured({}, "这是今天的简报：\n" + DIGEST_TEXT + "\n以上就是全部内容。")
    await screen.findByText("三篇稿子各停在不同地方。")
    expect(document.querySelectorAll(".wb-digest__section")).toHaveLength(2)
  })

  it("degrades a truncated document to the quiet line without crashing", async () => {
    renderStructured({}, DIGEST_TEXT.slice(0, 40))
    expect(await screen.findByText("简报已返回，但没有拆出可执行的条目。")).toBeInTheDocument()
    expect(document.querySelectorAll(".wb-digest__section")).toHaveLength(0)
    expect(document.querySelector(".wb-daily--structured")).toBeNull()
  })

  it("drops headline-less sections, unknown blocks and empty item lists", async () => {
    renderStructured(
      {},
      JSON.stringify({
        sections: [
          { kicker: "缺标题就被丢掉", blocks: [{ type: "mystery", items: [] }] },
          {
            headline: "坏块都被丢掉",
            blocks: [
              { type: "timeline", items: [{ time: "10-15" }, { label: "缺时间" }, "不是对象"] },
              { type: "chips", items: [] },
            ],
          },
          { headline: "有效的网格", blocks: [{ type: "grid", items: [{ title: "格子" }] }] },
        ],
      }),
    )
    await screen.findByText("有效的网格")
    expect(screen.queryByText("缺标题就被丢掉")).toBeNull()
    expect(document.querySelectorAll(".wb-digest__section")).toHaveLength(2)
    expect(document.querySelector(".wb-digest__timeline")).toBeNull()
    expect(screen.getByText("格子")).toBeInTheDocument()
  })

  it("caps the document at four sections", async () => {
    renderStructured(
      {},
      JSON.stringify({
        sections: Array.from({ length: 6 }, (_, index) => ({ headline: `标题 ${index + 1}` })),
      }),
    )
    await screen.findByText("标题 1")
    expect(document.querySelectorAll(".wb-digest__section")).toHaveLength(4)
    expect(screen.queryByText("标题 5")).toBeNull()
  })

  it("renders action bubbles only when the jump is real", async () => {
    // Host without callbacks: the model's proposals stay unrendered.
    renderStructured()
    await screen.findByText("三篇稿子各停在不同地方。")
    expect(document.querySelectorAll(".wb-digest__action")).toHaveLength(0)
    cleanup()

    const onAskAI = vi.fn()
    const onOpenPaper = vi.fn()
    renderStructured({ onAskAI, onOpenPaper })
    await screen.findByText("三篇稿子各停在不同地方。")
    const bubbles = document.querySelectorAll(".wb-digest__action")
    expect(bubbles).toHaveLength(2)
    fireEvent.click(screen.getByRole("button", { name: "整理成一张推进视图" }))
    expect(onAskAI).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: "一起打开逐条对" }))
    expect(onOpenPaper).toHaveBeenCalledWith("p-1")
  })

  it("hides a paper bubble whose id is not among the papers", async () => {
    renderStructured(
      { onOpenPaper: vi.fn() },
      JSON.stringify({
        sections: [
          {
            headline: "引用了不存在的论文",
            action: { label: "打开它", kind: "paper", paper_id: "ghost-id" },
          },
          {
            headline: "引用了真实论文",
            action: { label: "打开这篇稿子", kind: "paper", paper_id: "p-1" },
          },
        ],
      }),
    )
    await screen.findByText("引用了真实论文")
    expect(screen.queryByText("打开它")).toBeNull()
    expect(screen.getByText("打开这篇稿子")).toBeInTheDocument()
  })

  it("caches the raw document and re-renders it without a second request", async () => {
    renderStructured()
    await screen.findByText("三篇稿子各停在不同地方。")
    const cached = JSON.parse(localStorage.getItem(CACHE_KEY) ?? "{}") as Record<string, string>
    expect(cached.text).toBe(DIGEST_TEXT)
    cleanup()
    vi.mocked(api.startAIDailyDigest).mockClear()
    renderCard()
    await screen.findByText("三篇稿子各停在不同地方。")
    expect(api.startAIDailyDigest).not.toHaveBeenCalled()
  })
})

describe("DailyDigestCard degrades every AI failure to one line", () => {
  it.each([
    [428, "需要先在偏好设置里批准隐私提示，AI 才能读取论文数据。"],
    [409, "当前 AI 配置已被停用。"],
    [503, "AI 服务当前不可用，稍后再试。"],
    [400, "工作台里还没有论文可以汇总。"],
    [500, "今日建议生成失败，请稍后重试。"],
  ])("turns HTTP %i into a quiet line and no toast", async (status, line) => {
    vi.mocked(api.startAIDailyDigest).mockRejectedValue(
      new api.APIError(status, `HTTP ${status}`, {
        title: "Nope",
        detail: `problem+json detail for ${status}`,
        code: "ai_unavailable",
        request_id: "req-1",
      }),
    )
    renderCard()
    expect(await screen.findByText(line)).toBeInTheDocument()
    expect(screen.queryAllByRole("listitem")).toHaveLength(0)
    // Once, not retried: a cold app must not hammer an unavailable provider.
    expect(api.startAIDailyDigest).toHaveBeenCalledTimes(1)
    expect(useToastStore.getState().toasts).toEqual([])
    // The signal half of the card stays useful with no answer at all.
    expect(screen.getByRole("heading", { name: "先看逾期与临近截止的论文。" })).toBeInTheDocument()
  })

  it("keeps the request error text available for support without shouting it", async () => {
    vi.mocked(api.startAIDailyDigest).mockRejectedValue(new api.APIError(503, "no provider"))
    renderCard()
    const line = await screen.findByText("AI 服务当前不可用，稍后再试。")
    expect(line).toHaveAttribute("title", "no provider")
  })

  it("shows a failed job as the generic failure line", async () => {
    vi.mocked(api.startAIDailyDigest).mockResolvedValue({ job: job("failed"), session: session([]) })
    renderCard()
    expect(await screen.findByText("今日建议生成失败，请稍后重试。")).toBeInTheDocument()
    expect(api.getAIChat).not.toHaveBeenCalled()
    expect(useToastStore.getState().toasts).toEqual([])
  })

  it("leaves the manual run in place after a failure", async () => {
    vi.mocked(api.startAIDailyDigest).mockRejectedValueOnce(
      new api.APIError(503, "no provider"),
    )
    renderCard()
    await screen.findByText("AI 服务当前不可用，稍后再试。")
    expect(api.startAIDailyDigest).toHaveBeenCalledTimes(1)
    vi.mocked(api.startAIDailyDigest).mockResolvedValueOnce({
      job: job("succeeded"),
      session: session([]),
    })
    fireEvent.click(screen.getByRole("button", { name: "生成今日建议" }))
    await waitFor(() => expect(api.startAIDailyDigest).toHaveBeenCalledTimes(2))
    await screen.findAllByRole("listitem")
  })
})
