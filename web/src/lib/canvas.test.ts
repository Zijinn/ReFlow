import { afterEach, describe, expect, it } from "vitest"

import {
  applyCanvas,
  canvasPhotoVeilMax,
  canvasPhotoVeilMin,
  clampCanvasPhotoVeil,
  defaultCanvasPhotoVeil,
} from "./canvas"

const root = document.documentElement

afterEach(() => {
  delete root.dataset.canvas
  delete root.dataset.canvasPhoto
  root.style.removeProperty("--canvas-photo-src")
  root.style.removeProperty("--canvas-photo-veil")
})

describe("clampCanvasPhotoVeil", () => {
  it("reels in anything outside the slider range", () => {
    expect(clampCanvasPhotoVeil(canvasPhotoVeilMin - 1)).toBe(canvasPhotoVeilMin)
    expect(clampCanvasPhotoVeil(canvasPhotoVeilMax + 1)).toBe(canvasPhotoVeilMax)
    expect(clampCanvasPhotoVeil(90.6)).toBe(91)
  })

  // 持久化值是 unknown：旧版本存的档位、手改 localStorage、序列化丢精度都可能送进来
  // 一个非数字，直接拼进 CSS 会得到 `--canvas-photo-veil: undefined%`。
  it("falls back to the default for non-numbers", () => {
    for (const value of [undefined, null, NaN, "91", Infinity, {}]) {
      expect(clampCanvasPhotoVeil(value)).toBe(defaultCanvasPhotoVeil)
    }
  })
})

describe("applyCanvas", () => {
  it("writes the clamped veil with the photo on", () => {
    applyCanvas({ theme: "meadow", photo: "data:image/jpeg;base64,AAAA", veil: 12 })
    expect(root.dataset.canvasPhoto).toBe("on")
    expect(root.style.getPropertyValue("--canvas-photo-veil")).toBe(`${canvasPhotoVeilMin}%`)
  })

  it("takes the photo attributes off when there is no usable photo", () => {
    applyCanvas({ theme: "aurora", photo: "", veil: 91 })
    expect(root.dataset.canvasPhoto).toBeUndefined()
    expect(root.style.getPropertyValue("--canvas-photo-src")).toBe("")
  })

  // photo 只可能来自我们自己的 toDataURL，但它仍然是拼进 url() 的用户字符串：
  // 命中括号/引号就整组属性删掉，而不是截断后写进去。
  it("refuses a photo string that could escape url()", () => {
    applyCanvas({ theme: "aurora", photo: `url("x")`, veil: 91 })
    expect(root.dataset.canvasPhoto).toBeUndefined()
    expect(root.style.getPropertyValue("--canvas-photo-veil")).toBe("")
  })
})
