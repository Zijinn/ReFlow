import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { CreateAIProfileInput } from "../api/client"
import type { AIProfile, AIProvider } from "../api/types"
import { useReaderStore } from "../store/reader"
import { AIProfileDialog, type EditAIProfileInput } from "./AIProfileDialog"

const providers: AIProvider[] = [
  { id: "openai_compatible", name: "OpenAI compatible" },
  { id: "ollama", name: "Ollama" },
]

function profile(overrides: Partial<AIProfile> = {}): AIProfile {
  return {
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
    ...overrides,
  }
}

// AppShell 就是这样挂这个对话框的：open 是真 state，关闭走 onOpenChange。
function Harness(props: {
  profile?: AIProfile
  onCreate: (input: CreateAIProfileInput) => void
  onSave: (input: EditAIProfileInput) => void
}) {
  const [open, setOpen] = useState(true)
  return (
    <AIProfileDialog
      open={open}
      providers={providers}
      profile={props.profile}
      pending={false}
      error={null}
      onOpenChange={setOpen}
      onCreate={props.onCreate}
      onSave={props.onSave}
    />
  )
}

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("AIProfileDialog create mode", () => {
  it("keeps the add form unchanged and posts the full profile", async () => {
    const onCreate = vi.fn()
    render(<Harness profile={undefined} onCreate={onCreate} onSave={vi.fn()} />)

    expect(screen.getByRole("dialog", { name: "Add AI provider" })).toBeInTheDocument()
    expect(screen.getByLabelText("Profile name")).toHaveValue("")
    expect(screen.getByLabelText("Server URL")).toHaveValue("https://api.openai.com/v1")
    expect(screen.getByLabelText("Temperature")).toBeInTheDocument()
    expect(screen.getByRole("checkbox", { name: "Default AI provider" })).toBeChecked()
    // 远程端点必须先授权发送正文，否则提交按钮按不动。
    expect(screen.getByRole("button", { name: "Add provider" })).toBeDisabled()
    fireEvent.click(screen.getByRole("checkbox", { name: /Article content may be sent/ }))
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "sk-secret" } })
    fireEvent.click(screen.getByRole("button", { name: "Add provider" }))

    await waitFor(() =>
      expect(onCreate).toHaveBeenCalledWith({
        provider: "openai_compatible",
        name: "OpenAI compatible",
        endpoint: "https://api.openai.com/v1",
        model: "gpt-4.1-mini",
        api_key: "sk-secret",
        settings: { temperature: 0.2 },
        allow_private_network: false,
        remote_content_approved: true,
        is_default: true,
      }),
    )
  })
})

describe("AIProfileDialog edit mode", () => {
  it("prefills the stored profile and locks the provider kind", () => {
    render(<Harness profile={profile()} onCreate={vi.fn()} onSave={vi.fn()} />)

    expect(screen.getByRole("dialog", { name: "Edit AI provider" })).toBeInTheDocument()
    expect(screen.getByRole("combobox", { name: "Provider" })).toBeDisabled()
    expect(screen.getByText(/provider kind can't be changed/)).toBeInTheDocument()
    expect(screen.getByLabelText("Profile name")).toHaveValue("Lab gateway")
    expect(screen.getByLabelText("Server URL")).toHaveValue("https://gateway.example.edu/v1")
    expect(screen.getByLabelText("Model")).toHaveValue("gpt-4.1-mini")
    // 密钥只写入不回显，编辑时永远是空的并带上留空即保留的提示。
    expect(screen.getByLabelText("API key")).toHaveValue("")
    expect(screen.getByLabelText("API key")).toHaveAttribute("placeholder", "Leave blank to keep the saved value")
    expect(screen.getByText(/never shown; leave blank/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled()
  })

  it("saves name, endpoint and model without touching the stored key", async () => {
    const saves: EditAIProfileInput[] = []
    const onSave = vi.fn((input: EditAIProfileInput) => {
      saves.push(input)
    })
    render(<Harness profile={profile()} onCreate={vi.fn()} onSave={onSave} />)

    fireEvent.change(screen.getByLabelText("Profile name"), { target: { value: "Campus vLLM" } })
    fireEvent.change(screen.getByLabelText("Server URL"), {
      target: { value: "https://vllm.example.edu/v1" },
    })
    fireEvent.change(screen.getByLabelText("Model"), { target: { value: "qwen3:32b" } })
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(saves[0]).toEqual({
      name: "Campus vLLM",
      endpoint: "https://vllm.example.edu/v1",
      model: "qwen3:32b",
      allow_private_network: false,
      remote_content_approved: true,
    })
    // PATCH 里出现 api_key 就会覆盖已存的那一把，空字符串也不行，所以键必须缺席。
    expect(saves[0]).not.toHaveProperty("api_key")
  })

  it("sends a replacement key only when the user types one", async () => {
    const saves: EditAIProfileInput[] = []
    const onSave = vi.fn((input: EditAIProfileInput) => {
      saves.push(input)
    })
    render(<Harness profile={profile()} onCreate={vi.fn()} onSave={onSave} />)

    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "   " } })
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(saves[0]).not.toHaveProperty("api_key"))

    fireEvent.change(screen.getByLabelText("API key"), { target: { value: " sk-new " } })
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(saves[1]?.api_key).toBe("sk-new"))
  })

  it("hides the fields PATCH cannot express safely", () => {
    render(<Harness profile={profile()} onCreate={vi.fn()} onSave={vi.fn()} />)

    // 温度回填不了（profiles 响应不含 settings），默认提供商由列表行的按钮负责。
    expect(screen.queryByLabelText("Temperature")).not.toBeInTheDocument()
    expect(screen.queryByRole("checkbox", { name: "Default AI provider" })).not.toBeInTheDocument()
  })

  it("requires privacy approval again when a legacy profile reaches a remote endpoint", async () => {
    const onSave = vi.fn()
    render(
      <Harness
        profile={profile({ remote_content_approved: false })}
        onCreate={vi.fn()}
        onSave={onSave}
      />,
    )

    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled()
    fireEvent.click(screen.getByRole("checkbox", { name: /Article content may be sent/ }))
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled()
    fireEvent.change(screen.getByLabelText("Model"), { target: { value: "gpt-4.1" } })
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith(
        expect.objectContaining({ model: "gpt-4.1", remote_content_approved: true }),
      ),
    )
  })

  it("sends the private-network flag for a local Ollama profile", async () => {
    const onSave = vi.fn()
    render(
      <Harness
        profile={profile({
          provider: "ollama",
          name: "Local Ollama",
          endpoint: "http://127.0.0.1:11434",
          model: "qwen3:8b",
          remote_content_approved: false,
          allow_private_network: true,
        })}
        onCreate={vi.fn()}
        onSave={onSave}
      />,
    )

    expect(screen.queryByLabelText("API key")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled()
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        name: "Local Ollama",
        endpoint: "http://127.0.0.1:11434",
        model: "qwen3:8b",
        allow_private_network: true,
        remote_content_approved: false,
      }),
    )
  })

  it("keeps the save button blocked while the model is empty", () => {
    render(<Harness profile={profile()} onCreate={vi.fn()} onSave={vi.fn()} />)

    fireEvent.change(screen.getByLabelText("Model"), { target: { value: "  " } })
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled()
  })
})
