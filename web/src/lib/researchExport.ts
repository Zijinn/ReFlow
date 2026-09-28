import type { ResearchPaper } from "../api/types"
import {
  buildGb7714Reference,
  citationSourceKey,
  isEnglishPaper,
  normalizeDoi,
  sortReferences,
} from "./research"

function escapeHtml(value: string): string {
  return value.replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!,
  )
}

// Self-contained export: no remote @import (a Google Fonts fetch would leak
// the reader's IP/UA to a third party every time the snapshot is opened).
// A system serif stack covers both Latin and CJK.
const EXPORT_CSS = `:root {
  --bg:#f5f7fb; --panel:#ffffff; --text:#172033; --muted:#7b8497; --line:#e7ebf2;
  --accent:#5367e8; --accent-soft:#eef0ff; --green:#16866a; --green-soft:#e7f7f0; --card-border:#e7ebf2;
}
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--text); font-family:Georgia,"Times New Roman","Songti SC","SimSun",serif; line-height:1.7; }
.wrap { max-width:900px; margin:0 auto; padding:48px 28px 64px; }
header.page { text-align:center; margin-bottom:36px; }
.eyebrow { color:var(--accent); font-size:11px; letter-spacing:2px; font-weight:800; }
h1 { margin:10px 0 8px; font-size:30px; letter-spacing:-.5px; }
.sub { color:var(--muted); font-size:13px; margin:0; }
.stats { margin-top:18px; display:flex; justify-content:center; gap:10px; flex-wrap:wrap; }
.stat { background:var(--panel); border:1px solid var(--card-border); border-radius:99px; padding:7px 15px; font-size:12px; color:var(--muted); }
.stat b { color:var(--text); margin-right:4px; }
.ref-section { background:var(--panel); border:1px solid var(--card-border); border-radius:16px; padding:28px 32px; margin-bottom:24px; box-shadow:0 10px 30px rgba(33,45,80,.05); }
.ref-section h2 { margin:0; font-size:18px; }
.sec-sub { color:var(--muted); font-size:12px; margin:5px 0 14px; }
ol.refs { margin:0; padding-left:0; list-style:none; }
.ref-item { padding:15px 0; border-bottom:1px solid var(--line); }
.ref-item:last-child { border-bottom:0; }
.ref-main { display:flex; align-items:baseline; gap:10px; font-size:14px; }
.ref-no { color:var(--accent); font-weight:700; flex:none; }
.ref-text { flex:1; min-width:0; }
.more-btn { flex:none; border:1px solid var(--line); background:transparent; color:var(--accent); border-radius:8px; padding:3px 10px; font-size:12px; cursor:pointer; font-weight:600; }
.more-btn:hover { background:var(--accent-soft); }
.ref-detail { display:none; margin:10px 0 2px 34px; padding:14px 16px; background:var(--bg); border:1px solid var(--line); border-radius:12px; }
.ref-detail.open { display:block; }
.d-block { display:flex; gap:12px; font-size:13px; margin-bottom:10px; }
.d-block:last-child { margin-bottom:0; }
.d-label { flex:none; width:52px; color:var(--muted); font-weight:700; padding-top:1px; }
.d-abstract { margin:0; color:#586277; line-height:1.9; flex:1; }
.d-kws { display:flex; flex-wrap:wrap; gap:6px; }
.kw { background:var(--green-soft); color:var(--green); border-radius:7px; padding:2px 9px; font-size:12px; font-weight:600; }
.d-cit { font-weight:700; }
.d-src { color:var(--muted); font-weight:400; font-size:12px; }
.d-doi { color:var(--accent); text-decoration:none; word-break:break-all; }
.d-doi:hover { text-decoration:underline; }
.d-empty { color:var(--muted); }
footer { text-align:center; color:var(--muted); font-size:12px; margin-top:10px; }
@media (max-width:640px) { .wrap { padding:32px 16px 48px; } .ref-section { padding:20px 18px; } .ref-detail { margin-left:0; } .ref-main { flex-wrap:wrap; } }
@media print { .ref-detail { display:block !important; } .more-btn { display:none; } body { background:#fff; } }`

