import { describe, expect, it } from "vitest"

import { sanitizeArticleHTML } from "./articleHtml"

describe("sanitizeArticleHTML", () => {
  it("keeps readable article markup", () => {
    const cleaned = sanitizeArticleHTML("<p>Body</p><math><mi>x</mi></math>")
    expect(cleaned).toContain("<p>Body</p>")
    expect(cleaned).toContain("<math>")
  })

  it("removes forms, controls and inline layout overlays", () => {
    const cleaned = sanitizeArticleHTML(
      '<p>OK</p><form action="https://evil.example"><input name="password"><button type="submit">Login</button></form><div style="position:fixed;inset:0;z-index:9999">overlay</div>',
    )
    expect(cleaned).toContain("<p>OK</p>")
    expect(cleaned).not.toContain("<form")
    expect(cleaned).not.toContain("<input")
    expect(cleaned).not.toContain("<button")
    expect(cleaned).not.toContain("style=")
    expect(cleaned).not.toContain("position:fixed")
  })
})
