package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")

const feedColumns = `
	id, url, canonical_url, site_url, title, description, icon_url, format,
	content_kind, etag, last_modified, last_checked_at, last_success_at, next_check_at,
	failure_count, last_error_code, last_error_message, created_at, updated_at`

func GetFeed(ctx context.Context, db *sql.DB, feedID string) (domain.Feed, error) {
	row := db.QueryRowContext(ctx, "SELECT "+feedColumns+" FROM feeds WHERE id = ?", feedID)
	feed, err := scanFeed(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Feed{}, ErrNotFound
	}
	return feed, err
}

func GetFeedByCanonicalURL(ctx context.Context, db *sql.DB, canonicalURL string) (domain.Feed, error) {
	row := db.QueryRowContext(ctx, "SELECT "+feedColumns+" FROM feeds WHERE canonical_url = ?", canonicalURL)
	feed, err := scanFeed(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Feed{}, ErrNotFound
	}
	return feed, err
}

func ListFeeds(ctx context.Context, db *sql.DB, profileID string) ([]domain.Feed, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+qualifiedFeedColumns("f")+`
		FROM feeds f
		JOIN subscriptions s ON s.feed_id = f.id
		WHERE s.profile_id = ?
		ORDER BY COALESCE(s.title_override, f.title) COLLATE NOCASE, f.id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	defer rows.Close()

	feeds := make([]domain.Feed, 0)
	for rows.Next() {
		feed, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, feed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feeds: %w", err)
	}
	return feeds, nil
}

func SaveNewFeed(
	ctx context.Context,
	db *sql.DB,
	profileID string,
	sourceURL string,
	canonicalURL string,
	parsed domain.ParsedFeed,
	etag *string,
	lastModified *string,
	folderID *string,
	titleOverride *string,
) (domain.Feed, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Feed{}, fmt.Errorf("begin save feed transaction: %w", err)
	}
	defer tx.Rollback()

	feedID := uuid.NewString()
	now := time.Now().UTC()
	nextCheck := now.Add(30 * time.Minute)
	contentKind := domain.DetectContentKind(canonicalURL, parsed.SiteURL, parsed.Entries)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO feeds (
			id, url, canonical_url, site_url, title, description, icon_url, format,
			content_kind, etag, last_modified, last_checked_at, last_success_at, next_check_at,
			failure_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(canonical_url) DO UPDATE SET
			url = excluded.url,
			site_url = excluded.site_url,
			title = excluded.title,
			description = excluded.description,
			icon_url = excluded.icon_url,
			format = excluded.format,
			content_kind = CASE WHEN excluded.content_kind != 'general' THEN excluded.content_kind ELSE feeds.content_kind END,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			last_checked_at = excluded.last_checked_at,
			last_success_at = excluded.last_success_at,
			next_check_at = excluded.next_check_at,
			failure_count = 0,
			last_error_code = NULL,
			last_error_message = NULL,
			updated_at = excluded.updated_at`,
		feedID, sourceURL, canonicalURL, nullable(parsed.SiteURL), parsed.Title,
		nullable(parsed.Description), nullable(parsed.IconURL), parsed.Format,
		contentKind, nullable(etag), nullable(lastModified), formatTime(now), formatTime(now), formatTime(nextCheck),
		formatTime(now), formatTime(now),
	)
	if err != nil {
		return domain.Feed{}, fmt.Errorf("upsert feed: %w", err)
	}

	if err := tx.QueryRowContext(ctx, "SELECT id FROM feeds WHERE canonical_url = ?", canonicalURL).Scan(&feedID); err != nil {
		return domain.Feed{}, fmt.Errorf("resolve feed ID: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO subscriptions (
			id, profile_id, feed_id, folder_id, title_override, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, feed_id) DO UPDATE SET
			folder_id = COALESCE(excluded.folder_id, subscriptions.folder_id),
			title_override = COALESCE(excluded.title_override, subscriptions.title_override),
			updated_at = excluded.updated_at`,
		uuid.NewString(), profileID, feedID, nullable(folderID), nullable(titleOverride), formatTime(now), formatTime(now),
	)
	if err != nil {
		return domain.Feed{}, fmt.Errorf("upsert subscription: %w", err)
	}
	if err := rescheduleFeed(ctx, tx, feedID, now); err != nil {
		return domain.Feed{}, err
	}

	if _, err := upsertEntries(ctx, tx, profileID, feedID, parsed.Entries, now); err != nil {
		return domain.Feed{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Feed{}, fmt.Errorf("commit feed: %w", err)
	}
	return GetFeed(ctx, db, feedID)
}

func SaveFeedRefresh(
	ctx context.Context,
	db *sql.DB,
	profileID string,
	feedID string,
	parsed domain.ParsedFeed,
	etag *string,
	lastModified *string,
) ([]string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin refresh transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	nextCheck, err := nextFeedCheck(ctx, tx, feedID, now)
	if err != nil {
		return nil, err
	}
	contentKind := domain.DetectContentKind("", parsed.SiteURL, parsed.Entries)
	result, err := tx.ExecContext(ctx, `
		UPDATE feeds SET
			site_url = ?, title = ?, description = ?, icon_url = ?, format = ?,
			content_kind = CASE WHEN ? != 'general' THEN ? ELSE content_kind END,
			etag = ?, last_modified = ?, last_checked_at = ?, last_success_at = ?,
			next_check_at = ?, failure_count = 0, last_error_code = NULL,
			last_error_message = NULL, updated_at = ?
		WHERE id = ?`,
		nullable(parsed.SiteURL), parsed.Title, nullable(parsed.Description), nullable(parsed.IconURL), parsed.Format,
		contentKind, contentKind,
		nullable(etag), nullable(lastModified), formatTime(now), formatTime(now), formatTime(nextCheck), formatTime(now), feedID,
	)
	if err != nil {
		return nil, fmt.Errorf("update feed refresh: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil, ErrNotFound
	}

	inserted, err := upsertEntries(ctx, tx, profileID, feedID, parsed.Entries, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit feed refresh: %w", err)
	}
	return inserted, nil
}

func MarkFeedNotModified(ctx context.Context, db *sql.DB, feedID string) error {
	now := time.Now().UTC()
	nextCheck, err := nextFeedCheck(ctx, db, feedID, now)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		UPDATE feeds SET last_checked_at = ?, last_success_at = ?, next_check_at = ?,
			failure_count = 0, last_error_code = NULL, last_error_message = NULL, updated_at = ?
		WHERE id = ?`, formatTime(now), formatTime(now), formatTime(nextCheck), formatTime(now), feedID)
	if err != nil {
		return fmt.Errorf("mark feed not modified: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func MarkFeedFailure(ctx context.Context, db *sql.DB, feedID, code, message string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin feed failure update: %w", err)
	}
	defer tx.Rollback()

	var failureCount int
	if err := tx.QueryRowContext(ctx, "SELECT failure_count FROM feeds WHERE id = ?", feedID).Scan(&failureCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read feed failure count: %w", err)
	}

	now := time.Now().UTC()
	failureCount++
	backoffMinutes := min(360, 5*failureCount*failureCount)
	_, err = tx.ExecContext(ctx, `
		UPDATE feeds SET last_checked_at = ?, failure_count = failure_count + 1,
			last_error_code = ?, last_error_message = ?,
			next_check_at = ?, updated_at = ? WHERE id = ?`,
		formatTime(now), code, message, formatTime(now.Add(time.Duration(backoffMinutes)*time.Minute)), formatTime(now), feedID)
	if err != nil {
		return fmt.Errorf("mark feed failure: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit feed failure update: %w", err)
	}
	return nil
}

