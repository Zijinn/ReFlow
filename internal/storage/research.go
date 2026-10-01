package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

var researchKinds = map[string]struct{}{
	domain.ResearchKindResearch:  {},
	domain.ResearchKindSubmitted: {},
	domain.ResearchKindPublished: {},
}

const (
	researchTextLimit     = 500
	researchLongTextLimit = 2000
	researchReorderLimit  = 500
)

// researchPaperTagLimit is how many labels one paper may wear at once: the
// tag column only has room for a few pills before the row stops reading.
const researchPaperTagLimit = 12

// researchTagQueryChunk keeps the bulk association lookup under SQLite's
// ceiling on host parameters, however long the paper list grows.
const researchTagQueryChunk = 100

// ResearchValidationError reports a client-fixable research paper problem so
// the API layer can answer 400 instead of 500.
type ResearchValidationError struct{ Reason string }

func (e *ResearchValidationError) Error() string { return e.Reason }

type researchField struct {
	name  string
	value string
	limit int
}

func checkResearchFields(fields []researchField) error {
	for _, field := range fields {
		if utf8.RuneCountInString(field.value) > field.limit {
			return &ResearchValidationError{Reason: fmt.Sprintf("field %q exceeds %d characters", field.name, field.limit)}
		}
	}
	return nil
}

func checkResearchStrings(name string, values []string) error {
	for index, value := range values {
		if utf8.RuneCountInString(value) > researchTextLimit {
			return &ResearchValidationError{Reason: fmt.Sprintf("field %s[%d] exceeds %d characters", name, index, researchTextLimit)}
		}
	}
	return nil
}

// checkResearchTagIDs bounds one paper's label list. Tag ids are not resolved
// against the palette on purpose: an id the user assigned before a palette
// reload is still their assignment, and rejecting it would turn a race into a
// failed save.
func checkResearchTagIDs(values []string) error {
	if err := checkResearchStrings("tag_ids", values); err != nil {
		return err
	}
	if count := len(uniqueResearchTagIDs(values)); count > researchPaperTagLimit {
		return &ResearchValidationError{Reason: fmt.Sprintf("a paper accepts at most %d tags, got %d", researchPaperTagLimit, count)}
	}
	return nil
}

// uniqueResearchTagIDs drops repeats while keeping the caller's order.
func uniqueResearchTagIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func checkResearchStages(stages []domain.ResearchStage) error {
	for index, stage := range stages {
		if utf8.RuneCountInString(stage.Name) > researchTextLimit {
			return &ResearchValidationError{Reason: fmt.Sprintf("stages[%d].name exceeds %d characters", index, researchTextLimit)}
		}
		if err := checkResearchStages(stage.Children); err != nil {
			return err
		}
	}
	return nil
}

func checkResearchHistory(history []domain.SubmissionRecord) error {
	for index, record := range history {
		fields := []researchField{
			{fmt.Sprintf("history[%d].journal", index), record.Journal, researchTextLimit},
			{fmt.Sprintf("history[%d].date", index), record.Date, researchTextLimit},
			{fmt.Sprintf("history[%d].status", index), record.Status, researchTextLimit},
		}
		if err := checkResearchFields(fields); err != nil {
			return err
		}
	}
	return nil
}

