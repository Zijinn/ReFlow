package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Zijinn/ReFlow/internal/domain"
)

type entryCursor struct {
	PublishedAt string `json:"published_at"`
	ID          string `json:"id"`
}

func ListEntries(ctx context.Context, db *sql.DB, filter domain.EntryFilter) (domain.EntryPage, error) {
	if filter.ProfileID == "" {
		filter.ProfileID = domain.DefaultProfileID
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 30
	}

	aiLanguage := strings.TrimSpace(filter.AILanguage)
	if aiLanguage == "" {
		aiLanguage = "English"
	}
	args := []any{aiLanguage, aiLanguage, filter.ProfileID}
	joins := ""
	conditions := []string{"s.profile_id = ?", "s.hide_from_timeline = 0"}
	if filter.FeedID != "" {
		conditions = append(conditions, "e.feed_id = ?")
		args = append(args, filter.FeedID)
	}
	if filter.FolderID != "" {
		conditions = append(conditions, "s.folder_id = ?")
		args = append(args, filter.FolderID)
	}
	if filter.TagID != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM entry_tags et WHERE et.entry_id = e.id AND et.tag_id = ?)")
		args = append(args, filter.TagID)
	}
	if filter.ContentKind != "" {
		conditions = append(conditions, "f.content_kind = ?")
		args = append(args, filter.ContentKind)
	}
	if filter.Since != nil {
		conditions = append(conditions, "e.published_at >= ?")
		args = append(args, formatTime(*filter.Since))
	}
	switch filter.State {
	case "unread":
		conditions = append(conditions, "COALESCE(es.is_read, 0) = 0")
	case "starred":
		conditions = append(conditions, "COALESCE(es.is_starred, 0) = 1")
	case "read_later":
		conditions = append(conditions, "COALESCE(es.is_read_later, 0) = 1")
	}
	if strings.TrimSpace(filter.Query) != "" {
		joins, conditions, args = appendSearchFilter(filter.Query, joins, conditions, args)
	}
	if filter.Cursor != "" {
		cursor, err := decodeEntryCursor(filter.Cursor)
		if err != nil {
			return domain.EntryPage{}, err
		}
		conditions = append(conditions, "(e.published_at < ? OR (e.published_at = ? AND e.id < ?))")
		args = append(args, cursor.PublishedAt, cursor.PublishedAt, cursor.ID)
	}

	args = append(args, filter.Limit+1)
	query := `
		SELECT e.id, e.feed_id, COALESCE(s.title_override, f.title), e.guid,
			e.canonical_url, e.title, e.author, e.summary, e.published_at,
			e.discovered_at, e.content_hash, e.lead_image_url, e.audio_url,
			e.video_url, e.language, e.doi,
			(SELECT ar.result_text FROM ai_results ar
				WHERE ar.profile_id = s.profile_id AND ar.entry_id = e.id
					AND ar.operation = 'title_translation' AND ar.language = ?
				ORDER BY ar.created_at DESC LIMIT 1),
			(SELECT ar.result_text FROM ai_results ar
				WHERE ar.profile_id = s.profile_id AND ar.entry_id = e.id
					AND ar.operation = 'summary' AND ar.language = ?
				ORDER BY ar.created_at DESC LIMIT 1),
			COALESCE(es.is_read, 0), COALESCE(es.is_starred, 0),
			COALESCE(es.is_read_later, 0), COALESCE(es.updated_at, e.discovered_at),
			(SELECT GROUP_CONCAT(et.tag_id) FROM entry_tags et WHERE et.entry_id = e.id)
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		JOIN subscriptions s ON s.feed_id = e.feed_id
		LEFT JOIN entry_states es ON es.entry_id = e.id AND es.profile_id = s.profile_id
		` + joins + `
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY e.published_at DESC, e.id DESC
		LIMIT ?`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return domain.EntryPage{}, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Entry, 0, filter.Limit+1)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return domain.EntryPage{}, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return domain.EntryPage{}, fmt.Errorf("iterate entries: %w", err)
	}

	page := domain.EntryPage{Items: items}
	if len(items) > filter.Limit {
		page.Items = items[:filter.Limit]
		last := page.Items[len(page.Items)-1]
		cursor, err := encodeEntryCursor(last)
		if err != nil {
			return domain.EntryPage{}, err
		}
		page.NextCursor = &cursor
	}
	return page, nil
}

