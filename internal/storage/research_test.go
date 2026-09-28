package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func newResearchTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestResearchPaperLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "First", Authors: []string{"A"},
	})
	if err != nil {
		t.Fatalf("create research paper: %v", err)
	}
	if created.Position != 0 {
		t.Fatalf("expected position 0, got %d", created.Position)
	}
	if created.LastUpdated == "" {
		t.Fatal("expected last_updated to be set")
	}

	if _, err := time.Parse(time.RFC3339Nano, created.LastUpdated); err != nil {
		t.Fatalf("last_updated must be RFC3339, got %q: %v", created.LastUpdated, err)
	}

	second, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Second",
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.Position != 1 {
		t.Fatalf("expected position 1, got %d", second.Position)
	}

	title := "First (edited)"
	stages := []domain.ResearchStage{{Name: "Intro", Done: true, Children: nil}}
	updated, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID, domain.ResearchPaperPatch{
		Title: &title, Stages: &stages,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != title {
		t.Fatalf("expected updated title, got %q", updated.Title)
	}
	if len(updated.Stages) != 1 || !updated.Stages[0].Done {
		t.Fatalf("stages not persisted: %+v", updated.Stages)
	}

	// Reorder: put second before first.
	if err := ReorderResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch, []string{second.ID, created.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	list, err := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("reorder not applied: %+v", list)
	}

	if err := DeleteResearchPaper(ctx, db, domain.DefaultProfileID, second.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	remaining, _ := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch)
	if len(remaining) != 1 {
		t.Fatalf("expected one remaining, got %d", len(remaining))
	}
}

func TestMoveResearchPaperInheritsJournal(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	journal := "Journal of Testing"
	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindSubmitted, Title: "Paper", Authors: []string{"A", "B"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID, domain.ResearchPaperPatch{
		CurrentJournal: &journal,
	}); err != nil {
		t.Fatalf("set current journal: %v", err)
	}

	moved, err := MoveResearchPaper(ctx, db, domain.DefaultProfileID, created.ID, domain.ResearchKindPublished)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.Kind != domain.ResearchKindPublished {
		t.Fatalf("expected published kind, got %q", moved.Kind)
	}
	if moved.Journal != journal {
		t.Fatalf("expected current journal to carry over, got %q", moved.Journal)
	}
	// The moved paper should no longer appear under its old kind.
	submitted, _ := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindSubmitted)
	if len(submitted) != 0 {
		t.Fatalf("expected no submitted papers, got %d", len(submitted))
	}
}

func TestMoveResearchPaperKeepsStagesAndHistory(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	stages := []domain.ResearchStage{
		{Name: "Intro", Done: true, Children: []domain.ResearchStage{{Name: "Draft outline"}}},
		{Name: "Experiments"},
	}
	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Progress", Stages: stages,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	history := []domain.SubmissionRecord{{Journal: "JFE", Date: "2026-01-05", Status: "submitted"}}
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{History: &history}); err != nil {
		t.Fatalf("set history: %v", err)
	}

	moved, err := MoveResearchPaper(ctx, db, domain.DefaultProfileID, created.ID, domain.ResearchKindSubmitted)
	if err != nil {
		t.Fatalf("move to submitted: %v", err)
	}
	if len(moved.Stages) != 2 || len(moved.Stages[0].Children) != 1 || !moved.Stages[0].Done {
		t.Fatalf("move cleared stages: %+v", moved.Stages)
	}
	if len(moved.History) != 1 || moved.History[0].Journal != "JFE" {
		t.Fatalf("move cleared history: %+v", moved.History)
	}

	published, err := MoveResearchPaper(ctx, db, domain.DefaultProfileID, created.ID, domain.ResearchKindPublished)
	if err != nil {
		t.Fatalf("move to published: %v", err)
	}
	if len(published.Stages) != 2 || len(published.History) != 1 {
		t.Fatalf("second move cleared progress data: stages=%+v history=%+v", published.Stages, published.History)
	}
}

func TestConcurrentCreateAndMoveAssignUniquePositions(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	const workers = 5
	var wg sync.WaitGroup
	createErrs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
				Kind: domain.ResearchKindResearch, Title: fmt.Sprintf("Concurrent %d", i),
			}); err != nil {
				createErrs <- err
			}
		}(i)
	}
	wg.Wait()
	close(createErrs)
	for err := range createErrs {
		t.Fatalf("concurrent create: %v", err)
	}
	research, err := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if positions := uniquePositions(research); len(positions) != workers {
		t.Fatalf("concurrent creates produced duplicate positions: %v", positions)
	}

	var moveWg sync.WaitGroup
	moveErrs := make(chan error, workers)
	for _, paper := range research {
		moveWg.Add(1)
		go func(id string) {
			defer moveWg.Done()
			if _, err := MoveResearchPaper(ctx, db, domain.DefaultProfileID, id, domain.ResearchKindSubmitted); err != nil {
				moveErrs <- err
			}
		}(paper.ID)
	}
	moveWg.Wait()
	close(moveErrs)
	for err := range moveErrs {
		t.Fatalf("concurrent move: %v", err)
	}
	submitted, err := ListResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindSubmitted)
	if err != nil {
		t.Fatalf("list submitted: %v", err)
	}
	if positions := uniquePositions(submitted); len(positions) != workers {
		t.Fatalf("concurrent moves produced duplicate positions: %v", positions)
	}
}

func uniquePositions(papers []domain.ResearchPaper) map[int]struct{} {
	positions := make(map[int]struct{}, len(papers))
	for _, paper := range papers {
		if _, duplicate := positions[paper.Position]; duplicate {
			continue
		}
		positions[paper.Position] = struct{}{}
	}
	return positions
}

func TestResearchPaperFieldValidation(t *testing.T) {
	ctx := context.Background()
	db := newResearchTestDB(t)

	var validation *ResearchValidationError
	if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Negative", SubmissionCount: -1,
	}); !errors.As(err, &validation) {
		t.Fatalf("negative submission_count should be rejected, got %v", err)
	}

	long := strings.Repeat("标", researchTextLimit+1)
	if _, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: long,
	}); !errors.As(err, &validation) {
		t.Fatalf("overlong title should be rejected, got %v", err)
	}

	created, err := CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Valid",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	count := -2
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{SubmissionCount: &count}); !errors.As(err, &validation) {
		t.Fatalf("negative patched submission_count should be rejected, got %v", err)
	}
	notes := strings.Repeat("n", researchLongTextLimit+1)
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{Notes: &notes}); !errors.As(err, &validation) {
		t.Fatalf("overlong notes should be rejected, got %v", err)
	}
	okNotes := strings.Repeat("n", researchLongTextLimit)
	if _, err := UpdateResearchPaper(ctx, db, domain.DefaultProfileID, created.ID,
		domain.ResearchPaperPatch{Notes: &okNotes}); err != nil {
		t.Fatalf("notes at the limit should be accepted: %v", err)
	}
	if err := ReorderResearchPapers(ctx, db, domain.DefaultProfileID, domain.ResearchKindResearch,
		make([]string, researchReorderLimit+1)); !errors.As(err, &validation) {
		t.Fatalf("oversized reorder should be rejected, got %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO research_papers
		(id, profile_id, kind, submission_count) VALUES ('x', ?, 'research', -1)`,
		domain.DefaultProfileID); err == nil {
		t.Fatal("migration CHECK should reject a negative submission_count")
	}
}
