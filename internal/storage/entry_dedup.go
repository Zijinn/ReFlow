package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	feedcore "github.com/Zijinn/ReFlow/internal/feed"
)

type entryIdentityRecord struct {
	id           string
	feedID       string
	guid         string
	canonicalURL string
	identityHash string
	createdAt    string
}

type pendingDedupEntry struct {
	entryIdentityRecord
	title, author, publishedAt, plainText string
}

// ReconcileEntryIdentities backfills identity data after the identity migration and
// merges duplicates within the same feed while preserving reading state and tags.
// Entries stream feed-by-feed so a large library never sits entirely in memory,
// and each feed merges in its own transaction.
func ReconcileEntryIdentities(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.feed_id, COALESCE(e.guid, ''), COALESCE(e.canonical_url, ''),
			COALESCE(e.identity_hash, ''), e.title, COALESCE(e.author, ''), e.published_at,
			COALESCE(ec.plain_text, ''), e.created_at
		FROM entries e
		LEFT JOIN entry_contents ec ON ec.entry_id = e.id
		ORDER BY e.feed_id, e.created_at, e.id`)
	if err != nil {
		return fmt.Errorf("list entries for deduplication: %w", err)
	}

	batch := make([]pendingDedupEntry, 0)
	currentFeed := ""
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := reconcileFeedEntries(ctx, db, batch)
		batch = batch[:0]
		return err
	}
	for rows.Next() {
		var entry pendingDedupEntry
		if err := rows.Scan(&entry.id, &entry.feedID, &entry.guid, &entry.canonicalURL, &entry.identityHash,
			&entry.title, &entry.author, &entry.publishedAt, &entry.plainText, &entry.createdAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan entry for deduplication: %w", err)
		}
		if currentFeed != "" && entry.feedID != currentFeed {
			if err := flush(); err != nil {
				rows.Close()
				return err
			}
		}
		currentFeed = entry.feedID
		batch = append(batch, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate entries for deduplication: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close entries for deduplication: %w", err)
	}
	return flush()
}

func reconcileFeedEntries(ctx context.Context, db *sql.DB, entries []pendingDedupEntry) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entry deduplication: %w", err)
	}
	defer tx.Rollback()
	groups := make(map[string][]entryIdentityRecord)
	normalizedURLs := make(map[string]string)
	for _, entry := range entries {
		canonicalURL := entry.canonicalURL
		if canonicalURL != "" {
			if normalized, normalizeErr := feedcore.NormalizeURL(canonicalURL); normalizeErr == nil {
				canonicalURL = normalized
				normalizedURLs[entry.id] = canonicalURL
			}
		}
		publishedAt, parseErr := time.Parse(time.RFC3339Nano, entry.publishedAt)
		var publishedPointer *time.Time
		if parseErr == nil {
			publishedPointer = &publishedAt
		}
		identityHash := feedcore.ComputeIdentityHash(entry.title, entry.author, publishedPointer, entry.plainText)
		if identityHash != entry.identityHash {
			if _, err := tx.ExecContext(ctx, "UPDATE entries SET identity_hash = ? WHERE id = ?", identityHash, entry.id); err != nil {
				return fmt.Errorf("backfill entry identity: %w", err)
			}
		}
		record := entryIdentityRecord{id: entry.id, feedID: entry.feedID, guid: entry.guid, canonicalURL: canonicalURL, identityHash: identityHash, createdAt: entry.createdAt}
		for _, key := range []string{
			identityKey("guid", record.guid),
			identityKey("url", record.canonicalURL),
			identityKey("identity", record.identityHash),
		} {
			if key != "" {
				groups[key] = append(groups[key], record)
			}
		}
	}

	seenPairs := make(map[string]bool)
	removed := make(map[string]bool)
	for _, group := range groups {
		active := make([]entryIdentityRecord, 0, len(group))
		for _, record := range group {
			if !removed[record.id] {
				active = append(active, record)
			}
		}
		if len(active) < 2 {
			continue
		}
		sort.SliceStable(active, func(i, j int) bool { return active[i].createdAt < active[j].createdAt })
		survivor := active[0]
		for _, duplicate := range active[1:] {
			pair := survivor.id + "\x00" + duplicate.id
			if seenPairs[pair] {
				continue
			}
			seenPairs[pair] = true
			if err := mergeEntry(ctx, tx, survivor.id, duplicate.id); err != nil {
				return err
			}
			removed[duplicate.id] = true
		}
	}
	for entryID, canonicalURL := range normalizedURLs {
		if removed[entryID] || canonicalURL == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE entries SET canonical_url = ? WHERE id = ?", canonicalURL, entryID); err != nil {
			return fmt.Errorf("normalize entry URL: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entry deduplication: %w", err)
	}
	return nil
}

func identityKey(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return kind + ":" + value
}

func mergeEntry(ctx context.Context, tx *sql.Tx, survivorID, duplicateID string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO entry_states (profile_id, entry_id, is_read, is_starred, is_read_later, read_at, updated_at, updated_by_device_id)
		SELECT profile_id, ?, is_read, is_starred, is_read_later, read_at, updated_at, updated_by_device_id
		FROM entry_states WHERE entry_id = ?
		ON CONFLICT(profile_id, entry_id) DO UPDATE SET
			is_read = MIN(entry_states.is_read, excluded.is_read),
			is_starred = MAX(entry_states.is_starred, excluded.is_starred),
			is_read_later = MAX(entry_states.is_read_later, excluded.is_read_later),
			read_at = COALESCE(entry_states.read_at, excluded.read_at),
			updated_at = MAX(entry_states.updated_at, excluded.updated_at),
			updated_by_device_id = COALESCE(entry_states.updated_by_device_id, excluded.updated_by_device_id)`, survivorID, duplicateID); err != nil {
		return fmt.Errorf("merge entry state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_tags(entry_id, tag_id) SELECT ?, tag_id FROM entry_tags WHERE entry_id = ?`, survivorID, duplicateID); err != nil {
		return fmt.Errorf("merge entry tags: %w", err)
	}
	// ai_results is unique on (profile_id, entry_id, operation, language,
	// input_hash): drop the duplicate's cached results that would collide with
	// the survivor's before re-pointing the rest.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM ai_results WHERE entry_id = ? AND EXISTS (
			SELECT 1 FROM ai_results survivor
			WHERE survivor.entry_id = ?
				AND survivor.profile_id = ai_results.profile_id
				AND survivor.operation = ai_results.operation
				AND survivor.language = ai_results.language
				AND survivor.input_hash = ai_results.input_hash
		)`, duplicateID, survivorID); err != nil {
		return fmt.Errorf("drop conflicting duplicate ai results: %w", err)
	}
	for _, statement := range []string{
		"UPDATE ai_results SET entry_id = ? WHERE entry_id = ?",
		"UPDATE ai_chat_sessions SET entry_id = ? WHERE entry_id = ?",
		"UPDATE ai_usage SET entry_id = ? WHERE entry_id = ?",
		"UPDATE entry_annotations SET entry_id = ? WHERE entry_id = ?",
	} {
		if _, err := tx.ExecContext(ctx, statement, survivorID, duplicateID); err != nil {
			return fmt.Errorf("merge entry references: %w", err)
		}
	}
	// entries_fts is a virtual table without foreign keys; drop the duplicate's
	// row before deleting the entry itself.
	if _, err := tx.ExecContext(ctx, "DELETE FROM entries_fts WHERE entry_id = ?", duplicateID); err != nil {
		return fmt.Errorf("delete duplicate search row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entries WHERE id = ?", duplicateID); err != nil {
		return fmt.Errorf("delete duplicate entry: %w", err)
	}
	return nil
}

// ReconcileEntryDOIs merges entries that share a DOI across feeds — the same
// paper arriving through both a journal feed and an aggregator. Entries stream
// ordered by DOI so each group merges in its own transaction; the earliest
// copy survives.
func ReconcileEntryDOIs(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, created_at, doi FROM entries
		WHERE doi IS NOT NULL AND doi != ''
		ORDER BY doi, created_at, id`)
	if err != nil {
		return fmt.Errorf("list entries for DOI deduplication: %w", err)
	}
	type doiRecord struct{ id, createdAt string }
	currentDOI := ""
	group := make([]doiRecord, 0)
	flush := func() error {
		if len(group) < 2 {
			group = group[:0]
			return nil
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin DOI deduplication: %w", err)
		}
		defer tx.Rollback()
		survivor := group[0]
		for _, duplicate := range group[1:] {
			if err := mergeEntry(ctx, tx, survivor.id, duplicate.id); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit DOI deduplication: %w", err)
		}
		group = group[:0]
		return nil
	}
	for rows.Next() {
		var record doiRecord
		var doi string
		if err := rows.Scan(&record.id, &record.createdAt, &doi); err != nil {
			rows.Close()
			return fmt.Errorf("scan entry DOI: %w", err)
		}
		if doi != currentDOI {
			if err := flush(); err != nil {
				rows.Close()
				return err
			}
			currentDOI = doi
		}
		group = append(group, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate entries for DOI deduplication: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close entry DOI rows: %w", err)
	}
	return flush()
}
