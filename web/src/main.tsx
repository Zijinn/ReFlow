// Source Serif 4 backs the optional serif reading mode. Manrope was dropped: it
// sat after the system faces in --font-ui, so it never rendered a glyph.
import "@fontsource-variable/source-serif-4"
import "./styles.css"
import "./components/workbench/phase3-research.css"
import "./components/workbench/phase3-published.css"
import "./components/workbench/phase3-submitted.css"
import "./components/workbench/phase4.css"
import "./components/workbench/phase5.css"
import "./components/workbench/phase6.css"
import "./components/workbench/phase7-notes.css"
import "./components/workbench/phase8-shell-actions.css"
import "./components/workbench/phase9-ai-context.css"
import "./components/workbench/phase10-digest.css"

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { registerSW } from "virtual:pwa-register"

import App from "./App"
import { applyCanvas, isCanvasTheme } from "./lib/canvas"
import { trackDesktopPlatform } from "./lib/desktop"
import { installScrollReveal } from "./lib/scroll-reveal"
import { applyTheme } from "./lib/theme"

trackDesktopPlatform()
applyPersistedTheme()
// 滚动条"滚才显形"是一个全局捕获监听（scroll 不冒泡，组件里各绑各的会漏掉表格深处的
// 笔记格），所以挂在这里，不进 React 生命周期。
installScrollReveal()

// persist 存的是偏好片段，旧版本没有画布那三个键，所以这里全部按 unknown 读再回退默认。
interface PersistedPreferences {
  theme?: unknown
  accentTheme?: unknown
  canvasTheme?: unknown
  canvasPhoto?: unknown
  canvasPhotoVeil?: unknown
}

// Apply the persisted theme before first paint so a dark/light preference does
// not flash the wrong palette while React boots. AppShell takes the attribute over
// on mount and keeps it in sync, so both use applyTheme.
function applyPersistedTheme() {
  let persisted: PersistedPreferences | null
  try {
    persisted = (
      JSON.parse(localStorage.getItem("reflow-reader-preferences") ?? "null") as {
        state?: PersistedPreferences
      } | null
    )?.state ?? null
  } catch {
    persisted = null
  }
  const theme = persisted?.theme
  applyTheme(theme === "light" || theme === "dark" || theme === "system" ? theme : "system")
  const accentThemes = [
    "academic-blue",
    "graphite",
    "forest",
    "wine",
    "indigo",
    "warm-paper",
  ] as const
  const accentTheme = persisted?.accentTheme
  document.documentElement.dataset.accent = accentThemes.includes(
    accentTheme as (typeof accentThemes)[number],
  )
    ? (accentTheme as string)
    : "academic-blue"

  // 画布偏好共用这一次解析：canvasPhoto 是几十万字节的 data URL，读两遍等于首帧前多解
  // 一次 JSON。store rehydrate 之后 AppShell 会再 applyCanvas 一遍，值相同、写入幂等，
  // 所以中间不会有闪烁。
  const canvasTheme = persisted?.canvasTheme
  const canvasPhoto = persisted?.canvasPhoto
  applyCanvas({
    theme: isCanvasTheme(canvasTheme) ? canvasTheme : "aurora",
    photo: typeof canvasPhoto === "string" ? canvasPhoto : "",
    veil: persisted?.canvasPhotoVeil,
  })
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchOnWindowFocus: true,
    },
  },
})

registerSW({ immediate: true })

const root = document.getElementById("root")
if (!root) throw new Error("Missing root element")

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