func validateResearchPaper(paper domain.ResearchPaper) error {
	if paper.SubmissionCount < 0 {
		return &ResearchValidationError{Reason: "submission_count must not be negative"}
	}
	fields := []researchField{
		{"title", paper.Title, researchTextLimit},
		{"file_path", paper.FilePath, researchTextLimit},
		{"next_action", paper.NextAction, researchTextLimit},
		{"notes", paper.Notes, researchLongTextLimit},
		{"research_area", paper.ResearchArea, researchTextLimit},
		{"status", paper.Status, researchTextLimit},
		{"target_journal", paper.TargetJournal, researchTextLimit},
		{"current_journal", paper.CurrentJournal, researchTextLimit},
		{"submission_date", paper.SubmissionDate, researchTextLimit},
		{"manuscript_id", paper.ManuscriptID, researchTextLimit},
		{"target_level", paper.TargetLevel, researchTextLimit},
		{"editor", paper.Editor, researchTextLimit},
		{"deadline", paper.Deadline, researchTextLimit},
		{"abstract", paper.Abstract, researchLongTextLimit},
		{"journal", paper.Journal, researchTextLimit},
		{"language", paper.Language, researchTextLimit},
		{"year", paper.Year, researchTextLimit},
		{"volume", paper.Volume, researchTextLimit},
		{"issue", paper.Issue, researchTextLimit},
		{"pages", paper.Pages, researchTextLimit},
		{"doi", paper.DOI, researchTextLimit},
		{"citation_source", paper.CitationSource, researchTextLimit},
	}
	if err := checkResearchFields(fields); err != nil {
		return err
	}
	if err := checkResearchStrings("authors", paper.Authors); err != nil {
		return err
	}
	if err := checkResearchStrings("keywords", paper.Keywords); err != nil {
		return err
	}
	if err := checkResearchTagIDs(paper.TagIDs); err != nil {
		return err
	}
	if err := checkResearchStages(paper.Stages); err != nil {
		return err
	}
	return checkResearchHistory(paper.History)
}

func validateResearchPatch(patch domain.ResearchPaperPatch) error {
	if patch.SubmissionCount != nil && *patch.SubmissionCount < 0 {
		return &ResearchValidationError{Reason: "submission_count must not be negative"}
	}
	fields := make([]researchField, 0, 22)
	add := func(name string, value *string, limit int) {
		if value != nil {
			fields = append(fields, researchField{name, *value, limit})
		}
	}
	add("title", patch.Title, researchTextLimit)
	add("file_path", patch.FilePath, researchTextLimit)
	add("next_action", patch.NextAction, researchTextLimit)
	add("notes", patch.Notes, researchLongTextLimit)
	add("research_area", patch.ResearchArea, researchTextLimit)
	add("status", patch.Status, researchTextLimit)
	add("target_journal", patch.TargetJournal, researchTextLimit)
	add("current_journal", patch.CurrentJournal, researchTextLimit)
	add("submission_date", patch.SubmissionDate, researchTextLimit)
	add("manuscript_id", patch.ManuscriptID, researchTextLimit)
	add("target_level", patch.TargetLevel, researchTextLimit)
	add("editor", patch.Editor, researchTextLimit)
	add("deadline", patch.Deadline, researchTextLimit)
	add("abstract", patch.Abstract, researchLongTextLimit)
	add("journal", patch.Journal, researchTextLimit)
	add("language", patch.Language, researchTextLimit)
	add("year", patch.Year, researchTextLimit)
	add("volume", patch.Volume, researchTextLimit)
	add("issue", patch.Issue, researchTextLimit)
	add("pages", patch.Pages, researchTextLimit)
	add("doi", patch.DOI, researchTextLimit)
	add("citation_source", patch.CitationSource, researchTextLimit)
	if err := checkResearchFields(fields); err != nil {
		return err
	}
	if patch.Authors != nil {
		if err := checkResearchStrings("authors", *patch.Authors); err != nil {
			return err
		}
	}
	if patch.Keywords != nil {
		if err := checkResearchStrings("keywords", *patch.Keywords); err != nil {
			return err
		}
	}
	if patch.TagIDs != nil {
		if err := checkResearchTagIDs(*patch.TagIDs); err != nil {
			return err
		}
	}
	if patch.Stages != nil {
		if err := checkResearchStages(*patch.Stages); err != nil {
			return err
		}
	}
	if patch.History != nil {
		if err := checkResearchHistory(*patch.History); err != nil {
			return err
		}
	}
	return nil
}

// IsResearchKind reports whether kind is a valid workspace bucket.
func IsResearchKind(kind string) bool {
	_, ok := researchKinds[kind]
	return ok
}

const researchColumns = `id, kind, position, title, authors_json, keywords_json, file_path,
	next_action, notes, research_area, status, target_journal, stages_json,
	current_journal, submission_date, manuscript_id, submission_count, target_level, editor,
	deadline, history_json, abstract, journal, language, year, volume, issue, pages, doi,
	citations, citation_source, citation_updated_at, last_updated, created_at, updated_at`

