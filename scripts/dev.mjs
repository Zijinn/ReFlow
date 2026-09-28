import { spawn } from "node:child_process"

const pnpm = process.platform === "win32" ? "pnpm.cmd" : "pnpm"
const children = [
  spawn("go", ["run", "./cmd/reflow-server"], { env: process.env, stdio: "inherit" }),
  spawn(pnpm, ["--dir", "web", "dev"], { env: process.env, stdio: "inherit" }),
]

let stopping = false
function stop(signal = "SIGTERM") {
  if (stopping) return
  stopping = true
  for (const child of children) {
    if (!child.killed) child.kill(signal)
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
