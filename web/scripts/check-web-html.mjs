import { readFile } from "node:fs/promises"

// Guards `pnpm build` output. reflow-server serves web/dist as plain static
// files, so the entry must stay an ES module script; the desktop rewrite
// (check-desktop-html.mjs) must not leak into this build or the page renders
// blank. Run after `vite build` and before the desktop build overwrites dist.
const html = await readFile(new URL("../dist/index.html", import.meta.url), "utf8")
const entry = html.match(/<script\b[^>]*\bsrc="\/assets\/[^"]+\.js"[^>]*>/)?.[0]

if (!entry) {
  throw new Error("web build HTML must load a bundled entry script")
}
if (!/\btype="module"/.test(entry)) {
  throw new Error('web build HTML must load the ESM entry with type="module"')
}

const entryPath = entry.match(/\bsrc="([^"]+)"/)?.[1]
const javascript = await readFile(new URL(`../dist${entryPath}`, import.meta.url), "utf8")
if (!/(^|[;}])\s*export(\s|[{*])/.test(javascript)) {
  throw new Error("web build entry is not an ES module; check vite build format")
}