function nowMinute(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function citationSourceLabel(source: string): string {
  // Map the stable enum key (and the legacy hard-coded label) to the
  // display name; the export document is Chinese-only by design.
  const key = citationSourceKey(source)
  if (key === "crossref") return "Crossref"
  return "手工录入"
}

function itemHtml(paper: ResearchPaper, index: number): string {
  const ref = buildGb7714Reference(paper)
  const abstract = paper.abstract || ""
  const keywords = paper.keywords || []
  const citations = paper.citations
  const source = citationSourceLabel(paper.citation_source)
  const doi = normalizeDoi(paper.doi)
  // paper.id is escaped into the attribute value; the toggle handler looks
  // it up via dataset + getElementById instead of an inline onclick string.
  const detailId = escapeHtml(`d-${paper.id}`)
  return `<li class="ref-item">
    <div class="ref-main"><span class="ref-no">[${index + 1}]</span><span class="ref-text">${escapeHtml(ref)}</span><button class="more-btn" type="button" data-detail="${detailId}">查看更多</button></div>
    <div class="ref-detail" id="${detailId}">
      <div class="d-block"><span class="d-label">摘要</span><p class="d-abstract">${abstract ? escapeHtml(abstract) : "（暂无摘要）"}</p></div>
      <div class="d-block"><span class="d-label">关键词</span><span class="d-kws">${keywords.length ? keywords.map((k) => `<span class="kw">${escapeHtml(k)}</span>`).join("") : '<span class="d-empty">（暂无关键词）</span>'}</span></div>
      <div class="d-block"><span class="d-label">引用</span><span class="d-cit">${citations ?? "—"} 次<span class="d-src">（来源：${escapeHtml(source)}）</span></span></div>
      ${doi ? `<div class="d-block"><span class="d-label">DOI</span><a class="d-doi" href="https://doi.org/${escapeHtml(doi)}" target="_blank" rel="noopener">https://doi.org/${escapeHtml(doi)}</a></div>` : ""}
    </div>
  </li>`
}

function sectionHtml(title: string, papers: ResearchPaper[]): string {
  if (!papers.length) return ""
  return `<section class="ref-section"><h2>${title}</h2><p class="sec-sub">${papers.length} 篇 · GB/T 7714-2025 · 顺序编码制</p><ol class="refs">${papers.map(itemHtml).join("")}</ol></section>`
}

// buildPublicationsExport renders a fully self-contained static HTML snapshot
// with every publication's data hard-coded, matching the original dashboard's
// one-click export.
export function buildPublicationsExport(published: ResearchPaper[]): string {
  const zh = sortReferences(
    published.filter((p) => !isEnglishPaper(p)),
    "year-desc",
  )
  const en = sortReferences(
    published.filter((p) => isEnglishPaper(p)),
    "year-desc",
  )
  const totalCites = published.reduce((sum, p) => sum + (Number(p.citations) || 0), 0)
  const exportedAt = nowMinute()
  return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>已发表论文 · Research Workspace</title>
<style>${EXPORT_CSS}</style>
</head>
<body>
<div class="wrap">
  <header class="page">
    <div class="eyebrow">RESEARCH WORKSPACE · PUBLICATIONS</div>
    <h1>已发表论文</h1>
    <p class="sub">按 GB/T 7714-2025 顺序编码制生成 · 导出于 ${exportedAt}</p>
    <div class="stats">
      <span class="stat"><b>${zh.length}</b>中文发表</span>
      <span class="stat"><b>${en.length}</b>英文发表</span>
      <span class="stat"><b>${totalCites}</b>总引用</span>
    </div>
  </header>
  ${sectionHtml("中文发表", zh)}
  ${sectionHtml("英文发表", en)}
  <footer>由 ReFlow 科研工作台生成 · 数据为导出时刻快照</footer>
</div>
<script>
document.addEventListener("click", function (e) {
  var btn = e.target && e.target.closest ? e.target.closest(".more-btn") : null;
  if (!btn) return;
  var id = btn.getAttribute("data-detail");
  var d = id ? document.getElementById(id) : null;
  if (!d) return;
  var open = d.classList.toggle("open");
  btn.textContent = open ? "收起" : "查看更多";
});
</script>
</body>
</html>`
}

export function downloadPublicationsExport(published: ResearchPaper[]): void {
  const html = buildPublicationsExport(published)
  const blob = new Blob([html], { type: "text/html;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement("a")
  const stamp = new Date().toISOString().slice(0, 10).replace(/-/g, "")
  anchor.href = url
  anchor.download = `ResearchWorkspace-Publications-${stamp}.html`
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
