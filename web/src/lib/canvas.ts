import type { CanvasTheme } from "../store/reader"

// 合法值清单：main.tsx 首帧前从 localStorage 拿到的只是 unknown，校验过才写进 dataset。
export const canvasThemes = ["aurora", "meadow", "orchid", "slate"] as const

export function isCanvasTheme(value: unknown): value is CanvasTheme {
  return (canvasThemes as readonly unknown[]).includes(value)
}

// 蒙纱是一根连续拉轴，不再是三档。上限 96% 是"几乎看不见图"；下限放开到 0（完全没有纱）：
// 用户明确要求能一路调到最低，代价由面板上那行安全线警告就地写明，而不是由代码替他挡在某个
// 地板之上。87 这条线还在，但它只剩提示阈值——它是量出来的：浅档白纱压到 87% 时最坏情形
// （照片里一处纯黑）画布落在 rgb(222)，11px 三级墨 4.58:1 刚过 AA，再低就破线。
export const canvasPhotoVeilMin = 0
export const canvasPhotoVeilMax = 96
export const canvasPhotoVeilSafe = 87

export const defaultCanvasPhotoVeil = 91

// 持久化值是 unknown：localStorage 可能被手改，也可能来自写死三档的旧版本，进 CSS 之前
// 一律收进拉轴区间；非数字（NaN / undefined / 字符串）直接回默认档。
export function clampCanvasPhotoVeil(value: unknown): number {
  if (typeof value !== "number" || !Number.isFinite(value)) return defaultCanvasPhotoVeil
  return Math.min(canvasPhotoVeilMax, Math.max(canvasPhotoVeilMin, Math.round(value)))
}

// --canvas-photo-src 是全站唯一把用户字符串拼进 CSS 的地方。photo 虽然只可能来自
// 我们自己的 toDataURL（base64，不含引号与括号），仍要把 `"`、`)`、`url(` 当硬闸：
// 一旦命中就整组属性删掉，绝不把可疑字符串写进 url()。
const CSS_URL_UNSAFE = /[")]|url\(/i

export function applyCanvas(input: { theme: CanvasTheme; photo: string; veil: unknown }) {
  const root = document.documentElement
  const { theme, photo, veil } = input
  root.dataset.canvas = theme

  // photo 为空必须删属性而不是写空串：CSS 只认 [data-canvas-photo="on"]，留着空串
  // 就等于声明“有背景图”，画布会去用一张不存在的图，渐变主题直接失效。
  if (photo === "" || CSS_URL_UNSAFE.test(photo)) {
    delete root.dataset.canvasPhoto
    root.style.removeProperty("--canvas-photo-src")
    root.style.removeProperty("--canvas-photo-veil")
    return
  }

  root.dataset.canvasPhoto = "on"
  root.style.setProperty("--canvas-photo-src", `url("${photo}")`)
  // 这里收一次就够了：写 CSS 的唯一入口，调用方从 store/localStorage 拿到的都可能是
  // 区间外的野值。
  root.style.setProperty("--canvas-photo-veil", `${clampCanvasPhotoVeil(veil)}%`)
}
