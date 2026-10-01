package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestResearchTagCRUD(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	first, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "  急件  ")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if first.Name != "急件" {
		t.Fatalf("expected trimmed name, got %q", first.Name)
	}
	if first.Position != 0 {
		t.Fatalf("expected position 0, got %d", first.Position)
	}
	second, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "慢工")
	if err != nil {
		t.Fatalf("create second tag: %v", err)
	}
	if second.Position != 1 {
		t.Fatalf("expected position 1, got %d", second.Position)
	}

	tags, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 2 || tags[0].ID != first.ID || tags[1].ID != second.ID {
		t.Fatalf("palette order wrong: %+v", tags)
	}

	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件"); !errors.Is(err, ErrDuplicateResearchTag) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "   "); !isTagValidationError(err) {
		t.Fatalf("expected validation error for blank name, got %v", err)
	}
	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, strings.Repeat("标", researchTagNameLimit+1)); !isTagValidationError(err) {
		t.Fatalf("expected validation error for long name, got %v", err)
	}

	renamed, err := RenameResearchTag(ctx, db, domain.DefaultProfileID, first.ID, "特急")
	if err != nil {
		t.Fatalf("rename tag: %v", err)
	}
	if renamed.Name != "特急" || renamed.Position != first.Position {
		t.Fatalf("rename changed more than the name: %+v", renamed)
	}
	if _, err := RenameResearchTag(ctx, db, domain.DefaultProfileID, first.ID, "慢工"); !errors.Is(err, ErrDuplicateResearchTag) {
		t.Fatalf("expected duplicate error on rename, got %v", err)
	}
	if _, err := RenameResearchTag(ctx, db, domain.DefaultProfileID, "missing", "名字"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found on rename, got %v", err)
	}

	if err := DeleteResearchTag(ctx, db, domain.DefaultProfileID, first.ID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if err := DeleteResearchTag(ctx, db, domain.DefaultProfileID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found on second delete, got %v", err)
	}
	remaining, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != second.ID {
		t.Fatalf("expected only the untouched tag, got %+v", remaining)
	}
}

func TestDeleteResearchTagUntagsPapers(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	other, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "已投")
	if err != nil {
		t.Fatalf("create other tag: %v", err)
	}
	tagged, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Tagged", TagID: tag.ID,
	})
	if err != nil {
		t.Fatalf("create tagged paper: %v", err)
	}
	kept, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Kept", TagID: other.ID,
	})
	if err != nil {
		t.Fatalf("create other paper: %v", err)
	}

	if err := DeleteResearchTag(ctx, db, domain.DefaultProfileID, tag.ID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	afterDelete, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, tagged.ID)
	if err != nil {
		t.Fatalf("get untagged paper: %v", err)
	}
	if afterDelete.TagID != "" {
		t.Fatalf("expected paper to be untagged, got %q", afterDelete.TagID)
	}
	untouched, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, kept.ID)
	if err != nil {
		t.Fatalf("get untouched paper: %v", err)
	}
	if untouched.TagID != other.ID {
		t.Fatalf("expected untouched tag %q, got %q", other.ID, untouched.TagID)
	}
}

func TestRenameResearchTagKeepsPaperReferences(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "High")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	paper, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Paper", TagID: tag.ID,
	})
	if err != nil {
		t.Fatalf("create paper: %v", err)
	}
	if _, err := RenameResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, "顶刊冲刺"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	reloaded, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID)
	if err != nil {
		t.Fatalf("reload paper: %v", err)
	}
	if reloaded.TagID != tag.ID {
		t.Fatalf("rename dropped the paper reference: %q", reloaded.TagID)
	}
}

// The v6.3 upgrade replaces the fixed priority enum with user tags, so a
// database written by an older build must come out of Migrate with the same
// labels the user already had, in the old priority order.
func TestMigrationSeedsTagsFromLegacyPriority(t *testing.T) {
	ctx := context.Background()
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
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
		if version >= 16 {
			break
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := applyMigration(ctx, db, version, entry.Name(), string(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, title, priority string }{
		{"legacy-high", "High paper", "High"},
		{"legacy-average", "Average paper", "Average"},
		{"legacy-medium", "Medium paper", "Medium"},
		{"legacy-custom", "Custom paper", "复审中"},
		{"legacy-empty", "Untagged paper", ""},
	} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO research_papers (id, profile_id, kind, position, title, priority) VALUES (?, ?, 'research', 0, ?, ?)",
			row.id, domain.DefaultProfileID, row.title, row.priority); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("upgrade legacy database: %v", err)
	}

	tags, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list seeded tags: %v", err)
	}
	want := []string{"High", "Medium", "Average", "复审中"}
	if len(tags) != len(want) {
		t.Fatalf("expected %d seeded tags, got %+v", len(want), tags)
	}
	byName := make(map[string]domain.ResearchTag, len(tags))
	for index, tag := range tags {
		if tag.Position != index {
			t.Fatalf("expected contiguous positions, got %d for %q", tag.Position, tag.Name)
		}
		if tag.Name != want[index] {
			t.Fatalf("expected tag %d to be %q, got %q", index, want[index], tag.Name)
		}
		byName[tag.Name] = tag
	}
	for legacy, wantTag := range map[string]string{
		"legacy-high": "High", "legacy-medium": "Medium",
		"legacy-average": "Average", "legacy-custom": "复审中", "legacy-empty": "",
	} {
		paper, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, legacy)
		if err != nil {
			t.Fatalf("get %s: %v", legacy, err)
		}
		if wantTag == "" {
			if paper.TagID != "" {
				t.Fatalf("expected %s untagged, got %q", legacy, paper.TagID)
			}
			continue
		}
		if paper.TagID != byName[wantTag].ID {
			t.Fatalf("expected %s to point at %q, got %q", legacy, wantTag, paper.TagID)
		}
	}
}

func isTagValidationError(err error) bool {
	var validation *ResearchTagValidationError
	return errors.As(err, &validation)
}
