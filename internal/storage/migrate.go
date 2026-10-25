package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	// Reconcile backfills merge rows across many transactions, so they cannot ride
	// inside the migration that creates their columns. This ledger lets a crash
	// halfway through leave no completed marker, and the next boot retries the
	// (idempotent) backfill instead of silently skipping it forever.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS data_backfills (
			name TEXT PRIMARY KEY,
			completed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`); err != nil {
		return fmt.Errorf("create backfill table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}
		applied, err := isMigrationApplied(ctx, db, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := applyMigration(ctx, db, version, entry.Name(), string(body)); err != nil {
			return err
		}
	}
	if err := runBackfillOnce(ctx, db, "entry_identities", 6, ReconcileEntryIdentities); err != nil {
		return err
	}
	if err := runBackfillOnce(ctx, db, "entry_dois", 13, ReconcileEntryDOIs); err != nil {
		return err
	}
	return nil
}

func runBackfillOnce(ctx context.Context, db *sql.DB, name string, migration int, task func(context.Context, *sql.DB) error) error {
	applied, err := isMigrationApplied(ctx, db, migration)
	if err != nil || !applied {
		return err
	}
	var completed bool
	if err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM data_backfills WHERE name = ?)", name,
	).Scan(&completed); err != nil {
		return fmt.Errorf("check backfill %s: %w", name, err)
	}
	if completed {
		return nil
	}
	if err := task(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO data_backfills(name) VALUES (?) ON CONFLICT(name) DO NOTHING", name,
	); err != nil {
		return fmt.Errorf("record backfill %s: %w", name, err)
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("migration %q must begin with a numeric version and underscore", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("migration %q has invalid version", name)
	}
	return version, nil
}

func isMigrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return exists, nil
}

// applyMigration rechecks the ledger under the write lock. Two processes opening
// the same database can both pass the advisory check above; only the one that
// still sees the version missing may run the SQL, the other reports false.
func applyMigration(ctx context.Context, db *sql.DB, version int, name, body string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer tx.Rollback()

	var applied bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version,
	).Scan(&applied); err != nil {
		return false, fmt.Errorf("check migration %s under lock: %w", name, err)
	}
	if applied {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return false, fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, name) VALUES (?, ?)", version, name,
	); err != nil {
		return false, fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", name, err)
	}
	return true, nil
}
