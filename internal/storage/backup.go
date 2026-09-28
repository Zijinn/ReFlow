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
)

const BackupFormat = "reflow-backup"

type BackupDocument struct {
	Format        string        `json:"format"`
	Version       int           `json:"version"`
	SchemaVersion int           `json:"schema_version"`
	CreatedAt     time.Time     `json:"created_at"`
	Tables        []BackupTable `json:"tables"`
}

type BackupTable struct {
	Name    string          `json:"name"`
	Columns []string        `json:"columns"`
	Rows    [][]BackupValue `json:"rows"`
}

type BackupValue struct {
	Kind    string  `json:"kind"`
	Text    string  `json:"text,omitempty"`
	Integer int64   `json:"integer,omitempty"`
	Real    float64 `json:"real,omitempty"`
	Blob    string  `json:"blob,omitempty"`
}

var backupTables = []string{
	"profiles", "devices", "folders", "feeds", "subscriptions", "entries",
	"entry_contents", "entry_states", "entry_annotations", "tags", "feed_tags", "entry_tags",
	"jobs", "job_attempts", "rules", "sync_accounts", "sync_mappings",
	"ai_profiles", "ai_results", "ai_chat_sessions", "ai_chat_messages",
	"ai_usage",
	"processed_mutations", "saved_filters", "preferences", "zotero_exports",
	"research_papers",
}

func ExportBackup(ctx context.Context, db *sql.DB) (BackupDocument, error) {
	// A single read transaction gives every table the same snapshot; without
	// it, concurrent feed/job writes could leak dangling references into the
	// backup.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BackupDocument{}, fmt.Errorf("begin backup snapshot: %w", err)
	}
	defer tx.Rollback()
	document := BackupDocument{
		Format: BackupFormat, Version: 1, CreatedAt: time.Now().UTC(),
		Tables: make([]BackupTable, 0, len(backupTables)),
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&document.SchemaVersion); err != nil {
		return BackupDocument{}, fmt.Errorf("read schema version: %w", err)
	}
	for _, tableName := range backupTables {
		exists, err := tableExists(ctx, tx, tableName)
		if err != nil {
			return BackupDocument{}, err
		}
		if !exists {
			continue
		}
		table, err := exportTable(ctx, tx, tableName)
		if err != nil {
			return BackupDocument{}, err
		}
		document.Tables = append(document.Tables, table)
	}
	if err := tx.Commit(); err != nil {
		return BackupDocument{}, fmt.Errorf("commit backup snapshot: %w", err)
	}
	return document, nil
}

