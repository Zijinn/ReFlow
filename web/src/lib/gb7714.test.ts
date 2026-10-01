import { describe, expect, it } from "vitest"

import type { ResearchPaper } from "../api/types"
import { parseGb7714 } from "./gb7714"
import { buildGb7714Reference } from "./research"

describe("parseGb7714", () => {
  it("parses a complete Chinese journal reference with a sequence prefix", () => {
    const parsed = parseGb7714(
      "[1] 赵金阳. 非洲数字贸易规则的构建动因[J]. 国际经贸探索, 2026, 42(3): 12-25.",
    )
    expect(parsed).toEqual({
      title: "非洲数字贸易规则的构建动因",
      authors: ["赵金阳"],
      journal: "国际经贸探索",
      year: "2026",
      volume: "42",
      issue: "3",
      pages: "12-25",
      doi: "",
      url: "",
      type: "J",
    })
  })

  it("parses a complete English journal reference with multiple authors", () => {
    const parsed = parseGb7714(
      "Smith J, Brown T. The dynamics of digital trade rules[J]. Journal of Trade, 2026, 42(3): 12-25.",
    )
    expect(parsed).not.toBeNull()
    expect(parsed?.title).toBe("The dynamics of digital trade rules")
    expect(parsed?.authors).toEqual(["Smith J", "Brown T"])
    expect(parsed?.journal).toBe("Journal of Trade")
    expect(parsed?.year).toBe("2026")
    expect(parsed?.volume).toBe("42")
    expect(parsed?.issue).toBe("3")
    expect(parsed?.pages).toBe("12-25")
    expect(parsed?.type).toBe("J")
  })

  it("extracts a DOI, including a doi.org link and a trailing label", () => {
    const labelled = parseGb7714(
      "Smith J. Digital trade[J]. Journal of Trade, 2020, 5(2): 10-20. DOI:10.1234/abcd.2020.",
    )
    expect(labelled?.doi).toBe("10.1234/abcd.2020")
    expect(labelled?.url).toBe("")

    const asLink = parseGb7714(
      "Smith J. Digital trade[J]. Journal of Trade, 2020. https://doi.org/10.1234/abcd.2020.",
    )
    expect(asLink?.doi).toBe("10.1234/abcd.2020")
    expect(asLink?.url).toBe("")
  })

  it("keeps a non-DOI URL and drops the access date", () => {
    const parsed = parseGb7714(
      "张三. 数字贸易研究[J/OL]. 经贸期刊, 2021, 5(2): 10-20[2021-03-01]. https://example.cn/paper.",
    )
    expect(parsed).not.toBeNull()
    expect(parsed?.url).toBe("https://example.cn/paper")
    expect(parsed?.doi).toBe("")
    expect(parsed?.type).toBe("J/OL")
    expect(parsed?.journal).toBe("经贸期刊")
    expect(parsed?.pages).toBe("10-20")
  })

  it("accepts a reference without volume, issue or pages", () => {
    const parsed = parseGb7714("Author X. A short note[J]. Some Journal, 2020.")
    expect(parsed).not.toBeNull()
    expect(parsed?.title).toBe("A short note")
    expect(parsed?.authors).toEqual(["Author X"])
    expect(parsed?.journal).toBe("Some Journal")
    expect(parsed?.year).toBe("2020")
    expect(parsed?.volume).toBe("")
    expect(parsed?.issue).toBe("")
    expect(parsed?.pages).toBe("")
  })

  it("returns null for garbage input", () => {
    expect(parseGb7714("lorem ipsum dolor sit amet")).toBeNull()
    expect(parseGb7714("   ")).toBeNull()
    expect(parseGb7714("")).toBeNull()
  })

  it("reads non-journal type markers but keeps journal semantics for [J] only", () => {
    const monograph = parseGb7714("王某. 贸易制度研究[M]. 北京: 某出版社, 2019: 15-20.")
    expect(monograph).not.toBeNull()
    expect(monograph?.type).toBe("M")
    expect(monograph?.title).toBe("贸易制度研究")
    expect(monograph?.authors).toEqual(["王某"])
    expect(monograph?.journal).toBe("")
    expect(monograph?.year).toBe("2019")
  })

  it("round-trips buildGb7714Reference output for Chinese and English papers", () => {
    const base = {
      id: "p",
      kind: "published",
      position: 0,
      keywords: [],
      file_path: "",
      next_action: "",
      notes: "",
      research_area: "",
      status: "",
      tag_ids: [],
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
      language: "",
      volume: "42",
      issue: "3",
      pages: "12-25",
      citations: null,
      citation_source: "",
      citation_updated_at: "",
      last_updated: "",
      created_at: "",
      updated_at: "",
    } as unknown as ResearchPaper

    const zh: ResearchPaper = {
      ...base,
      title: "非洲数字贸易规则的构建动因",
      authors: ["赵金阳", "李某"],
      journal: "国际经贸探索",
      year: "2026",
      doi: "",
    }
    const zhParsed = parseGb7714(buildGb7714Reference(zh))
    expect(zhParsed?.title).toBe(zh.title)
    expect(zhParsed?.authors).toEqual(zh.authors)
    expect(zhParsed?.journal).toBe(zh.journal)
    expect(zhParsed?.year).toBe(zh.year)
    expect(zhParsed?.volume).toBe(zh.volume)
    expect(zhParsed?.issue).toBe(zh.issue)
    expect(zhParsed?.pages).toBe(zh.pages)

    const en: ResearchPaper = {
      ...base,
      title: "The dynamics of digital trade rules",
      authors: ["Smith J", "Brown T"],
      journal: "Journal of Trade",
      year: "2026",
      doi: "10.1234/trade.2026",
    }
    const enParsed = parseGb7714(`[7] ${buildGb7714Reference(en)}`)
    expect(enParsed?.title).toBe(en.title)
    expect(enParsed?.authors).toEqual(en.authors)
    expect(enParsed?.journal).toBe(en.journal)
    expect(enParsed?.doi).toBe("10.1234/trade.2026")
  })
})
