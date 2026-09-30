import { normalizeDoi } from "./research"

/**
 * Fields mirror `ResearchPaper` (title/authors/journal/year/volume/issue/pages/doi)
 * so a parsed reference can be turned into a patch without renaming anything.
 * `type` is the GB/T 7714 document-type marker ("J", "J/OL", "M", "C", …) and
 * `url` the trailing link; both are read out but only journal-like types ([J…])
 * feed the `journal` field.
 */
export interface ParsedReference {
  title: string
  authors: string[]
  journal: string
  year: string
  volume: string
  issue: string
  pages: string
  doi: string
  url: string
  type: string
}

const CJK = /[\u4e00-\u9fff]/
// Document-type marker such as [J], [J/OL], [M], [C], [EB/OL]. Access dates
// ([2021-03-01]) are stripped before this ever runs, and the [1] sequence
// prefix is removed up front, so digits never reach the letter-only pattern.
const TYPE_MARKER = /\[([A-Z]{1,4}(?:\/[A-Z]{1,4})?)\]/
const ACCESS_DATE = /\[\d{4}-\d{2}-\d{2}\]/g
const URL_PATTERN = /https?:\/\/[^\s[\]]+/i

/**
 * Inverse of `buildGb7714Reference` (lib/research.ts) for the common GB/T
 * 7714 journal shapes, tolerant of real-world variants: optional "[1]"
 * sequence prefix, Chinese or English author segments, [J]/[J/OL]/[M]/[C]
 * type markers, "year, volume(issue): pages" tails, fullwidth punctuation,
 * trailing DOI labels and URLs. Returns null when nothing bibliographic can
 * be read (no type marker, year, DOI or URL).
 */
export function parseGb7714(raw: string): ParsedReference | null {
  let text = (raw ?? "").replace(/\s+/g, " ").trim()
  if (!text) return null
  // Optional sequence-number prefix: "[1] 赵金阳. …"
  text = text.replace(/^\[\d+\]\s*/, "")
  // Fullwidth punctuation folded to ASCII so one set of patterns suffices.
  text = text
    .replace(/。/g, ".")
    .replace(/：/g, ": ")
    .replace(/；/g, "; ")
    .replace(/（/g, "(")
    .replace(/）/g, ")")
    .replace(/，\s*/g, ", ")
    .replace(/\s+/g, " ")
    .trim()

  let doi = ""
  let url = ""
  const urlMatch = text.match(URL_PATTERN)
  if (urlMatch) {
    const candidate = urlMatch[0].replace(/[.,;)\]]+$/, "")
    // A doi.org link is a DOI, not a generic URL.
    const asDoi = normalizeDoi(candidate)
    if (asDoi && /^10\.\d{4,9}\//i.test(asDoi)) doi = asDoi
    else url = candidate
    text = text.replace(urlMatch[0], " ")
  }
  text = text.replace(ACCESS_DATE, " ")
  if (!doi) {
    const doiMatch = text.match(/(?:\bDOI\s*:\s*)?(10\.\d{4,9}\/[^\s]+)/i)
    if (doiMatch) {
      const normalized = normalizeDoi((doiMatch[1] ?? "").replace(/[.,;)]+$/, ""))
      if (normalized) doi = normalized
      text = text.replace(doiMatch[0], " ")
    }
  }
  text = text
    .replace(/\bDOI\s*:\s*/gi, " ")
    .replace(/\s+/g, " ")
    .trim()

  // Split at the document-type marker: head = authors + title, tail = the rest.
  let type = ""
  let head = text
  let tail = ""
  const marker = text.match(TYPE_MARKER)
  if (marker && marker.index !== undefined) {
    type = (marker[1] ?? "").toUpperCase()
    head = text.slice(0, marker.index)
    tail = text.slice(marker.index + marker[0].length)
  }

  // The author segment ends at the last ". " before the title. buildGb7714Reference
  // always joins with ". "; English initials ("Smith J.") end in ". " too, but the
  // author/title boundary is the last one, so "Smith J, Brown T. Title" splits right.
  head = head.trim()
  let authorsPart = ""
  let title = head
  const boundary = head.lastIndexOf(". ")
  if (boundary > 0) {
    authorsPart = head.slice(0, boundary)
    title = head.slice(boundary + 2)
  }
  title = title.trim().replace(/\.+$/, "").trim()

  tail = tail.replace(/^\s*\.\s*/, "").trim()
  let journal = ""
  let year = ""
  let volume = ""
  let issue = ""
  let pages = ""
  const yearMatch = tail.match(/(?:^|[,;]\s*)(\d{4})(?!\d)/)
  if (yearMatch && yearMatch.index !== undefined) {
    year = yearMatch[1] ?? ""
    journal = tail
      .slice(0, yearMatch.index)
      .replace(/[,;]\s*$/, "")
      .trim()
    let rest = tail.slice(yearMatch.index + yearMatch[0].length)
    // ", 42(3)" / ", 42" — lazy up to a delimiter so "(3)" stays the issue.
    const volumeMatch = rest.match(/^\s*[,;]\s*([^(),;:]+?)\s*(?:\(([^)]*)\))?\s*(?=[(:.,;]|$)/)
    if (volumeMatch) {
      volume = (volumeMatch[1] ?? "").trim()
      issue = (volumeMatch[2] ?? "").trim()
      rest = rest.slice(volumeMatch[0]?.length ?? 0)
    } else {
      // buildGb7714Reference emits "year (issue)" when there is no volume.
      const issueOnly = rest.match(/^\s*\(([^)]*)\)/)
      if (issueOnly) {
        issue = (issueOnly[1] ?? "").trim()
        rest = rest.slice(issueOnly[0]?.length ?? 0)
      }
    }
    const pagesMatch = rest.match(/^\s*:\s*(.+?)\s*\.?\s*$/)
    if (pagesMatch) pages = (pagesMatch[1] ?? "").trim()
  } else if (tail) {
    journal = tail.replace(/\.\s*$/, "").trim()
  }
  // Only journal-like types ([J], [J/OL]) carry journal semantics; a monograph
  // or proceedings tail names a publisher/place, which must not land in journal.
  if (type && !type.startsWith("J")) journal = ""

  if (!title) return null
  if (!type && !year && !doi && !url) return null
  return {
    title,
    authors: splitAuthors(authorsPart),
    journal,
    year,
    volume,
    issue,
    pages,
    doi,
    url,
    type,
  }
}

function splitAuthors(segment: string): string[] {
  const cleaned = segment.replace(/\s*\.\s*$/, "").trim()
  if (!cleaned) return []
  if (CJK.test(cleaned)) {
    return cleaned
      .split(/[,;、]/)
      .map((name) => name.trim())
      .filter(Boolean)
  }
  return cleaned
    .split(/[,;]|\s+and\s+/i)
    .map((name) => name.trim().replace(/\.+$/, "").trim())
    .filter(Boolean)
}
