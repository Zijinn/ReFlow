package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func TestResearchPaperCarriesSeveralTags(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	var tags []domain.ResearchTag
	for _, name := range []string{"急件", "在改", "合作者"} {
		tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, name)
		if err != nil {
			t.Fatalf("create tag %q: %v", name, err)
		}
		tags = append(tags, tag)
	}

	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Several labels",
		TagIDs: []string{tags[2].ID, tags[0].ID, tags[2].ID},
	})
	if err != nil {
		t.Fatalf("create paper with tags: %v", err)
	}
	// The order the user assigned wins and a repeat collapses; palette order is
	// a separate concern, so the assignment survives a later reorder.
	if !sameStrings(created.TagIDs, []string{tags[2].ID, tags[0].ID}) {
		t.Fatalf("expected the assignment order without repeats, got %#v", created.TagIDs)
	}

	// The list route is what the workbench renders, so it has to carry the same
	// labels the single-paper read returns.
	items, err := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch)
	if err != nil {
		t.Fatalf("list papers: %v", err)
	}
	if len(items) != 1 || !sameStrings(items[0].TagIDs, created.TagIDs) {
		t.Fatalf("list lost the paper's tags: %+v", items)
	}

	title := "Renamed"
	untouched, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{Title: &title})
	if err != nil {
		t.Fatalf("patch without tags: %v", err)
	}
	if !sameStrings(untouched.TagIDs, created.TagIDs) {
		t.Fatalf("a patch that never mentioned tags rewrote them: %#v", untouched.TagIDs)
	}

	unknown := []string{"never-a-tag", tags[1].ID}
	resolved, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{TagIDs: &unknown})
	if err != nil {
		t.Fatalf("unknown tag ids should stay accepted: %v", err)
	}
	if !sameStrings(resolved.TagIDs, unknown) {
		t.Fatalf("expected the assignment to be stored as given, got %#v", resolved.TagIDs)
	}

	cleared := []string{}
	afterClear, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{TagIDs: &cleared})
	if err != nil {
		t.Fatalf("clear tags: %v", err)
	}
	if len(afterClear.TagIDs) != 0 {
		t.Fatalf("expected an empty assignment, got %#v", afterClear.TagIDs)
	}
	body, err := json.Marshal(afterClear)
	if err != nil {
		t.Fatalf("marshal paper: %v", err)
	}
	if !strings.Contains(string(body), `"tag_ids":[]`) {
		t.Fatalf("an unlabelled paper must answer an empty list, got %s", body)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM research_paper_tags WHERE paper_id = ?", created.ID).Scan(&rows); err != nil {
		t.Fatalf("count associations: %v", err)
	}
	if rows != 0 {
		t.Fatalf("clearing left %d association rows behind", rows)
	}
}

func TestResearchPaperTagLimits(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	filler := make([]string, researchPaperTagLimit)
	for index := range filler {
		filler[index] = fmt.Sprintf("tag-%d", index)
	}
	if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "At the ceiling", TagIDs: filler,
	}); err != nil {
		t.Fatalf("%d tags should be accepted: %v", researchPaperTagLimit, err)
	}
	// Repeats count once, so the ceiling is on distinct labels.
	repeated := append(append([]string{}, filler...), filler[0])
	if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Repeat", TagIDs: repeated,
	}); err != nil {
		t.Fatalf("a repeated tag should collapse under the ceiling: %v", err)
	}

	var validation *ResearchValidationError
	over := append(append([]string{}, filler...), "tag-over")
	if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Too many", TagIDs: over,
	}); !errors.As(err, &validation) {
		t.Fatalf("%d tags should be rejected, got %v", len(over), err)
	}
	paper, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Patched later",
	})
	if err != nil {
		t.Fatalf("create paper: %v", err)
	}
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID,
		domain.ResearchPaperPatch{TagIDs: &over}); !errors.As(err, &validation) {
		t.Fatalf("patching %d tags should be rejected, got %v", len(over), err)
	}

	long := []string{strings.Repeat("标", researchTextLimit+1)}
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID,
		domain.ResearchPaperPatch{TagIDs: &long}); !errors.As(err, &validation) {
		t.Fatalf("an overlong tag id should be rejected, got %v", err)
	}
	// A rejected patch must not have taken the old assignment down with it.
	reloaded, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID)
	if err != nil {
		t.Fatalf("reload paper: %v", err)
	}
	if len(reloaded.TagIDs) != 0 {
		t.Fatalf("a rejected patch changed the tags: %#v", reloaded.TagIDs)
	}
}

