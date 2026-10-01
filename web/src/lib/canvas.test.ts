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
  // 下限是 0：比它小的野值（-1）仍收回 0，上界与四舍五入照常收口。
  it("reels in anything outside the slider range", () => {
    expect(clampCanvasPhotoVeil(canvasPhotoVeilMin - 1)).toBe(canvasPhotoVeilMin)
    expect(clampCanvasPhotoVeil(canvasPhotoVeilMax + 1)).toBe(canvasPhotoVeilMax)
    expect(clampCanvasPhotoVeil(90.6)).toBe(91)
  })

  // 用户要能一路调到最低：0（完全没有纱）是可达的合法值，不再被抬回某个地板。
  it("keeps 0 as a reachable legal value", () => {
    expect(canvasPhotoVeilMin).toBe(0)
    expect(clampCanvasPhotoVeil(0)).toBe(0)
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
  // 12 在旧的 60% 地板之下：现在原样写进 CSS，clamp 只挡上界和野值、不再抬到下限。
  it("writes the veil through to CSS with the photo on", () => {
    applyCanvas({ theme: "meadow", photo: "data:image/jpeg;base64,AAAA", veil: 12 })
    expect(root.dataset.canvasPhoto).toBe("on")
    expect(root.style.getPropertyValue("--canvas-photo-veil")).toBe("12%")
  })

  // 拉轴最低档一路走通到 CSS 变量：0 就是 0%，即"完全没有纱"。
  it("writes the bottom of the slider as 0%", () => {
    applyCanvas({ theme: "meadow", photo: "data:image/jpeg;base64,AAAA", veil: 0 })
    expect(root.style.getPropertyValue("--canvas-photo-veil")).toBe("0%")
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
