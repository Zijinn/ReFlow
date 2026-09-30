import { Brain, CircleNotch, Sparkle } from "@phosphor-icons/react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { APIError, getAIChat, getJob, startAIDailyDigest } from "../../api/client"
import type { AIProfile, Job, ResearchPaper } from "../../api/types"
import { useTranslation } from "../../lib/i18n"
import { daysUntil, parseDeadline } from "./utils"

// One briefing per local day, cached in localStorage: the dashboard and the
// calendar host the same card, so switching tabs must not spend tokens twice.
const CACHE_KEY = "reflow-daily-digest"
const POLL_INTERVAL_MS = 800

type DigestFailure = { key: string; detail: string | null }

function todayToken(): string {
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

function readCache(): string | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<{ day: string; text: string }>
    if (typeof parsed.text !== "string" || parsed.day !== todayToken()) return null
    return parsed.text
  } catch {
    return null
  }
}

function writeCache(text: string) {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify({ day: todayToken(), text }))
  } catch {
    // Private-mode storage failures only lose the cache, never the answer.
  }
}

// ── Structured digest document ──────────────────────────────────────────────
// The digest prompt asks for one JSON object of section cards. Providers keep
// answering in prose, wrapping the object in ``` fences, or emitting half of
// it, so the cache stores the raw model string and every parse step below
// fails softly: the card falls back to the original `splitDigest` text path.

type DigestAction =
  | { kind: "ask"; label: string }
  | { kind: "paper"; label: string; paperID: string }

type DigestBlock =
  | { type: "timeline"; items: Array<{ time: string; label: string }> }
  | { type: "grid"; items: Array<{ title: string; text: string | null }> }
  | { type: "chips"; items: string[] }
  | {
      type: "status-rows"
      items: Array<{ status: string | null; title: string; meta: string | null; next: string | null }>
    }
  | { type: "quote"; text: string }
  | { type: "note"; text: string }

interface DigestSection {
  kicker: string | null
  headline: string
  lead: string | null
  blocks: DigestBlock[]
  closing: string | null
  action: DigestAction | null
}

type DigestDocument = DigestSection[]

const MAX_DIGEST_SECTIONS = 4
const MAX_DIGEST_BLOCKS_PER_SECTION = 4
const MAX_TIMELINE_ITEMS = 6
const MAX_GRID_ITEMS = 4
const MAX_CHIPS = 8
const MAX_STATUS_ROWS = 5

function digestString(value: unknown): string | null {
  if (typeof value !== "string") return null
  const trimmed = value.trim()
  return trimmed === "" ? null : trimmed
}

// stripFences removes a leading ``` / ```json line and a trailing ``` line. An
// unbalanced fence still strips what it can; if the remainder then fails to
// parse, the card degrades to the text path anyway.
function stripFences(raw: string): string {
  const text = raw.trim()
  if (!text.startsWith("```")) return text
  const lines = text.split(/\r?\n/)
  lines.shift()
  while (lines.length > 0 && (lines[lines.length - 1] ?? "").trim() === "") lines.pop()
  const last = lines[lines.length - 1]
  if (last !== undefined && last.trim() === "```") lines.pop()
  return lines.join("\n").trim()
}

// parseLoose accepts a bare JSON object or one buried between preamble and
// trailing prose; a truncated object simply fails both attempts.
function parseLooseJSON(text: string): unknown {
  if (!text.includes("{")) return null
  const attempts = [text]
  const start = text.indexOf("{")
  const end = text.lastIndexOf("}")
  if (start >= 0 && end > start) attempts.push(text.slice(start, end + 1))
  for (const attempt of attempts) {
    try {
      const parsed = JSON.parse(attempt) as unknown
      if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) return parsed
    } catch {
      // fall through to the next attempt, then to the text fallback
    }
  }
  return null
}

