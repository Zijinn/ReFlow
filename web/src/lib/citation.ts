// Citation formatting for the literature workflow. Both formats are built
// from feed metadata only — title, authors, source title as the container,
// publication date, DOI, and URL — which is what journal RSS/Atom feeds
// reliably carry.

import type { Entry } from "../api/types"
import { splitAuthors } from "./metadata"

function bibtexKey(entry: Entry): string {
  const firstAuthor = splitAuthors(entry.author)[0] ?? ""
  const surname = firstAuthor.includes(" ")
    ? (firstAuthor.split(" ").pop() ?? firstAuthor)
    : firstAuthor
  const year = new Date(entry.published_at).getFullYear()
  const firstTitleWord = entry.title
    .split(/\s+/)[0]
    ?.replace(/[^A-Za-z0-9]/g, "")
    .toLowerCase()
  return `${surname.replace(/[^A-Za-z0-9]/g, "") || "entry"}${Number.isNaN(year) ? "" : year}${firstTitleWord ?? ""}`
}

function stripControlCharacters(value: string): string {
  let output = ""
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0
    output += code < 32 || code === 127 ? " " : character
  }
  return output
}

function bibtexEscape(value: string): string {
  return stripControlCharacters(value)
    .replace(/[{}]/g, "")
    .replace(/\\/g, "\\textbackslash{}")
    .replace(/([&%$#_])/g, "\\$1")
}

/** BibTeX @article entry; falls back to url when the entry has no DOI. */
export function formatBibTeX(entry: Entry): string {
  const authors = splitAuthors(entry.author)
  const date = new Date(entry.published_at)
  const fields: Array<[string, string]> = []
  if (authors.length > 0) fields.push(["author", authors.join(" and ")])
  fields.push(["title", entry.title])
  if (entry.feed_title) fields.push(["journaltitle", entry.feed_title])
  if (!Number.isNaN(date.getTime())) fields.push(["year", String(date.getFullYear())])
  if (entry.doi) fields.push(["doi", entry.doi])
  if (entry.canonical_url) fields.push(["url", entry.canonical_url])
  const body = fields.map(([key, value]) => `  ${key} = {${bibtexEscape(value)}}`).join(",\n")
  return `@article{${bibtexKey(entry)},\n${body}\n}`
}

/**
 * GB/T 7714-2015 numeric style for an electronic journal article:
 * 主要责任者. 题名[J/OL]. 刊名, 年. 出处（DOI 或 URL）.
 */
export function formatGBT7714(entry: Entry): string {
  const authors = splitAuthors(entry.author)
  const date = new Date(entry.published_at)
  const year = Number.isNaN(date.getTime()) ? "" : String(date.getFullYear())
  const segments: string[] = []
  const authorText = authors.length > 0 ? authors.join(", ") : ""
  segments.push(authorText ? `${authorText}. ` : "")
  segments.push(`${entry.title}[J/OL]. `)
  if (entry.feed_title) {
    segments.push(`${entry.feed_title}${year ? `, ${year}` : ""}. `)
  } else if (year) {
    segments.push(`${year}. `)
  }
  const locator = entry.doi
    ? `https://doi.org/${entry.doi}`
    : (entry.canonical_url ?? "")
  if (locator) segments.push(`${locator}.`)
  return segments.join("").trim()
}
