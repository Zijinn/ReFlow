import { describe, expect, it } from "vitest"

import { canvasPhotoVeilLevels, defaultCanvasPhotoVeil } from "../lib/canvas"
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
    // 默认值必须是三档之一，否则偏好面板里没有一个选项是选中的
    expect(canvasPhotoVeilLevels.map((level) => level.value)).toContain(
      useReaderStore.getState().canvasPhotoVeil,
    )
  })

  it("is included in the persisted partialize output", () => {
    useReaderStore.getState().setCanvasTheme("orchid")
    useReaderStore.getState().setCanvasPhoto("data:image/jpeg;base64,AAAA")
    useReaderStore.getState().setCanvasPhotoVeil(canvasPhotoVeilLevels[2].value)
    const partialize = useReaderStore.persist.getOptions().partialize
    expect(partialize).toBeTypeOf("function")
    const persisted = partialize?.(useReaderStore.getState()) as Record<string, unknown>
    expect(persisted).toMatchObject({
      canvasTheme: "orchid",
      canvasPhoto: "data:image/jpeg;base64,AAAA",
      canvasPhotoVeil: canvasPhotoVeilLevels[2].value,
    })
    useReaderStore.getState().setCanvasTheme("aurora")
    useReaderStore.getState().setCanvasPhoto("")
    useReaderStore.getState().setCanvasPhotoVeil(defaultCanvasPhotoVeil)
  })
})
