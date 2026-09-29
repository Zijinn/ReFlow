import type { CanvasTheme } from "../store/reader"

// 合法值清单：main.tsx 首帧前从 localStorage 拿到的只是 unknown，校验过才写进 dataset。
export const canvasThemes = ["aurora", "meadow", "orchid", "slate"] as const

export function isCanvasTheme(value: unknown): value is CanvasTheme {
  return (canvasThemes as readonly unknown[]).includes(value)
}

// 蒙纱三档。数值写进 <html> 的 --canvas-photo-veil，但真正的下限在 CSS 里用 max()
// 守着（styles.css 实测：浅档 87% / 深档 88% 才让 11px 三级墨过 AA，因为工作台里的
// 行文字没有任何材质垫背）。三档都排在地板之上，所以用户怎么选都不会把对比度选没，
// 差别只在"照片还剩多少看得清"。
export const canvasPhotoVeilLevels = [
  { value: 88, labelKey: "canvasPhotoVeilSoft" },
  { value: 91, labelKey: "canvasPhotoVeilStandard" },
  { value: 95, labelKey: "canvasPhotoVeilStrong" },
] as const

export const defaultCanvasPhotoVeil = 91

// --canvas-photo-src 是全站唯一把用户字符串拼进 CSS 的地方。photo 虽然只可能来自
// 我们自己的 toDataURL（base64，不含引号与括号），仍要把 `"`、`)`、`url(` 当硬闸：
// 一旦命中就整组属性删掉，绝不把可疑字符串写进 url()。
const CSS_URL_UNSAFE = /[")]|url\(/i

export function applyCanvas(input: { theme: CanvasTheme; photo: string; veil: number }) {
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
  root.style.setProperty("--canvas-photo-veil", `${veil}%`)
}
