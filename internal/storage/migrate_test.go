package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != len(entries) {
		t.Fatalf("expected %d migrations, got %d", len(entries), count)
	}
	for _, table := range []string{"profiles", "feeds", "subscriptions", "entries", "entry_states", "jobs"} {
		var exists int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if exists != 1 {
			t.Fatalf("expected table %s to exist", table)
		}
	}
}

func TestEachPriorMigrationUpgradesToLatest(t *testing.T) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for prior := 0; prior < len(entries); prior++ {
		t.Run("from_"+strconv.Itoa(prior), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reflow.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.ExecContext(context.Background(), `CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			)`); err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				version, err := migrationVersion(entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				if version > prior {
					break
				}
				body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				if err := applyMigration(context.Background(), db, version, entry.Name(), string(body)); err != nil {
					t.Fatal(err)
				}
			}
			if err := Migrate(context.Background(), db); err != nil {
				t.Fatalf("upgrade from migration %d: %v", prior, err)
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != len(entries) {
				t.Fatalf("expected %d migrations after upgrade, got %d", len(entries), count)
			}
			for _, table := range []string{"sync_accounts", "ai_profiles", "ai_usage", "zotero_exports"} {
				var exists int
				if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&exists); err != nil {
					t.Fatal(err)
				}
				if exists != 1 {
					t.Fatalf("upgraded schema is missing %s", table)
				}
			}
			rows, err := db.Query("PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if rows.Next() {
				t.Fatal("upgraded schema has a foreign-key violation")
			}
		})
	}
}
