import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchTag } from "../api/types"
import { APIError } from "../api/client"
import { useReaderStore } from "../store/reader"
import { useToastStore } from "../store/toast"
import { PreferencesDialog, type PreferenceTab } from "./PreferencesDialog"

// 标签管理独立成偏好设置里的一栏（用户的原始要求：「这些标签应该在设置里面单独设一个
// 标签栏，可以在里面进行标签管理」）。这一栏是整份面板里唯一会写 research 调色板的
// 地方，所以它的每条网络出口都被替掉，测试永远碰不到 fetch。
vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>()
  return {
    ...actual,
    // 第三个入参是存下来的颜色（"" = 跟着调色板下标走），mock 要把它原样回显，
    // 否则下面的选色断言只能看见名字、看不见颜色落库。
    createResearchTag: vi.fn((name: string) =>
      Promise.resolve({ id: `t-new-${name}`, name, position: 3, color: "", color_enabled: true }),
    ),
    deleteResearchTag: vi.fn(() => Promise.resolve()),
    listPreferences: vi.fn(() => Promise.resolve({ items: {} })),
    listResearchTags: vi.fn(() =>
      Promise.resolve({
        tags: [
          { id: "t-high", name: "High", position: 0, color: "", color_enabled: true },
          { id: "t-medium", name: "Medium", position: 1, color: "", color_enabled: true },
          { id: "t-field", name: "Fieldwork", position: 2, color: "", color_enabled: true },
        ] satisfies ResearchTag[],
      }),
    ),
    putPreference: vi.fn(() => Promise.resolve({ items: {} })),
    reorderResearchTags: vi.fn(() => Promise.resolve()),
    // PATCH 的入参是"要改的那几个字段"，mock 也只回显这几个：颜色-only 的写回不该
    // 被当成改名，染色开关那一格更是只有 color_enabled 一个字段。
    updateResearchTag: vi.fn(
      (tagID: string, patch: { name?: string; color?: string; color_enabled?: boolean }) =>
        Promise.resolve({
          id: tagID,
          name: patch.name ?? "Fieldwork",
          position: 0,
          color: patch.color ?? "",
          color_enabled: patch.color_enabled ?? true,
        }),
    ),
  }
})

import * as api from "../api/client"

// 真实调用方（AppShell）是这样管的：open 是一个 state，onOpenChange 会把它关掉。
// 测试里也必须这样，否则"改名按 Esc 会不会把整页设置一起带走"这种冒泡缺陷在
// 常量 open={true} 下根本暴露不出来。
function Harness() {
  const [open, setOpen] = useState(true)
  const [activeTab, setActiveTab] = useState<PreferenceTab>("interface")
  return (
    <PreferencesDialog
      open={open}
      activeTab={activeTab}
      onOpenChange={setOpen}
      onTabChange={setActiveTab}
      theme="light"
      restorePending={false}
      error={null}
      devices={[]}
      syncAccounts={[]}
      aiProfiles={[]}
      folders={[]}
      subscriptions={[]}
      pairingCodePending={false}
      onRestore={vi.fn()}
      onCreatePairingCode={vi.fn()}
      onRevokeDevice={vi.fn()}
      onAddSyncAccount={vi.fn()}
      onEditSyncAccount={vi.fn()}
      onToggleSyncAccount={vi.fn()}
      onRunSyncAccount={vi.fn()}
      onDeleteSyncAccount={vi.fn()}
      onOrganizeLibrary={vi.fn()}
      onAddAIProfile={vi.fn()}
      onEditAIProfile={vi.fn()}
      onToggleAIProfile={vi.fn()}
      onDefaultAIProfile={vi.fn()}
      onDeleteAIProfile={vi.fn()}
    />
  )
}

function openTagPane() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  )
  // 面板默认停在「界面」，标签栏要自己点进去。
  fireEvent.click(screen.getByRole("button", { name: "Research tags" }))
  return client
}

function rowNames(): (string | null)[] {
  return Array.from(document.querySelectorAll(".pref-tag-row .pref-tag-name")).map(
    (node) => node.textContent,
  )
}

