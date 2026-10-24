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

func TestLibrarySnapshotRoundTripsTaskBoard(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	thread, err := CreateTaskThread(ctx, db, domain.DefaultProfileID, "主线任务（论文+项目）")
	if err != nil {
		t.Fatal(err)
	}
	first, err := CreateTaskLayer(ctx, db, domain.DefaultProfileID, thread.ID, strPointer("第一层"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateTaskLayer(ctx, db, domain.DefaultProfileID, thread.ID, strPointer("第二层"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := CreateTaskItem(ctx, db, domain.DefaultProfileID, thread.ID, "任务管理器产品开发", "")
	if err != nil {
		t.Fatal(err)
	}
	// One card placed in two layers, 2 of 2 steps done in the first and 0 of 3 in
	// the second. The board prints 40 on both of its cells, so a snapshot missing
	// either placement, either date, or any of the five steps reads a different
	// number after a restore — progress is the assertion that covers all of them
	// at once.
	early, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, item.ID, first.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	late, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, item.ID, second.ID, "2026-10-12", "2026-11-07")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"收集意见", "整理需求"} {
		created, err := CreateTaskStep(ctx, db, domain.DefaultProfileID, early.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, created.ID,
			domain.TaskStepPatch{Done: boolPointer(true)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"排期", "交付", "验收"} {
		if _, err := CreateTaskStep(ctx, db, domain.DefaultProfileID, late.ID, name); err != nil {
			t.Fatal(err)
		}
	}
	before, err := GetTaskThread(ctx, db, domain.DefaultProfileID, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Items) != 1 || before.Items[0].Progress != 40 {
		t.Fatalf("expected the seeded card to read 40 before the snapshot, got %+v", before.Items)
	}

	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	// Churn every tier the snapshot has to carry: the thread name, a whole card,
	// and a step on a card that survives.
	if _, err := UpdateTaskThread(ctx, db, domain.DefaultProfileID, thread.ID,
		domain.TaskThreadPatch{Name: strPointer("本地改名")}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTaskStep(ctx, db, domain.DefaultProfileID, late.ID, "本地多出来的一步"); err != nil {
		t.Fatal(err)
	}
	spare, err := CreateTaskItem(ctx, db, domain.DefaultProfileID, thread.ID, "本地新建又删掉的任务", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteTaskItem(ctx, db, domain.DefaultProfileID, spare.ID); err != nil {
		t.Fatal(err)
	}

	if err := RestoreLibrarySnapshot(ctx, db, document); err != nil {
		t.Fatal(err)
	}

	restored, err := GetTaskThread(ctx, db, domain.DefaultProfileID, thread.ID)
	if err != nil {
		t.Fatalf("task thread missing after snapshot restore: %v", err)
	}
	if restored.Name != "主线任务（论文+项目）" {
		t.Fatalf("snapshot restore lost the thread name: %+v", restored)
	}
	if len(restored.Layers) != 2 {
		t.Fatalf("snapshot restore lost a layer: %+v", restored.Layers)
	}
	if len(restored.Items) != 1 || restored.Items[0].LayerCount != 2 {
		t.Fatalf("snapshot restore lost the card or its second placement: %+v", restored.Items)
	}
	card := restored.Items[0]
	if card.Progress != 40 || card.Stats.TotalSteps != 5 || card.Stats.DoneSteps != 2 {
		t.Fatalf("snapshot restore changed the checklist: %+v", card)
	}
	// The dates ride on the placement, not the card, so this is the assertion that
	// the (task, layer) row itself came back rather than being re-created blank.
	var window *domain.TaskAssignment
	for index := range card.Assignments {
		if card.Assignments[index].LayerID == second.ID {
			window = &card.Assignments[index]
		}
	}
	if window == nil || window.StartDate != "2026-10-12" || window.DueDate != "2026-11-07" {
		t.Fatalf("snapshot restore lost the placement's dates: %+v", card.Assignments)
	}
	if restored.Stats.Active != 1 || restored.Stats.Slots != 2 || restored.Stats.SlotsClosed != 1 {
		t.Fatalf("snapshot restore changed the derived counts: %+v", restored.Stats)
	}
	if !restored.Layers[0].Unlocked || !restored.Layers[1].Unlocked {
		t.Fatalf("a fully closed first layer must leave the second unlocked: %+v", restored.Layers)
	}
}

func TestLibrarySnapshotIsEmptyCountsTaskThreads(t *testing.T) {
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
	// A reader who only uses 长任务 has work in the library. Counting nothing here
	// would let a first-ever sync pull the cloud copy over their board without
	// asking.
	if _, err := CreateTaskThread(ctx, db, domain.DefaultProfileID, "只有主线"); err != nil {
		t.Fatal(err)
	}
	document, err = ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if LibrarySnapshotIsEmpty(document) {
		t.Fatal("a lone task thread must keep the snapshot non-empty")
	}
}

func TestLibrarySnapshotRoundTripsQuickTasks(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	task, err := CreateQuickTask(ctx, db, domain.DefaultProfileID, "扬州报销整理", "", "2026-10-12", "2026-10-14", nil)
	if err != nil {
		t.Fatal(err)
	}
	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, task.ID,
		domain.QuickTaskPatch{Title: strPointer("本地改名")}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreLibrarySnapshot(ctx, db, document); err != nil {
		t.Fatal(err)
	}
	restored, err := GetQuickTask(ctx, db, domain.DefaultProfileID, task.ID)
	if err != nil {
		t.Fatalf("quick task missing after snapshot restore: %v", err)
	}
	if restored.Title != "扬州报销整理" || restored.StartDate != "2026-10-12" || restored.DueDate != "2026-10-14" {
		t.Fatalf("snapshot restore lost quick task fields: %+v", restored)
	}
}

func TestLibrarySnapshotIsEmptyCountsQuickTasks(t *testing.T) {
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
	if _, err := CreateQuickTask(ctx, db, domain.DefaultProfileID, "只有即时任务", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	document, err = ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if LibrarySnapshotIsEmpty(document) {
		t.Fatal("a lone quick task must keep the snapshot non-empty")
	}
}
