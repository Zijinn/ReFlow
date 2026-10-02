import {
  ArrowsClockwise,
  CheckSquare,
  CircleNotch,
  Plus,
  Sparkle,
  Star,
  WarningCircle,
} from "@phosphor-icons/react"
import { useVirtualizer } from "@tanstack/react-virtual"
import { useEffect, useMemo, useRef, useState } from "react"

import type { Entry, LibraryScope, Subscription, ViewMode } from "../api/types"
import { localizedScopeTitle, useTranslation, type Locale, type Translator } from "../lib/i18n"
import { formatAuthors } from "../lib/metadata"

interface TimelinePaneProps {
  scope: LibraryScope
  entries: Entry[]
  subscriptions: Subscription[]
  selectedEntryID: string | null
  viewMode: ViewMode
  isLoading: boolean
  isFetchingNext: boolean
  hasNextPage: boolean
  error: Error | null
  markReadPending: boolean
  refreshPending: boolean
  onScopeChange: (scope: LibraryScope) => void
  onSelect: (entryID: string) => void
  onAdd: () => void
  onRetry: () => void
  onLoadMore: () => void
  onMarkAllRead: () => void
  onRefresh: (feedID: string) => void
  onToggleStar: (entry: Entry) => void
}

export function TimelinePane(props: TimelinePaneProps) {
  const { locale, t } = useTranslation()
  const scrollRef = useRef<HTMLDivElement>(null)
  const subscriptionIcons = useMemo(
    () =>
      new Map(
        props.subscriptions.map(
          (subscription) => [subscription.feed_id, subscription.icon_url] as const,
        ),
      ),
    [props.subscriptions],
  )
  const selectedFeedID = props.scope.kind === "feed" ? props.scope.id : null
  // A per-feed view mode overrides the global preference while that feed is the
  // active scope. Without this the context-menu choice persisted but never
  // affected rendering.
  const viewMode =
    props.subscriptions.find((subscription) => subscription.feed_id === selectedFeedID)?.view_mode ??
    props.viewMode
  const virtualizer = useVirtualizer({
    count: props.entries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => (viewMode === "compact" ? 64 : 104),
    overscan: 7,
    getItemKey: (index) => props.entries[index]?.id ?? index,
  })
  // Keep the selected row in view when j/k (or any other source) moves the
  // selection somewhere off screen.
  useEffect(() => {
    const index = props.entries.findIndex((entry) => entry.id === props.selectedEntryID)
    if (index >= 0) virtualizer.scrollToIndex(index, { align: "auto" })
  }, [props.entries, props.selectedEntryID, virtualizer])
  const virtualItems = virtualizer.getVirtualItems()
  const onScroll = () => {
    const element = scrollRef.current
    if (!element || !props.hasNextPage || props.isFetchingNext) return
    if (element.scrollHeight - element.scrollTop - element.clientHeight < 640) props.onLoadMore()
  }

  return (
    <section className="timeline" aria-labelledby="timeline-title">
      <header className="pane-header library-page-header">
        <div className="pane-header__titles">
          <h1 id="timeline-title">{localizedScopeTitle(props.scope, locale)}</h1>
        </div>
        <div className="pane-header__actions">
          {selectedFeedID && (
            <button
              className="icon-button"
              type="button"
              aria-label={t("refreshFeed")}
              title={t("refreshFeed")}
              disabled={props.refreshPending}
              onClick={() => props.onRefresh(selectedFeedID)}
            >
              {props.refreshPending ? <CircleNotch className="spin" /> : <ArrowsClockwise />}
            </button>
          )}
        </div>
      </header>
      <div className="timeline-filterbar">
        <div className="timeline-filterbar__scopes" role="group" aria-label={t("articleFilters")}>
          <button
            className={
              props.scope.kind === "all" ? "filter-chip filter-chip--active" : "filter-chip"
            }
            type="button"
            aria-pressed={props.scope.kind === "all"}
            onClick={() => props.onScopeChange({ kind: "all", title: "All feeds" })}
          >
            {t("all")}
          </button>
          <button
            className={
              props.scope.kind === "unread" ? "filter-chip filter-chip--active" : "filter-chip"
            }
            type="button"
            aria-pressed={props.scope.kind === "unread"}
            onClick={() => props.onScopeChange({ kind: "unread", title: "Unread" })}
          >
            {t("unread")}
          </button>
          <button
            className={
              props.scope.kind === "saved" ? "filter-chip filter-chip--active" : "filter-chip"
            }
            type="button"
            aria-pressed={props.scope.kind === "saved"}
            onClick={() => props.onScopeChange({ kind: "saved", title: "Saved" })}
          >
            {t("saved")}
          </button>
        </div>
        <div className="timeline-filterbar__actions">
          {/* 动作不留在筛选组里：上面三枚 chip 改的是 scope（同一时刻只能有一枚按下，
              所以它们带 aria-pressed 并成一组），这一枚按下去改变一串条目的状态、
              按完回到原样。混在同一个 role="group" aria-label="articleFilters" 里，
              读屏念出的是"四条筛选"而其中一条根本不是筛选。
              图标是给 ≤620 那一档准备的：那边把文字标签 display:none 只留 38px 方盒，
              没有图标它就是个空盒子。 */}
          <button
            className="button button--quiet timeline-mark-read"
            type="button"
            aria-label={t("markAllRead")}
            title={t("markAllRead")}
            disabled={props.entries.length === 0 || props.markReadPending}
            onClick={props.onMarkAllRead}
          >
            {props.markReadPending ? <CircleNotch className="spin" /> : <CheckSquare />}
            {/* 可见文案与 aria-label 必须是同一句：这里按下去标的是当前筛选里的全部条目，
                而 WCAG 2.5.3 要求无障碍名字包含看得见那几个字——"Mark read" 不是
                "Mark all read" 的子串，两套说法就是不合格。窄屏把这一行藏掉时，
                无障碍名字仍由上面的 aria-label 提供。 */}
            <span>{t("markAllRead")}</span>
          </button>
        </div>
      </div>
      {props.scope.kind === "today" && (
        <TodayOverview entries={props.entries} subscriptions={props.subscriptions} />
      )}
      {props.isLoading ? (
        <TimelineSkeleton />
      ) : props.error ? (
        <div className="pane-state" role="alert">
          <WarningCircle aria-hidden="true" />
          <h2>{t("timelineUnavailable")}</h2>
          <p>{props.error.message}</p>
          <button className="button button--secondary" type="button" onClick={props.onRetry}>
            {t("retry")}
          </button>
        </div>
      ) : props.entries.length === 0 ? (
        <div className="timeline__empty">
          <div className="empty-mark" aria-hidden="true">
            <span />
            <span />
            <span />
          </div>
          <h2>{props.scope.kind === "unread" ? t("caughtUp") : t("readingTrailStarts")}</h2>
          <p>{props.scope.kind === "unread" ? t("unreadWillAppear") : t("addFeedOrImport")}</p>
          <button className="button button--primary" type="button" onClick={props.onAdd}>
            <Plus />
            {t("addFeed")}
          </button>
        </div>
      ) : (
        <div className="timeline-results">
          <div className="timeline-scroll" ref={scrollRef} onScroll={onScroll}>
            <div className="timeline-list" style={{ height: `${virtualizer.getTotalSize()}px` }}>
              {virtualItems.map((virtualItem) => {
                const entry = props.entries[virtualItem.index]
                if (!entry) return null
                return (
                  <div
                    className="timeline-list__row"
                    data-index={virtualItem.index}
                    key={entry.id}
                    ref={virtualizer.measureElement}
                    style={{ transform: `translateY(${virtualItem.start}px)` }}
                  >
                    <TimelineEntry
                      entry={entry}
                      selected={entry.id === props.selectedEntryID}
                      viewMode={viewMode}
                      feedIconURL={subscriptionIcons.get(entry.feed_id) ?? null}
                      showFeedIcon={props.scope.kind !== "feed"}
                      locale={locale}
                      t={t}
                      onSelect={() => props.onSelect(entry.id)}
                      onToggleStar={() => props.onToggleStar(entry)}
                    />
                  </div>
                )
              })}
            </div>
            {props.isFetchingNext && (
              <div className="timeline-loading-more">
                <CircleNotch className="spin" />
                {t("loading")}
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}

function TimelineEntry({
  entry,
  selected,
  viewMode,
  feedIconURL,
  showFeedIcon,
  locale,
  t,
  onSelect,
  onToggleStar,
}: {
  entry: Entry
  selected: boolean
  viewMode: ViewMode
  feedIconURL: string | null
  showFeedIcon: boolean
  locale: Locale
  t: Translator
  onSelect: () => void
  onToggleStar: () => void
}) {
  const [leadImageFailed, setLeadImageFailed] = useState(false)
  const hasImage = Boolean(entry.lead_image_url) && !leadImageFailed
  const displayAuthors = formatAuthors(entry.author)
  return (
    <article
      className={`timeline-entry timeline-entry--${viewMode}${hasImage ? " timeline-entry--has-image" : ""}${selected ? " timeline-entry--selected" : ""}${entry.state.is_read ? " timeline-entry--read" : ""}`}
    >
      {hasImage && <TimelineEntryImage entry={entry} onFailed={() => setLeadImageFailed(true)} />}
      <button
        className="timeline-entry__main"
        type="button"
        aria-current={selected ? "true" : undefined}
        onClick={onSelect}
      >
        <div className="timeline-entry__meta">
          <span className="timeline-entry__feed">
            {showFeedIcon && <TimelineFeedIcon entry={entry} iconURL={feedIconURL} />}
            {entry.feed_title}
          </span>
          <time dateTime={entry.published_at}>
            {formatRelativeTime(entry.published_at, locale)}
          </time>
        </div>
        <h2>{entry.title || t("untitled")}</h2>
        {entry.ai_translated_title && (
          <p className="timeline-entry__translation">{entry.ai_translated_title}</p>
        )}
        {(entry.ai_summary || entry.summary) && (
          <p
            className={
              entry.ai_summary
                ? "timeline-entry__summary timeline-entry__summary--ai"
                : "timeline-entry__summary"
            }
          >
            {entry.ai_summary ?? entry.summary}
          </p>
        )}
        {displayAuthors && <span className="timeline-entry__author">{displayAuthors}</span>}
        {entry.doi && (
          <span className="timeline-entry__doi" title={entry.doi}>
            DOI
          </span>
        )}
      </button>
      <button
        className={entry.state.is_starred ? "entry-star entry-star--active" : "entry-star"}
        type="button"
        aria-label={entry.state.is_starred ? t("removeStar") : t("starArticle")}
        title={entry.state.is_starred ? t("removeStar") : t("starArticle")}
        onClick={onToggleStar}
      >
        <Star weight={entry.state.is_starred ? "fill" : "regular"} />
      </button>
    </article>
  )
}

/**
 * Renders the per-entry lead image. Only mounted when the entry actually has
 * one: journal feeds carry no per-entry images, and falling back to the feed
 * icon here produced a column of identical thumbnails.
 */
function TimelineEntryImage({ entry, onFailed }: { entry: Entry; onFailed: () => void }) {
  return (
    <span className="timeline-entry__image" aria-hidden="true">
      <img
        src={entry.lead_image_url ?? ""}
        alt=""
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={onFailed}
      />
    </span>
  )
}

/** Small source marker shown in the meta line when the scope spans feeds. */
function TimelineFeedIcon({ entry, iconURL }: { entry: Entry; iconURL: string | null }) {
  const [failed, setFailed] = useState(false)
  if (!iconURL || failed) {
    return (
      <span className="timeline-entry__feed-icon timeline-entry__feed-icon--letter" aria-hidden="true">
        {entry.feed_title.slice(0, 1).toUpperCase()}
      </span>
    )
  }
  return (
    <span className="timeline-entry__feed-icon" aria-hidden="true">
      <img
        src={iconURL}
        alt=""
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
      />
    </span>
  )
}

function TodayOverview({
  entries,
  subscriptions,
}: {
  entries: Entry[]
  subscriptions: Subscription[]
}) {
  const { t } = useTranslation()
  const unread = entries.filter((entry) => !entry.state.is_read).length
  const activeSources = subscriptions.filter((subscription) => subscription.unread_count > 0)
  const topSources = [...activeSources]
    .sort((left, right) => right.unread_count - left.unread_count)
    .slice(0, 5)
  const peakUnread = Math.max(1, ...topSources.map((source) => source.unread_count))
  return (
    <section className="today-overview" aria-label={t("dailySignal")}>
      <div className="daily-signal">
        <div className="daily-signal__heading">
          <span className="daily-signal__icon" aria-hidden="true">
            <Sparkle weight="fill" />
          </span>
          <div>
            <p>{t("dailySignal")}</p>
            <h2>{t("todayBriefing")}</h2>
          </div>
        </div>
        <p className="daily-signal__description">{t("todayBriefingDescription")}</p>
        <dl>
          <div>
            <dt>{t("stories")}</dt>
            <dd>{entries.length}</dd>
          </div>
          <div>
            <dt>{t("sources")}</dt>
            <dd>{activeSources.length}</dd>
          </div>
          <div>
            <dt>{t("unread")}</dt>
            <dd>{unread}</dd>
          </div>
        </dl>
      </div>
      <div className="source-overview">
        <div className="source-overview__heading">
          <span>
            <i />
            {t("sourceOverview")}
          </span>
          <small>{t("orderedByActivity")}</small>
        </div>
        {topSources.length > 0 ? (
          <div className="source-overview__list">
            {topSources.map((source) => (
              <div className="source-overview__row" key={source.id}>
                <span className="source-overview__mark">
                  {source.title.slice(0, 1).toUpperCase()}
                </span>
                <strong>{source.title}</strong>
                <span className="source-overview__bar">
                  <i
                    style={{ width: `${Math.max(8, (source.unread_count / peakUnread) * 100)}%` }}
                  />
                </span>
                <small>{source.unread_count}</small>
              </div>
            ))}
          </div>
        ) : (
          <p className="source-overview__empty">{t("sourceOverviewEmpty")}</p>
        )}
      </div>
    </section>
  )
}

function TimelineSkeleton() {
  const { t } = useTranslation()
  return (
    <div className="timeline-skeleton" aria-label={t("loadingArticles")}>
      {Array.from({ length: 7 }, (_, index) => (
        <div className="skeleton-row" key={index}>
          <span />
          <span />
          <span />
        </div>
      ))}
    </div>
  )
}

function formatRelativeTime(value: string, locale: Locale) {
  const date = new Date(value)
  const deltaMinutes = Math.round((date.getTime() - Date.now()) / 60_000)
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto" })
  if (Math.abs(deltaMinutes) < 60) return formatter.format(deltaMinutes, "minute")
  const deltaHours = Math.round(deltaMinutes / 60)
  if (Math.abs(deltaHours) < 24) return formatter.format(deltaHours, "hour")
  const deltaDays = Math.round(deltaHours / 24)
  if (Math.abs(deltaDays) < 7) return formatter.format(deltaDays, "day")
  return new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(date)
}
