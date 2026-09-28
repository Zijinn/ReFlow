import type { ThemeMode } from "../store/reader"

export type ResolvedTheme = "light" | "dark"

const DARK_QUERY = "(prefers-color-scheme: dark)"

function prefersDarkScheme(): boolean {
  return window.matchMedia?.(DARK_QUERY).matches ?? false
}

// Every dark rule in styles.css is keyed on [data-theme="dark"] and the stylesheet
// has no prefers-color-scheme fallback, so "system" must be resolved to a concrete
// value before it reaches the attribute: writing "system" (or deleting the
// attribute) silently gives a dark OS the whole light palette.
export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode === "light" || mode === "dark") return mode
  return prefersDarkScheme() ? "dark" : "light"
}

export function applyTheme(mode: ThemeMode) {
  const resolved = resolveTheme(mode)
  document.documentElement.dataset.theme = resolved
  // Native widgets (scrollbars, form controls) can keep tracking the OS directly in
  // system mode, because the palette above has already resolved to the same value.
  document.documentElement.style.colorScheme = mode === "system" ? "light dark" : resolved
}
