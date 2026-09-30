import {
  Books,
  CaretDown,
  CaretRight,
  CircleNotch,
  Flask,
  Funnel,
  FolderOpen,
  FolderSimplePlus,
  NewspaperClipping,
  Plus,
  Sparkle,
  Star,
  Tag as TagIcon,
  Tray,
  WarningCircle,
} from "@phosphor-icons/react"
import { useState, type DragEvent, type MouseEvent, type ReactNode } from "react"

import type { Folder, LibraryScope, SavedFilter, Subscription, Tag, ViewMode } from "../api/types"
import { localizedScopeTitle, useTranslation } from "../lib/i18n"
import { useReaderStore, type AppView } from "../store/reader"
import { Brand } from "./Brand"
import { FolderContextMenu } from "./FolderContextMenu"
import { SubscriptionContextMenu } from "./SubscriptionContextMenu"

interface SidebarProps {
  appView: AppView
  onAppViewChange: (view: AppView) => void
  scope: LibraryScope
  subscriptions: Subscription[]
  folders: Folder[]
  tags: Tag[]
  savedFilters: SavedFilter[]
  onScopeChange: (scope: LibraryScope) => void
  onAdd: () => void
  onOrganizeLibrary: () => void
  /**
   * The AI / theme / preferences cluster, owned by AppShell and mounted into
   * the pinned bottom-left block. AppShell hands over `null` below the 900px
   * breakpoint, where this whole aside is `display: none` and the same cluster
   * is rendered into MobileNav instead — two live copies would double every
   * accessible name and every tab stop.
   */
  actions?: ReactNode
  onMarkFeedRead: (feedID: string) => void
  onMarkFolderRead: (folderID: string) => void
  onRefreshFeed: (feedID: string) => void
  onMoveFeed: (feedID: string, folderID: string | null) => void
  onRenameFeed: (feedID: string, name: string) => void
  onRenameFolder: (folderID: string, name: string) => void
  onCreateSubfolder: (parentID: string, name: string) => void
  onDeleteFolder: (folderID: string) => void
  onMoveFolder: (folderID: string, parentID: string | null) => void
  onMergeFeeds: (feedID: string, targetFeedID: string) => void
  onReorderFolder: (folderID: string, targetID: string, before: boolean) => void
  onReorderFeed: (feedID: string, targetID: string, before: boolean) => void
  onDeleteFeed: (feedID: string) => void
  onChangeFeedView: (feedID: string, viewMode: ViewMode) => void
  onChangeFeedRefresh: (
    feedID: string,
    policy: Subscription["refresh_policy"],
    intervalMinutes: number,
  ) => void
}

const workspaceScopes: Array<{ scope: LibraryScope; icon: typeof Sparkle }> = [
  { scope: { kind: "today", title: "Today" }, icon: Sparkle },
  { scope: { kind: "all", title: "All feeds" }, icon: Books },
  { scope: { kind: "unread", title: "Unread" }, icon: Tray },
  { scope: { kind: "saved", title: "Saved" }, icon: Star },
]

