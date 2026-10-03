package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestLibrarySnapshotIsPortableAndPreservesLocalAccounts(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	guid := "portable-entry"
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Portable feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, Title: "Portable article", PublishedAt: time.Now().UTC(),
			ContentHash: "portable-hash", SanitizedHTML: "<p>Portable</p>", PlainText: "Portable",
		}},
	}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSyncAccount(ctx, db, CreateSyncAccountParams{
		ID: "local-webdav", Provider: "webdav", Name: "Local WebDAV",
		Endpoint: "https://dav.example.test/reflow.json", EncryptedCredentials: []byte("ciphertext"),
		Enabled: true, SyncIntervalMinutes: 30,
	}); err != nil {
		t.Fatal(err)
	}

	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sync_accounts") || strings.Contains(string(body), "ciphertext") {
		t.Fatalf("portable snapshot exposed local account data: %s", body)
	}
	if err := RestoreLibrarySnapshot(ctx, db, document); err != nil {
		t.Fatal(err)
	}
	accounts, err := ListSyncAccounts(ctx, db, domain.DefaultProfileID)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "local-webdav" {
		t.Fatalf("local sync account was not preserved: %+v %v", accounts, err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Portable article" {
		t.Fatalf("snapshot library did not restore: %+v %v", page, err)
	}
}

func TestExportLibrarySnapshotClosesReadTransaction(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := ExportLibrarySnapshot(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE profiles SET display_name = display_name"); err != nil {
		t.Fatalf("successful export did not release its transaction: %v", err)
	}

	if _, err := db.ExecContext(ctx, "DROP TABLE preferences"); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportLibrarySnapshot(ctx, db); err == nil {
		t.Fatal("expected export with a missing table to fail")
	}
	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil || result != 1 {
		t.Fatalf("failed export did not roll back its transaction: result=%d err=%v", result, err)
	}
}

func TestRestoreLibrarySnapshotRejectsForeignKeyViolations(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for tableIndex := range document.Tables {
		table := &document.Tables[tableIndex]
		if table.Name != "folders" {
			continue
		}
		row := make([]BackupValue, len(table.Columns))
		for columnIndex, column := range table.Columns {
			switch column {
			case "id":
				row[columnIndex] = BackupValue{Kind: "text", Text: "orphan-folder"}
			case "profile_id":
				row[columnIndex] = BackupValue{Kind: "text", Text: "missing-profile"}
			case "name":
				row[columnIndex] = BackupValue{Kind: "text", Text: "Orphan"}
			case "position":
				row[columnIndex] = BackupValue{Kind: "integer"}
			case "created_at", "updated_at":
				row[columnIndex] = BackupValue{Kind: "text", Text: "2026-01-01T00:00:00Z"}
			default:
				row[columnIndex] = BackupValue{Kind: "null"}
			}
		}
		table.Rows = append(table.Rows, row)
	}
	if err := RestoreLibrarySnapshot(ctx, db, document); err == nil {
		t.Fatal("expected foreign-key violation to reject snapshot")
	}
}

func TestLibrarySnapshotFingerprintIgnoresExportTime(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	firstHash, _ := LibrarySnapshotFingerprint(first)
	secondHash, _ := LibrarySnapshotFingerprint(second)
	if firstHash == "" || firstHash != secondHash {
		t.Fatalf("stable snapshot fingerprint mismatch: %q %q", firstHash, secondHash)
	}
}

func TestLibrarySnapshotRoundTripsResearchPapers(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Tracked paper",
		Authors: []string{"Ann"}, Stages: []domain.ResearchStage{{Name: "Draft", Done: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件", "")
	if err != nil {
		t.Fatal(err)
	}
	extra, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "合作者", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{TagIDs: &[]string{tag.ID, extra.ID}}); err != nil {
		t.Fatal(err)
	}
	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	title := "Local edit"
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	untagged := []string{}
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{TagIDs: &untagged}); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, strPointer("本地改名"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := RestoreLibrarySnapshot(ctx, db, document); err != nil {
		t.Fatal(err)
	}
	restored, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, created.ID)
	if err != nil {
		t.Fatalf("research paper missing after snapshot restore: %v", err)
	}
	if restored.Title != "Tracked paper" || len(restored.Stages) != 1 || len(restored.Authors) != 1 {
		t.Fatalf("snapshot restore lost research data: %+v", restored)
	}
	if got := strings.Join(restored.TagIDs, ","); got != tag.ID+","+extra.ID {
		t.Fatalf("snapshot restore lost the paper's tags: %#v", restored.TagIDs)
	}
	restoredTag, err := GetResearchTag(ctx, db, domain.DefaultProfileID, tag.ID)
	if err != nil {
		t.Fatalf("research tag missing after snapshot restore: %v", err)
	}
	if restoredTag.Name != "急件" {
		t.Fatalf("snapshot restore lost the tag name: %+v", restoredTag)
	}
}

func TestLibrarySnapshotIsEmptyCountsResearchPapers(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !LibrarySnapshotIsEmpty(document) {
		t.Fatal("empty database snapshot should be considered empty")
	}
	if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Only a paper",
	}); err != nil {
		t.Fatal(err)
	}
	document, err = ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if LibrarySnapshotIsEmpty(document) {
		t.Fatal("a lone research paper must keep the snapshot non-empty")
	}
}
