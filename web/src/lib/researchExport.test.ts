import { describe, expect, it } from "vitest"

import type { ResearchPaper } from "../api/types"
import { buildPublicationsExport } from "./researchExport"

function paper(overrides: Partial<ResearchPaper> = {}): ResearchPaper {
  return {
    id: "11111111-1111-1111-1111-111111111111",
    kind: "published",
    position: 0,
    title: "Growth Effects of Rail",
    authors: ["张三"],
    keywords: ["交通", "增长"],
    file_path: "",
    next_action: "",
    notes: "",
    research_area: "",
    status: "",
    tag_id: "",
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
    abstract: "摘要文本",
    journal: "经济研究",
    language: "",
    year: "2025",
    volume: "",
    issue: "",
    pages: "",
    doi: "",
    citations: 7,
    citation_source: "manual",
    citation_updated_at: "",
    last_updated: "",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

describe("buildPublicationsExport", () => {
  it("is fully self-contained: no third-party font fetch", () => {
    const html = buildPublicationsExport([paper()])
    expect(html).not.toContain("fonts.googleapis.com")
    expect(html).not.toContain("@import")
  })

  it("escapes hostile ids and uses data-detail instead of inline onclick", () => {
    const evil = `x" onmouseover="alert(1)`
    const html = buildPublicationsExport([paper({ id: evil })])
    expect(html).not.toContain("onclick=")
    expect(html).not.toContain(`onmouseover="alert`)
    expect(html).toContain("data-detail=")
    // The quote is escaped, so the attribute cannot break out.
    expect(html).toContain("&quot;")
  })

  it("renders manual and legacy sources as 手工录入", () => {
    const html = buildPublicationsExport([
      paper({ id: "a", citation_source: "manual" }),
      paper({ id: "b", citation_source: "手工录入" }),
      paper({ id: "c", citation_source: "Crossref" }),
    ])
    expect(html).toContain("来源：手工录入")
    expect(html).toContain("来源：Crossref")
  })
})