func MarkEntriesRead(ctx context.Context, db *sql.DB, filter domain.EntryFilter) (int64, error) {
	if filter.ProfileID == "" {
		filter.ProfileID = domain.DefaultProfileID
	}
	args := []any{filter.ProfileID}
	conditions := []string{"s.profile_id = ?", "s.hide_from_timeline = 0", "COALESCE(es.is_read, 0) = 0"}
	if filter.FeedID != "" {
		conditions = append(conditions, "e.feed_id = ?")
		args = append(args, filter.FeedID)
	}
	if filter.FolderID != "" {
		conditions = append(conditions, "s.folder_id = ?")
		args = append(args, filter.FolderID)
	}
	if filter.TagID != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM entry_tags et WHERE et.entry_id = e.id AND et.tag_id = ?)")
		args = append(args, filter.TagID)
	}
	if filter.ContentKind != "" {
		conditions = append(conditions, "f.content_kind = ?")
		args = append(args, filter.ContentKind)
	}
	if filter.Since != nil {
		conditions = append(conditions, "e.published_at >= ?")
		args = append(args, formatTime(*filter.Since))
	}
	switch filter.State {
	case "starred":
		conditions = append(conditions, "COALESCE(es.is_starred, 0) = 1")
	case "read_later":
		conditions = append(conditions, "COALESCE(es.is_read_later, 0) = 1")
	}
	joins := ""
	if strings.TrimSpace(filter.Query) != "" {
		joins, conditions, args = appendSearchFilter(filter.Query, joins, conditions, args)
	}

	// One statement instead of one upsert per row: with a large library the
	// per-row loop held a single write transaction long enough for other
	// writers to hit busy_timeout.
	now := formatTime(time.Now().UTC())
	query := `
		INSERT INTO entry_states (profile_id, entry_id, is_read, read_at, updated_at)
		SELECT ?, e.id, 1, ?, ?
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		JOIN subscriptions s ON s.feed_id = e.feed_id
		LEFT JOIN entry_states es ON es.entry_id = e.id AND es.profile_id = s.profile_id
		` + joins + `
		WHERE ` + strings.Join(conditions, " AND ") + `
		ON CONFLICT(profile_id, entry_id) DO UPDATE SET
			is_read = 1, read_at = COALESCE(entry_states.read_at, excluded.read_at),
			updated_at = excluded.updated_at`
	result, err := db.ExecContext(ctx, query, append([]any{filter.ProfileID, now, now}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("mark entries read: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mark entries read count: %w", err)
	}
	return affected, nil
}

func SaveReadabilityContent(ctx context.Context, db *sql.DB, entryID, sanitizedHTML, plainText string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin readability update: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE entry_contents SET readability_html = ?, readability_text = ?, readability_fetched_at = ?, updated_at = ?
		WHERE entry_id = ?`, sanitizedHTML, plainText, formatTime(now), formatTime(now), entryID)
	if err != nil {
		return fmt.Errorf("save readability content: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	var title, author, summary string
	if err := tx.QueryRowContext(ctx, `
		SELECT e.title, COALESCE(e.author, ''), COALESCE(e.summary, '')
		FROM entries e WHERE e.id = ?`, entryID).Scan(&title, &author, &summary); err != nil {
		return fmt.Errorf("read entry search fields: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entries_fts WHERE entry_id = ?", entryID); err != nil {
		return fmt.Errorf("replace readability search row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO entries_fts (entry_id, title, author, summary, plain_text)
		VALUES (?, ?, ?, ?, ?)`, entryID, title, author, summary, plainText); err != nil {
		return fmt.Errorf("index readability content: %w", err)
	}
	return tx.Commit()
}