func DeleteSubscription(ctx context.Context, db *sql.DB, profileID, feedID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete subscription: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM subscriptions WHERE profile_id = ? AND feed_id = ?", profileID, feedID)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM feeds WHERE id = ? AND NOT EXISTS (
			SELECT 1 FROM subscriptions WHERE feed_id = feeds.id
		)`, feedID); err != nil {
		return fmt.Errorf("delete orphan feed: %w", err)
	}
	return tx.Commit()
}

func ListSubscriptions(ctx context.Context, db *sql.DB, profileID string) ([]domain.Subscription, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.profile_id, s.feed_id, s.folder_id, s.position,
			COALESCE(s.title_override, f.title), f.icon_url, f.url, f.site_url,
			(SELECT COUNT(*) FROM entries e
				LEFT JOIN entry_states es ON es.entry_id = e.id AND es.profile_id = s.profile_id
				WHERE e.feed_id = s.feed_id AND COALESCE(es.is_read, 0) = 0),
			s.view_mode, s.refresh_policy, s.refresh_interval_minutes,
			f.failure_count, f.last_error_code, f.last_error_message, f.last_success_at,
			s.hide_from_timeline, s.created_at, s.updated_at
		FROM subscriptions s
		JOIN feeds f ON f.id = s.feed_id
		WHERE s.profile_id = ?
		ORDER BY COALESCE(s.folder_id, ''), s.position, COALESCE(s.title_override, f.title) COLLATE NOCASE, s.id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Subscription, 0)
	for rows.Next() {
		var item domain.Subscription
		var folderID, iconURL, siteURL, lastErrorCode, lastErrorMessage, lastSuccessAt sql.NullString
		var feedURL string
		var hidden int
		var createdAt, updatedAt string
		if err := rows.Scan(
			&item.ID, &item.ProfileID, &item.FeedID, &folderID, &item.Position,
			&item.Title, &iconURL, &feedURL, &siteURL, &item.UnreadCount,
			&item.ViewMode, &item.RefreshPolicy, &item.RefreshIntervalMinutes,
			&item.FailureCount, &lastErrorCode, &lastErrorMessage, &lastSuccessAt,
			&hidden, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		item.FolderID = stringPointer(folderID)
		item.IconURL = stringPointer(iconURL)
		item.FeedURL = feedURL
		item.SiteURL = stringPointer(siteURL)
		item.LastErrorCode = stringPointer(lastErrorCode)
		item.LastErrorMessage = stringPointer(lastErrorMessage)
		item.LastSuccessAt = stringPointer(lastSuccessAt)
		item.HideFromTimeline = hidden == 1
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func UpdateSubscription(ctx context.Context, db *sql.DB, profileID, feedID string, patch domain.SubscriptionPatch) (domain.Subscription, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("begin subscription update: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	setFolder, setTitle := 0, 0
	setRefreshPolicy := 0
	if patch.SetFolderID {
		setFolder = 1
	}
	if patch.SetTitleOverride {
		setTitle = 1
	}
	if patch.RefreshPolicy != nil {
		setRefreshPolicy = 1
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET
			folder_id = CASE WHEN ? = 1 THEN ? ELSE folder_id END,
			title_override = CASE WHEN ? = 1 THEN ? ELSE title_override END,
			view_mode = COALESCE(?, view_mode),
			refresh_interval_minutes = COALESCE(?, refresh_interval_minutes),
			refresh_policy = CASE WHEN ? = 1 THEN ?
				WHEN ? IS NULL THEN refresh_policy
				WHEN ? > 0 THEN 'fixed' ELSE 'inherit' END,
			hide_from_timeline = COALESCE(?, hide_from_timeline),
			position = COALESCE(?, position),
			updated_at = ?
		WHERE profile_id = ? AND feed_id = ?`,
		setFolder, nullable(patch.FolderID), setTitle, nullable(patch.TitleOverride),
		nullable(patch.ViewMode), nullableInt(patch.RefreshIntervalMinutes), setRefreshPolicy, nullable(patch.RefreshPolicy), nullableInt(patch.RefreshIntervalMinutes), nullableInt(patch.RefreshIntervalMinutes),
		nullableBool(patch.HideFromTimeline), nullableInt(patch.Position), formatTime(now), profileID, feedID,
	)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("update subscription: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return domain.Subscription{}, ErrNotFound
	}
	if patch.RefreshPolicy != nil || patch.RefreshIntervalMinutes != nil {
		if err := rescheduleFeed(ctx, tx, feedID, now); err != nil {
			return domain.Subscription{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Subscription{}, fmt.Errorf("commit subscription update: %w", err)
	}

	items, err := ListSubscriptions(ctx, db, profileID)
	if err != nil {
		return domain.Subscription{}, err
	}
	for _, item := range items {
		if item.FeedID == feedID {
			return item, nil
		}
	}
	return domain.Subscription{}, ErrNotFound
}

func ListDueFeeds(ctx context.Context, db *sql.DB, limit int) ([]domain.Feed, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT `+feedColumns+` FROM feeds
		WHERE next_check_at IS NULL OR next_check_at <= ?
		ORDER BY COALESCE(next_check_at, created_at), id LIMIT ?`, formatTime(time.Now().UTC()), limit)
	if err != nil {
		return nil, fmt.Errorf("list due feeds: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Feed, 0)
	for rows.Next() {
		feed, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, feed)
	}
	return items, rows.Err()
}

type feedQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const (
	defaultFeedRefreshInterval = 30 * time.Minute
	neverFeedRefreshInterval   = 365 * 24 * time.Hour
)

func nextFeedCheck(ctx context.Context, queryer feedQueryer, feedID string, now time.Time) (time.Time, error) {
	var total, neverCount, fixedMinutes sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN refresh_policy = 'never' THEN 1 ELSE 0 END), 0),
			COALESCE(MIN(CASE WHEN refresh_policy = 'fixed' AND refresh_interval_minutes > 0
				THEN refresh_interval_minutes END), 0)
		FROM subscriptions WHERE feed_id = ?`, feedID).Scan(&total, &neverCount, &fixedMinutes); err != nil {
		return time.Time{}, fmt.Errorf("read feed refresh policy: %w", err)
	}
	if total.Valid && total.Int64 > 0 && neverCount.Valid && neverCount.Int64 == total.Int64 {
		return now.Add(neverFeedRefreshInterval), nil
	}
	if fixedMinutes.Valid && fixedMinutes.Int64 > 0 {
		return now.Add(time.Duration(fixedMinutes.Int64) * time.Minute), nil
	}
	return now.Add(defaultFeedRefreshInterval), nil
}