func scanResearchPaper(scanner interface {
	Scan(dest ...any) error
}) (domain.ResearchPaper, error) {
	var (
		paper                                              domain.ResearchPaper
		authorsJSON, keywordsJSON, stagesJSON, historyJSON string
		citations                                          sql.NullInt64
		createdAt, updatedAt                               string
	)
	if err := scanner.Scan(
		&paper.ID, &paper.Kind, &paper.Position, &paper.Title, &authorsJSON, &keywordsJSON,
		&paper.FilePath, &paper.NextAction, &paper.Notes, &paper.ResearchArea, &paper.Status,
		&paper.TargetJournal, &stagesJSON, &paper.CurrentJournal,
		&paper.SubmissionDate, &paper.ManuscriptID, &paper.SubmissionCount, &paper.TargetLevel,
		&paper.Editor, &paper.Deadline, &historyJSON, &paper.Abstract, &paper.Journal, &paper.Language,
		&paper.Year, &paper.Volume, &paper.Issue, &paper.Pages, &paper.DOI, &citations,
		&paper.CitationSource, &paper.CitationUpdatedAt, &paper.LastUpdated, &createdAt, &updatedAt,
	); err != nil {
		return domain.ResearchPaper{}, err
	}
	authors, err := decodeStringSlice("authors_json", authorsJSON)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	paper.Authors = authors
	keywords, err := decodeStringSlice("keywords_json", keywordsJSON)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	paper.Keywords = keywords
	// Labels live in their own table, so a scanned paper starts with an empty
	// list rather than nil: an unlabelled paper must answer [] like authors and
	// keywords do, and attachResearchPaperTags only fills rows it finds.
	paper.TagIDs = make([]string, 0)
	stages, err := decodeStages(stagesJSON)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	paper.Stages = stages
	history, err := decodeHistory(historyJSON)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	paper.History = history
	if citations.Valid {
		value := int(citations.Int64)
		paper.Citations = &value
	}
	paper.CreatedAt = parseTime(createdAt)
	paper.UpdatedAt = parseTime(updatedAt)
	return paper, nil
}