func TestDeleteResearchTagLiftsOnlyThatLabel(t *testing.T) {
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
	extra, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "学生一作")
	if err != nil {
		t.Fatalf("create extra tag: %v", err)
	}
	tagged, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Tagged", TagIDs: []string{tag.ID, other.ID},
	})
	if err != nil {
		t.Fatalf("create tagged paper: %v", err)
	}
	kept, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Kept", TagIDs: []string{other.ID, extra.ID},
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
	if !sameStrings(afterDelete.TagIDs, []string{other.ID}) {
		t.Fatalf("expected only the deleted label to go, got %#v", afterDelete.TagIDs)
	}
	untouched, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, kept.ID)
	if err != nil {
		t.Fatalf("get untouched paper: %v", err)
	}
	if !sameStrings(untouched.TagIDs, []string{other.ID, extra.ID}) {
		t.Fatalf("deleting one tag disturbed another paper's labels: %#v", untouched.TagIDs)
	}
	var orphans int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM research_paper_tags WHERE tag_id = ?", tag.ID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("deleting a tag left %d associations behind", orphans)
	}
}

func TestDeleteResearchPaperClearsItsLabels(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	paper, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Doomed", TagIDs: []string{tag.ID},
	})
	if err != nil {
		t.Fatalf("create paper: %v", err)
	}
	if err := DeleteResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID); err != nil {
		t.Fatalf("delete paper: %v", err)
	}
	if err := DeleteResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found on second delete, got %v", err)
	}
	var orphans int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM research_paper_tags WHERE paper_id = ?", paper.ID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("deleting a paper left %d associations behind", orphans)
	}
	// The palette itself is untouched, so the label is ready for the next paper.
	if _, err := GetResearchTag(ctx, db, domain.DefaultProfileID, tag.ID); err != nil {
		t.Fatalf("deleting a paper removed the tag: %v", err)
	}
}

func TestReorderResearchTags(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	var created []domain.ResearchTag
	for _, name := range []string{"急件", "在改", "合作者"} {
		tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, name)
		if err != nil {
			t.Fatalf("create tag %q: %v", name, err)
		}
		created = append(created, tag)
	}
	paper, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Order matters",
		TagIDs: []string{created[2].ID, created[0].ID},
	})
	if err != nil {
		t.Fatalf("create paper: %v", err)
	}

	ordered := []string{created[1].ID, created[2].ID, created[0].ID, "never-a-tag"}
	if err := ReorderResearchTags(ctx, db, domain.DefaultProfileID, ordered); err != nil {
		t.Fatalf("reorder tags: %v", err)
	}
	tags, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	want := []string{created[1].ID, created[2].ID, created[0].ID}
	if len(tags) != len(want) {
		t.Fatalf("expected %d tags, got %+v", len(want), tags)
	}
	for index, tag := range tags {
		if tag.ID != want[index] || tag.Position != index {
			t.Fatalf("tag %d should be %q at position %d, got %+v", index, want[index], index, tag)
		}
	}

	// Dragging the palette is not re-assigning papers: a paper keeps the label
	// order its own assignment was made in.
	reloaded, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, paper.ID)
	if err != nil {
		t.Fatalf("reload paper: %v", err)
	}
	if !sameStrings(reloaded.TagIDs, []string{created[2].ID, created[0].ID}) {
		t.Fatalf("palette reorder changed a paper's assignment order: %#v", reloaded.TagIDs)
	}

	var validation *ResearchTagValidationError
	if err := ReorderResearchTags(ctx, db, domain.DefaultProfileID,
		make([]string, researchReorderLimit+1)); !errors.As(err, &validation) {
		t.Fatalf("oversized tag reorder should be rejected, got %v", err)
	}
}

// The workspace renders every paper of a kind at once, so the label load must
// batch across the whole list instead of asking per paper - and batching has to
// survive a list longer than one query's worth of ids.
func TestListResearchPapersLoadsLabelsAcrossChunks(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	const papers = researchTagQueryChunk*2 + 7
	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	other, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改")
	if err != nil {
		t.Fatalf("create other tag: %v", err)
	}
	values := make([]string, 0, papers)
	args := make([]any, 0, papers)
	for index := range papers {
		values = append(values, fmt.Sprintf("('paper-%d', ?, 'research', %d, 'Paper %d')", index, index, index))
		args = append(args, domain.DefaultProfileID)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO research_papers (id, profile_id, kind, position, title) VALUES "+strings.Join(values, ","),
		args...); err != nil {
		t.Fatalf("seed papers: %v", err)
	}
	for index := range papers {
		if index%3 != 0 {
			continue
		}
		for position, tagID := range []string{tag.ID, other.ID} {
			if _, err := db.ExecContext(ctx,
				"INSERT INTO research_paper_tags (paper_id, tag_id, position) VALUES (?, ?, ?)",
				fmt.Sprintf("paper-%d", index), tagID, position); err != nil {
				t.Fatalf("seed association: %v", err)
			}
		}
	}

	items, err := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch)
	if err != nil {
		t.Fatalf("list papers: %v", err)
	}
	if len(items) != papers {
		t.Fatalf("expected %d papers, got %d", papers, len(items))
	}
	for index, paper := range items {
		want := []string{}
		if index%3 == 0 {
			want = []string{tag.ID, other.ID}
		}
		if !sameStrings(paper.TagIDs, want) {
			t.Fatalf("paper %d carries %#v, want %#v", index, paper.TagIDs, want)
		}
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
		Kind: domain.ResearchKindResearch, Title: "Paper", TagIDs: []string{tag.ID},
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
	if !sameStrings(reloaded.TagIDs, []string{tag.ID}) {
		t.Fatalf("rename dropped the paper reference: %#v", reloaded.TagIDs)
	}
}

