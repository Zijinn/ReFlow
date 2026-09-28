package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const LibrarySnapshotFormat = "reflow-library-snapshot"

var librarySnapshotTables = []string{
	"folders", "feeds", "subscriptions", "entries", "entry_contents", "entry_states",
	"entry_annotations", "tags", "feed_tags", "entry_tags", "rules", "saved_filters", "preferences",
	"research_papers",
}

// ExportLibrarySnapshot intentionally excludes device tokens, background jobs,
// provider credentials, and local sync metadata so a snapshot stays portable.
func ExportLibrarySnapshot(ctx context.Context, db *sql.DB) (BackupDocument, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BackupDocument{}, fmt.Errorf("begin library snapshot: %w", err)
	}
	defer tx.Rollback()

	document := BackupDocument{
		Format: LibrarySnapshotFormat, Version: 1, CreatedAt: time.Now().UTC(),
		Tables: make([]BackupTable, 0, len(librarySnapshotTables)),
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&document.SchemaVersion); err != nil {
		return BackupDocument{}, fmt.Errorf("read schema version: %w", err)
	}
	for _, name := range librarySnapshotTables {
		table, err := exportTable(ctx, tx, name)
		if err != nil {
			return BackupDocument{}, err
		}
		makeSnapshotPortable(&table)
		sort.Slice(table.Rows, func(i, j int) bool {
			left, _ := json.Marshal(table.Rows[i])
			right, _ := json.Marshal(table.Rows[j])
			return string(left) < string(right)
		})
		document.Tables = append(document.Tables, table)
	}
	if err := tx.Commit(); err != nil {
		return BackupDocument{}, fmt.Errorf("commit library snapshot: %w", err)
	}
	return document, nil
}

func RestoreLibrarySnapshot(ctx context.Context, db *sql.DB, document BackupDocument) error {
	if document.Format != LibrarySnapshotFormat || document.Version != 1 {
		return errors.New("unsupported ReFlow library snapshot format")
	}
	var currentSchema int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentSchema); err != nil {
		return fmt.Errorf("read current schema version: %w", err)
	}
	if document.SchemaVersion != currentSchema {
		return fmt.Errorf("snapshot schema version %d does not match ReFlow schema version %d", document.SchemaVersion, currentSchema)
	}
	allowed := make(map[string]struct{}, len(librarySnapshotTables))
	for _, name := range librarySnapshotTables {
		allowed[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(document.Tables))
	for _, table := range document.Tables {
		if _, ok := allowed[table.Name]; !ok {
			return fmt.Errorf("snapshot contains unsupported table %q", table.Name)
		}
		if _, duplicate := seen[table.Name]; duplicate {
			return fmt.Errorf("snapshot contains duplicate table %q", table.Name)
		}
		seen[table.Name] = struct{}{}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return fmt.Errorf("snapshot table %q has a row with the wrong column count", table.Name)
			}
		}
	}
	for _, name := range librarySnapshotTables {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("snapshot is missing table %q", name)
		}
	}

	connection, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire snapshot restore connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for snapshot restore: %w", err)
	}
	defer connection.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot restore: %w", err)
	}
	defer tx.Rollback()

	for index := len(librarySnapshotTables) - 1; index >= 0; index-- {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(librarySnapshotTables[index])); err != nil {
			return fmt.Errorf("clear snapshot table %s: %w", librarySnapshotTables[index], err)
		}
	}
	for _, table := range document.Tables {
		currentColumns, err := tableColumns(ctx, tx, table.Name)
		if err != nil {
			return err
		}
		if !sameStrings(currentColumns, table.Columns) {
			return fmt.Errorf("snapshot table %q columns do not match the current schema", table.Name)
		}
		if len(table.Rows) == 0 {
			continue
		}
		quotedColumns := make([]string, len(table.Columns))
		placeholders := make([]string, len(table.Columns))
		for index, column := range table.Columns {
			quotedColumns[index] = quoteIdentifier(column)
			placeholders[index] = "?"
		}
		statement := "INSERT INTO " + quoteIdentifier(table.Name) + " (" + strings.Join(quotedColumns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
		for _, row := range table.Rows {
			values := make([]any, len(row))
			for index, value := range row {
				decoded, decodeErr := value.decode()
				if decodeErr != nil {
					return fmt.Errorf("decode %s row: %w", table.Name, decodeErr)
				}
				values[index] = decoded
			}
			if _, err := tx.ExecContext(ctx, statement, values...); err != nil {
				return fmt.Errorf("restore snapshot table %s: %w", table.Name, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM ai_chat_messages WHERE session_id IN (
			SELECT id FROM ai_chat_sessions
			WHERE entry_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = ai_chat_sessions.entry_id)
		);
		DELETE FROM ai_chat_sessions
		WHERE entry_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = ai_chat_sessions.entry_id);
		DELETE FROM ai_results
		WHERE NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = ai_results.entry_id);
		UPDATE ai_usage SET entry_id = NULL
		WHERE entry_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = ai_usage.entry_id);
		DELETE FROM sync_mappings WHERE local_kind = 'entry'
			AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = sync_mappings.local_id);
		DELETE FROM sync_mappings WHERE local_kind = 'feed'
			AND NOT EXISTS (SELECT 1 FROM feeds f WHERE f.id = sync_mappings.local_id);
		DELETE FROM zotero_exports
		WHERE NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = zotero_exports.entry_id);
	`); err != nil {
		return fmt.Errorf("clean local snapshot references: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entries_fts"); err != nil {
		return fmt.Errorf("clear search index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO entries_fts (entry_id, title, author, summary, plain_text)
		SELECT e.id, e.title, COALESCE(e.author, ''), COALESCE(e.summary, ''),
			COALESCE(NULLIF(ec.readability_text, ''), ec.plain_text, '')
			FROM entries e LEFT JOIN entry_contents ec ON ec.entry_id = e.id`); err != nil {
		return fmt.Errorf("rebuild search index: %w", err)
	}
	violations, err := countForeignKeyViolations(ctx, tx)
	if err != nil {
		return err
	}
	if violations > 0 {
		return fmt.Errorf("library snapshot restore would leave %d foreign key violations", violations)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot restore: %w", err)
	}
	return nil
}