export function Sidebar(props: SidebarProps) {
  const { locale, t } = useTranslation()
  const openFolders = useReaderStore((state) => state.openFolders)
  const toggleFolder = useReaderStore((state) => state.toggleFolder)
  const sseState = useReaderStore((state) => state.sseState)
  const [contextMenu, setContextMenu] = useState<
    | {
        kind: "subscription"
        subscription: Subscription
        position: { x: number; y: number }
      }
    | { kind: "folder"; folder: Folder; position: { x: number; y: number } }
    | null
  >(null)
  const openContextMenu = (event: MouseEvent, subscription: Subscription) => {
    event.preventDefault()
    setContextMenu({
      kind: "subscription",
      subscription,
      position: { x: event.clientX, y: event.clientY },
    })
  }
  const openFolderContextMenu = (event: MouseEvent, folder: Folder) => {
    event.preventDefault()
    setContextMenu({
      kind: "folder",
      folder,
      position: { x: event.clientX, y: event.clientY },
    })
  }
  const requestRename = (currentName: string) => {
    const nextName = window.prompt(t("rename"), currentName)?.trim()
    return nextName && nextName !== currentName ? nextName : null
  }
  const openURL = (value: string | null | undefined) => {
    if (!value) return
    window.open(value, "_blank", "noopener,noreferrer")
  }
  return (
    <aside className="sidebar" aria-label={t("primaryNavigation")}>
      <div className="sidebar__header">
        <Brand />
      </div>
      <nav className="app-view-switch" aria-label={t("spaces")}>
        <button
          className={
            props.appView === "reader"
              ? "app-view-switch__item app-view-switch__item--active"
              : "app-view-switch__item"
          }
          type="button"
          aria-current={props.appView === "reader" ? "page" : undefined}
          title={t("switchToReader")}
          onClick={() => props.onAppViewChange("reader")}
        >
          <NewspaperClipping aria-hidden="true" weight={props.appView === "reader" ? "fill" : "regular"} />
          <span>{t("reader")}</span>
        </button>
        <button
          className={
            props.appView === "workbench"
              ? "app-view-switch__item app-view-switch__item--active"
              : "app-view-switch__item"
          }
          type="button"
          aria-current={props.appView === "workbench" ? "page" : undefined}
          title={t("switchToWorkbench")}
          onClick={() => props.onAppViewChange("workbench")}
        >
          <Flask aria-hidden="true" weight={props.appView === "workbench" ? "fill" : "regular"} />
          <span>{t("workbench")}</span>
        </button>
      </nav>
      <nav className="workspace-segment" aria-label={t("libraryViews")}>
        {workspaceScopes.map(({ scope, icon: Icon }) => {
          const active = props.scope.kind === scope.kind
          return (
            <button
              className={active ? "workspace-segment__item workspace-segment__item--active" : "workspace-segment__item"}
              key={scope.kind}
              type="button"
              aria-current={active ? "page" : undefined}
              title={localizedScopeTitle(scope, locale)}
              onClick={() => props.onScopeChange(scope)}
            >
              <Icon aria-hidden="true" weight={active ? "fill" : "regular"} />
              <span className="workspace-segment__label">{localizedScopeTitle(scope, locale)}</span>
            </button>
          )
        })}
      </nav>
      <section className="subscription-section" aria-labelledby="subscriptions-title">
        <div className="library-toolbar">
          <h2 id="subscriptions-title">{t("subscriptions")}</h2>
          <button
            className="icon-button icon-button--small"
            type="button"
            aria-label={t("addFeed")}
            title={t("addFeed")}
            onClick={props.onAdd}
          >
            <Plus />
          </button>
          <button
            className="icon-button icon-button--small"
            type="button"
            aria-label={t("addFolder")}
            title={t("addFolder")}
            onClick={props.onOrganizeLibrary}
          >
            <FolderSimplePlus />
          </button>
        </div>
        <div className="subscription-scroll">
          {props.savedFilters.length > 0 && (
            <div className="library-group">
              <h3>{t("filters")}</h3>
              {props.savedFilters.map((filter) => {
                const active = props.scope.kind === "filter" && props.scope.id === filter.id
                return (
                  <button
                    className={active ? "folder-row folder-row--active" : "folder-row"}
                    type="button"
                    aria-current={active ? "page" : undefined}
                    key={filter.id}
                    onClick={() =>
                      props.onScopeChange({
                        kind: "filter",
                        id: filter.id,
                        title: filter.name,
                        query: filter.query,
                      })
                    }
                  >
                    <Funnel aria-hidden="true" />
                    <span>{filter.name}</span>
                  </button>
                )
              })}
            </div>
          )}
          {props.tags.length > 0 && (
            <div className="library-group">
              <h3>{t("tags")}</h3>
              {props.tags.map((tag) => {
                const active = props.scope.kind === "tag" && props.scope.id === tag.id
                return (
                  <button
                    className={active ? "folder-row folder-row--active" : "folder-row"}
                    type="button"
                    aria-current={active ? "page" : undefined}
                    key={tag.id}
                    onClick={() =>
                      props.onScopeChange({ kind: "tag", id: tag.id, title: tag.name })
                    }
                  >
                    <span
                      className="sidebar-tag-mark"
                      style={tag.color ? { backgroundColor: tag.color } : undefined}
                    >
                      <TagIcon aria-hidden="true" />
                    </span>
                    <span>{tag.name}</span>
                  </button>
                )
              })}
            </div>
          )}
          <div className="library-group library-group--tree">
            <FolderTree
              folders={props.folders}
              subscriptions={props.subscriptions}
              scope={props.scope}
              openFolders={openFolders}
              onToggleFolder={toggleFolder}
              onScopeChange={props.onScopeChange}
              onContextMenu={openContextMenu}
              onFolderContextMenu={openFolderContextMenu}
              onMoveFeed={props.onMoveFeed}
              onMoveFolder={props.onMoveFolder}
              onMergeFeeds={props.onMergeFeeds}
              onReorderFolder={props.onReorderFolder}
              onReorderFeed={props.onReorderFeed}
            />
            {props.folders.length === 0 && props.subscriptions.length === 0 && (
              <div className="sidebar-library-empty">
                <Tray aria-hidden="true" />
                <span>{t("noSubscriptions")}</span>
              </div>
            )}
          </div>
        </div>
      </section>
      {/* Pinned under `.subscription-section`, which owns the free flex space:
          this block is `flex: 0 0 auto`, so the cluster stays at the sidebar's
          bottom-left while the feed tree scrolls above it. */}
      {props.actions ? <div className="sidebar__actions">{props.actions}</div> : null}
      {sseState === "reconnecting" && (
        <footer className="sidebar__footer" role="status">
          <span className="server-state">
            <CircleNotch className="spin server-state__indicator" aria-hidden="true" />
            <strong>{t("reconnecting")}</strong>
          </span>
        </footer>
      )}
      {contextMenu?.kind === "subscription" && (
        <SubscriptionContextMenu
          subscription={contextMenu.subscription}
          folders={props.folders}
          position={contextMenu.position}
          onClose={() => setContextMenu(null)}
          onRename={() => {
            const name = requestRename(contextMenu.subscription.title)
            if (name) props.onRenameFeed(contextMenu.subscription.feed_id, name)
          }}
          onMarkRead={() => props.onMarkFeedRead(contextMenu.subscription.feed_id)}
          onRefresh={() => props.onRefreshFeed(contextMenu.subscription.feed_id)}
          onMove={(folderID) => props.onMoveFeed(contextMenu.subscription.feed_id, folderID)}
          onDelete={() => props.onDeleteFeed(contextMenu.subscription.feed_id)}
          onOpenSource={() => openURL(contextMenu.subscription.feed_url)}
          onOpenWebsite={() => openURL(contextMenu.subscription.site_url)}
          onCopyID={() => void navigator.clipboard?.writeText(contextMenu.subscription.feed_id)}
          onChangeView={(viewMode) =>
            props.onChangeFeedView(contextMenu.subscription.feed_id, viewMode)
          }
          onChangeRefresh={(policy, intervalMinutes) =>
            props.onChangeFeedRefresh(contextMenu.subscription.feed_id, policy, intervalMinutes)
          }
        />
      )}
      {contextMenu?.kind === "folder" && (
        <FolderContextMenu
          folder={contextMenu.folder}
          position={contextMenu.position}
          onClose={() => setContextMenu(null)}
          onRename={() => {
            const name = requestRename(contextMenu.folder.name)
            if (name) props.onRenameFolder(contextMenu.folder.id, name)
          }}
          onNewSubfolder={() => {
            const name = window.prompt(t("folderName"))?.trim()
            if (name) props.onCreateSubfolder(contextMenu.folder.id, name)
          }}
          onMarkAllRead={() => props.onMarkFolderRead(contextMenu.folder.id)}
          onDelete={() => props.onDeleteFolder(contextMenu.folder.id)}
        />
      )}
    </aside>
  )
}