func GetEntry(ctx context.Context, db *sql.DB, profileID, entryID string, requestedLanguage ...string) (domain.Entry, error) {
	aiLanguage := "English"
	if len(requestedLanguage) > 0 && strings.TrimSpace(requestedLanguage[0]) != "" {
		aiLanguage = strings.TrimSpace(requestedLanguage[0])
	}
	row := db.QueryRowContext(ctx, `
		SELECT e.id, e.feed_id, COALESCE(s.title_override, f.title), e.guid,
			e.canonical_url, e.title, e.author, e.summary, e.published_at,
			e.discovered_at, e.content_hash, e.lead_image_url, e.audio_url,
			e.video_url, e.language, e.doi,
			(SELECT ar.result_text FROM ai_results ar
				WHERE ar.profile_id = s.profile_id AND ar.entry_id = e.id
					AND ar.operation = 'title_translation' AND ar.language = ?
				ORDER BY ar.created_at DESC LIMIT 1),
			(SELECT ar.result_text FROM ai_results ar
				WHERE ar.profile_id = s.profile_id AND ar.entry_id = e.id
					AND ar.operation = 'summary' AND ar.language = ?
				ORDER BY ar.created_at DESC LIMIT 1),
			COALESCE(es.is_read, 0), COALESCE(es.is_starred, 0),
			COALESCE(es.is_read_later, 0), COALESCE(es.updated_at, e.discovered_at),
			COALESCE(ec.sanitized_html, ''), ec.readability_html,
			(SELECT GROUP_CONCAT(et.tag_id) FROM entry_tags et WHERE et.entry_id = e.id)
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.profile_id = ?
		LEFT JOIN entry_states es ON es.entry_id = e.id AND es.profile_id = s.profile_id
		LEFT JOIN entry_contents ec ON ec.entry_id = e.id
		WHERE e.id = ?`, aiLanguage, aiLanguage, profileID, entryID)
	entry, err := scanEntryDetail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Entry{}, ErrNotFound
	}
	return entry, err
}

