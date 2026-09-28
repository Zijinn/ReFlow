package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestFolderCycleAndAutomationRules(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root, err := EnsureFolder(ctx, db, domain.DefaultProfileID, nil, "Research")
	if err != nil {
		t.Fatal(err)
	}
	child, err := EnsureFolder(ctx, db, domain.DefaultProfileID, &root.ID, "Economics")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateFolder(ctx, db, domain.DefaultProfileID, root.ID, true, &child.ID, nil, nil); err == nil {
		t.Fatal("expected folder cycle to be rejected")
	}
	moved, err := UpdateFolder(ctx, db, domain.DefaultProfileID, child.ID, true, nil, nil, nil)
	if err != nil || moved.ParentID != nil {
		t.Fatalf("expected explicit null parent to move folder to root: %+v, %v", moved, err)
	}
	if moved.Position <= root.Position {
		t.Fatalf("expected new sibling folder to be appended after existing ones: %d <= %d", moved.Position, root.Position)
	}
	if _, err := UpdateFolder(ctx, db, domain.DefaultProfileID, moved.ID, false, nil, nil, nil); err != nil || moved.ParentID != nil {
		t.Fatalf("expected omitted parent to keep folder at root: %+v, %v", moved, err)
	}

	tag, err := CreateTag(ctx, db, domain.DefaultProfileID, "Important", nil)
	if err != nil {
		t.Fatal(err)
	}
	conditions, _ := json.Marshal(map[string]string{"title_contains": "reflow"})
	actions, _ := json.Marshal(map[string]any{"star": true, "read_later": true, "add_tag_ids": []string{tag.ID}})
	if _, err := CreateRule(ctx, db, domain.DefaultProfileID, "Save ReFlow posts", true, 0, conditions, actions); err != nil {
		t.Fatal(err)
	}
	guid := "rule-entry"
	entryURL := "https://example.com/rule"
	feed, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Rules", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "ReFlow automation",
			PublishedAt: time.Now().UTC(), ContentHash: "rule-hash", PlainText: "body",
		}},
	}, nil, nil, &child.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyRulesToFeed(ctx, db, domain.DefaultProfileID, feed.ID, nil); err != nil {
		t.Fatal(err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: feed.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 || !page.Items[0].State.IsStarred || !page.Items[0].State.IsReadLater {
		t.Fatalf("rule actions did not apply: %+v, %v", page, err)
	}
	var tagCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entry_tags WHERE entry_id = ? AND tag_id = ?", page.Items[0].ID, tag.ID).Scan(&tagCount); err != nil || tagCount != 1 {
		t.Fatalf("rule tag action failed: count=%d err=%v", tagCount, err)
	}
	if len(page.Items[0].TagIDs) != 1 || page.Items[0].TagIDs[0] != tag.ID {
		t.Fatalf("entry response omitted assigned tag: %+v", page.Items[0].TagIDs)
	}
	tagged, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, TagID: tag.ID, Limit: 10})
	if err != nil || len(tagged.Items) != 1 || tagged.Items[0].ID != page.Items[0].ID {
		t.Fatalf("tag filter failed: %+v, %v", tagged, err)
	}
	if err := SetEntryTags(ctx, db, domain.DefaultProfileID, page.Items[0].ID, []string{tag.ID, tag.ID}); err != nil {
		t.Fatal(err)
	}
	detail, err := GetEntry(ctx, db, domain.DefaultProfileID, page.Items[0].ID)
	if err != nil || len(detail.TagIDs) != 1 || detail.TagIDs[0] != tag.ID {
		t.Fatalf("entry detail tags failed: %+v, %v", detail.TagIDs, err)
	}
	if err := AddEntryTagsByName(ctx, db, domain.DefaultProfileID, page.Items[0].ID, []string{"Economic Policy", "economic policy", "Data Flows"}); err != nil {
		t.Fatal(err)
	}
	detail, err = GetEntry(ctx, db, domain.DefaultProfileID, page.Items[0].ID)
	if err != nil || len(detail.TagIDs) != 3 {
		t.Fatalf("generated tags should preserve manual tags and deduplicate names: %+v, %v", detail.TagIDs, err)
	}
	allTags, err := ListTags(ctx, db, domain.DefaultProfileID)
	if err != nil || len(allTags) != 3 {
		t.Fatalf("generated tag records were not deduplicated: %+v, %v", allTags, err)
	}

	filterQuery := json.RawMessage(`{"state":"starred"}`)
	if _, err := CreateSavedFilter(ctx, db, domain.DefaultProfileID, "Favorites", filterQuery); err != nil {
		t.Fatal(err)
	}
	filters, err := ListSavedFilters(ctx, db, domain.DefaultProfileID)
	if err != nil || len(filters) != 1 {
		t.Fatalf("saved filters failed: %+v, %v", filters, err)
	}
}