type DragItem = { type: "subscription" | "folder"; id: string }
type DropPosition = "before" | "after" | "inside"
type DropIndicator = { id: string; position: DropPosition }

const DRAG_MIME = "application/x-reflow-library"

function FolderTree(props: {
  folders: Folder[]
  subscriptions: Subscription[]
  scope: LibraryScope
  openFolders: Record<string, boolean>
  onToggleFolder: (folderID: string) => void
  onScopeChange: (scope: LibraryScope) => void
  onContextMenu: (event: MouseEvent, subscription: Subscription) => void
  onFolderContextMenu: (event: MouseEvent, folder: Folder) => void
  onMoveFeed: (feedID: string, folderID: string | null) => void
  onMoveFolder: (folderID: string, parentID: string | null) => void
  onMergeFeeds: (feedID: string, targetFeedID: string) => void
  onReorderFolder: (folderID: string, targetID: string, before: boolean) => void
  onReorderFeed: (feedID: string, targetID: string, before: boolean) => void
}) {
  const { t } = useTranslation()
  const [dragging, setDragging] = useState<DragItem | null>(null)
  const [dropIndicator, setDropIndicator] = useState<DropIndicator | null>(null)

  const folderByID = new Map(props.folders.map((folder) => [folder.id, folder]))
  const childrenByParent = new Map<string | null, Folder[]>()
  const subscriptionsByFolder = new Map<string | null, Subscription[]>()
  for (const folder of props.folders) {
    const parent = folder.parent_id ?? null
    const children = childrenByParent.get(parent) ?? []
    children.push(folder)
    childrenByParent.set(parent, children)
  }
  for (const subscription of props.subscriptions) {
    const folderID = subscription.folder_id ?? null
    const items = subscriptionsByFolder.get(folderID) ?? []
    items.push(subscription)
    subscriptionsByFolder.set(folderID, items)
  }

  // Descendant lookup is used to block dropping a folder into itself or its
  // own children — the backend rejects the cycle with a 409, so hide the
  // affordance up front instead of failing after the drop.
  const descendantIDs = (folderID: string) => {
    const ids = new Set<string>()
    const walk = (parentID: string) => {
      for (const child of childrenByParent.get(parentID) ?? []) {
        ids.add(child.id)
        walk(child.id)
      }
    }
    walk(folderID)
    return ids
  }
  // A folder can only be dropped onto a target that is NOT inside its own
  // subtree. Check the dragged folder's descendants, not the target's.
  const folderDropBlocked = (draggedID: string, targetID: string) =>
    draggedID === targetID || descendantIDs(draggedID).has(targetID)

  const dragPayload = (event: DragEvent): DragItem | null => {
    try {
      return JSON.parse(event.dataTransfer.getData(DRAG_MIME)) as DragItem
    } catch {
      return null
    }
  }
  const allowDrop = (event: DragEvent) => {
    event.preventDefault()
    event.dataTransfer.dropEffect = "move"
  }
  const clearDragState = () => {
    setDragging(null)
    setDropIndicator(null)
  }
  const clearDropIndicator = (event: DragEvent) => {
    const nextTarget = event.relatedTarget
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) return
    setDropIndicator(null)
  }
  const dropZone = (event: DragEvent): "before" | "after" | "merge" => {
    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientY - rect.top) / rect.height
    if (ratio < 0.28) return "before"
    if (ratio > 0.72) return "after"
    return "merge"
  }
  const startDrag = (event: DragEvent, item: DragItem) => {
    event.dataTransfer.effectAllowed = "move"
    event.dataTransfer.setData(DRAG_MIME, JSON.stringify(item))
    setDragging(item)
  }

  const renderSubscription = (subscription: Subscription, depth: number) => {
    const active = props.scope.kind === "feed" && props.scope.id === subscription.feed_id
    const indicator =
      dropIndicator?.id === `subscription:${subscription.feed_id}` ? dropIndicator.position : null
    const draggable = dragging?.type === "subscription" && dragging.id !== subscription.feed_id
    return (
      <button
        className={[
          "feed-row feed-row--nested",
          active ? "feed-row--active" : "",
          indicator ? `library-drop-target library-drop-target--${indicator}` : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={{ paddingLeft: `calc(var(--space-5) + ${depth} * var(--space-7))` }}
        key={subscription.id}
        type="button"
        draggable
        aria-current={active ? "page" : undefined}
        onDragStart={(event) =>
          startDrag(event, { type: "subscription", id: subscription.feed_id })
        }
        onDragEnd={clearDragState}
        onDragOver={(event) => {
          allowDrop(event)
          if (!draggable) return
          const zone = dropZone(event)
          setDropIndicator({
            id: `subscription:${subscription.feed_id}`,
            position: zone === "merge" ? "inside" : zone,
          })
        }}
        onDragLeave={clearDropIndicator}
        onDrop={(event) => {
          event.preventDefault()
          event.stopPropagation()
          clearDragState()
          const source = dragPayload(event)
          if (!source || source.type !== "subscription" || source.id === subscription.feed_id)
            return
          const zone = dropZone(event)
          if (zone === "merge") {
            props.onMergeFeeds(source.id, subscription.feed_id)
            return
          }
          const sourceItem = props.subscriptions.find((item) => item.feed_id === source.id)
          if (sourceItem && sourceItem.folder_id !== subscription.folder_id) {
            props.onMoveFeed(source.id, subscription.folder_id)
          } else {
            props.onReorderFeed(source.id, subscription.feed_id, zone === "before")
          }
        }}
        onClick={() =>
          props.onScopeChange({ kind: "feed", id: subscription.feed_id, title: subscription.title })
        }
        onContextMenu={(event) => props.onContextMenu(event, subscription)}
      >
        <span className="feed-row__mark" aria-hidden="true">
          <span>{subscription.title.slice(0, 1).toUpperCase()}</span>
          {subscription.icon_url && (
            <img
              src={subscription.icon_url}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              onError={(event) => {
                event.currentTarget.hidden = true
              }}
            />
          )}
        </span>
        <span className="feed-row__title">{subscription.title}</span>
        {subscription.failure_count > 0 && (
          <span
            className="feed-row__alert"
            role="img"
            aria-label={t("feedFailing")}
            title={`${t("feedFailing")} ×${subscription.failure_count}${
              subscription.last_error_message
                ? ` · ${t("feedFailingDetail")}: ${subscription.last_error_message}`
                : ""
            }`}
          >
            <WarningCircle weight="fill" />
          </span>
        )}
        <span className="feed-row__count">{subscription.unread_count}</span>
      </button>
    )
  }

  const renderLevel = (parentID: string | null, depth: number, ancestors: string[]): ReactNode[] => {
    const children = childrenByParent.get(parentID) ?? []
    const rows: ReactNode[] = []
    for (const subscription of subscriptionsByFolder.get(parentID) ?? []) {
      rows.push(renderSubscription(subscription, depth))
    }
    for (const folder of children) {
      const active = props.scope.kind === "folder" && props.scope.id === folder.id
      const childFolders = childrenByParent.get(folder.id) ?? []
      const directSubscriptions = subscriptionsByFolder.get(folder.id) ?? []
      const hasChildren = childFolders.length > 0 || directSubscriptions.length > 0
      const descendants = descendantIDs(folder.id)
      const unread = props.subscriptions
        .filter(
          (subscription) =>
            subscription.folder_id &&
            (subscription.folder_id === folder.id || descendants.has(subscription.folder_id)),
        )
        .reduce((total, subscription) => total + subscription.unread_count, 0)
      const expanded = props.openFolders[folder.id] ?? true
      const indicator = dropIndicator?.id === `folder:${folder.id}` ? dropIndicator.position : null
      // Block when the dragged folder's own subtree contains this row — that
      // drop would create a cycle. Self is included via folderDropBlocked.
      const draggingBlocked =
        dragging !== null &&
        dragging.type === "folder" &&
        folderDropBlocked(dragging.id, folder.id)

      rows.push(
        <div
          className={[
            "folder-tree-row",
            indicator ? `library-drop-target library-drop-target--${indicator}` : "",
          ]
            .filter(Boolean)
            .join(" ")}
          key={folder.id}
          style={{ paddingLeft: `calc(var(--space-5) + ${depth} * var(--space-7))` }}
          draggable
          onDragStart={(event) => startDrag(event, { type: "folder", id: folder.id })}
          onDragEnd={clearDragState}
          onDragOver={(event) => {
            allowDrop(event)
            if (!dragging || draggingBlocked) return
            if (dragging.type === "subscription") {
              setDropIndicator({ id: `folder:${folder.id}`, position: "inside" })
              return
            }
            const zone = dropZone(event)
            setDropIndicator({
              id: `folder:${folder.id}`,
              position: zone === "merge" ? "inside" : zone,
            })
          }}
          onDragLeave={clearDropIndicator}
          onDrop={(event) => {
            event.preventDefault()
            event.stopPropagation()
            clearDragState()
            const source = dragPayload(event)
            if (!source || source.id === folder.id) return
            if (source.type === "subscription") {
              props.onMoveFeed(source.id, folder.id)
              return
            }
            if (folderDropBlocked(source.id, folder.id)) return
            const zone = dropZone(event)
            if (zone === "merge") {
              props.onMoveFolder(source.id, folder.id)
              return
            }
            const sourceFolder = folderByID.get(source.id)
            if (sourceFolder && sourceFolder.parent_id !== folder.parent_id) {
              props.onMoveFolder(source.id, folder.parent_id ?? null)
              return
            }
            props.onReorderFolder(source.id, folder.id, zone === "before")
          }}
          onContextMenu={(event) => props.onFolderContextMenu(event, folder)}
        >
          <button
            className={active ? "folder-row folder-row--active" : "folder-row"}
            type="button"
            aria-current={active ? "page" : undefined}
            aria-expanded={hasChildren ? expanded : undefined}
            onClick={() => {
              props.onScopeChange({ kind: "folder", id: folder.id, title: folder.name })
              if (hasChildren) props.onToggleFolder(folder.id)
            }}
          >
            <FolderOpen aria-hidden="true" weight={active ? "fill" : "regular"} />
            <span>{folder.name}</span>
            <span className="folder-row__count">{unread}</span>
          </button>
          {hasChildren && (
            <button
              className="folder-row__toggle"
              type="button"
              aria-label={expanded ? t("collapseFolder") : t("expandFolder")}
              title={expanded ? t("collapseFolder") : t("expandFolder")}
              aria-expanded={expanded}
              onClick={() => props.onToggleFolder(folder.id)}
            >
              {expanded ? <CaretDown aria-hidden="true" /> : <CaretRight aria-hidden="true" />}
            </button>
          )}
        </div>,
      )
      // Ancestors guard against folder-parent cycles in stored data — such a
      // folder (and its subtree) is reachable from neither the root nor any
      // orphan fallback, so rendering it would loop forever.
      if (expanded && !ancestors.includes(folder.id)) {
        rows.push(...renderLevel(folder.id, depth + 1, [...ancestors, folder.id]))
      }
    }
    return rows
  }

  const rows = renderLevel(null, 0, [])
  // Folders whose parent chain never reaches the root (broken reference or a
  // stored cycle) render once at the bottom so they stay manageable.
  const renderedIDs = new Set<string>()
  for (const folder of props.folders) {
    if (renderedIDs.has(folder.id)) continue
    let cursor: Folder | undefined = folder
    const chain: string[] = []
    while (cursor && cursor.parent_id && !renderedIDs.has(cursor.id) && !chain.includes(cursor.id)) {
      chain.push(cursor.id)
      cursor = folderByID.get(cursor.parent_id)
    }
    if (cursor && !cursor.parent_id) {
      // Reachable from the root — already rendered by renderLevel(null).
      for (const id of chain) renderedIDs.add(id)
      continue
    }
    rows.push(...renderLevel(folder.parent_id ?? null, 0, []))
    for (const orphan of childrenByParent.get(folder.parent_id ?? null) ?? []) {
      renderedIDs.add(orphan.id)
    }
  }

  const rootIndicator = dropIndicator?.id === "root" ? dropIndicator.position : null
  return (
    <div
      className={rootIndicator ? "folder-tree folder-tree--drop-target" : "folder-tree"}
      onDragOver={(event) => {
        if (!dragging) return
        const target = event.target as HTMLElement
        if (target.closest(".folder-tree-row") || target.closest(".feed-row--nested")) return
        allowDrop(event)
        setDropIndicator({ id: "root", position: "inside" })
      }}
      onDragLeave={(event) => {
        if (event.currentTarget.contains(event.relatedTarget as Node)) return
        setDropIndicator(null)
      }}
      onDrop={(event) => {
        const source = dragPayload(event)
        if (!source) return
        event.preventDefault()
        clearDragState()
        if (source.type === "subscription") {
          const item = props.subscriptions.find((entry) => entry.feed_id === source.id)
          if (item?.folder_id) props.onMoveFeed(source.id, null)
          return
        }
        const item = folderByID.get(source.id)
        if (item?.parent_id) props.onMoveFolder(source.id, null)
      }}
    >
      {rows}
    </div>
  )
}