func UpdateEntryState(ctx context.Context, db *sql.DB, profileID, entryID string, patch domain.EntryStatePatch) (domain.EntryState, error) {
	if patch.MutationID == "" {
		return domain.EntryState{}, errors.New("mutation ID is required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.EntryState{}, fmt.Errorf("begin entry state update: %w", err)
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM entries e
			JOIN subscriptions s ON s.feed_id = e.feed_id
			WHERE e.id = ? AND s.profile_id = ?
		)`, entryID, profileID).Scan(&exists); err != nil {
		return domain.EntryState{}, fmt.Errorf("authorize entry state update: %w", err)
	}
	if !exists {
		return domain.EntryState{}, ErrNotFound
	}

	var alreadyProcessed bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM processed_mutations WHERE mutation_id = ?)", patch.MutationID,
	).Scan(&alreadyProcessed); err != nil {
		return domain.EntryState{}, fmt.Errorf("check mutation: %w", err)
	}
	if !alreadyProcessed {
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO entry_states (
				profile_id, entry_id, is_read, is_starred, is_read_later,
				read_at, updated_at, updated_by_device_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(profile_id, entry_id) DO UPDATE SET
				is_read = COALESCE(?, entry_states.is_read),
				is_starred = COALESCE(?, entry_states.is_starred),
				is_read_later = COALESCE(?, entry_states.is_read_later),
				read_at = CASE WHEN COALESCE(?, entry_states.is_read) = 1
					THEN COALESCE(entry_states.read_at, excluded.read_at) ELSE NULL END,
				updated_at = excluded.updated_at,
				updated_by_device_id = excluded.updated_by_device_id`,
			profileID, entryID, boolValue(patch.IsRead, false), boolValue(patch.IsStarred, false),
			boolValue(patch.IsReadLater, false), readAtValue(patch.IsRead, now), formatTime(now), nullable(patch.DeviceID),
			nullableBool(patch.IsRead), nullableBool(patch.IsStarred), nullableBool(patch.IsReadLater), nullableBool(patch.IsRead),
		)
		if err != nil {
			return domain.EntryState{}, fmt.Errorf("upsert entry state: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO processed_mutations(mutation_id, device_id) VALUES (?, ?)",
			patch.MutationID, nullable(patch.DeviceID),
		); err != nil {
			return domain.EntryState{}, fmt.Errorf("record mutation: %w", err)
		}
	}

	state, err := getEntryState(ctx, tx, profileID, entryID)
	if err != nil {
		return domain.EntryState{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.EntryState{}, fmt.Errorf("commit entry state: %w", err)
	}
	return state, nil
}

// PruneProcessedMutations removes idempotency records older than the cutoff;
// mutation IDs only need to be remembered long enough for offline clients to
// replay their outbox.
func PruneProcessedMutations(ctx context.Context, db *sql.DB, olderThan time.Time) (int64, error) {
	result, err := db.ExecContext(ctx, "DELETE FROM processed_mutations WHERE processed_at < ?", formatTime(olderThan))
	if err != nil {
		return 0, fmt.Errorf("prune processed mutations: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// PruneReadEntries applies the retention policy: read entries published before
// the cutoff are deleted, while starred, read-later, and annotated entries are
// always kept. The FTS index has no foreign keys, so its rows go first.
func PruneReadEntries(ctx context.Context, db *sql.DB, profileID string, publishedBefore time.Time) (int64, error) {
	const selection = `
		SELECT e.id FROM entries e
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.profile_id = ?
		LEFT JOIN entry_states es ON es.entry_id = e.id AND es.profile_id = s.profile_id
		WHERE e.published_at < ?
			AND COALESCE(es.is_read, 0) = 1
			AND COALESCE(es.is_starred, 0) = 0
			AND COALESCE(es.is_read_later, 0) = 0
			AND NOT EXISTS (SELECT 1 FROM entry_annotations a WHERE a.entry_id = e.id)`
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin retention prune: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM entries_fts WHERE entry_id IN ("+selection+")", profileID, formatTime(publishedBefore)); err != nil {
		return 0, fmt.Errorf("prune search index: %w", err)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM entries WHERE id IN ("+selection+")", profileID, formatTime(publishedBefore))
	if err != nil {
		return 0, fmt.Errorf("prune read entries: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit retention prune: %w", err)
	}
	return affected, nil
}

func getEntryState(ctx context.Context, tx *sql.Tx, profileID, entryID string) (domain.EntryState, error) {
	var state domain.EntryState
	var isRead, isStarred, isReadLater int
	var updatedAt string
	if err := tx.QueryRowContext(ctx, `
		SELECT is_read, is_starred, is_read_later, updated_at
		FROM entry_states WHERE profile_id = ? AND entry_id = ?`, profileID, entryID,
	).Scan(&isRead, &isStarred, &isReadLater, &updatedAt); err != nil {
		return domain.EntryState{}, fmt.Errorf("get entry state: %w", err)
	}
	state.IsRead = isRead == 1
	state.IsStarred = isStarred == 1
	state.IsReadLater = isReadLater == 1
	state.UpdatedAt = parseTime(updatedAt)
	return state, nil
}

func scanEntry(row scanner) (domain.Entry, error) {
	var entry domain.Entry
	var guid, canonicalURL, author, summary, leadImage, audio, video, language, doi, translatedTitle, aiSummary, tagIDs sql.NullString
	var publishedAt, discoveredAt, stateUpdatedAt string
	var isRead, isStarred, isReadLater int
	if err := row.Scan(
		&entry.ID, &entry.FeedID, &entry.FeedTitle, &guid,
		&canonicalURL, &entry.Title, &author, &summary, &publishedAt,
		&discoveredAt, &entry.ContentHash, &leadImage, &audio, &video, &language, &doi, &translatedTitle, &aiSummary,
		&isRead, &isStarred, &isReadLater, &stateUpdatedAt, &tagIDs,
	); err != nil {
		return domain.Entry{}, fmt.Errorf("scan entry: %w", err)
	}
	populateEntry(&entry, guid, canonicalURL, author, summary, leadImage, audio, video, language, publishedAt, discoveredAt, isRead, isStarred, isReadLater, stateUpdatedAt)
	entry.AITranslatedTitle = stringPointer(translatedTitle)
	entry.DOI = stringPointer(doi)
	entry.AISummary = stringPointer(aiSummary)
	entry.TagIDs = splitTagIDs(tagIDs)
	return entry, nil
}

func scanEntryDetail(row scanner) (domain.Entry, error) {
	var entry domain.Entry
	var guid, canonicalURL, author, summary, leadImage, audio, video, language, doi, translatedTitle, aiSummary, tagIDs sql.NullString
	var publishedAt, discoveredAt, stateUpdatedAt string
	var isRead, isStarred, isReadLater int
	var readability sql.NullString
	if err := row.Scan(
		&entry.ID, &entry.FeedID, &entry.FeedTitle, &guid,
		&canonicalURL, &entry.Title, &author, &summary, &publishedAt,
		&discoveredAt, &entry.ContentHash, &leadImage, &audio, &video, &language, &doi, &translatedTitle, &aiSummary,
		&isRead, &isStarred, &isReadLater, &stateUpdatedAt,
		&entry.SanitizedHTML, &readability, &tagIDs,
	); err != nil {
		return domain.Entry{}, err
	}
	populateEntry(&entry, guid, canonicalURL, author, summary, leadImage, audio, video, language, publishedAt, discoveredAt, isRead, isStarred, isReadLater, stateUpdatedAt)
	entry.AITranslatedTitle = stringPointer(translatedTitle)
	entry.DOI = stringPointer(doi)
	entry.AISummary = stringPointer(aiSummary)
	entry.ReadabilityHTML = stringPointer(readability)
	entry.TagIDs = splitTagIDs(tagIDs)
	return entry, nil
}

func splitTagIDs(value sql.NullString) []string {
	if !value.Valid || value.String == "" {
		return []string{}
	}
	return strings.Split(value.String, ",")
}

func populateEntry(
	entry *domain.Entry,
	guid, canonicalURL, author, summary, leadImage, audio, video, language sql.NullString,
	publishedAt, discoveredAt string,
	isRead, isStarred, isReadLater int,
	stateUpdatedAt string,
) {
	entry.GUID = stringPointer(guid)
	entry.CanonicalURL = stringPointer(canonicalURL)
	entry.Author = stringPointer(author)
	entry.Summary = stringPointer(summary)
	entry.LeadImageURL = stringPointer(leadImage)
	entry.AudioURL = stringPointer(audio)
	entry.VideoURL = stringPointer(video)
	entry.Language = stringPointer(language)
	entry.PublishedAt = parseTime(publishedAt)
	entry.DiscoveredAt = parseTime(discoveredAt)
	entry.State = domain.EntryState{
		IsRead:      isRead == 1,
		IsStarred:   isStarred == 1,
		IsReadLater: isReadLater == 1,
		UpdatedAt:   parseTime(stateUpdatedAt),
	}
}

func encodeEntryCursor(entry domain.Entry) (string, error) {
	body, err := json.Marshal(entryCursor{PublishedAt: formatTime(entry.PublishedAt), ID: entry.ID})
	if err != nil {
		return "", fmt.Errorf("encode entry cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func decodeEntryCursor(value string) (entryCursor, error) {
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return entryCursor{}, errors.New("invalid entry cursor")
	}
	var cursor entryCursor
	if err := json.Unmarshal(body, &cursor); err != nil || cursor.ID == "" || cursor.PublishedAt == "" {
		return entryCursor{}, errors.New("invalid entry cursor")
	}
	return cursor, nil
}

func escapeFTSQuery(value string) string {
	words := strings.Fields(value)
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		quoted = append(quoted, `"`+strings.ReplaceAll(word, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND ")
}

// appendSearchFilter routes full-text search through entries_fts, falling back
// to LIKE when any word is shorter than three runes: the trigram tokenizer
// cannot index or match tokens below three characters.
func appendSearchFilter(query, joins string, conditions []string, args []any) (string, []string, []any) {
	words := strings.Fields(query)
	useFTS := len(words) > 0
	for _, word := range words {
		if utf8.RuneCountInString(word) < 3 {
			useFTS = false
			break
		}
	}
	if useFTS {
		joins += " JOIN entries_fts ON entries_fts.entry_id = e.id "
		conditions = append(conditions, "entries_fts MATCH ?")
		args = append(args, escapeFTSQuery(query))
		return joins, conditions, args
	}
	for _, word := range words {
		like := "%" + escapeLikePattern(word) + "%"
		conditions = append(conditions,
			`(e.title LIKE ? ESCAPE '\' OR e.author LIKE ? ESCAPE '\' OR e.summary LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	return joins, conditions, args
}

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func boolValue(value *bool, fallback bool) int {
	if value == nil {
		if fallback {
			return 1
		}
		return 0
	}
	if *value {
		return 1
	}
	return 0
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return boolValue(value, false)
}

func readAtValue(isRead *bool, now time.Time) any {
	if isRead != nil && *isRead {
		return formatTime(now)
	}
	return nil
}