// The v6.3 upgrade replaces the fixed priority enum with user tags, so a
// database written by an older build must come out of Migrate with the same
// labels the user already had, in the old priority order.
func TestMigrationSeedsTagsFromLegacyPriority(t *testing.T) {
	ctx := context.Background()
	db := openDatabaseBeforeMigration(t, ctx, 16)
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
			if len(paper.TagIDs) != 0 {
				t.Fatalf("expected %s untagged, got %#v", legacy, paper.TagIDs)
			}
			continue
		}
		if !sameStrings(paper.TagIDs, []string{byName[wantTag].ID}) {
			t.Fatalf("expected %s to point at %q, got %#v", legacy, wantTag, paper.TagIDs)
		}
	}
}

// v6.4 stored one tag id per paper; the join table has to inherit exactly the
// assignments that still name a label of the same profile, and drop the rest —
// another profile's id would surface its owner's label here.
func TestMigrationMovesPaperTagIntoAssociations(t *testing.T) {
	ctx := context.Background()
	db := openDatabaseBeforeMigration(t, ctx, 17)

	mine, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件")
	if err != nil {
		t.Fatalf("create own tag: %v", err)
	}
	theirs, err := CreateResearchTag(ctx, db, "other-profile", "别人的")
	if err != nil {
		t.Fatalf("create foreign tag: %v", err)
	}
	for _, row := range []struct{ id, tagID string }{
		{"paper-tagged", mine.ID},
		{"paper-untagged", ""},
		{"paper-foreign", theirs.ID},
		{"paper-gone", "deleted-long-ago"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO research_papers
			(id, profile_id, kind, position, title, tag_id)
			VALUES (?, ?, 'research', 0, ?, ?)`,
			row.id, domain.DefaultProfileID, row.id, row.tagID); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("upgrade v6.4 database: %v", err)
	}

	for _, want := range []struct {
		paperID string
		tagIDs  []string
	}{
		{"paper-tagged", []string{mine.ID}},
		{"paper-untagged", []string{}},
		{"paper-foreign", []string{}},
		{"paper-gone", []string{}},
	} {
		paper, err := GetResearchPaper(ctx, db, domain.DefaultProfileID, want.paperID)
		if err != nil {
			t.Fatalf("get %s: %v", want.paperID, err)
		}
		if !sameStrings(paper.TagIDs, want.tagIDs) {
			t.Fatalf("%s carried the wrong tags over: got %#v, want %#v",
				want.paperID, paper.TagIDs, want.tagIDs)
		}
	}
	var associations int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM research_paper_tags").Scan(&associations); err != nil {
		t.Fatalf("count associations: %v", err)
	}
	if associations != 1 {
		t.Fatalf("expected one carried-over association, got %d", associations)
	}
	var position int
	if err := db.QueryRowContext(ctx,
		"SELECT position FROM research_paper_tags WHERE paper_id = 'paper-tagged'").Scan(&position); err != nil {
		t.Fatalf("read carried position: %v", err)
	}
	if position != 0 {
		t.Fatalf("a carried-over label should land first, got position %d", position)
	}
	// The column is gone, so a stale build cannot read a half-migrated schema.
	if _, err := db.ExecContext(ctx, "SELECT tag_id FROM research_papers"); err == nil {
		t.Fatal("research_papers.tag_id survived the migration")
	}
}

// openDatabaseBeforeMigration builds a database exactly as the newest migration
// below `before` left it, so a later migration is tested against the schema
// shipped users actually have.
func openDatabaseBeforeMigration(t *testing.T, ctx context.Context, before int) *sql.DB {
	t.Helper()
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
		if version >= before {
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
	return db
}

func isTagValidationError(err error) bool {
	var validation *ResearchTagValidationError
	return errors.As(err, &validation)
}
