import DOMPurify, { type Config } from "dompurify"

// Article bodies are feed/snapshot input. The server's bluemonday policy is the
// first line; this is the client-side boundary, so keep it narrower than the
// permissive DOMPurify HTML profile: forms, embedded chrome and inline layout
// styles have no place inside the reader and enable UI redressing.
export const articleSanitizeConfig: Config = {
  USE_PROFILES: { html: true, mathMl: true },
  FORBID_TAGS: [
    "form",
    "input",
    "button",
    "select",
    "textarea",
    "style",
    "link",
    "meta",
    "iframe",
    "object",
    "embed",
  ],
  FORBID_ATTR: ["style", "action", "formaction", "srcdoc"],
}

export function sanitizeArticleHTML(html: string): string {
  return DOMPurify.sanitize(html, articleSanitizeConfig)
}
