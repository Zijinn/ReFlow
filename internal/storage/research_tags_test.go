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
	resetResearchTagPalette(t, ctx, db)

	first, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "  急件  ", "red")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if first.Name != "急件" {
		t.Fatalf("expected trimmed name, got %q", first.Name)
	}
	if first.Position != 0 {
		t.Fatalf("expected position 0, got %d", first.Position)
	}
	if first.Color != "red" {
		t.Fatalf("expected the chosen colour to come back, got %q", first.Color)
	}
	second, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "慢工", "")
	if err != nil {
		t.Fatalf("create second tag: %v", err)
	}
	if second.Position != 1 {
		t.Fatalf("expected position 1, got %d", second.Position)
	}
	if second.Color != "" {
		t.Fatalf("an unpicked colour must store as empty, got %q", second.Color)
	}

	tags, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 2 || tags[0].ID != first.ID || tags[1].ID != second.ID {
		t.Fatalf("palette order wrong: %+v", tags)
	}
	if tags[0].Color != "red" || tags[1].Color != "" {
		t.Fatalf("list lost the colours: %+v", tags)
	}

	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件", "teal"); !errors.Is(err, ErrDuplicateResearchTag) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "   ", "teal"); !isTagValidationError(err) {
		t.Fatalf("expected validation error for blank name, got %v", err)
	}
	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, strings.Repeat("标", researchTagNameLimit+1), "teal"); !isTagValidationError(err) {
		t.Fatalf("expected validation error for long name, got %v", err)
	}
	if _, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "染色", "crimson"); !isTagValidationError(err) {
		t.Fatalf("expected validation error for unknown colour, got %v", err)
	}

	updated, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, first.ID, strPointer("特急"), nil, nil)
	if err != nil {
		t.Fatalf("update tag: %v", err)
	}
	if updated.Name != "特急" || updated.Position != first.Position {
		t.Fatalf("update changed more than the name: %+v", updated)
	}
	if updated.Color != "red" {
		t.Fatalf("a rename that never mentioned colour dropped it: %+v", updated)
	}
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, first.ID, strPointer("慢工"), nil, nil); !errors.Is(err, ErrDuplicateResearchTag) {
		t.Fatalf("expected duplicate error on rename, got %v", err)
	}
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, "missing", strPointer("名字"), nil, nil); !errors.Is(err, ErrNotFound) {
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
		tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, name, "")
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

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改", "")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	other, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "已投", "")
	if err != nil {
		t.Fatalf("create other tag: %v", err)
	}
	extra, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "学生一作", "")
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

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改", "")
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
	resetResearchTagPalette(t, ctx, db)

	var created []domain.ResearchTag
	seeds := []struct{ name, color string }{
		{"急件", "red"}, {"在改", "teal"}, {"合作者", "violet"},
	}
	for _, seed := range seeds {
		tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, seed.name, seed.color)
		if err != nil {
			t.Fatalf("create tag %q: %v", seed.name, err)
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
	colors := map[string]string{created[0].ID: "red", created[1].ID: "teal", created[2].ID: "violet"}
	for index, tag := range tags {
		if tag.ID != want[index] || tag.Position != index {
			t.Fatalf("tag %d should be %q at position %d, got %+v", index, want[index], index, tag)
		}
		// The tint belongs to the label, not to the row it happens to sit in, so
		// dragging the palette reorders chips without re-colouring them.
		if tag.Color != colors[tag.ID] {
			t.Fatalf("tag %d (%q) came back %q, want %q", index, tag.ID, tag.Color, colors[tag.ID])
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
	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "急件", "")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	other, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改", "")
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

func TestUpdateResearchTagKeepsPaperReferences(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)
	resetResearchTagPalette(t, ctx, db)

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "High", "red")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	paper, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Paper", TagIDs: []string{tag.ID},
	})
	if err != nil {
		t.Fatalf("create paper: %v", err)
	}
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, strPointer("顶刊冲刺"), nil, nil); err != nil {
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

	// The palette is written with raw SQL here: this schema predates the colour
	// column, so the storage helper would be testing a table it has not seen.
	for _, row := range []struct{ id, profileID, name string }{
		{"mine-tag", domain.DefaultProfileID, "急件"},
		{"theirs-tag", "other-profile", "别人的"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO research_tags
			(id, profile_id, name, position, created_at, updated_at)
			VALUES (?, ?, ?, 0, '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
			row.id, row.profileID, row.name); err != nil {
			t.Fatalf("seed tag %q: %v", row.name, err)
		}
	}
	mine := domain.ResearchTag{ID: "mine-tag", Name: "急件"}
	theirs := domain.ResearchTag{ID: "theirs-tag", Name: "别人的"}
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

// The workbench tints a chip from the tag's own colour, so a brand-new install
// must already answer the three priority labels with the colours the owner
// asked for: High red, Medium amber, Low green.
func TestFreshDatabaseSeedsPriorityTagsWithColors(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	tags, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list seeded tags: %v", err)
	}
	want := []domain.ResearchTag{
		{Name: "High", Position: 0, Color: "red"},
		{Name: "Medium", Position: 1, Color: "amber"},
		{Name: "Low", Position: 2, Color: "green"},
	}
	if len(tags) != len(want) {
		t.Fatalf("expected the three priority labels, got %+v", tags)
	}
	for index, expected := range want {
		tag := tags[index]
		if tag.ID == "" || tag.Name != expected.Name || tag.Position != expected.Position || tag.Color != expected.Color {
			t.Fatalf("seed %d should be %+v, got %+v", index, expected, tag)
		}
	}

	// The seed fills positions 0..2, so the user's first label still appends.
	extra, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "合作者", "violet")
	if err != nil {
		t.Fatalf("create tag after the seed: %v", err)
	}
	if extra.Position != 3 || extra.Color != "violet" {
		t.Fatalf("expected position 3 with the chosen colour, got %+v", extra)
	}
}

