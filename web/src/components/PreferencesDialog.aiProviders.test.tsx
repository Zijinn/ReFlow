import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { AIProfile } from "../api/types"
import { useReaderStore } from "../store/reader"
import { PreferencesDialog, type PreferenceTab } from "./PreferencesDialog"

// 提供商行的编辑入口是这一份测试的重点，面板内部自带的标签查询全部替掉，
// 测试永远碰不到 fetch。
vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>()
  return {
    ...actual,
    listPreferences: vi.fn(() => Promise.resolve({ items: {} })),
    listResearchTags: vi.fn(() => Promise.resolve({ tags: [] })),
    createResearchTag: vi.fn(),
    deleteResearchTag: vi.fn(),
    reorderResearchTags: vi.fn(),
    updateResearchTag: vi.fn(),
    putPreference: vi.fn(() => Promise.resolve({ items: {} })),
  }
})

const gateway: AIProfile = {
  id: "profile-1",
  provider: "openai_compatible",
  name: "Lab gateway",
  endpoint: "https://gateway.example.edu/v1",
  model: "gpt-4.1-mini",
  enabled: true,
  allow_private_network: false,
  remote_content_approved: true,
  is_default: true,
  last_used_at: null,
  last_error_code: null,
  last_error_message: null,
  created_at: "2026-07-22T00:00:00Z",
  updated_at: "2026-07-22T00:00:00Z",
}

const localOllama: AIProfile = {
  id: "profile-2",
  provider: "ollama",
  name: "Local Ollama",
  endpoint: "http://127.0.0.1:11434",
  model: "qwen3:8b",
  enabled: false,
  allow_private_network: true,
  remote_content_approved: false,
  is_default: false,
  last_used_at: null,
  last_error_code: null,
  last_error_message: "AI request timed out",
  created_at: "2026-07-22T00:00:00Z",
  updated_at: "2026-07-22T00:00:00Z",
}

const profiles: AIProfile[] = [gateway, localOllama]

const handlers = {
  onAddAIProfile: vi.fn(),
  onEditAIProfile: vi.fn(),
  onToggleAIProfile: vi.fn(),
  onDefaultAIProfile: vi.fn(),
  onDeleteAIProfile: vi.fn(),
}

// 真实调用方（AppShell）把 open 和当前栏位放在 state 里，onOpenChange 负责关。
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
      aiProfiles={profiles}
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
      onAddAIProfile={handlers.onAddAIProfile}
      onEditAIProfile={handlers.onEditAIProfile}
      onToggleAIProfile={handlers.onToggleAIProfile}
      onDefaultAIProfile={handlers.onDefaultAIProfile}
      onDeleteAIProfile={handlers.onDeleteAIProfile}
    />
  )
}

function openAIPane() {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
      }
    >
      <Harness />
    </QueryClientProvider>,
  )
  fireEvent.click(screen.getByRole("button", { name: "AI & language" }))
}

describe("PreferencesDialog AI providers", () => {
  beforeEach(() => {
    useReaderStore.setState({ locale: "en-US" })
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it("edits a provider in place instead of forcing a re-add", async () => {
    openAIPane()
    const edit = await screen.findByRole("button", { name: "Edit Lab gateway" })
    expect(edit).toHaveAttribute("title", "Edit AI provider")
    fireEvent.click(edit)
    await waitFor(() => expect(handlers.onEditAIProfile).toHaveBeenCalledWith(gateway))
  })

  it("still exposes the add, default and delete actions next to edit", async () => {
    openAIPane()
    await screen.findByRole("button", { name: "Edit Lab gateway" })

    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    expect(handlers.onAddAIProfile).toHaveBeenCalledTimes(1)
    // 已是默认的那一行不能把自己再设一遍。
    expect(screen.getByRole("button", { name: /Use by default Lab gateway/ })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: /Use by default Local Ollama/ }))
    expect(handlers.onDefaultAIProfile).toHaveBeenCalledWith("profile-2")
    fireEvent.click(screen.getByRole("button", { name: "Delete Local Ollama" }))
    expect(handlers.onDeleteAIProfile).toHaveBeenCalledWith(localOllama.id)
    expect(screen.getByRole("checkbox", { name: "Enable Local Ollama" })).not.toBeChecked()
  })
})
