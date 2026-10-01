import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ResearchTag } from "../api/types"
import { APIError } from "../api/client"
import { useReaderStore } from "../store/reader"
import { useToastStore } from "../store/toast"
import { PreferencesDialog } from "./PreferencesDialog"

// 标签管理独立成偏好设置里的一栏（用户的原始要求：「这些标签应该在设置里面单独设一个
// 标签栏，可以在里面进行标签管理」）。这一栏是整份面板里唯一会写 research 调色板的
// 地方，所以它的每条网络出口都被替掉，测试永远碰不到 fetch。
vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>()
  return {
    ...actual,
    createResearchTag: vi.fn((name: string) =>
      Promise.resolve({ id: `t-new-${name}`, name, position: 3 }),
    ),
    deleteResearchTag: vi.fn(() => Promise.resolve()),
    listPreferences: vi.fn(() => Promise.resolve({ items: {} })),
    listResearchTags: vi.fn(() =>
      Promise.resolve({
        tags: [
          { id: "t-high", name: "High", position: 0 },
          { id: "t-medium", name: "Medium", position: 1 },
          { id: "t-field", name: "Fieldwork", position: 2 },
        ] satisfies ResearchTag[],
      }),
    ),
    putPreference: vi.fn(() => Promise.resolve({ items: {} })),
    reorderResearchTags: vi.fn(() => Promise.resolve()),
    updateResearchTag: vi.fn((tagID: string, name: string) =>
      Promise.resolve({ id: tagID, name, position: 0 }),
    ),
  }
})

import * as api from "../api/client"

// 真实调用方（AppShell）是这样管的：open 是一个 state，onOpenChange 会把它关掉。
// 测试里也必须这样，否则"改名按 Esc 会不会把整页设置一起带走"这种冒泡缺陷在
// 常量 open={true} 下根本暴露不出来。
function Harness() {
  const [open, setOpen] = useState(true)
  return (
    <PreferencesDialog
      open={open}
      onOpenChange={setOpen}
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
    await waitFor(() => expect(api.updateResearchTag).toHaveBeenCalledWith("t-field", "Desk notes"))
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