// Only an empty palette is seeded: a user who already arranged labels must not
// wake up to a second, competing set, and a label typed by hand as "High" must
// keep its own row instead of colliding with the seed.
func TestMigrationSeedsPriorityTagsPerProfileWithEmptyPalette(t *testing.T) {
	ctx := context.Background()
	db := openDatabaseBeforeMigration(t, ctx, 18)

	const otherProfile = "00000000-0000-4000-8000-000000000009"
	if _, err := db.ExecContext(ctx,
		"INSERT INTO profiles (id, display_name) VALUES (?, ?)", otherProfile, "Work"); err != nil {
		t.Fatalf("add profile: %v", err)
	}
	// Raw SQL because this schema is the one before the colour column exists.
	if _, err := db.ExecContext(ctx, `INSERT INTO research_tags
		(id, profile_id, name, position, created_at, updated_at)
		VALUES ('built-by-hand', ?, 'High', 0, '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
		domain.DefaultProfileID); err != nil {
		t.Fatalf("seed a hand-made palette: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("upgrade to the colour migration: %v", err)
	}

	mine, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list existing palette: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != "built-by-hand" {
		t.Fatalf("a profile that already owns labels was seeded anyway: %+v", mine)
	}
	if mine[0].Color != "" {
		t.Fatalf("a pre-colour label must read as unpicked, got %q", mine[0].Color)
	}

	fresh, err := ListResearchTags(ctx, db, otherProfile)
	if err != nil {
		t.Fatalf("list new profile's palette: %v", err)
	}
	if len(fresh) != 3 || fresh[0].Name != "High" || fresh[0].Color != "red" ||
		fresh[1].Name != "Medium" || fresh[1].Color != "amber" ||
		fresh[2].Name != "Low" || fresh[2].Color != "green" {
		t.Fatalf("an empty palette did not get the priority defaults: %+v", fresh)
	}
	for index, tag := range fresh {
		if tag.ID == "" || tag.Position != index {
			t.Fatalf("seed %d needs its own id and priority order, got %+v", index, tag)
		}
	}
}

// A patch sends whichever control the user touched, so a nil field has to leave
// the column alone while a bad colour is refused before anything is written.
func TestUpdateResearchTagAppliesPartialPatch(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)
	resetResearchTagPalette(t, ctx, db)

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "在改", " gray ")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if tag.Color != "gray" {
		t.Fatalf("expected the colour to be trimmed, got %q", tag.Color)
	}
	other, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "已投", "blue")
	if err != nil {
		t.Fatalf("create other tag: %v", err)
	}

	// Colour only: the label keeps its name, position and papers.
	recoloured, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, strPointer("orange"), nil)
	if err != nil {
		t.Fatalf("recolour: %v", err)
	}
	if recoloured.Name != "在改" || recoloured.Position != tag.Position || recoloured.Color != "orange" {
		t.Fatalf("recolour changed more than the colour: %+v", recoloured)
	}

	// Both at once.
	both, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, strPointer(" 修改中 "), strPointer("teal"), nil)
	if err != nil {
		t.Fatalf("patch name and colour: %v", err)
	}
	if both.Name != "修改中" || both.Color != "teal" {
		t.Fatalf("expected a trimmed name and the new colour, got %+v", both)
	}

	// Clearing the choice is a real value: the client goes back to index tinting.
	cleared, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, strPointer(""), nil)
	if err != nil {
		t.Fatalf("clear colour: %v", err)
	}
	if cleared.Color != "" {
		t.Fatalf("expected an unpicked colour, got %+v", cleared)
	}

	// Renaming a tag to the name it already has is not a clash with itself.
	sameName, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, strPointer("修改中"), strPointer("teal"), nil)
	if err != nil {
		t.Fatalf("reapply the tag's own name: %v", err)
	}
	if sameName.Name != "修改中" || sameName.Color != "teal" {
		t.Fatalf("expected the tag unchanged, got %+v", sameName)
	}

	// An empty patch answers the tag without rewriting it.
	untouched, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if untouched.Name != sameName.Name || untouched.Color != sameName.Color {
		t.Fatalf("an empty patch rewrote the tag: %+v", untouched)
	}

	// Existence still beats the duplicate check, so a deleted tag cannot leak
	// another label's name clash as a 409.
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, "missing", strPointer(other.Name), nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found before duplicate, got %v", err)
	}
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, strPointer(other.Name), nil, nil); !errors.Is(err, ErrDuplicateResearchTag) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	for _, bad := range []struct {
		name, color string
	}{{"修改中", "crimson"}, {"", "teal"}, {strings.Repeat("标", researchTagNameLimit+1), "teal"}} {
		if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID,
			tag.ID, strPointer(bad.name), strPointer(bad.color), nil); !isTagValidationError(err) {
			t.Fatalf("patch %+v should be refused, got %v", bad, err)
		}
	}
	// A refused patch must not have half-applied: the colour stayed put.
	rejected, err := GetResearchTag(ctx, db, domain.DefaultProfileID, tag.ID)
	if err != nil {
		t.Fatalf("reload tag: %v", err)
	}
	if rejected.Name != "修改中" || rejected.Color != "teal" {
		t.Fatalf("a refused patch wrote to the row: %+v", rejected)
	}
	// A client-fixable field problem is reported even about a tag that is gone.
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, "missing", nil, strPointer("magenta"), nil); !isTagValidationError(err) {
		t.Fatalf("expected validation before lookup, got %v", err)
	}
}

// Every name the stylesheet ships has to round-trip, because the colour picker
// offers exactly these swatches.
func TestResearchTagAcceptsEveryShippedColor(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)
	resetResearchTagPalette(t, ctx, db)

	for index, color := range researchTagColorNames {
		name := fmt.Sprintf("色-%d", index)
		tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, name, color)
		if err != nil {
			t.Fatalf("create tag %q with %q: %v", name, color, err)
		}
		if tag.Color != color {
			t.Fatalf("create echoed %q, want %q", tag.Color, color)
		}
		stored, err := GetResearchTag(ctx, db, domain.DefaultProfileID, tag.ID)
		if err != nil {
			t.Fatalf("reload tag: %v", err)
		}
		if stored.Color != color {
			t.Fatalf("tag stored colour %q, want %q", stored.Color, color)
		}
		if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, strPointer(color), nil); err != nil {
			t.Fatalf("patch colour %q: %v", color, err)
		}
	}
	body, err := json.Marshal(domain.ResearchTag{ID: "x", Name: "x", Color: ""})
	if err != nil {
		t.Fatalf("marshal tag: %v", err)
	}
	if !strings.Contains(string(body), `"color":""`) {
		t.Fatalf("an unpicked colour must serialise as the empty string, got %s", body)
	}
}

// The colour switch is a second column on purpose: turning the tint off has to
// leave the picked colour alone, so switching it back on returns the chip to the
// exact shade the user chose rather than to the palette default.
func TestResearchTagColorSwitchKeepsThePickedColor(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)
	resetResearchTagPalette(t, ctx, db)

	tag, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "高优先级", "red")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	// A tag created without a word about colour paints: an invisible new label
	// would leave the settings pane with nothing to switch back on.
	if !tag.ColorEnabled {
		t.Fatalf("a new tag should ship with its colour enabled, got %+v", tag)
	}

	off, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, nil, boolPointer(false))
	if err != nil {
		t.Fatalf("disable the colour: %v", err)
	}
	if off.ColorEnabled || off.Color != "red" {
		t.Fatalf("disabling must mute the colour, not erase it, got %+v", off)
	}

	on, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, nil, boolPointer(true))
	if err != nil {
		t.Fatalf("re-enable the colour: %v", err)
	}
	if !on.ColorEnabled || on.Color != "red" {
		t.Fatalf("expected the picked colour to survive the switch, got %+v", on)
	}

	// A patch that never mentions the switch cannot flip it: renaming while the
	// colour is on leaves it on.
	renamed, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID,
		strPointer("改名了"), nil, nil)
	if err != nil {
		t.Fatalf("rename while enabled: %v", err)
	}
	if !renamed.ColorEnabled {
		t.Fatalf("a name-only patch flipped the colour switch: %+v", renamed)
	}
	// And the same holds the other way: once it is off, an empty patch keeps it off.
	if _, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, nil, boolPointer(false)); err != nil {
		t.Fatalf("disable the tag: %v", err)
	}
	untouched, err := UpdateResearchTag(ctx, db, domain.DefaultProfileID, tag.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if untouched.ColorEnabled {
		t.Fatalf("an empty patch flipped the colour switch: %+v", untouched)
	}

	// The switch is per tag: muting one label leaves its palette neighbours alone.
	neighbour, err := CreateResearchTag(ctx, db, domain.DefaultProfileID, "数据待补", "blue")
	if err != nil {
		t.Fatalf("create neighbour tag: %v", err)
	}
	kept, err := GetResearchTag(ctx, db, domain.DefaultProfileID, neighbour.ID)
	if err != nil {
		t.Fatalf("reload neighbour: %v", err)
	}
	if !kept.ColorEnabled {
		t.Fatalf("muting one tag muted its neighbour: %+v", kept)
	}
}

// Every palette row written before migration 0019 has to come back painted, or
// the ALTER's default silently mutes the labels a user already relied on.
func TestResearchTagColorEnabledBackfillsExistingRows(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	tags, err := ListResearchTags(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list seeded palette: %v", err)
	}
	if len(tags) == 0 {
		t.Fatal("migration 0018 seeded no palette, so this test proves nothing")
	}
	for _, tag := range tags {
		if !tag.ColorEnabled {
			t.Fatalf("pre-existing tag %q came back unmuted: %+v", tag.Name, tag)
		}
	}
}

// resetResearchTagPalette empties the palette migration 0018 hands every fresh
// profile, so a test can assert on positions and counts against only the labels
// it created itself.
func resetResearchTagPalette(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{"DELETE FROM research_paper_tags", "DELETE FROM research_tags"} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
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