func RestoreBackup(ctx context.Context, db *sql.DB, document BackupDocument) error {
	if document.Format != BackupFormat || document.Version != 1 {
		return errors.New("unsupported ReFlow backup format")
	}

	connection, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire restore connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for restore: %w", err)
	}
	defer connection.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backup restore: %w", err)
	}
	defer tx.Rollback()

	if err := validateBackupForRestore(ctx, tx, document); err != nil {
		return err
	}
	for index := len(backupTables) - 1; index >= 0; index-- {
		name := backupTables[index]
		exists, existsErr := tableExists(ctx, tx, name)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(name)); err != nil {
				return fmt.Errorf("clear table %s: %w", name, err)
			}
		}
	}
	for _, table := range document.Tables {
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
				return fmt.Errorf("restore table %s: %w", table.Name, err)
			}
		}
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
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs SET state = 'queued', started_at = NULL, finished_at = NULL,
			error_code = 'restored', error_message = 'Recovered from backup',
			scheduled_at = ?, updated_at = ? WHERE state = 'running'`,
		formatTime(time.Now().UTC()), formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("recover restored jobs: %w", err)
	}
	violations, err := countForeignKeyViolations(ctx, tx)
	if err != nil {
		return err
	}
	if violations > 0 {
		return fmt.Errorf("backup restore would leave %d foreign key violations", violations)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup restore: %w", err)
	}
	return nil
}

func validateBackupForRestore(ctx context.Context, tx *sql.Tx, document BackupDocument) error {
	var currentSchema int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentSchema); err != nil {
		return fmt.Errorf("read current schema version: %w", err)
	}
	if document.SchemaVersion != currentSchema {
		return fmt.Errorf("backup schema version %d does not match ReFlow schema version %d", document.SchemaVersion, currentSchema)
	}

	allowed := make(map[string]struct{}, len(backupTables))
	required := make(map[string][]string, len(backupTables))
	for _, name := range backupTables {
		allowed[name] = struct{}{}
		exists, err := tableExists(ctx, tx, name)
		if err != nil {
			return fmt.Errorf("check backup table %s: %w", name, err)
		}
		if !exists {
			continue
		}
		columns, err := tableColumns(ctx, tx, name)
		if err != nil {
			return err
		}
		required[name] = columns
	}

	seen := make(map[string]struct{}, len(document.Tables))
	containsProfiles := false
	for _, table := range document.Tables {
		if _, ok := allowed[table.Name]; !ok {
			return fmt.Errorf("backup contains unsupported table %q", table.Name)
		}
		currentColumns, exists := required[table.Name]
		if !exists {
			return fmt.Errorf("backup contains table %q which does not exist in the current schema", table.Name)
		}
		if _, duplicate := seen[table.Name]; duplicate {
			return fmt.Errorf("backup contains duplicate table %q", table.Name)
		}
		seen[table.Name] = struct{}{}
		if !sameStrings(currentColumns, table.Columns) {
			return fmt.Errorf("backup table %q columns do not match the current schema", table.Name)
		}
		if table.Name == "profiles" && len(table.Rows) > 0 {
			containsProfiles = true
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return fmt.Errorf("backup table %q has a row with the wrong column count", table.Name)
			}
		}
	}
	for _, name := range backupTables {
		if _, exists := required[name]; !exists {
			continue
		}
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("backup is missing table %q", name)
		}
	}
	if !containsProfiles {
		return errors.New("backup does not contain a profile")
	}
	return nil
}

func countForeignKeyViolations(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return 0, fmt.Errorf("foreign key check: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	return count, rows.Err()
}

func exportTable(ctx context.Context, db queryer, name string) (BackupTable, error) {
	columns, err := tableColumns(ctx, db, name)
	if err != nil {
		return BackupTable{}, err
	}
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quoteIdentifier(column)
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(quoted, ",")+" FROM "+quoteIdentifier(name))
	if err != nil {
		return BackupTable{}, fmt.Errorf("export table %s: %w", name, err)
	}
	defer rows.Close()
	table := BackupTable{Name: name, Columns: columns, Rows: make([][]BackupValue, 0)}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return BackupTable{}, fmt.Errorf("scan backup table %s: %w", name, err)
		}
		encoded := make([]BackupValue, len(values))
		for index, value := range values {
			encoded[index], err = encodeBackupValue(value)
			if err != nil {
				return BackupTable{}, fmt.Errorf("encode backup table %s: %w", name, err)
			}
		}
		table.Rows = append(table.Rows, encoded)
	}
	return table, rows.Err()
}

func encodeBackupValue(value any) (BackupValue, error) {
	switch typed := value.(type) {
	case nil:
		return BackupValue{Kind: "null"}, nil
	case string:
		return BackupValue{Kind: "text", Text: typed}, nil
	case []byte:
		return BackupValue{Kind: "blob", Blob: base64.StdEncoding.EncodeToString(typed)}, nil
	case int64:
		return BackupValue{Kind: "integer", Integer: typed}, nil
	case float64:
		return BackupValue{Kind: "real", Real: typed}, nil
	case bool:
		if typed {
			return BackupValue{Kind: "integer", Integer: 1}, nil
		}
		return BackupValue{Kind: "integer"}, nil
	default:
		body, err := json.Marshal(typed)
		if err != nil {
			return BackupValue{}, err
		}
		return BackupValue{Kind: "text", Text: string(body)}, nil
	}
}

func (value BackupValue) decode() (any, error) {
	switch value.Kind {
	case "null":
		return nil, nil
	case "text":
		return value.Text, nil
	case "integer":
		return value.Integer, nil
	case "real":
		return value.Real, nil
	case "blob":
		decoded, err := base64.StdEncoding.DecodeString(value.Blob)
		if err != nil {
			return nil, err
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unknown backup value kind %q", value.Kind)
	}
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func tableColumns(ctx context.Context, db queryer, name string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(name)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTableColumns(rows, name)
}

func scanTableColumns(rows *sql.Rows, name string) ([]string, error) {
	columns := make([]string, 0)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("read columns for %s: %w", name, err)
		}
		columns = append(columns, columnName)
	}
	return columns, rows.Err()
}

func tableExists(ctx context.Context, db queryer, name string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)", name).Scan(&exists)
	return exists, err
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
