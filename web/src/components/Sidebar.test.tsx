import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ReactNode } from "react"

import type { Folder, LibraryScope, Subscription } from "../api/types"
import { useReaderStore } from "../store/reader"
import { ShellActions } from "./ShellActions"
import { Sidebar } from "./Sidebar"

const folder: Folder = {
  id: "folder-1",
  parent_id: null,
  name: "Research",
  position: 0,
  created_at: "2026-07-21T00:00:00Z",
  updated_at: "2026-07-21T00:00:00Z",
}

const nestedFolder: Folder = {
  id: "folder-2",
  parent_id: "folder-1",
  name: "Papers",
  position: 0,
  created_at: "2026-07-21T00:00:00Z",
  updated_at: "2026-07-21T00:00:00Z",
}

const subscription: Subscription = {
  id: "subscription-1",
  feed_id: "feed-1",
  folder_id: "folder-1",
  position: 0,
  title: "Example feed",
  icon_url: null,
  feed_url: "https://example.com/feed.xml",
  site_url: "https://example.com",
  unread_count: 2,
  failure_count: 0,
  last_error_code: null,
  last_error_message: null,
  last_success_at: null,
  view_mode: "standard",
  refresh_policy: "inherit",
  refresh_interval_minutes: 0,
  hide_from_timeline: false,
  created_at: "2026-07-21T00:00:00Z",
  updated_at: "2026-07-21T00:00:00Z",
}

beforeEach(() => {
  useReaderStore.setState({ locale: "en-US", openFolders: { "folder-1": true } })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Sidebar rename menus", () => {
  it("renames a subscription through the in-app dialog, never window.prompt", () => {
    const onRenameFeed = vi.fn()
    // window.prompt never shows in the WKWebView/WebView2 desktop shell.
    const promptSpy = vi.spyOn(window, "prompt").mockReturnValue("Renamed feed")
    renderSidebar({ onRenameFeed })

    fireEvent.contextMenu(screen.getByRole("button", { name: "Example feed2" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }))

    // The dialog opens pre-filled with the current title, all selected.
    const input = screen.getByLabelText<HTMLInputElement>("Feed name")
    expect(input.value).toBe("Example feed")
    fireEvent.change(input, { target: { value: "Renamed feed" } })
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))

    expect(promptSpy).not.toHaveBeenCalled()
    expect(onRenameFeed).toHaveBeenCalledWith("feed-1", "Renamed feed")
  })

  it("renames a folder through the in-app dialog, never window.prompt", () => {
    const onRenameFolder = vi.fn()
    const promptSpy = vi.spyOn(window, "prompt").mockReturnValue("Papers")
    renderSidebar({ onRenameFolder })

    fireEvent.contextMenu(screen.getByRole("button", { name: "Research2" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }))

    const input = screen.getByLabelText<HTMLInputElement>("Folder name")
    expect(input.value).toBe("Research")
    fireEvent.change(input, { target: { value: "Papers" } })
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))

    expect(promptSpy).not.toHaveBeenCalled()
    expect(onRenameFolder).toHaveBeenCalledWith("folder-1", "Papers")
  })

  it("keeps an unchanged folder name a no-op", () => {
    const onRenameFolder = vi.fn()
    renderSidebar({ onRenameFolder })

    fireEvent.contextMenu(screen.getByRole("button", { name: "Research2" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }))
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))

    expect(onRenameFolder).not.toHaveBeenCalled()
  })
})