// feedsPollingColumns are maintained by the refresh loop and change on every
// poll — including bare 304 responses — without any user-visible change.
// Hashing them marked the library "locally changed" around the clock, which
// trapped multi-device sync in a permanent both-sides-changed conflict. The
// values still travel inside the snapshot payload; they are only excluded
// from change detection.
var feedsPollingColumns = []string{
	"etag", "last_modified", "last_checked_at", "last_success_at",
	"next_check_at", "failure_count", "last_error_code", "last_error_message",
	"updated_at",
}

func LibrarySnapshotFingerprint(document BackupDocument) (string, error) {
	tables := document.Tables
	copied := false
	for index, table := range tables {
		if table.Name != "feeds" {
			continue
		}
		if sanitized := sanitizeFeedsForFingerprint(table); sanitized != nil {
			if !copied {
				tables = make([]BackupTable, len(document.Tables))
				copy(tables, document.Tables)
				copied = true
			}
			tables[index] = *sanitized
		}
	}
	body, err := json.Marshal(struct {
		Version       int           `json:"version"`
		SchemaVersion int           `json:"schema_version"`
		Tables        []BackupTable `json:"tables"`
	}{Version: document.Version, SchemaVersion: document.SchemaVersion, Tables: tables})
	if err != nil {
		return "", fmt.Errorf("encode snapshot fingerprint: %w", err)
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func sanitizeFeedsForFingerprint(table BackupTable) *BackupTable {
	volatile := make(map[int]struct{}, len(feedsPollingColumns))
	for columnIndex, column := range table.Columns {
		for _, name := range feedsPollingColumns {
			if column == name {
				volatile[columnIndex] = struct{}{}
			}
		}
	}
	if len(volatile) == 0 {
		return nil
	}
	rows := make([][]BackupValue, len(table.Rows))
	for rowIndex, row := range table.Rows {
		cloned := make([]BackupValue, len(row))
		copy(cloned, row)
		for columnIndex := range volatile {
			cloned[columnIndex] = BackupValue{Kind: "null"}
		}
		rows[rowIndex] = cloned
	}
	return &BackupTable{Name: table.Name, Columns: table.Columns, Rows: rows}
}

func LibrarySnapshotIsEmpty(document BackupDocument) bool {
	for _, table := range document.Tables {
		switch table.Name {
		case "feeds", "subscriptions", "entries", "research_papers":
			if len(table.Rows) > 0 {
				return false
			}
		}
	}
	return true
}

func makeSnapshotPortable(table *BackupTable) {
	if table.Name != "entry_states" {
		return
	}
	deviceColumn := -1
	for index, column := range table.Columns {
		if column == "updated_by_device_id" {
			deviceColumn = index
			break
		}
	}
	if deviceColumn < 0 {
		return
	}
	for index := range table.Rows {
		table.Rows[index][deviceColumn] = BackupValue{Kind: "null"}
	}
}
