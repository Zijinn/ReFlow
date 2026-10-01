package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Zijinn/ReFlow/internal/domain"
)

// researchTagNameLimit is short on purpose: a tag renders as a pill inside a
// fixed table column, so a sentence-long label would wrap the row tall.
const researchTagNameLimit = 40

// ErrDuplicateResearchTag reports a name clash inside one profile so the API
// layer can answer 409 instead of leaking the UNIQUE constraint as a 500.
var ErrDuplicateResearchTag = errors.New("duplicate research tag name")

// ResearchTagValidationError reports a client-fixable tag problem so the API
// layer can answer 400 instead of 500.
type ResearchTagValidationError struct{ Reason string }

func (e *ResearchTagValidationError) Error() string { return e.Reason }

func validateResearchTagName(name string) error {
	if name == "" {
		return &ResearchTagValidationError{Reason: "tag name must not be empty"}
	}
	if utf8.RuneCountInString(name) > researchTagNameLimit {
		return &ResearchTagValidationError{Reason: fmt.Sprintf("tag name exceeds %d characters", researchTagNameLimit)}
	}
	return nil
}

// researchTagNameTaken reports whether another tag of the profile already
// uses the name; ignoreID excludes the tag being renamed from the comparison.
func researchTagNameTaken(ctx context.Context, querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, profileID, name, ignoreID string,
) (bool, error) {
	var count int
	if err := querier.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM research_tags WHERE profile_id = ? AND name = ? AND id <> ?",
		profileID, name, ignoreID).Scan(&count); err != nil {
		return false, fmt.Errorf("check research tag name: %w", err)
	}
	return count > 0, nil
}

func scanResearchTag(scanner interface {
	Scan(dest ...any) error
}) (domain.ResearchTag, error) {
	var tag domain.ResearchTag
	if err := scanner.Scan(&tag.ID, &tag.Name, &tag.Position); err != nil {
		return domain.ResearchTag{}, err
	}
	return tag, nil
}

const researchTagColumns = `id, name, position`

// ListResearchTags returns a profile's palette in priority order.
func ListResearchTags(ctx context.Context, db *sql.DB, profileID string) ([]domain.ResearchTag, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT "+researchTagColumns+" FROM research_tags WHERE profile_id = ? ORDER BY position",
		profileID)
	if err != nil {
		return nil, fmt.Errorf("list research tags: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ResearchTag, 0)
	for rows.Next() {
		tag, err := scanResearchTag(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, tag)
	}
	return items, rows.Err()
}

// CreateResearchTag appends a label to the end of the palette.
func CreateResearchTag(ctx context.Context, db *sql.DB, profileID, name string) (domain.ResearchTag, error) {
	name = strings.TrimSpace(name)
	if err := validateResearchTagName(name); err != nil {
		return domain.ResearchTag{}, err
	}
	now := time.Now().UTC()
	tag := domain.ResearchTag{ID: uuid.NewString(), Name: name}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ResearchTag{}, fmt.Errorf("begin research tag create: %w", err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(position)+1, 0) FROM research_tags WHERE profile_id = ?",
		profileID).Scan(&tag.Position); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("compute research tag position: %w", err)
	}
	if exists, err := researchTagNameTaken(ctx, tx, profileID, name, ""); err != nil {
		return domain.ResearchTag{}, err
	} else if exists {
		return domain.ResearchTag{}, ErrDuplicateResearchTag
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO research_tags (id, profile_id, name, position, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		tag.ID, profileID, tag.Name, tag.Position, formatTime(now), formatTime(now)); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("create research tag: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("commit research tag create: %w", err)
	}
	return tag, nil
}

// RenameResearchTag changes a label; papers keep referencing it by id, so no
// paper row moves.
func RenameResearchTag(ctx context.Context, db *sql.DB, profileID, id, name string) (domain.ResearchTag, error) {
	name = strings.TrimSpace(name)
	if err := validateResearchTagName(name); err != nil {
		return domain.ResearchTag{}, err
	}
	// Existence first so an unknown id always answers 404, even when the name
	// it asked for happens to be taken by another tag.
	if _, err := GetResearchTag(ctx, db, profileID, id); err != nil {
		return domain.ResearchTag{}, err
	}
	if exists, err := researchTagNameTaken(ctx, db, profileID, name, id); err != nil {
		return domain.ResearchTag{}, err
	} else if exists {
		return domain.ResearchTag{}, ErrDuplicateResearchTag
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE research_tags SET name = ?, updated_at = ? WHERE profile_id = ? AND id = ?",
		name, formatTime(time.Now().UTC()), profileID, id); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("rename research tag: %w", err)
	}
	return GetResearchTag(ctx, db, profileID, id)
}

// GetResearchTag returns one tag by id.
func GetResearchTag(ctx context.Context, db *sql.DB, profileID, id string) (domain.ResearchTag, error) {
	tag, err := scanResearchTag(db.QueryRowContext(ctx,
		"SELECT "+researchTagColumns+" FROM research_tags WHERE profile_id = ? AND id = ?", profileID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ResearchTag{}, ErrNotFound
	}
	if err != nil {
		return domain.ResearchTag{}, fmt.Errorf("get research tag: %w", err)
	}
	return tag, nil
}

// DeleteResearchTag removes a label and untags every paper wearing it; an
// orphaned association would render as a pill nobody can manage.
func DeleteResearchTag(ctx context.Context, db *sql.DB, profileID, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin research tag delete: %w", err)
	}
	defer tx.Rollback()
	// The label goes first: a delete that matched no row of this profile's
	// palette must leave every paper's assignments untouched, and the deferred
	// rollback undoes it.
	result, err := tx.ExecContext(ctx,
		"DELETE FROM research_tags WHERE profile_id = ? AND id = ?", profileID, id)
	if err != nil {
		return fmt.Errorf("delete research tag: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete research tag rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM research_paper_tags WHERE tag_id = ?", id); err != nil {
		return fmt.Errorf("untag research papers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit research tag delete: %w", err)
	}
	return nil
}

// ReorderResearchTags rewrites the position of every listed tag to match
// orderedIDs, which is how the settings palette drags into a priority order.
// Ids not belonging to the profile are ignored.
func ReorderResearchTags(ctx context.Context, db *sql.DB, profileID string, orderedIDs []string) error {
	if len(orderedIDs) > researchReorderLimit {
		return &ResearchTagValidationError{Reason: fmt.Sprintf("reorder accepts at most %d tag ids", researchReorderLimit)}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin research tag reorder: %w", err)
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	for position, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx,
			"UPDATE research_tags SET position = ?, updated_at = ? WHERE profile_id = ? AND id = ?",
			position, now, profileID, id); err != nil {
			return fmt.Errorf("reorder research tag: %w", err)
		}
	}
	return tx.Commit()
}
