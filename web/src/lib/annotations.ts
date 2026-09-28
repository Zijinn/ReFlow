export type AnnotationStyle = "highlight" | "underline" | "wavy"

export interface ReaderAnnotation {
  id: string
  entryID: string
  quote: string
  prefix: string
  suffix: string
  style: AnnotationStyle
  note: string
  createdAt: string
}

export interface SerializedSelection {
  quote: string
  prefix: string
  suffix: string
}

interface TextSegment {
  node: Text
  start: number
  end: number
}

const CONTEXT_LENGTH = 48

function textSegments(root: HTMLElement): TextSegment[] {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const segments: TextSegment[] = []
  let offset = 0
  let current = walker.nextNode()
  while (current) {
    const node = current as Text
    const length = node.data.length
    segments.push({ node, start: offset, end: offset + length })
    offset += length
    current = walker.nextNode()
  }
  return segments
}

function unwrapAnnotations(root: HTMLElement) {
  const wrappers = Array.from(root.querySelectorAll<HTMLElement>("[data-reflow-annotation]"))
  for (const wrapper of wrappers.reverse()) wrapper.replaceWith(...Array.from(wrapper.childNodes))
  root.normalize()
}

function findQuote(text: string, annotation: ReaderAnnotation): number {
  let bestIndex = -1
  let bestScore = -1
  let index = text.indexOf(annotation.quote)
  while (index >= 0) {
    const prefix = text.slice(Math.max(0, index - annotation.prefix.length), index)
    const suffix = text.slice(index + annotation.quote.length, index + annotation.quote.length + annotation.suffix.length)
    const score = Number(prefix.endsWith(annotation.prefix)) + Number(suffix.startsWith(annotation.suffix))
    if (score > bestScore) {
      bestIndex = index
      bestScore = score
    }
    index = text.indexOf(annotation.quote, index + Math.max(1, annotation.quote.length))
  }
  return bestIndex
}

function wrapRange(root: HTMLElement, annotation: ReaderAnnotation) {
  const segments = textSegments(root)
  const text = segments.map((segment) => segment.node.data).join("")
  const start = findQuote(text, annotation)
  if (start < 0) return
  const end = start + annotation.quote.length
  const affected = segments.filter((segment) => segment.end > start && segment.start < end)

  for (const segment of affected.reverse()) {
    const localStart = Math.max(0, start - segment.start)
    const localEnd = Math.min(segment.node.data.length, end - segment.start)
    if (localEnd <= localStart) continue
    const selected = segment.node.splitText(localStart)
    selected.splitText(localEnd - localStart)
    const wrapper = document.createElement("span")
    wrapper.className = `reader-annotation reader-annotation--${annotation.style}`
    wrapper.dataset.reflowAnnotation = annotation.id
    if (annotation.note) wrapper.title = annotation.note
    selected.replaceWith(wrapper)
    wrapper.append(selected)
  }
}

export function serializeSelection(root: HTMLElement, range: Range): SerializedSelection | null {
  if (!root.contains(range.commonAncestorContainer)) return null
  const rawQuote = range.toString()
  const quote = rawQuote.trim()
  if (!quote) return null

  const before = document.createRange()
  before.selectNodeContents(root)
  before.setEnd(range.startContainer, range.startOffset)
  const leadingWhitespace = rawQuote.length - rawQuote.trimStart().length
  const start = before.toString().length + leadingWhitespace
  const text = root.textContent ?? ""

  return {
    quote,
    prefix: text.slice(Math.max(0, start - CONTEXT_LENGTH), start),
    suffix: text.slice(start + quote.length, start + quote.length + CONTEXT_LENGTH),
  }
}

export function applyAnnotations(root: HTMLElement, annotations: ReaderAnnotation[]) {
  unwrapAnnotations(root)
  for (const annotation of annotations) wrapRange(root, annotation)
}
