package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func seedAnnotationEntry(t *testing.T, dbPath string) (*sql.DB, string) {
	t.Helper()
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	feed := domain.ParsedFeed{
		Title: "经济研究", Format: "rss",
		Entries: []domain.ParsedEntry{{
			Title: "数据要素市场化配置研究", PublishedAt: time.Now().UTC(), ContentHash: "annotation-hash",
			SanitizedHTML: "<p>正文</p>", PlainText: "正文",
		}},
	}
	if _, err := SaveNewFeed(context.Background(), db, domain.DefaultProfileID, "https://example.com/ann", "https://example.com/ann", feed, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	page, err := ListEntries(context.Background(), db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("seed entry: %v (items=%d)", err, len(page.Items))
	}
	return db, page.Items[0].ID
}

func TestEntryAnnotationLifecycle(t *testing.T) {
	ctx := context.Background()
	db, entryID := seedAnnotationEntry(t, filepath.Join(t.TempDir(), "reflow.db"))

	created, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, domain.EntryAnnotation{
		Style: "highlight", Quote: "数据要素", Prefix: "", Suffix: "市场化", Note: "核心概念",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.EntryID != entryID || created.CreatedAt.IsZero() {
		t.Fatalf("unexpected created annotation: %+v", created)
	}
	second, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, domain.EntryAnnotation{
		Style: "underline", Quote: "市场化配置",
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := ListEntryAnnotations(ctx, db, domain.DefaultProfileID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != created.ID || items[1].ID != second.ID {
		t.Fatalf("list returned %+v", items)
	}

	note := "更新后的批注"
	updated, err := UpdateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, created.ID, domain.EntryAnnotationPatch{Note: &note})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Note != note || updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("update returned %+v", updated)
	}

	if err := DeleteEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, second.ID); err != nil {
		t.Fatal(err)
	}
	items, err = ListEntryAnnotations(ctx, db, domain.DefaultProfileID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("after delete: %+v", items)
	}
	if err := DeleteEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete should be ErrNotFound, got %v", err)
	}
}

func TestEntryAnnotationValidation(t *testing.T) {
	ctx := context.Background()
	db, entryID := seedAnnotationEntry(t, filepath.Join(t.TempDir(), "reflow.db"))

	var validation *AnnotationValidationError
	if _, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, domain.EntryAnnotation{Style: "neon", Quote: "x"}); !errors.As(err, &validation) {
		t.Fatalf("invalid style should be a validation error, got %v", err)
	}
	if _, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, domain.EntryAnnotation{Style: "highlight", Quote: "  "}); !errors.As(err, &validation) {
		t.Fatalf("empty quote should be a validation error, got %v", err)
	}
	if _, err := UpdateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, "anything", domain.EntryAnnotationPatch{}); !errors.As(err, &validation) {
		t.Fatalf("empty patch should be a validation error, got %v", err)
	}
	if _, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, "missing-entry", domain.EntryAnnotation{Style: "highlight", Quote: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown entry should be ErrNotFound, got %v", err)
	}
	if _, err := CreateEntryAnnotation(ctx, db, "other-profile", entryID, domain.EntryAnnotation{Style: "highlight", Quote: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign profile should be ErrNotFound, got %v", err)
	}
	items, err := ListEntryAnnotations(ctx, db, "other-profile", entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("foreign profile must not see annotations: %+v", items)
	}
}

func TestEntryAnnotationsSurviveBackupAndSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, entryID := seedAnnotationEntry(t, filepath.Join(dir, "source.db"))
	if _, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, entryID, domain.EntryAnnotation{
		Style: "wavy", Quote: "数据要素", Note: "备份验证",
	}); err != nil {
		t.Fatal(err)
	}

	backup, err := ExportBackup(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	restored, err := Open(ctx, filepath.Join(dir, "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := RestoreBackup(ctx, restored, backup); err != nil {
		t.Fatal(err)
	}
	items, err := ListEntryAnnotations(ctx, restored, domain.DefaultProfileID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Note != "备份验证" || items[0].Style != "wavy" {
		t.Fatalf("backup round-trip lost annotation: %+v", items)
	}

	// The portable library snapshot must carry annotations too.
	snapshotDB, snapshotEntryID := seedAnnotationEntry(t, filepath.Join(dir, "snapshot-source.db"))
	if _, err := CreateEntryAnnotation(ctx, snapshotDB, domain.DefaultProfileID, snapshotEntryID, domain.EntryAnnotation{
		Style: "highlight", Quote: "市场化配置",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ExportLibrarySnapshot(ctx, snapshotDB)
	if err != nil {
		t.Fatal(err)
	}
	snapshotDB.Close()

	snapshotTarget, err := Open(ctx, filepath.Join(dir, "snapshot-target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotTarget.Close()
	if err := RestoreLibrarySnapshot(ctx, snapshotTarget, snapshot); err != nil {
		t.Fatal(err)
	}
	items, err = ListEntryAnnotations(ctx, snapshotTarget, domain.DefaultProfileID, snapshotEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Quote != "市场化配置" {
		t.Fatalf("snapshot round-trip lost annotation: %+v", items)
	}
}
