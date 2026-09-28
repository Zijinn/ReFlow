export type DesktopPlatform = "macos" | "windows" | null
export type DesktopWindowAction = "minimise" | "maximise" | "close"

interface WailsHostWindow extends Window {
  _wails?: {
    environment?: {
      OS?: string
    }
  }
}

export function desktopPlatform(host: WailsHostWindow = window): DesktopPlatform {
  const os = host._wails?.environment?.OS
  if (os === "darwin") return "macos"
  if (os === "windows") return "windows"
  return null
}

export function applyDesktopPlatform() {
  const platform = desktopPlatform()
  if (!platform) return false
  document.documentElement.dataset.desktop = platform
  return true
}

// Wails injects window._wails.environment from the native side, which can land
// after this module evaluates — the runtime's own drag handler polls for it the
// same way (drag.js "last resort, poll for environment"). Without the retry the
// macOS titlebar inset never applies and the brand mark sits under the traffic
// lights, while the caption buttons render because WorkspaceHeader re-reads the
// platform during a later React render.
export function trackDesktopPlatform({ intervalMs = 50, maxTicks = 100 } = {}) {
  if (applyDesktopPlatform()) return
  let ticks = 0
  const timer = window.setInterval(() => {
    ticks += 1
    if (applyDesktopPlatform() || ticks >= maxTicks) window.clearInterval(timer)
  }, intervalMs)
}

export async function controlDesktopWindow(action: DesktopWindowAction) {
  if (desktopPlatform() !== "windows") return

  const { Window: desktopWindow } = await import("@wailsio/runtime")
  if (action === "minimise") await desktopWindow.Minimise()
  if (action === "maximise") await desktopWindow.ToggleMaximise()
  if (action === "close") await desktopWindow.Close()
}
