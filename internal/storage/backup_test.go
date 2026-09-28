package storage

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/secretbox"
)

func TestBackupRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	guid := "backup-entry"
	entryURL := "https://example.com/backup-entry"
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Backup feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "Backup searchable entry",
			PublishedAt: time.Now().UTC(), ContentHash: "backup-hash",
			SanitizedHTML: "<p>backup body</p>", PlainText: "backup needle",
		}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err := ExportBackup(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if document.Format != BackupFormat || document.SchemaVersion != len(migrations) || len(document.Tables) == 0 {
		t.Fatalf("unexpected backup metadata: %+v", document)
	}
	if err := DeleteSubscription(ctx, db, domain.DefaultProfileID, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(ctx, db, document); err != nil {
		t.Fatal(err)
	}
	feeds, err := ListFeeds(ctx, db, domain.DefaultProfileID)
	if err != nil || len(feeds) != 1 || feeds[0].Title != "Backup feed" {
		t.Fatalf("unexpected restored feeds: %+v, %v", feeds, err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Query: "needle", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("restored FTS failed: %+v, %v", page, err)
	}
}

func TestRestoreRejectsIncompleteDocumentsWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BackupDocument)
	}{
		{
			name: "schema version mismatch",
			mutate: func(document *BackupDocument) {
				document.SchemaVersion++
			},
		},
		{
			name: "missing table",
			mutate: func(document *BackupDocument) {
				document.Tables = document.Tables[:len(document.Tables)-1]
			},
		},
		{
			name: "wrong columns",
			mutate: func(document *BackupDocument) {
				document.Tables[0].Columns[0] = "wrong_column"
			},
		},
		{
			name: "truncated row",
			mutate: func(document *BackupDocument) {
				document.Tables[0].Rows[0] = document.Tables[0].Rows[0][:len(document.Tables[0].Rows[0])-1]
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.ExecContext(ctx, "UPDATE profiles SET display_name = 'Before restore'"); err != nil {
				t.Fatal(err)
			}
			document, err := ExportBackup(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&document)
			if err := RestoreBackup(ctx, db, document); err == nil {
				t.Fatal("expected malformed backup to be rejected")
			}
			var profileName string
			if err := db.QueryRowContext(ctx, "SELECT display_name FROM profiles WHERE id = ?", domain.DefaultProfileID).Scan(&profileName); err != nil {
				t.Fatal(err)
			}
			if profileName != "Before restore" {
				t.Fatalf("restore validation mutated profile: %q", profileName)
			}
		})
	}
}

func TestRestoreRejectsUnknownTablesBeforeMutation(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	document, err := ExportBackup(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	document.Tables = append(document.Tables, BackupTable{Name: "unexpected"})
	if err := RestoreBackup(ctx, db, document); err == nil {
		t.Fatal("expected unknown table to be rejected")
	}
	var profiles int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM profiles").Scan(&profiles); err != nil || profiles != 1 {
		t.Fatalf("restore validation mutated database: profiles=%d err=%v", profiles, err)
	}
}

func TestBackupRestorePreservesEncryptedCredentialsWithMasterKey(t *testing.T) {
	ctx := context.Background()
	keyPath := filepath.Join(t.TempDir(), "master.key")
	box, err := secretbox.LoadOrCreate(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	aiCiphertext, err := box.Seal([]byte("ai-secret"), []byte("reflow:ai-profile:backup-ai-profile"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateAIProfile(ctx, source, CreateAIProfileParams{
		ID: "backup-ai-profile", Provider: "ollama", Name: "Backup Ollama",
		Endpoint: "http://127.0.0.1:11434", Model: "qwen3:8b", EncryptedAPIKey: aiCiphertext,
		Enabled: true, AllowPrivateNetwork: true,
	}); err != nil {
		t.Fatal(err)
	}
	syncCiphertext, err := box.Seal([]byte("sync-secret"), []byte("reflow:sync-account:backup-sync-account"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSyncAccount(ctx, source, CreateSyncAccountParams{
		ID: "backup-sync-account", Provider: "miniflux", Name: "Backup Miniflux",
		Endpoint: "https://sync.example.test", EncryptedCredentials: syncCiphertext,
		Enabled: true, SyncIntervalMinutes: 30,
	}); err != nil {
		t.Fatal(err)
	}
	document, err := ExportBackup(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := RestoreBackup(ctx, target, document); err != nil {
		t.Fatal(err)
	}
	aiRecord, err := GetAIProfileRecord(ctx, target, domain.DefaultProfileID, "backup-ai-profile")
	if err != nil {
		t.Fatal(err)
	}
	decryptedAI, err := box.Open(aiRecord.EncryptedAPIKey, []byte("reflow:ai-profile:backup-ai-profile"))
	if err != nil || !bytes.Equal(decryptedAI, []byte("ai-secret")) {
		t.Fatalf("AI credential did not recover with the original key: %q %v", decryptedAI, err)
	}
	syncRecord, err := GetSyncAccountRecord(ctx, target, domain.DefaultProfileID, "backup-sync-account")
	if err != nil {
		t.Fatal(err)
	}
	decryptedSync, err := box.Open(syncRecord.EncryptedCredentials, []byte("reflow:sync-account:backup-sync-account"))
	if err != nil || !bytes.Equal(decryptedSync, []byte("sync-secret")) {
		t.Fatalf("sync credential did not recover with the original key: %q %v", decryptedSync, err)
	}
	otherBox, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherBox.Open(aiRecord.EncryptedAPIKey, []byte("reflow:ai-profile:backup-ai-profile")); err == nil {
		t.Fatal("encrypted backup credential unexpectedly opened with a different master key")
	}
}