async function ready() {
  await waitFor(() => expect(document.querySelectorAll(".pref-tag-row")).toHaveLength(3))
}

// 按名字取某一行的容器：closest 的返回类型是 Element，而 within 要 HTMLElement，
// 所以颜色那几条测试统一走这个钩子，免得每处都 cast 一遍。
function rowOfName(name: string): HTMLElement {
  const row = screen.getByText(name).closest(".pref-tag-row")
  if (!row) throw new Error(`no tag row for ${name}`)
  return row as HTMLElement
}

describe("PreferencesDialog research tags", () => {
  beforeEach(() => {
    useReaderStore.setState({ locale: "en-US" })
    useToastStore.setState({ toasts: [] })
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it("lists the palette in order on its own tab", async () => {
    openTagPane()
    await ready()
    expect(rowNames()).toEqual(["High", "Medium", "Fieldwork"])
    // 色点与工作台同一套调色板下标：第一枚是 red。
    expect(
      document.querySelector(".pref-tag-row .pref-tag-name")!.closest(".pref-tag-row")!
        .querySelector(".wb-dot"),
    ).toHaveClass("wb-dot--red")
    // 首行没有更靠前的位置，尾行没有更靠后的位置。
    expect(screen.getByRole("button", { name: "Move up High" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Move down Fieldwork" })).toBeDisabled()
  })

  it("creates a tag from the pane and keeps empty input from submitting", async () => {
    openTagPane()
    await ready()
    const input = screen.getByRole("textbox", { name: "New tag" })
    expect(screen.getByRole("button", { name: "New tag" })).toBeDisabled()
    fireEvent.change(input, { target: { value: "  Placebo  " } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() => expect(api.createResearchTag).toHaveBeenCalledWith("Placebo"))
    // 建完清空，避免下一次 Enter 把旧名字再提交一遍。
    await waitFor(() => expect(input).toHaveValue(""))
  })

  it("says plainly when a name is already taken", async () => {
    vi.mocked(api.createResearchTag).mockRejectedValueOnce(new APIError(409, "duplicate"))
    openTagPane()
    await ready()
    const input = screen.getByRole("textbox", { name: "New tag" })
    fireEvent.change(input, { target: { value: "High" } })
    fireEvent.keyDown(input, { key: "Enter" })
    // 只报"创建失败"会被当成网络问题一直重试，重名要单独一句。
    await waitFor(() =>
      expect(useToastStore.getState().toasts.some((e) => /already goes by that name/i.test(e.message))).toBe(
        true,
      ),
    )
  })

  it("renames a tag with a single click", async () => {
    openTagPane()
    await ready()
    fireEvent.click(screen.getByText("Fieldwork"))
    const input = screen.getByRole("textbox", { name: "Fieldwork" })
    fireEvent.change(input, { target: { value: "Desk notes" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() =>
      expect(api.updateResearchTag).toHaveBeenCalledWith("t-field", { name: "Desk notes" }),
    )
  })

  it("cancels a rename on Escape without closing the settings dialog", async () => {
    openTagPane()
    await ready()
    fireEvent.click(screen.getByText("Fieldwork"))
    const input = screen.getByRole("textbox", { name: "Fieldwork" })
    fireEvent.change(input, { target: { value: "Abandoned" } })
    fireEvent.keyDown(input, { key: "Escape" })
    // Esc 在偏好面板里是"关掉整个对话框"的快捷键：改名格若不截住冒泡，
    // 用户一按 Esc 就发现整页设置没了，以为改名把面板弄崩了。
    expect(screen.getByRole("dialog", { name: "Research tags" })).toBeInTheDocument()
    expect(screen.queryByRole("textbox", { name: "Fieldwork" })).not.toBeInTheDocument()
    expect(rowNames()).toEqual(["High", "Medium", "Fieldwork"])
    expect(api.updateResearchTag).not.toHaveBeenCalled()
  })

  it("still lets Escape close the pane when nothing is being renamed", async () => {
    // 闸门只在编辑中抬起：不编辑时 Esc 必须照旧关掉整页设置，不然面板会变成按不动。
    openTagPane()
    await ready()
    fireEvent.keyDown(document, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("reorders the palette through the reorder endpoint", async () => {
    openTagPane()
    await ready()
    fireEvent.click(screen.getByRole("button", { name: "Move up Fieldwork" }))
    await waitFor(() =>
      expect(api.reorderResearchTags).toHaveBeenCalledWith(["t-high", "t-field", "t-medium"]),
    )
    fireEvent.click(screen.getByRole("button", { name: "Move down High" }))
    await waitFor(() =>
      expect(api.reorderResearchTags).toHaveBeenLastCalledWith(["t-medium", "t-high", "t-field"]),
    )
  })

  it("sends the picked colour on its own", async () => {
    // 后端契约是"没带的字段一律不动"，所以色块只写 color，不再重发现名。
    openTagPane()
    await ready()
    fireEvent.click(within(rowOfName("Fieldwork")).getByRole("button", { name: "Tag color: Teal" }))
    await waitFor(() => expect(api.updateResearchTag).toHaveBeenCalledWith("t-field", { color: "teal" }))
  })

  it("marks the stored colour as pressed and falls back to the palette index", async () => {
    // 色块与行首色点读的是同一个来源：自选色钉住，空串跟着下标走。
    vi.mocked(api.listResearchTags).mockResolvedValue({
      tags: [
        { id: "t-high", name: "High", position: 0, color: "violet", color_enabled: true },
        { id: "t-medium", name: "Medium", position: 1, color: "", color_enabled: true },
        { id: "t-field", name: "Fieldwork", position: 2, color: "teal", color_enabled: true },
      ],
    })
    openTagPane()
    await ready()
    const rows = Array.from(document.querySelectorAll<HTMLElement>(".pref-tag-row"))
    expect(rows).toHaveLength(3)
    expect(rows[0]!.querySelector(".wb-dot")).toHaveClass("wb-dot--violet")
    // Medium 是空串：第二个下标 → amber。
    expect(rows[1]!.querySelector(".wb-dot")).toHaveClass("wb-dot--amber")
    expect(rows[2]!.querySelector(".wb-dot")).toHaveClass("wb-dot--teal")
    expect(within(rows[0]!).getByRole("button", { name: "Tag color: Violet" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
    expect(within(rows[0]!).getByRole("button", { name: "Tag color: Auto" })).toHaveAttribute(
      "aria-pressed",
      "false",
    )
    // 空串那行的「自动」是按下态。
    expect(within(rows[1]!).getByRole("button", { name: "Tag color: Auto" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
  })

  it("writes an empty colour when 自动 takes over again", async () => {
    // 「自动」不是把颜色设成某个具体值，而是交还给下标：存回空串。
    vi.mocked(api.listResearchTags).mockResolvedValue({
      tags: [
        { id: "t-high", name: "High", position: 0, color: "violet", color_enabled: true },
        { id: "t-medium", name: "Medium", position: 1, color: "", color_enabled: true },
        { id: "t-field", name: "Fieldwork", position: 2, color: "teal", color_enabled: true },
      ],
    })
    openTagPane()
    await ready()
    const row = rowOfName("High")
    fireEvent.click(within(row).getByRole("button", { name: "Tag color: Auto" }))
    await waitFor(() => expect(api.updateResearchTag).toHaveBeenCalledWith("t-high", { color: "" }))
  })

  it("keeps quiet when the already-selected colour is clicked", async () => {
    // PATCH 成功会整栏刷新，白闪一次也是闪：同一枚色块再点不该发请求。
    vi.mocked(api.listResearchTags).mockResolvedValue({
      tags: [
        { id: "t-high", name: "High", position: 0, color: "violet", color_enabled: true },
        { id: "t-medium", name: "Medium", position: 1, color: "", color_enabled: true },
        { id: "t-field", name: "Fieldwork", position: 2, color: "teal", color_enabled: true },
      ],
    })
    openTagPane()
    await ready()
    const row = rowOfName("Fieldwork")
    expect(row.querySelector(".pref-tag-swatch--on")).toHaveClass("wb-badge--teal")
    fireEvent.click(within(row).getByRole("button", { name: "Tag color: Teal" }))
    await Promise.resolve()
    expect(api.updateResearchTag).not.toHaveBeenCalled()
  })

  it("switches the colour off without losing the pick", async () => {
    // 开关管的是"涂不涂"，不是"存了哪一档"：关掉后色点回中性、色块排淡一档，
    // 但选中那颗仍是按下态，所以重新打开回到原样。写回的 PATCH 只有开关这一个字段。
    // 三枚一组的数量与 ready() 一致：这份 mock 的返回值会留到后面的用例（afterEach
    // 只 clear 调用记录，不 reset 实现），少一枚就会把后面几条一起拖成超时。
    vi.mocked(api.listResearchTags).mockResolvedValue({
      tags: [
        { id: "t-high", name: "High", position: 0, color: "violet", color_enabled: true },
        { id: "t-medium", name: "Medium", position: 1, color: "", color_enabled: true },
        { id: "t-field", name: "Fieldwork", position: 2, color: "teal", color_enabled: false },
      ],
    })
    openTagPane()
    await ready()
    const muted = rowOfName("Fieldwork")
    expect(rowOfName("High").querySelector(".wb-dot")).toHaveClass("wb-dot--violet")
    expect(muted.querySelector(".wb-dot")).not.toHaveClass("wb-dot--teal")
    expect(muted.querySelector(".pref-tag-color-group")).toHaveClass("pref-tag-color-group--off")
    expect(within(muted).getByRole("checkbox", { name: "Use color: Fieldwork" })).not.toBeChecked()
    expect(within(muted).getByRole("button", { name: "Tag color: Teal" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )

    fireEvent.click(within(rowOfName("High")).getByRole("checkbox", { name: "Use color: High" }))
    await waitFor(() =>
      expect(api.updateResearchTag).toHaveBeenCalledWith("t-high", { color_enabled: false }),
    )
  })

  it("asks before deleting, then removes the tag", async () => {
    openTagPane()
    await ready()
    const row = screen.getByText("Fieldwork").closest(".pref-tag-row")!
    fireEvent.click(row.querySelector(".pref-tag-delete") as HTMLElement)
    // 删除会牵连所有挂着它的论文，所以先要一句带名字的确认，不能直接动手。
    expect(
      await screen.findByText(/Delete “Fieldwork”/, { selector: ".confirm-dialog__message" }),
    ).toBeInTheDocument()
    expect(api.deleteResearchTag).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.deleteResearchTag).toHaveBeenCalledWith("t-field"))
  })

  it("keeps the palette when the delete is cancelled", async () => {
    openTagPane()
    await ready()
    const row = screen.getByText("Fieldwork").closest(".pref-tag-row")!
    fireEvent.click(row.querySelector(".pref-tag-delete") as HTMLElement)
    await screen.findByText(/Fieldwork/, { selector: ".confirm-dialog__message" })
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(api.deleteResearchTag).not.toHaveBeenCalled()
    await ready()
  })

  it("refills the palette from the server after a write", async () => {
    // 增删改都要刷 ["research-tags"]，同时刷 ["research"]（工作台三种 kind 查询的
    // 公共前缀）：删除会把标签从所有论文上摘掉，工作台的 tag_ids 得跟着变。
    const client = openTagPane()
    await ready()
    const spy = vi.spyOn(client, "invalidateQueries")
    vi.mocked(api.deleteResearchTag).mockResolvedValueOnce(undefined)
    const row = screen.getByText("High").closest(".pref-tag-row")!
    fireEvent.click(row.querySelector(".pref-tag-delete") as HTMLElement)
    await screen.findByText(/High/, { selector: ".confirm-dialog__message" })
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.deleteResearchTag).toHaveBeenCalledWith("t-high"))
    await waitFor(() => {
      const keys = spy.mock.calls.map((call) => call[0]?.queryKey)
      expect(keys).toContainEqual(["research-tags"])
      expect(keys).toContainEqual(["research"])
    })
  })
})