function sanitizeDigestBlock(value: unknown): DigestBlock | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  const block = value as Record<string, unknown>
  const rawItems = Array.isArray(block.items) ? block.items : []
  switch (block.type) {
    case "timeline": {
      const items = rawItems
        .map((entry) => {
          if (!entry || typeof entry !== "object") return null
          const row = entry as Record<string, unknown>
          const time = digestString(row.time)
          const label = digestString(row.label)
          return time && label ? { time, label } : null
        })
        .filter((entry): entry is { time: string; label: string } => entry !== null)
        .slice(0, MAX_TIMELINE_ITEMS)
      return items.length > 0 ? { type: "timeline", items } : null
    }
    case "grid": {
      const items = rawItems
        .map((entry) => {
          if (!entry || typeof entry !== "object") return null
          const row = entry as Record<string, unknown>
          const title = digestString(row.title)
          return title ? { title, text: digestString(row.text) } : null
        })
        .filter((entry): entry is { title: string; text: string | null } => entry !== null)
        .slice(0, MAX_GRID_ITEMS)
      return items.length > 0 ? { type: "grid", items } : null
    }
    case "chips": {
      const items = rawItems
        .map(digestString)
        .filter((chip): chip is string => chip !== null)
        .slice(0, MAX_CHIPS)
      return items.length > 0 ? { type: "chips", items } : null
    }
    case "status-rows": {
      const items = rawItems
        .map((entry) => {
          if (!entry || typeof entry !== "object") return null
          const row = entry as Record<string, unknown>
          const title = digestString(row.title)
          if (!title) return null
          return {
            status: digestString(row.status),
            title,
            meta: digestString(row.meta),
            next: digestString(row.next),
          }
        })
        .filter(
          (
            entry,
          ): entry is {
            status: string | null
            title: string
            meta: string | null
            next: string | null
          } => entry !== null,
        )
        .slice(0, MAX_STATUS_ROWS)
      return items.length > 0 ? { type: "status-rows", items } : null
    }
    case "quote":
    case "note": {
      const text = digestString(block.text)
      return text ? { type: block.type, text } : null
    }
    default:
      return null
  }
}

function sanitizeDigestAction(value: unknown): DigestAction | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  const action = value as Record<string, unknown>
  const label = digestString(action.label)
  if (!label) return null
  if (action.kind === "ask") return { kind: "ask", label }
  const paperID = digestString(action.paper_id)
  if (paperID) return { kind: "paper", label, paperID }
  return null
}

function sanitizeDigestSection(value: unknown): DigestSection | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  const section = value as Record<string, unknown>
  // The headline is the assertion the whole section argues for; without one
  // there is nothing to render, so the section is dropped rather than patched.
  const headline = digestString(section.headline)
  if (!headline) return null
  const blocks = Array.isArray(section.blocks)
    ? section.blocks
        .map(sanitizeDigestBlock)
        .filter((block): block is DigestBlock => block !== null)
        .slice(0, MAX_DIGEST_BLOCKS_PER_SECTION)
    : []
  return {
    headline,
    kicker: digestString(section.kicker),
    lead: digestString(section.lead),
    blocks,
    closing: digestString(section.closing),
    action: sanitizeDigestAction(section.action),
  }
}

function parseDigest(raw: string): DigestDocument | null {
  const parsed = parseLooseJSON(stripFences(raw))
  if (parsed === null) return null
  const sections = (parsed as Record<string, unknown>).sections
  if (!Array.isArray(sections)) return null
  const cleaned = sections
    .map(sanitizeDigestSection)
    .filter((section): section is DigestSection => section !== null)
    .slice(0, MAX_DIGEST_SECTIONS)
  return cleaned.length > 0 ? cleaned : null
}

// The legacy text answer is shaped like the reader's 今日信号 card: one lead
// line then at most five one-line items. Split on lines rather than trusting
// any markdown the provider might or might not emit.
function splitDigest(text: string): { lead: string | null; items: string[] } {
  const lines = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
  const strip = (line: string) =>
    line
      .replace(/^([-*•·]|\d+[.)])\s+/, "")
      .replace(/\*\*/g, "")
      .trim()
  const isItem = (line: string) => /^([-*•·]|\d+[.)])\s/.test(line)
  const first = lines[0]
  const lead = first && !isItem(first) ? strip(first) : null
  const rest = lead ? lines.slice(1) : lines
  return { lead, items: rest.map(strip).filter(Boolean).slice(0, 5) }
}

