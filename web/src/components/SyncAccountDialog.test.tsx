import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { APIError } from "../api/client"
import type { SyncAccount, SyncProvider } from "../api/types"
import { useReaderStore } from "../store/reader"
import { SyncAccountDialog } from "./SyncAccountDialog"

const providers: SyncProvider[] = [
  { id: "webdav", name: "WebDAV" },
  { id: "icloud", name: "iCloud Drive" },
]

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US" })
})

afterEach(() => {
  cleanup()
})

describe("SyncAccountDialog", () => {
  it("fills the Nutstore WebDAV preset and tests without saving", async () => {
    const onSave = vi.fn()
    const onTest = vi.fn().mockResolvedValue({
      ok: true,
      endpoint: "https://dav.jianguoyun.com/dav/ReFlow/reflow-library.json",
    })
    render(
      <SyncAccountDialog
        open
        providers={providers}
        initialProvider="webdav"
        pending={false}
        error={null}
        onOpenChange={vi.fn()}
        onSave={onSave}
        onTest={onTest}
      />,
    )

    fireEvent.click(screen.getByRole("button", { name: "Use Nutstore" }))
    expect(screen.getByLabelText("Account name")).toHaveValue("Nutstore")
    expect(screen.getByLabelText("Snapshot file URL")).toHaveValue(
      "https://dav.jianguoyun.com/dav/ReFlow/",
    )
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "researcher@example.com" },
    })
    fireEvent.change(screen.getByLabelText("Password / app password"), {
      target: { value: "application-secret" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }))

    await waitFor(() => expect(onTest).toHaveBeenCalledTimes(1))
    expect(onTest).toHaveBeenCalledWith({
      account_id: undefined,
      provider: "webdav",
      endpoint: "https://dav.jianguoyun.com/dav/ReFlow/",
      credentials: {
        username: "researcher@example.com",
        password: "application-secret",
        token: undefined,
        api_key: undefined,
      },
      allow_private_network: false,
    })
    expect(await screen.findByText(/Connection successful/)).toBeInTheDocument()
    expect(onSave).not.toHaveBeenCalled()
  })

  it("explains Nutstore application passwords after an HTTP 401", async () => {
    const onTest = vi.fn().mockRejectedValue(
      new APIError(401, "authentication_error (HTTP 401): Unauthorized", {
        code: "authentication_error",
      }),
    )
    render(
      <SyncAccountDialog
        open
        providers={providers}
        initialProvider="webdav"
        pending={false}
        error={null}
        onOpenChange={vi.fn()}
        onSave={vi.fn()}
        onTest={onTest}
      />,
    )

    fireEvent.change(screen.getByLabelText("Snapshot file URL"), {
      target: { value: "https://dav.jianguoyun.com/dav/" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Nutstore requires your account email and a third-party app password",
    )
  })

  it("ignores a connection response after test inputs change", async () => {
    const testResult = deferred<{ ok: true; endpoint: string }>()
    const onTest = vi.fn().mockReturnValue(testResult.promise)
    render(
      <SyncAccountDialog
        open
        providers={providers}
        initialProvider="webdav"
        pending={false}
        error={null}
        onOpenChange={vi.fn()}
        onSave={vi.fn()}
        onTest={onTest}
      />,
    )

    fireEvent.change(screen.getByLabelText("Snapshot file URL"), {
      target: { value: "https://dav.example.com/old/" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }))
    await waitFor(() => expect(onTest).toHaveBeenCalledOnce())

    fireEvent.change(screen.getByLabelText("Snapshot file URL"), {
      target: { value: "https://dav.example.com/new/" },
    })
    await act(async () => {
      testResult.resolve({ ok: true, endpoint: "https://dav.example.com/old/library.json" })
      await testResult.promise
    })

    expect(screen.queryByText(/Connection successful/)).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled()
  })

  it("prefills editable settings while leaving encrypted credentials blank", () => {
    const account: SyncAccount = {
      id: "webdav-account",
      provider: "webdav",
      name: "My Nutstore",
      endpoint: "https://dav.jianguoyun.com/dav/reflow-library.json",
      enabled: true,
      allow_private_network: false,
      sync_interval_minutes: 180,
      last_sync_at: null,
      next_sync_at: null,
      last_attempt_at: null,
      last_error_code: null,
      last_error_message: null,
      created_at: "2026-07-22T00:00:00Z",
      updated_at: "2026-07-22T00:00:00Z",
    }
    const onSave = vi.fn()
    render(
      <SyncAccountDialog
        open
        providers={providers}
        account={account}
        pending={false}
        error={null}
        onOpenChange={vi.fn()}
        onSave={onSave}
        onTest={vi.fn()}
      />,
    )

    expect(screen.getByRole("combobox", { name: "Provider" })).toBeDisabled()
    expect(screen.getByLabelText("Account name")).toHaveValue("My Nutstore")
    expect(screen.getByLabelText("Username")).toHaveValue("")
    expect(screen.getByLabelText("Password / app password")).toHaveValue("")
    fireEvent.change(screen.getByLabelText("Account name"), { target: { value: "Research" } })
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Research",
        credentials: {
          username: undefined,
          password: undefined,
          token: undefined,
          api_key: undefined,
        },
      }),
    )
  })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}
