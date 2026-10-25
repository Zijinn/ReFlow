import { spawn } from "node:child_process"
import { existsSync } from "node:fs"

if (existsSync(".env")) {
  try {
    process.loadEnvFile(".env")
  } catch (error) {
    console.error(`Could not load .env: ${error.message}`)
  }
}

const pnpm = process.platform === "win32" ? "pnpm.cmd" : "pnpm"
const detached = process.platform !== "win32"
const children = [
  spawn("go", ["run", "./cmd/reflow-server"], {
    env: process.env,
    stdio: "inherit",
    detached,
  }),
  spawn(pnpm, ["--dir", "web", "dev"], { env: process.env, stdio: "inherit", detached }),
]

let stopping = false
function stop(signal = "SIGTERM") {
  if (stopping) return
  stopping = true
  for (const child of children) {
    if (child.killed || child.pid === undefined) continue
    if (process.platform === "win32") {
      spawn("taskkill", ["/pid", String(child.pid), "/T", "/F"], { stdio: "ignore" })
    } else {
      try {
        process.kill(-child.pid, signal)
      } catch {
        child.kill(signal)
      }
    }
  }
}

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    stop(signal)
    process.exit(0)
  })
}

for (const child of children) {
  child.on("error", (error) => {
    console.error(error.message)
    stop()
    process.exitCode = 1
  })
  child.on("exit", (code, signal) => {
    if (stopping) return
    stop()
    process.exitCode = signal ? 1 : (code ?? 1)
  })
}