// Every degraded path maps to one short, honest line inside the card. Nothing
// here raises a toast: a cold app with no provider must stay quiet.
function digestFailure(error: unknown): DigestFailure {
  if (error instanceof APIError) {
    if (error.status === 428) return { key: "digestPrivacyRequired", detail: error.message }
    if (error.status === 409) return { key: "digestProfileDisabled", detail: error.message }
    if (error.status === 503) return { key: "digestUnavailable", detail: error.message }
    if (error.status === 400) return { key: "digestNoPapers", detail: error.message }
  }
  return { key: "digestFailed", detail: error instanceof Error ? error.message : null }
}

function delay(ms: number) {
  return new Promise<void>((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

function DigestBlockView(props: { block: DigestBlock; nextLabel: string }) {
  switch (props.block.type) {
    case "timeline":
      return (
        <ol className="wb-digest__timeline">
          {props.block.items.map((entry, index) => (
            <li className="wb-digest__timeline-item" key={`${index}-${entry.time}`}>
              <span className="wb-digest__timeline-time">{entry.time}</span>
              <span className="wb-digest__timeline-rail" aria-hidden="true" />
              <span className="wb-digest__timeline-label">{entry.label}</span>
            </li>
          ))}
        </ol>
      )
    case "grid":
      return (
        <div className="wb-digest__grid">
          {props.block.items.map((cell, index) => (
            <div className="wb-digest__cell" key={`${index}-${cell.title}`}>
              <strong>{cell.title}</strong>
              {cell.text && <span>{cell.text}</span>}
            </div>
          ))}
        </div>
      )
    case "chips":
      return (
        <div className="wb-digest__chips">
          {props.block.items.map((chip, index) => (
            <span className="wb-digest__chip" key={`${index}-${chip}`}>
              {chip}
            </span>
          ))}
        </div>
      )
    case "status-rows":
      return (
        <ul className="wb-digest__rows">
          {props.block.items.map((row, index) => (
            <li className="wb-digest__row" key={`${index}-${row.title}`}>
              <span className="wb-digest__row-top">
                {row.status && <span className="wb-digest__pill">{row.status}</span>}
                <strong className="wb-digest__row-title">{row.title}</strong>
              </span>
              {row.meta && <span className="wb-digest__row-meta">{row.meta}</span>}
              {row.next && (
                <span className="wb-digest__row-next">
                  <b>{props.nextLabel}</b>
                  {row.next}
                </span>
              )}
            </li>
          ))}
        </ul>
      )
    case "quote":
      return <blockquote className="wb-digest__quote">{props.block.text}</blockquote>
    case "note":
      return <p className="wb-digest__note">{props.block.text}</p>
  }
}

function DigestSectionCard(props: {
  section: DigestSection
  action: { label: string; run: () => void } | null
  nextLabel: string
}) {
  return (
    <article className="wb-digest__section">
      {props.section.kicker && (
        <p className="wb-digest__kicker">
          <i aria-hidden="true" />
          {props.section.kicker}
        </p>
      )}
      <h3 className="wb-digest__headline">{props.section.headline}</h3>
      {props.section.lead && <p className="wb-digest__lead">{props.section.lead}</p>}
      {props.section.blocks.map((block, index) => (
        <DigestBlockView block={block} key={index} nextLabel={props.nextLabel} />
      ))}
      {props.section.closing && <p className="wb-digest__closing">{props.section.closing}</p>}
      {props.action && (
        <button className="wb-digest__action" type="button" onClick={props.action.run}>
          {props.action.label}
        </button>
      )}
    </article>
  )
}

/**
 * The AI version of the reader's `TodayOverview` card. A compliant provider
 * answers with the structured document and the plan half becomes a stack of
 * section cards (kicker → assertive headline → blocks → action bubble); any
 * prose, fenced, or truncated answer degrades to the original lead + numbered
 * items list. Action bubbles render only when the host can actually perform
 * the jump they promise.
 */
export function DailyDigestCard(props: {
  papers: ResearchPaper[]
  profiles: AIProfile[]
  onConfigure?: () => void
  /** Opens the AI panel; wired by the workbench shell to the reader's toggle. */
  onAskAI?: () => void
  /** Jump to a paper's own view; only ids that exist in `papers` are honoured. */
  onOpenPaper?: (paperID: string) => void
}) {
  const { locale, t } = useTranslation()
  const [text, setText] = useState<string | null>(() => readCache())
  const [running, setRunning] = useState(false)
  const [failure, setFailure] = useState<DigestFailure | null>(null)
  const runIDRef = useRef(0)
  const autoStartedRef = useRef(false)

  const activeProfile = useMemo(
    () =>
      props.profiles.find((profile) => profile.is_default && profile.enabled) ??
      props.profiles.find((profile) => profile.enabled) ??
      null,
    [props.profiles],
  )
  // Same idiom as the operation prompts: an explicit language, never "auto".
  const language = locale === "zh-CN" ? "Simplified Chinese" : "English"

  const stats = useMemo(() => {
    let dueSoon = 0
    let overdue = 0
    for (const paper of props.papers) {
      const date = parseDeadline(paper.deadline)
      if (!date) continue
      const days = daysUntil(date)
      if (days < 0) overdue += 1
      else if (days <= 7) dueSoon += 1
    }
    return { dueSoon, overdue }
  }, [props.papers])

  const run = useCallback(async () => {
    const profileID = activeProfile?.id
    if (!profileID) return
    const runID = runIDRef.current + 1
    runIDRef.current = runID
    setRunning(true)
    setFailure(null)
    try {
      const started = await startAIDailyDigest({ profileID, language })
      let job: Job = started.job
      while (job.state === "queued" || job.state === "running") {
        await delay(POLL_INTERVAL_MS)
        if (runIDRef.current !== runID) return
        job = await getJob(started.job.id)
      }
      if (runIDRef.current !== runID) return
      if (job.state !== "succeeded") {
        setFailure({ key: "digestFailed", detail: job.error_message || null })
        return
      }
      // The digest session holds exactly one assistant message; it must never
      // be reused as a chat session, so it is read here and dropped.
      const session = await getAIChat(started.session.id)
      if (runIDRef.current !== runID) return
      let answer = ""
      for (const message of session.messages) {
        if (message.role === "assistant" && message.content.trim() !== "") answer = message.content
      }
      if (answer === "") {
        setFailure({ key: "digestEmpty", detail: null })
        return
      }
      setText(answer)
      // The raw model string is cached verbatim — JSON or prose — so the
      // cache keeps both render shapes without a second stored form.
      writeCache(answer)
    } catch (error) {
      if (runIDRef.current !== runID) return
      setFailure(digestFailure(error))
    } finally {
      if (runIDRef.current === runID) setRunning(false)
    }
  }, [activeProfile?.id, language])

  // A profile list arrives asynchronously, so this can only fire once AI is
  // actually configured — and once per mount at that. The kick is scheduled
  // after paint so a cold app never blocks first render on an AI round trip.
  useEffect(() => {
    if (!activeProfile || autoStartedRef.current) return
    autoStartedRef.current = true
    if (text !== null) return
    const timer = window.setTimeout(() => void run(), 0)
    return () => window.clearTimeout(timer)
  }, [activeProfile, run, text])

  // Unmount (tab switch, view switch) abandons an in-flight poll rather than
  // writing state on a dead component.
  useEffect(
    () => () => {
      runIDRef.current += 1
    },
    [],
  )

  // Structured first: any failure in parseDigest falls through to the original
  // line-splitting so prose answers keep rendering exactly as before.
  const doc = useMemo(() => (text ? parseDigest(text) : null), [text])
  const { lead, items } = useMemo(
    () => (doc ? { lead: null, items: [] } : splitDigest(text ?? "")),
    [doc, text],
  )
  const paperIDs = useMemo(() => new Set(props.papers.map((paper) => paper.id)), [props.papers])

  // A bubble only appears when its jump is real: "ask" needs the host's AI
  // panel callback, "paper" needs the open-paper callback plus an id that
  // actually belongs to the papers this card was handed.
  const { onAskAI, onOpenPaper } = props
  const sectionActions = useMemo(
    () =>
      (doc ?? []).map((section) => {
        const action = section.action
        if (!action) return null
        if (action.kind === "ask") {
          return onAskAI ? { label: action.label, run: onAskAI } : null
        }
        if (!onOpenPaper || !paperIDs.has(action.paperID)) return null
        const paperID = action.paperID
        return { label: action.label, run: () => onOpenPaper(paperID) }
      }),
    [doc, onAskAI, onOpenPaper, paperIDs],
  )

  const failureText = failure ? t(failure.key) : null

  return (
    <section
      className={`wb-daily${doc ? " wb-daily--structured" : ""}`}
      aria-label={t("dailyProgressPlan")}
    >
      <div className="wb-daily__signal">
        <div className="wb-daily__heading">
          <span className="wb-daily__icon" aria-hidden="true">
            <Sparkle weight="fill" />
          </span>
          <div>
            <p>{t("dailyProgressEyebrow")}</p>
            <h2>{lead ?? t("dailyProgressLeadFallback")}</h2>
          </div>
        </div>
        <p className="wb-daily__description">{t("dailyProgressDescription")}</p>
        <dl>
          <div>
            <dt>{t("digestStatPapers")}</dt>
            <dd>{props.papers.length}</dd>
          </div>
          <div>
            <dt>{t("digestStatDueSoon")}</dt>
            <dd>{stats.dueSoon}</dd>
          </div>
          <div>
            <dt>{t("digestStatOverdue")}</dt>
            <dd>{stats.overdue}</dd>
          </div>
        </dl>
        <div className="wb-daily__actions">
          {activeProfile ? (
            <button className="wb-btn" type="button" disabled={running} onClick={() => void run()}>
              {running ? <CircleNotch className="spin" /> : <Sparkle />}
              {text ? t("digestRegenerate") : t("digestGenerate")}
            </button>
          ) : props.onConfigure ? (
            <button className="wb-btn" type="button" onClick={props.onConfigure}>
              <Brain />
              {t("configureAI")}
            </button>
          ) : null}
        </div>
      </div>
      <div className="wb-daily__plan">
        <div className="wb-daily__plan-head">
          <span>
            <i />
            {t("digestPlanTitle")}
          </span>
          {text && <small>{t("digestPlanFresh")}</small>}
        </div>
        {doc ? (
          <div className="wb-digest">
            {doc.map((section, index) => (
              <DigestSectionCard
                section={section}
                action={sectionActions[index] ?? null}
                key={`${index}-${section.headline.slice(0, 24)}`}
                nextLabel={t("digestNextPrefix")}
              />
            ))}
          </div>
        ) : items.length > 0 ? (
          <ol className="wb-daily__items">
            {items.map((item, index) => (
              <li className="wb-daily__item" key={`${index}-${item.slice(0, 24)}`}>
                <span className="wb-daily__item-mark" aria-hidden="true">
                  {index + 1}
                </span>
                <span className="wb-daily__item-text">{item}</span>
              </li>
            ))}
          </ol>
        ) : running ? (
          <p className="wb-daily__quiet" role="status">
            {t("digestRunning")}
          </p>
        ) : failureText ? (
          <p className="wb-daily__quiet" title={failure?.detail ?? undefined}>
            {failureText}
          </p>
        ) : activeProfile ? (
          <p className="wb-daily__quiet">{text ? t("digestNoItems") : t("digestIdle")}</p>
        ) : (
          <p className="wb-daily__quiet">{t("digestNotConfigured")}</p>
        )}
      </div>
    </section>
  )
}