// ListResearchPapers returns every paper of a kind ordered by position.
func ListResearchPapers(ctx context.Context, db *sql.DB, profileID, kind string) ([]domain.ResearchPaper, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT "+researchColumns+" FROM research_papers WHERE profile_id = ? AND kind = ? ORDER BY position, created_at",
		profileID, kind)
	if err != nil {
		return nil, fmt.Errorf("list research papers: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ResearchPaper, 0)
	for rows.Next() {
		paper, err := scanResearchPaper(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, paper)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list research papers: %w", err)
	}
	// Labels come from a second query, so the paper cursor goes first instead of
	// holding a connection and a read snapshot across it.
	rows.Close()
	if err := attachResearchPaperTags(ctx, db, items); err != nil {
		return nil, err
	}
	return items, nil
}

// attachResearchPaperTags fills each paper's TagIDs with the labels assigned
// to it, in the order the user assigned them. One query covers the whole page
// instead of one per paper, and it is chunked so the paper ids never outrun
// SQLite's host-parameter ceiling.
func attachResearchPaperTags(ctx context.Context, db *sql.DB, items []domain.ResearchPaper) error {
	for start := 0; start < len(items); start += researchTagQueryChunk {
		chunk := items[start:min(start+researchTagQueryChunk, len(items))]
		placeholders := make([]string, len(chunk))
		args := make([]any, 0, len(chunk))
		for index := range chunk {
			placeholders[index] = "?"
			args = append(args, chunk[index].ID)
		}
		rows, err := db.QueryContext(ctx,
			`SELECT paper_id, tag_id FROM research_paper_tags
			 WHERE paper_id IN (`+strings.Join(placeholders, ",")+") ORDER BY position, tag_id",
			args...)
		if err != nil {
			return fmt.Errorf("list research paper tags: %w", err)
		}
		grouped, err := groupResearchPaperTags(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for index := range chunk {
			if assigned := grouped[chunk[index].ID]; len(assigned) > 0 {
				chunk[index].TagIDs = assigned
			}
		}
	}
	return nil
}

func groupResearchPaperTags(rows *sql.Rows) (map[string][]string, error) {
	grouped := make(map[string][]string)
	for rows.Next() {
		var paperID, tagID string
		if err := rows.Scan(&paperID, &tagID); err != nil {
			return nil, fmt.Errorf("read research paper tags: %w", err)
		}
		grouped[paperID] = append(grouped[paperID], tagID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read research paper tags: %w", err)
	}
	return grouped, nil
}

// GetResearchPaper returns one paper by id.
func GetResearchPaper(ctx context.Context, db *sql.DB, profileID, id string) (domain.ResearchPaper, error) {
	paper, err := scanResearchPaper(db.QueryRowContext(ctx,
		"SELECT "+researchColumns+" FROM research_papers WHERE profile_id = ? AND id = ?", profileID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ResearchPaper{}, ErrNotFound
	}
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("get research paper: %w", err)
	}
	items := []domain.ResearchPaper{paper}
	if err := attachResearchPaperTags(ctx, db, items); err != nil {
		return domain.ResearchPaper{}, err
	}
	return items[0], nil
}

// CreateResearchPaper inserts a new paper at the top of its kind list: the
// row the user just made is the row they are looking for, so it takes
// position MIN-1 instead of MAX+1 and the existing order stays untouched.
// The caller supplies kind and any starting field values through paper.
func CreateResearchPaper(ctx context.Context, db *sql.DB, profileID string, paper domain.ResearchPaper) (domain.ResearchPaper, error) {
	if !IsResearchKind(paper.Kind) {
		return domain.ResearchPaper{}, errors.New("invalid research paper kind")
	}
	if err := validateResearchPaper(paper); err != nil {
		return domain.ResearchPaper{}, err
	}
	now := time.Now().UTC()
	paper.ID = uuid.NewString()
	if paper.LastUpdated == "" {
		paper.LastUpdated = formatTime(now)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("begin research create: %w", err)
	}
	defer tx.Rollback()
	var position int
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MIN(position)-1, 0) FROM research_papers WHERE profile_id = ? AND kind = ?",
		profileID, paper.Kind).Scan(&position); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("compute research position: %w", err)
	}
	paper.Position = position
	if _, err := tx.ExecContext(ctx, `INSERT INTO research_papers (
		id, profile_id, kind, position, title, authors_json, keywords_json, file_path,
		next_action, notes, research_area, status, target_journal, stages_json,
		current_journal, submission_date, manuscript_id, submission_count, target_level, editor,
		deadline, history_json, abstract, journal, language, year, volume, issue, pages, doi,
		citations, citation_source, citation_updated_at, last_updated, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		paper.ID, profileID, paper.Kind, paper.Position, paper.Title, encodeStringSlice(paper.Authors),
		encodeStringSlice(paper.Keywords), paper.FilePath, paper.NextAction, paper.Notes,
		paper.ResearchArea, paper.Status, paper.TargetJournal, encodeStages(paper.Stages),
		paper.CurrentJournal, paper.SubmissionDate, paper.ManuscriptID, paper.SubmissionCount,
		paper.TargetLevel, paper.Editor, paper.Deadline, encodeHistory(paper.History), paper.Abstract,
		paper.Journal, paper.Language, paper.Year, paper.Volume, paper.Issue, paper.Pages, paper.DOI,
		nullableIntValue(paper.Citations), paper.CitationSource, paper.CitationUpdatedAt,
		paper.LastUpdated, formatTime(now), formatTime(now)); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("create research paper: %w", err)
	}
	if err := replaceResearchPaperTags(ctx, tx, paper.ID, paper.TagIDs); err != nil {
		return domain.ResearchPaper{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("commit research create: %w", err)
	}
	return GetResearchPaper(ctx, db, profileID, paper.ID)
}

// UpdateResearchPaper applies a patch and refreshes the last_updated stamp.
func UpdateResearchPaper(ctx context.Context, db *sql.DB, profileID, id string, patch domain.ResearchPaperPatch) (domain.ResearchPaper, error) {
	if err := validateResearchPatch(patch); err != nil {
		return domain.ResearchPaper{}, err
	}
	now := time.Now().UTC()
	sets := []string{"updated_at = ?", "last_updated = ?"}
	args := []any{formatTime(now), formatTime(now)}
	add := func(column string, value any) {
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}
	if patch.Title != nil {
		add("title", *patch.Title)
	}
	if patch.Authors != nil {
		add("authors_json", encodeStringSlice(*patch.Authors))
	}
	if patch.Keywords != nil {
		add("keywords_json", encodeStringSlice(*patch.Keywords))
	}
	if patch.FilePath != nil {
		add("file_path", *patch.FilePath)
	}
	if patch.NextAction != nil {
		add("next_action", *patch.NextAction)
	}
	if patch.Notes != nil {
		add("notes", *patch.Notes)
	}
	if patch.ResearchArea != nil {
		add("research_area", *patch.ResearchArea)
	}
	if patch.Status != nil {
		add("status", *patch.Status)
	}
	if patch.TargetJournal != nil {
		add("target_journal", *patch.TargetJournal)
	}
	if patch.Stages != nil {
		add("stages_json", encodeStages(*patch.Stages))
	}
	if patch.CurrentJournal != nil {
		add("current_journal", *patch.CurrentJournal)
	}
	if patch.SubmissionDate != nil {
		add("submission_date", *patch.SubmissionDate)
	}
	if patch.ManuscriptID != nil {
		add("manuscript_id", *patch.ManuscriptID)
	}
	if patch.SubmissionCount != nil {
		add("submission_count", *patch.SubmissionCount)
	}
	if patch.TargetLevel != nil {
		add("target_level", *patch.TargetLevel)
	}
	if patch.Editor != nil {
		add("editor", *patch.Editor)
	}
	if patch.Deadline != nil {
		add("deadline", *patch.Deadline)
	}
	if patch.History != nil {
		add("history_json", encodeHistory(*patch.History))
	}
	if patch.Abstract != nil {
		add("abstract", *patch.Abstract)
	}
	if patch.Journal != nil {
		add("journal", *patch.Journal)
	}
	if patch.Language != nil {
		add("language", *patch.Language)
	}
	if patch.Year != nil {
		add("year", *patch.Year)
	}
	if patch.Volume != nil {
		add("volume", *patch.Volume)
	}
	if patch.Issue != nil {
		add("issue", *patch.Issue)
	}
	if patch.Pages != nil {
		add("pages", *patch.Pages)
	}
	if patch.DOI != nil {
		add("doi", *patch.DOI)
	}
	if patch.SetCitations {
		add("citations", nullableIntValue(patch.Citations))
		add("citation_updated_at", formatTime(now))
	}
	if patch.CitationSource != nil {
		add("citation_source", *patch.CitationSource)
	}

	args = append(args, profileID, id)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("begin research update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		"UPDATE research_papers SET "+joinComma(sets)+" WHERE profile_id = ? AND id = ?", args...)
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("update research paper: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.ResearchPaper{}, ErrNotFound
	}
	// The labels live beside the row, so they only move when the request said
	// something about them: nil leaves the assignment alone, an empty slice
	// clears it.
	if patch.TagIDs != nil {
		if err := replaceResearchPaperTags(ctx, tx, id, *patch.TagIDs); err != nil {
			return domain.ResearchPaper{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("commit research update: %w", err)
	}
	return GetResearchPaper(ctx, db, profileID, id)
}

// replaceResearchPaperTags rewrites one paper's label assignments to tagIDs,
// keeping the caller's order as the stored position. Repeats collapse because
// the association key is (paper_id, tag_id); unknown tag ids are stored as
// given, so a palette that arrives later still finds the assignment the user
// made.
func replaceResearchPaperTags(ctx context.Context, tx *sql.Tx, paperID string, tagIDs []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM research_paper_tags WHERE paper_id = ?", paperID); err != nil {
		return fmt.Errorf("clear research paper tags: %w", err)
	}
	for position, tagID := range uniqueResearchTagIDs(tagIDs) {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO research_paper_tags (paper_id, tag_id, position) VALUES (?, ?, ?)",
			paperID, tagID, position); err != nil {
			return fmt.Errorf("assign research paper tag: %w", err)
		}
	}
	return nil
}

// DeleteResearchPaper removes one paper and the label assignments that only
// meant something to it; a leftover association would travel through every
// later snapshot of the library.
func DeleteResearchPaper(ctx context.Context, db *sql.DB, profileID, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin research delete: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM research_papers WHERE profile_id = ? AND id = ?", profileID, id)
	if err != nil {
		return fmt.Errorf("delete research paper: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM research_paper_tags WHERE paper_id = ?", id); err != nil {
		return fmt.Errorf("delete research paper tags: %w", err)
	}
	return tx.Commit()
}

// ReorderResearchPapers rewrites the position of every listed paper of a kind
// to match orderedIDs. IDs not belonging to the profile/kind are ignored.
func ReorderResearchPapers(ctx context.Context, db *sql.DB, profileID, kind string, orderedIDs []string) error {
	if !IsResearchKind(kind) {
		return errors.New("invalid research paper kind")
	}
	if len(orderedIDs) > researchReorderLimit {
		return &ResearchValidationError{Reason: fmt.Sprintf("reorder accepts at most %d paper ids", researchReorderLimit)}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin research reorder: %w", err)
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	for position, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx,
			"UPDATE research_papers SET position = ?, updated_at = ? WHERE profile_id = ? AND kind = ? AND id = ?",
			position, now, profileID, kind, id); err != nil {
			return fmt.Errorf("reorder research paper: %w", err)
		}
	}
	return tx.Commit()
}

// MoveResearchPaper changes a paper's kind and appends it to the destination
// list, inheriting selected fields while preserving the stages and history
// trees each page renders by kind. It returns the moved paper.
func MoveResearchPaper(ctx context.Context, db *sql.DB, profileID, id, toKind string) (domain.ResearchPaper, error) {
	if !IsResearchKind(toKind) {
		return domain.ResearchPaper{}, errors.New("invalid research paper kind")
	}
	current, err := GetResearchPaper(ctx, db, profileID, id)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	if current.Kind == toKind {
		return current, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("begin research move: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var position int
	// Same rule as create: the paper the user just moved is the one they are
	// looking at, so it lands on top of the destination list, not buried at
	// the bottom of it.
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MIN(position)-1, 0) FROM research_papers WHERE profile_id = ? AND kind = ?",
		profileID, toKind).Scan(&position); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("compute research move position: %w", err)
	}
	// research -> submitted inherits title/authors/file path.
	// submitted -> published additionally maps current journal to journal.
	journal := current.Journal
	if toKind == domain.ResearchKindPublished && current.CurrentJournal != "" {
		journal = current.CurrentJournal
	}
	if _, err := tx.ExecContext(ctx, `UPDATE research_papers SET kind = ?, position = ?, journal = ?,
		updated_at = ?, last_updated = ?
		WHERE profile_id = ? AND id = ?`,
		toKind, position, journal, formatTime(now), formatTime(now), profileID, id); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("move research paper: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("commit research move: %w", err)
	}
	return GetResearchPaper(ctx, db, profileID, id)
}

// --- JSON helpers ---

func encodeStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	body, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func decodeStringSlice(column, raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("decode research %s: %w", column, err)
	}
	return values, nil
}

func encodeStages(stages []domain.ResearchStage) string {
	if len(stages) == 0 {
		return "[]"
	}
	body, err := json.Marshal(stages)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func decodeStages(raw string) ([]domain.ResearchStage, error) {
	if raw == "" {
		return []domain.ResearchStage{}, nil
	}
	var stages []domain.ResearchStage
	if err := json.Unmarshal([]byte(raw), &stages); err != nil {
		return nil, fmt.Errorf("decode research stages_json: %w", err)
	}
	return stages, nil
}

func encodeHistory(history []domain.SubmissionRecord) string {
	if len(history) == 0 {
		return "[]"
	}
	body, err := json.Marshal(history)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func decodeHistory(raw string) ([]domain.SubmissionRecord, error) {
	if raw == "" {
		return []domain.SubmissionRecord{}, nil
	}
	var history []domain.SubmissionRecord
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return nil, fmt.Errorf("decode research history_json: %w", err)
	}
	return history, nil
}

func nullableIntValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func joinComma(parts []string) string {
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}
	return result
}