func rescheduleFeed(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, feedID string, now time.Time) error {
	nextCheck, err := nextFeedCheck(ctx, queryer, feedID, now)
	if err != nil {
		return err
	}
	if _, err := queryer.ExecContext(ctx,
		"UPDATE feeds SET next_check_at = ?, updated_at = ? WHERE id = ?",
		formatTime(nextCheck), formatTime(now), feedID,
	); err != nil {
		return fmt.Errorf("schedule feed refresh: %w", err)
	}
	return nil
}

func upsertEntries(ctx context.Context, tx *sql.Tx, profileID, feedID string, entries []domain.ParsedEntry, now time.Time) ([]string, error) {
	inserted := make([]string, 0, len(entries))
	for _, entry := range entries {
		entryID, exists, err := findEntryID(ctx, tx, feedID, entry)
		if err != nil {
			return nil, err
		}
		if exists {
			// Skip the write/FTS churn when nothing meaningful changed; the
			// content hash covers guid, canonical URL, title and body text.
			var storedHash, storedPublished string
			var storedAuthor, storedSummary sql.NullString
			err := tx.QueryRowContext(ctx,
				"SELECT content_hash, author, summary, published_at FROM entries WHERE id = ?", entryID).
				Scan(&storedHash, &storedAuthor, &storedSummary, &storedPublished)
			if err != nil {
				return nil, fmt.Errorf("read stored entry: %w", err)
			}
			if storedHash == entry.ContentHash &&
				nullStringEqual(storedAuthor, entry.Author) &&
				nullStringEqual(storedSummary, entry.Summary) &&
				storedPublished == formatTime(entry.PublishedAt) {
				continue
			}
		}
		if !exists {
			// The same paper can arrive through several feeds (journal +
			// aggregator); DOI identity wins over per-feed identity, so keep
			// only the first copy.
			if entry.DOI != nil && *entry.DOI != "" {
				var existingDOIEntry string
				lookupErr := tx.QueryRowContext(ctx, "SELECT id FROM entries WHERE doi = ? LIMIT 1", *entry.DOI).Scan(&existingDOIEntry)
				if lookupErr == nil {
					continue
				}
				if !errors.Is(lookupErr, sql.ErrNoRows) {
					return nil, fmt.Errorf("check entry DOI: %w", lookupErr)
				}
			}
			entryID = uuid.NewString()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO entries (
					id, feed_id, guid, canonical_url, title, author, summary,
					published_at, discovered_at, content_hash, identity_hash, lead_image_url,
					audio_url, video_url, language, doi, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				entryID, feedID, nullable(entry.GUID), nullable(entry.CanonicalURL), entry.Title,
				nullable(entry.Author), nullable(entry.Summary), formatTime(entry.PublishedAt), formatTime(now),
				entry.ContentHash, nullable(&entry.IdentityHash), nullable(entry.LeadImageURL), nullable(entry.AudioURL), nullable(entry.VideoURL),
				nullable(entry.Language), nullable(entry.DOI), formatTime(now), formatTime(now),
			)
			if err != nil {
				return nil, fmt.Errorf("insert entry: %w", err)
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO entry_states (profile_id, entry_id, updated_at) VALUES (?, ?, ?)`,
				profileID, entryID, formatTime(now))
			if err != nil {
				return nil, fmt.Errorf("insert entry state: %w", err)
			}
			inserted = append(inserted, entryID)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE entries SET guid = ?, canonical_url = ?, title = ?, author = ?, summary = ?,
					published_at = ?, content_hash = ?, identity_hash = ?, lead_image_url = ?, audio_url = ?,
					video_url = ?, language = ?, doi = ?, updated_at = ? WHERE id = ?`,
				nullable(entry.GUID), nullable(entry.CanonicalURL), entry.Title, nullable(entry.Author), nullable(entry.Summary),
				formatTime(entry.PublishedAt), entry.ContentHash, nullable(&entry.IdentityHash), nullable(entry.LeadImageURL), nullable(entry.AudioURL),
				nullable(entry.VideoURL), nullable(entry.Language), nullable(entry.DOI), formatTime(now), entryID,
			)
			if err != nil {
				return nil, fmt.Errorf("update entry: %w", err)
			}
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO entry_contents (entry_id, source_html, sanitized_html, plain_text, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(entry_id) DO UPDATE SET source_html = excluded.source_html,
				sanitized_html = excluded.sanitized_html, plain_text = excluded.plain_text,
				updated_at = excluded.updated_at`,
			entryID, entry.SourceHTML, entry.SanitizedHTML, entry.PlainText, formatTime(now),
		)
		if err != nil {
			return nil, fmt.Errorf("upsert entry content: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
			DELETE FROM entries_fts WHERE entry_id = ?`, entryID)
		if err != nil {
			return nil, fmt.Errorf("delete entry search row: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO entries_fts (entry_id, title, author, summary, plain_text)
			VALUES (?, ?, ?, ?, ?)`,
			entryID, entry.Title, valueOrEmpty(entry.Author), valueOrEmpty(entry.Summary), entry.PlainText,
		)
		if err != nil {
			return nil, fmt.Errorf("upsert entry search row: %w", err)
		}
	}
	return inserted, nil
}

func nullStringEqual(stored sql.NullString, incoming *string) bool {
	if incoming == nil {
		return !stored.Valid
	}
	return stored.Valid && stored.String == *incoming
}

func findEntryID(ctx context.Context, tx *sql.Tx, feedID string, entry domain.ParsedEntry) (string, bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM entries
		WHERE feed_id = ? AND (
			(? IS NOT NULL AND guid = ?) OR
			(? IS NOT NULL AND canonical_url = ?) OR
			content_hash = ? OR
			(? IS NOT NULL AND identity_hash = ?)
		)
		ORDER BY CASE WHEN guid = ? THEN 0 WHEN canonical_url = ? THEN 1
			WHEN identity_hash = ? THEN 2 ELSE 3 END
		LIMIT 1`,
		feedID,
		nullable(entry.GUID), nullable(entry.GUID),
		nullable(entry.CanonicalURL), nullable(entry.CanonicalURL),
		entry.ContentHash,
		nullable(&entry.IdentityHash), nullable(&entry.IdentityHash),
		nullable(entry.GUID), nullable(entry.CanonicalURL), nullable(&entry.IdentityHash),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find existing entry: %w", err)
	}
	return id, true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanFeed(row scanner) (domain.Feed, error) {
	var feed domain.Feed
	var siteURL, description, iconURL, format, etag, lastModified sql.NullString
	var lastChecked, lastSuccess, nextCheck sql.NullString
	var lastErrorCode, lastErrorMessage sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(
		&feed.ID, &feed.URL, &feed.CanonicalURL, &siteURL, &feed.Title, &description, &iconURL, &format,
		&feed.ContentKind, &etag, &lastModified, &lastChecked, &lastSuccess, &nextCheck,
		&feed.FailureCount, &lastErrorCode, &lastErrorMessage, &createdAt, &updatedAt,
	)
	if err != nil {
		return domain.Feed{}, err
	}
	feed.SiteURL = stringPointer(siteURL)
	feed.Description = stringPointer(description)
	feed.IconURL = stringPointer(iconURL)
	feed.Format = stringPointer(format)
	feed.ETag = stringPointer(etag)
	feed.LastModified = stringPointer(lastModified)
	feed.LastCheckedAt = timePointer(lastChecked)
	feed.LastSuccessAt = timePointer(lastSuccess)
	feed.NextCheckAt = timePointer(nextCheck)
	feed.LastErrorCode = stringPointer(lastErrorCode)
	feed.LastErrorMessage = stringPointer(lastErrorMessage)
	feed.CreatedAt = parseTime(createdAt)
	feed.UpdatedAt = parseTime(updatedAt)
	return feed, nil
}

func qualifiedFeedColumns(alias string) string {
	return alias + `.id, ` + alias + `.url, ` + alias + `.canonical_url, ` + alias + `.site_url, ` +
		alias + `.title, ` + alias + `.description, ` + alias + `.icon_url, ` + alias + `.format, ` +
		alias + `.content_kind, ` + alias + `.etag, ` + alias + `.last_modified, ` + alias + `.last_checked_at, ` +
		alias + `.last_success_at, ` + alias + `.next_check_at, ` + alias + `.failure_count, ` +
		alias + `.last_error_code, ` + alias + `.last_error_message, ` + alias + `.created_at, ` + alias + `.updated_at`
}

func nullable(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePointer(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := parseTime(value.String)
	return &parsed
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed
	}
	parsed, _ = time.Parse("2006-01-02 15:04:05", value)
	return parsed.UTC()
}
