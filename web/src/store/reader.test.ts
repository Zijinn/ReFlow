import { describe, expect, it } from "vitest"

import {
  canvasPhotoVeilMax,
  canvasPhotoVeilMin,
  canvasPhotoVeilSafe,
  defaultCanvasPhotoVeil,
} from "../lib/canvas"
import { useReaderStore } from "./reader"

describe("accentTheme", () => {
  it("defaults to academic-blue", () => {
    expect(useReaderStore.getState().accentTheme).toBe("academic-blue")
  })

  it("updates when setAccentTheme is called", () => {
    useReaderStore.getState().setAccentTheme("forest")
    expect(useReaderStore.getState().accentTheme).toBe("forest")
    useReaderStore.getState().setAccentTheme("academic-blue")
    expect(useReaderStore.getState().accentTheme).toBe("academic-blue")
  })

  it("is included in the persisted partialize output", () => {
    useReaderStore.getState().setAccentTheme("wine")
    const partialize = useReaderStore.persist.getOptions().partialize
    expect(partialize).toBeTypeOf("function")
    const persisted = partialize?.(useReaderStore.getState()) as Record<string, unknown>
    expect(persisted).toHaveProperty("accentTheme", "wine")
    useReaderStore.getState().setAccentTheme("academic-blue")
  })
})

describe("canvas preferences", () => {
  it("defaults to the aurora gradient with no photo", () => {
    expect(useReaderStore.getState().canvasTheme).toBe("aurora")
    expect(useReaderStore.getState().canvasPhoto).toBe("")
    // 默认档必须落在拉轴区间里，而且不能落在"低于安全线"的那一段：面板一打开就该是
    // 一个不需要警告的起点。
    const veil = useReaderStore.getState().canvasPhotoVeil
    expect(veil).toBeGreaterThanOrEqual(canvasPhotoVeilMin)
    expect(veil).toBeLessThanOrEqual(canvasPhotoVeilMax)
    expect(veil).toBeGreaterThanOrEqual(canvasPhotoVeilSafe)
  })

  it("is included in the persisted partialize output", () => {
    useReaderStore.getState().setCanvasTheme("orchid")
    useReaderStore.getState().setCanvasPhoto("data:image/jpeg;base64,AAAA")
    useReaderStore.getState().setCanvasPhotoVeil(canvasPhotoVeilMax)
    const partialize = useReaderStore.persist.getOptions().partialize
    expect(partialize).toBeTypeOf("function")
    const persisted = partialize?.(useReaderStore.getState()) as Record<string, unknown>
    expect(persisted).toMatchObject({
      canvasTheme: "orchid",
      canvasPhoto: "data:image/jpeg;base64,AAAA",
      canvasPhotoVeil: canvasPhotoVeilMax,
    })
    useReaderStore.getState().setCanvasTheme("aurora")
    useReaderStore.getState().setCanvasPhoto("")
    useReaderStore.getState().setCanvasPhotoVeil(defaultCanvasPhotoVeil)
  })

  // 拉轴是连续值，区间外的输入（手改 localStorage、旧版本的档位）都得在进 CSS 之前收住。
  it("clamps the veil to the slider range", () => {
    useReaderStore.getState().setCanvasPhotoVeil(canvasPhotoVeilMin - 20)
    expect(useReaderStore.getState().canvasPhotoVeil).toBe(canvasPhotoVeilMin)
    useReaderStore.getState().setCanvasPhotoVeil(canvasPhotoVeilMax + 20)
    expect(useReaderStore.getState().canvasPhotoVeil).toBe(canvasPhotoVeilMax)
    useReaderStore.getState().setCanvasPhotoVeil(defaultCanvasPhotoVeil)
    expect(useReaderStore.getState().canvasPhotoVeil).toBe(defaultCanvasPhotoVeil)
  })
})