func TestMarkEntriesReadRespectsStateFilter(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	firstGUID, secondGUID := "starred-entry", "plain-entry"
	firstURL, secondURL := "https://example.com/starred", "https://example.com/plain"
	feed, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/bulk-feed", "https://example.com/bulk-feed", domain.ParsedFeed{
		Title: "Bulk state", Format: "rss", Entries: []domain.ParsedEntry{
			{GUID: &firstGUID, CanonicalURL: &firstURL, Title: "Starred", PublishedAt: time.Now().UTC(), ContentHash: "starred-hash", PlainText: "starred"},
			{GUID: &secondGUID, CanonicalURL: &secondURL, Title: "Plain", PublishedAt: time.Now().UTC().Add(-time.Minute), ContentHash: "plain-hash", PlainText: "plain"},
		},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: feed.ID, Limit: 10})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("list bulk state entries: %+v, %v", page, err)
	}
	starredID, plainID := page.Items[0].ID, page.Items[1].ID
	starred := true
	if _, err := UpdateEntryState(ctx, db, domain.DefaultProfileID, starredID, domain.EntryStatePatch{MutationID: "starred-filter-state", IsStarred: &starred}); err != nil {
		t.Fatal(err)
	}
	count, err := MarkEntriesRead(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, State: "starred"})
	if err != nil || count != 1 {
		t.Fatalf("mark starred entries read: count=%d err=%v", count, err)
	}
	starredEntry, err := GetEntry(ctx, db, domain.DefaultProfileID, starredID)
	if err != nil || !starredEntry.State.IsRead {
		t.Fatalf("starred entry was not marked read: %+v, %v", starredEntry.State, err)
	}
	plainEntry, err := GetEntry(ctx, db, domain.DefaultProfileID, plainID)
	if err != nil || plainEntry.State.IsRead {
		t.Fatalf("unfiltered entry was unexpectedly marked read: %+v, %v", plainEntry.State, err)
	}
}

func TestUpdateFolderAppendOrderAndProfileBoundary(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	destination, err := EnsureFolder(ctx, db, domain.DefaultProfileID, nil, "Destination")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureFolder(ctx, db, domain.DefaultProfileID, &destination.ID, "First"); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureFolder(ctx, db, domain.DefaultProfileID, &destination.ID, "Second"); err != nil {
		t.Fatal(err)
	}
	moving, err := EnsureFolder(ctx, db, domain.DefaultProfileID, nil, "Moving")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := UpdateFolder(ctx, db, domain.DefaultProfileID, moving.ID, true, &destination.ID, nil, nil)
	if err != nil || moved.Position != 2 {
		t.Fatalf("expected append position 2, folder=%+v err=%v", moved, err)
	}

	const otherProfile = "00000000-0000-4000-8000-000000000002"
	if _, err := db.ExecContext(ctx, "INSERT INTO profiles (id, display_name) VALUES (?, ?)", otherProfile, "Other"); err != nil {
		t.Fatal(err)
	}
	otherParent, err := EnsureFolder(ctx, db, otherProfile, nil, "Other parent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateFolder(ctx, db, domain.DefaultProfileID, moving.ID, true, &otherParent.ID, nil, nil); err == nil {
		t.Fatal("expected cross-profile parent to be rejected")
	}
	folders, err := ListFolders(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range folders {
		if folder.ID == moving.ID && (folder.ParentID == nil || *folder.ParentID != destination.ID) {
			t.Fatalf("cross-profile update changed parent: %+v", folder)
		}
	}
}

func TestUpdateFolderConcurrentOppositeMovesCannotCycle(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	folderA, err := EnsureFolder(ctx, db, domain.DefaultProfileID, nil, "A")
	if err != nil {
		t.Fatal(err)
	}
	folderB, err := EnsureFolder(ctx, db, domain.DefaultProfileID, nil, "B")
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	move := func(folderID, parentID string) {
		ready.Done()
		<-start
		_, updateErr := UpdateFolder(ctx, db, domain.DefaultProfileID, folderID, true, &parentID, nil, nil)
		results <- updateErr
	}
	go move(folderA.ID, folderB.ID)
	go move(folderB.ID, folderA.ID)
	ready.Wait()
	close(start)

	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one opposite move to succeed, got %d", successes)
	}

	var cycle bool
	if err := db.QueryRowContext(ctx, `
		WITH RECURSIVE path(id, parent_id, visited, cycle) AS (
			SELECT id, parent_id, ',' || id || ',', 0 FROM folders WHERE profile_id = ?
			UNION ALL
			SELECT f.id, f.parent_id, path.visited || f.id || ',', instr(path.visited, ',' || f.id || ',') > 0
			FROM folders f JOIN path ON f.id = path.parent_id WHERE path.cycle = 0
		)
		SELECT EXISTS(SELECT 1 FROM path WHERE cycle = 1)`, domain.DefaultProfileID).Scan(&cycle); err != nil {
		t.Fatal(err)
	}
	if cycle {
		t.Fatal("opposite moves created a folder cycle")
	}
}