describe("Sidebar folder interactions", () => {
  it("selects a folder and toggles it when its main row is clicked", () => {
    const onScopeChange = vi.fn()
    renderSidebar({ onScopeChange })

    const folderRow = screen.getByRole("button", { name: "Research2" })
    expect(screen.getByRole("button", { name: "Example feed2" })).toBeInTheDocument()
    fireEvent.click(folderRow)

    expect(onScopeChange).toHaveBeenCalledWith({
      kind: "folder",
      id: "folder-1",
      title: "Research",
    })
    // Clicking the row both selects the folder and collapses it.
    expect(screen.queryByRole("button", { name: "Example feed2" })).not.toBeInTheDocument()
    fireEvent.click(folderRow)
    expect(screen.getByRole("button", { name: "Example feed2" })).toBeInTheDocument()
  })

  it("collapses and expands via the toggle button only", () => {
    renderSidebar({})

    fireEvent.click(screen.getByRole("button", { name: "Collapse folder" }))
    expect(screen.queryByRole("button", { name: "Example feed2" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Expand folder" }))
    expect(screen.getByRole("button", { name: "Example feed2" })).toBeInTheDocument()
  })

  it("hides children of a collapsed folder instead of leaking them at the bottom", () => {
    useReaderStore.setState({ openFolders: { "folder-1": false } })
    renderSidebar({})

    expect(screen.getByRole("button", { name: "Research2" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Example feed2" })).not.toBeInTheDocument()
  })

  it("deletes a folder from its context menu", () => {
    const onDeleteFolder = vi.fn()
    vi.spyOn(window, "confirm").mockReturnValue(true)
    renderSidebar({ onDeleteFolder })

    fireEvent.contextMenu(screen.getByRole("button", { name: "Research2" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete folder" }))

    expect(onDeleteFolder).toHaveBeenCalledWith("folder-1")
  })

  it("marks a whole folder read from its context menu", () => {
    const onMarkFolderRead = vi.fn()
    renderSidebar({ onMarkFolderRead })

    fireEvent.contextMenu(screen.getByRole("button", { name: "Research2" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Mark all read" }))

    expect(onMarkFolderRead).toHaveBeenCalledWith("folder-1")
  })

  it("creates a subfolder through the in-app dialog, never window.prompt", () => {
    const onCreateSubfolder = vi.fn()
    const promptSpy = vi.spyOn(window, "prompt").mockReturnValue("Papers")
    renderSidebar({ onCreateSubfolder })

    fireEvent.contextMenu(screen.getByRole("button", { name: "Research2" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "New subfolder" }))

    const input = screen.getByLabelText<HTMLInputElement>("Folder name")
    expect(input.value).toBe("")
    // Submit stays disabled until a name exists.
    const submit = screen.getByRole("button", { name: "New subfolder" })
    expect(submit).toBeDisabled()
    fireEvent.change(input, { target: { value: "Papers" } })
    fireEvent.click(submit)

    expect(promptSpy).not.toHaveBeenCalled()
    expect(onCreateSubfolder).toHaveBeenCalledWith("folder-1", "Papers")
  })
})

describe("Sidebar drag and drop", () => {
  const dataTransfer = () => {
    const store = new Map<string, string>()
    return {
      effectAllowed: "",
      dropEffect: "",
      setData: (mime: string, value: string) => store.set(mime, value),
      getData: (mime: string) => store.get(mime) ?? "",
    }
  }

  it("nests a folder into another folder when dropped on its middle", () => {
    const onMoveFolder = vi.fn()
    renderSidebar({ onMoveFolder, folders: [folder, nestedFolder] })
    const transfer = dataTransfer()

    const target = screen.getByRole("button", { name: "Research2" }).closest(".folder-tree-row")!
    fireEvent.dragStart(screen.getByRole("button", { name: "Papers0" }).closest(".folder-tree-row")!, {
      dataTransfer: transfer,
    })
    // Middle of the row => "inside" drop zone.
    vi.spyOn(target as HTMLElement, "getBoundingClientRect").mockReturnValue({
      top: 0,
      height: 36,
    } as DOMRect)
    fireEvent.dragOver(target, { dataTransfer: transfer, clientY: 18 })
    fireEvent.drop(target, { dataTransfer: transfer, clientY: 18 })

    expect(onMoveFolder).toHaveBeenCalledWith("folder-2", "folder-1")
  })

  it("blocks dropping a folder onto its own descendant", () => {
    const onMoveFolder = vi.fn()
    const onReorderFolder = vi.fn()
    const { container } = renderSidebar({
      onMoveFolder,
      onReorderFolder,
      folders: [folder, nestedFolder],
    })
    const transfer = dataTransfer()

    const descendant = screen.getByRole("button", { name: "Papers0" }).closest(".folder-tree-row")!
    vi.spyOn(descendant as HTMLElement, "getBoundingClientRect").mockReturnValue({
      top: 0,
      height: 36,
    } as DOMRect)
    fireEvent.dragStart(screen.getByRole("button", { name: "Research2" }).closest(".folder-tree-row")!, {
      dataTransfer: transfer,
    })
    fireEvent.dragOver(descendant, { dataTransfer: transfer, clientY: 18 })
    fireEvent.drop(descendant, { dataTransfer: transfer, clientY: 18 })

    expect(onMoveFolder).not.toHaveBeenCalled()
    expect(onReorderFolder).not.toHaveBeenCalled()
    expect(container.querySelector(".library-drop-target")).toBeNull()
  })

  it("moves a subscription into a folder when dropped on it", () => {
    const onMoveFeed = vi.fn()
    renderSidebar({ onMoveFeed })
    const transfer = dataTransfer()

    const target = screen.getByRole("button", { name: "Research2" }).closest(".folder-tree-row")!
    fireEvent.dragStart(screen.getByRole("button", { name: "Example feed2" }), {
      dataTransfer: transfer,
    })
    fireEvent.dragOver(target, { dataTransfer: transfer })
    fireEvent.drop(target, { dataTransfer: transfer })

    expect(onMoveFeed).toHaveBeenCalledWith("feed-1", "folder-1")
  })
})

describe("Sidebar shell action slot", () => {
  it("pins the AI / theme / preferences cluster under the scrolling library", () => {
    const onPreferences = vi.fn()
    const { container } = renderSidebar({
      actions: (
        <ShellActions
          theme="light"
          onThemeChange={vi.fn()}
          aiOpen={false}
          onAI={vi.fn()}
          onPreferences={onPreferences}
        />
      ),
    })

    const block = container.querySelector(".sidebar__actions")
    expect(block).not.toBeNull()
    // Sibling order is what makes the block sit at the bottom-left: the
    // subscription section above it owns the flex space.
    expect(block?.previousElementSibling?.className).toBe("subscription-section")
    expect(block?.parentElement?.className).toBe("sidebar")
    expect(block?.querySelectorAll(".shell-actions .icon-button").length).toBe(3)

    fireEvent.click(screen.getByRole("button", { name: "Preferences" }))
    expect(onPreferences).toHaveBeenCalledTimes(1)
  })

  it("renders no bottom block when AppShell hands the cluster to the mobile bar", () => {
    const { container } = renderSidebar({ actions: null })

    expect(container.querySelector(".sidebar__actions")).toBeNull()
    expect(screen.queryByRole("button", { name: "Preferences" })).not.toBeInTheDocument()
  })
})

function renderSidebar(overrides: {
  folders?: Folder[]
  actions?: ReactNode
  onRenameFeed?: (feedID: string, name: string) => void
  onRenameFolder?: (folderID: string, name: string) => void
  onScopeChange?: (scope: LibraryScope) => void
  onDeleteFolder?: (folderID: string) => void
  onMarkFolderRead?: (folderID: string) => void
  onCreateSubfolder?: (parentID: string, name: string) => void
  onMoveFolder?: (folderID: string, parentID: string | null) => void
  onMoveFeed?: (feedID: string, folderID: string | null) => void
  onReorderFolder?: (folderID: string, targetID: string, before: boolean) => void
}) {
  return render(
    <Sidebar
      appView="reader"
      onAppViewChange={vi.fn()}
      scope={{ kind: "all", title: "All feeds" }}
      subscriptions={[subscription]}
      folders={overrides.folders ?? [folder]}
      tags={[]}
      savedFilters={[]}
      actions={overrides.actions}
      onScopeChange={overrides.onScopeChange ?? vi.fn()}
      onAdd={vi.fn()}
      onOrganizeLibrary={vi.fn()}
      onMarkFeedRead={vi.fn()}
      onMarkFolderRead={overrides.onMarkFolderRead ?? vi.fn()}
      onRefreshFeed={vi.fn()}
      onMoveFeed={overrides.onMoveFeed ?? vi.fn()}
      onRenameFeed={overrides.onRenameFeed ?? vi.fn()}
      onRenameFolder={overrides.onRenameFolder ?? vi.fn()}
      onCreateSubfolder={overrides.onCreateSubfolder ?? vi.fn()}
      onDeleteFolder={overrides.onDeleteFolder ?? vi.fn()}
      onMoveFolder={overrides.onMoveFolder ?? vi.fn()}
      onMergeFeeds={vi.fn()}
      onReorderFolder={overrides.onReorderFolder ?? vi.fn()}
      onReorderFeed={vi.fn()}
      onDeleteFeed={vi.fn()}
      onChangeFeedView={vi.fn()}
      onChangeFeedRefresh={vi.fn()}
    />,
  )
}
