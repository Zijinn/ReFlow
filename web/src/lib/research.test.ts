import { describe, expect, it } from "vitest"

import type { ResearchPaper } from "../api/types"
import {
  buildGb7714Reference,
  buildStageTemplate,
  citationSourceKey,
  computeProgress,
  isEnglishPaper,
  MANUAL_CITATION_SOURCE,
  normalizeDoi,
  sortReferences,
  STAGE_TEMPLATE_KEYS,
  stageTicks,
  toggleStageAt,
} from "./research"

function paper(overrides: Partial<ResearchPaper>): ResearchPaper {
  return {
    id: "1",
    kind: "published",
    position: 0,
    title: "",
    authors: [],
    keywords: [],
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

describe("isEnglishPaper", () => {
  it("treats a CJK journal name as Chinese", () => {
    expect(isEnglishPaper(paper({ journal: "经济研究" }))).toBe(false)
  })
  it("treats a latin journal name as English", () => {
    expect(isEnglishPaper(paper({ journal: "American Economic Review" }))).toBe(true)
  })
  it("falls back to the language field when the journal is empty", () => {
    expect(isEnglishPaper(paper({ journal: "", language: "en" }))).toBe(true)
    expect(isEnglishPaper(paper({ journal: "", language: "zh" }))).toBe(false)
  })
})

describe("normalizeDoi", () => {
  it("strips url and doi prefixes", () => {
    expect(normalizeDoi("https://doi.org/10.1000/xyz")).toBe("10.1000/xyz")
    expect(normalizeDoi("http://dx.doi.org/10.1000/xyz")).toBe("10.1000/xyz")
    expect(normalizeDoi("https://www.doi.org/10.1000/xyz")).toBe("10.1000/xyz")
    expect(normalizeDoi("doi: 10.1000/xyz")).toBe("10.1000/xyz")
  })
  it("only strips real doi.org resolvers from urls", () => {
    // A non-resolver URL keeps its path: the old prefix regex ate
    // "https://" alone and mangled unrelated links into fake-looking DOIs.
    expect(normalizeDoi("https://example.org/a/10.1000/xyz")).toBe(
      "https://example.org/a/10.1000/xyz",
    )
  })
  it("rejects placeholder dois", () => {
    expect(normalizeDoi("10.xxxx/placeholder")).toBe("")
    expect(normalizeDoi("10.XXXX/abc")).toBe("")
    expect(normalizeDoi("")).toBe("")
  })
  it("keeps legitimate dois whose suffix contains xxxx", () => {
    expect(normalizeDoi("10.1234/xxxx_data")).toBe("10.1234/xxxx_data")
    expect(normalizeDoi("https://doi.org/10.1234/axxxxy")).toBe("10.1234/axxxxy")
  })
})

describe("citationSourceKey", () => {
  it("recognizes crossref case-insensitively", () => {
    expect(citationSourceKey("Crossref")).toBe("crossref")
    expect(citationSourceKey(" crossref ")).toBe("crossref")
  })
  it("maps the manual key and the legacy Chinese label to manual", () => {
    expect(citationSourceKey(MANUAL_CITATION_SOURCE)).toBe("manual")
    expect(citationSourceKey("手工录入")).toBe("manual")
  })
  it("returns none for empty sources", () => {
    expect(citationSourceKey("")).toBe("none")
  })
})

describe("buildGb7714Reference", () => {
  it("formats an English journal article with volume(issue) and DOI", () => {
    const ref = buildGb7714Reference(
      paper({
        authors: ["John Smith", "Jane Q Doe"],
        title: "A Study",
        journal: "American Economic Review",
        year: "2020",
        volume: "110",
        issue: "3",
        pages: "1-20",
        doi: "10.1000/xyz",
      }),
    )
    expect(ref).toBe(
      "Smith J, Doe J Q. A Study[J]. American Economic Review, 2020, 110(3): 1-20. DOI:10.1000/xyz.",
    )
  })
  it("formats a Chinese article with full-width comma authors and no DOI", () => {
    const ref = buildGb7714Reference(
      paper({
        authors: ["张三", "李四"],
        title: "一项研究",
        journal: "经济研究",
        year: "2021",
        volume: "56",
        pages: "3-18",
        doi: "10.1000/abc",
      }),
    )
    expect(ref).toBe("张三，李四. 一项研究[J]. 经济研究, 2021, 56: 3-18.")
  })
})

describe("computeProgress", () => {
  it("counts only leaf stages", () => {
    const stages = [
      { name: "a", done: true, children: [] },
      {
        name: "b",
        done: false,
        children: [
          { name: "b1", done: true, children: [] },
          { name: "b2", done: false, children: [] },
        ],
      },
    ]
    // leaves: a(done), b1(done), b2(not) => 2/3
    expect(computeProgress(stages)).toBe(67)
  })
})

describe("stageTicks", () => {
  it("folds a parent's children into one segment", () => {
    const stages = [
      { name: "a", done: true, children: [] },
      {
        name: "b",
        done: false,
        children: [
          { name: "b1", done: true, children: [] },
          { name: "b2", done: false, children: [] },
        ],
      },
    ]
    expect(stageTicks(stages)).toEqual([
      { name: "a", fraction: 1 },
      { name: "b", fraction: 0.5 },
    ])
  })

  it("keeps a done parent that has no children full", () => {
    expect(stageTicks([{ name: "a", done: true, children: [] }])).toEqual([
      { name: "a", fraction: 1 },
    ])
  })
})

describe("buildStageTemplate", () => {
  it("follows the paper pipeline from framing to submission", () => {
    expect(buildStageTemplate((key) => key).map((stage) => stage.name)).toEqual([
      "stageFrame",
      "stageLiterature",
      "stageDataCollect",
      "stageDataClean",
      "stageDescriptive",
      "stageEmpirical",
      "stageRobustness",
      "stageConclusion",
      "stageDraft",
      "stagePolish",
      "stageSubmit",
    ])
  })

  it("labels every stage and leaves it unticked", () => {
    const stages = buildStageTemplate((key) => key)
    expect(stages).toHaveLength(STAGE_TEMPLATE_KEYS.length)
    expect(stages[0]).toEqual({ name: "stageFrame", done: false, children: [] })
  })
})

describe("toggleStageAt", () => {
  it("cascades completion to children", () => {
    const stages = [
      {
        name: "b",
        done: false,
        children: [{ name: "b1", done: false, children: [] }],
      },
    ]
    const next = toggleStageAt(stages, [0])
    expect(next[0]!.done).toBe(true)
    expect(next[0]!.children[0]!.done).toBe(true)
    // original untouched
    expect(stages[0]!.done).toBe(false)
  })
})

describe("sortReferences", () => {
  it("orders newest first by default", () => {
    const list = [paper({ id: "old", year: "2010" }), paper({ id: "new", year: "2020" })]
    expect(sortReferences(list, "year-desc").map((p) => p.id)).toEqual(["new", "old"])
  })
  it("puts sole-authored papers first", () => {
    const list = [
      paper({ id: "co", authors: ["A", "B"], year: "2020" }),
      paper({ id: "solo", authors: ["A"], year: "2019" }),
    ]
    expect(sortReferences(list, "solo-first").map((p) => p.id)).toEqual(["solo", "co"])
  })
})
