package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

// A routine feed poll must not change the library snapshot fingerprint. When
// it did, every refresh cycle marked the library "locally changed", and two
// devices could never leave the both-sides-changed conflict state.
func TestSnapshotFingerprintStableAcrossNoopRefresh(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	guid := "entry-guid"
	initial := domain.ParsedFeed{
		Title: "Example", Format: "rss",
		Entries: []domain.ParsedEntry{{
			GUID: &guid, Title: "Title",
			PublishedAt: time.Now().UTC(), ContentHash: "hash-one",
			SanitizedHTML: "<p>body</p>", PlainText: "body",
		}},
	}
	feed, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", initial, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	fingerprint := func() string {
		document, err := ExportLibrarySnapshot(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := LibrarySnapshotFingerprint(document)
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	base := fingerprint()

	// A conditional poll that returns 304 takes this path.
	if err := MarkFeedNotModified(ctx, db, feed.ID); err != nil {
		t.Fatal(err)
	}
	// A 200 response whose entries are all unchanged takes this path.
	if _, err := SaveFeedRefresh(ctx, db, domain.DefaultProfileID, feed.ID, initial, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Failures and backoff bookkeeping take this path.
	if err := MarkFeedFailure(ctx, db, feed.ID, "timeout", "dial"); err != nil {
		t.Fatal(err)
	}

	if after := fingerprint(); after != base {
		t.Fatalf("fingerprint changed after poll-only bookkeeping\nbefore: %s\nafter:  %s", base, after)
	}
}

// The normalization above must not hide genuine changes: feed metadata edits
// and new entries must still move the fingerprint.
func TestSnapshotFingerprintDetectsRealChanges(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	guid := "entry-guid"
	initial := domain.ParsedFeed{
		Title: "Example", Format: "rss",
		Entries: []domain.ParsedEntry{{
			GUID: &guid, Title: "Title",
			PublishedAt: time.Now().UTC(), ContentHash: "hash-one",
			SanitizedHTML: "<p>body</p>", PlainText: "body",
		}},
	}
	feed, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", initial, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	fingerprint := func() string {
		document, err := ExportLibrarySnapshot(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := LibrarySnapshotFingerprint(document)
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	base := fingerprint()

	changed := initial
	changed.Title = "Renamed feed"
	if _, err := SaveFeedRefresh(ctx, db, domain.DefaultProfileID, feed.ID, changed, nil, nil); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(); after == base {
		t.Fatal("fingerprint ignored a feed title change")
	}
	base = fingerprint()

	guidTwo := "entry-guid-two"
	withNew := initial
	withNew.Entries = append(withNew.Entries, domain.ParsedEntry{
		GUID: &guidTwo, Title: "Second article",
		PublishedAt: time.Now().UTC(), ContentHash: "hash-two",
		SanitizedHTML: "<p>more</p>", PlainText: "more",
	})
	if _, err := SaveFeedRefresh(ctx, db, domain.DefaultProfileID, feed.ID, withNew, nil, nil); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(); after == base {
		t.Fatal("fingerprint ignored a new entry")
	}
}

// Research papers are part of the library snapshot, so the fingerprint must
// move when they change while feed polling noise stays invisible.
func TestSnapshotFingerprintTracksResearchChanges(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fingerprint := func() string {
		document, err := ExportLibrarySnapshot(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := LibrarySnapshotFingerprint(document)
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	base := fingerprint()

	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "New paper",
	})
	if err != nil {
		t.Fatal(err)
	}
	after := fingerprint()
	if after == base {
		t.Fatal("fingerprint ignored a new research paper")
	}

	title := "Renamed paper"
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	afterEdit := fingerprint()
	if afterEdit == after {
		t.Fatal("fingerprint ignored a research paper edit")
	}

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件", "")
	if err != nil {
		t.Fatal(err)
	}
	if changed := fingerprint(); changed == afterEdit {
		t.Fatal("fingerprint ignored a new research tag")
	}
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, strPointer("特急"), nil); err != nil {
		t.Fatal(err)
	}
	if changed := fingerprint(); changed == afterEdit {
		t.Fatal("fingerprint ignored a research tag rename")
	}

	// A paper's labels live in their own table, so that table has to be part of
	// the fingerprinted payload or a device would sync as unchanged while the
	// assignments differ.
	beforeAssignment := fingerprint()
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{TagIDs: &[]string{tag.ID}}); err != nil {
		t.Fatal(err)
	}
	if changed := fingerprint(); changed == beforeAssignment {
		t.Fatal("fingerprint ignored a research paper tag assignment")
	}
	document, err := ExportLibrarySnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if rows := snapshotTableRowCount(document, "research_paper_tags"); rows != 1 {
		t.Fatalf("expected the tag association in the snapshot, got %d rows", rows)
	}
}

func snapshotTableRowCount(document BackupDocument, name string) int {
	for _, table := range document.Tables {
		if table.Name == name {
			return len(table.Rows)
		}
	}
	return -1
}
