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

// researchTagColorNames are the tints the workbench stylesheet ships as
// wb-badge--<name> / wb-dot--<name>. A name outside the list would store a
// colour no chip can render, so the server rejects it instead of silently
// keeping a dead value; "" is handled separately as "no explicit colour".
var researchTagColorNames = []string{
	"amber", "blue", "gray", "green", "orange", "red", "teal", "violet",
}

var researchTagColors = func() map[string]struct{} {
	colors := make(map[string]struct{}, len(researchTagColorNames))
	for _, color := range researchTagColorNames {
		colors[color] = struct{}{}
	}
	return colors
}()

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

// validateResearchTagColor normalises a colour choice and rejects a name the
// palette does not ship. The empty string passes through: it is the documented
// "no colour picked" value the client reads to fall back to index tinting.
func validateResearchTagColor(color string) (string, error) {
	trimmed := strings.TrimSpace(color)
	if trimmed == "" {
		return "", nil
	}
	if _, ok := researchTagColors[trimmed]; !ok {
		return "", &ResearchTagValidationError{
			Reason: fmt.Sprintf("tag color must be empty or one of %s", strings.Join(researchTagColorNames, ", ")),
		}
	}
	return trimmed, nil
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
	if err := scanner.Scan(&tag.ID, &tag.Name, &tag.Position, &tag.Color); err != nil {
		return domain.ResearchTag{}, err
	}
	return tag, nil
}

const researchTagColumns = `id, name, position, color`

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

// CreateResearchTag appends a label to the end of the palette. An empty color
// stores "no colour chosen", which is what lets the client keep tinting by
// index for palettes the user never recoloured.
func CreateResearchTag(ctx context.Context, db *sql.DB, profileID, name, color string) (domain.ResearchTag, error) {
	name = strings.TrimSpace(name)
	if err := validateResearchTagName(name); err != nil {
		return domain.ResearchTag{}, err
	}
	color, err := validateResearchTagColor(color)
	if err != nil {
		return domain.ResearchTag{}, err
	}
	now := time.Now().UTC()
	tag := domain.ResearchTag{ID: uuid.NewString(), Name: name, Color: color}
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
		`INSERT INTO research_tags (id, profile_id, name, position, color, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tag.ID, profileID, tag.Name, tag.Position, tag.Color, formatTime(now), formatTime(now)); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("create research tag: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("commit research tag create: %w", err)
	}
	return tag, nil
}

// UpdateResearchTag applies a partial patch: a nil field is left alone, so the
// settings pane renames, recolours, or does both in one request. Papers keep
// referencing the tag by id, so neither change rewrites a paper row.
func UpdateResearchTag(ctx context.Context, db *sql.DB, profileID, id string, name, color *string) (domain.ResearchTag, error) {
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if err := validateResearchTagName(trimmed); err != nil {
			return domain.ResearchTag{}, err
		}
		name = &trimmed
	}
	if color != nil {
		chosen, err := validateResearchTagColor(*color)
		if err != nil {
			return domain.ResearchTag{}, err
		}
		color = &chosen
	}
	// Existence first so an unknown id always answers 404, even when the name
	// it asked for happens to be taken by another tag.
	if _, err := GetResearchTag(ctx, db, profileID, id); err != nil {
		return domain.ResearchTag{}, err
	}
	if name != nil {
		if exists, err := researchTagNameTaken(ctx, db, profileID, *name, id); err != nil {
			return domain.ResearchTag{}, err
		} else if exists {
			return domain.ResearchTag{}, ErrDuplicateResearchTag
		}
	}
	sets := []string{"updated_at = ?"}
	args := []any{formatTime(time.Now().UTC())}
	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *name)
	}
	if color != nil {
		sets = append(sets, "color = ?")
		args = append(args, *color)
	}
	if len(sets) == 1 {
		// A patch that mentions nothing is not a write: the row keeps its
		// updated_at instead of being stamped by an empty request.
		return GetResearchTag(ctx, db, profileID, id)
	}
	args = append(args, profileID, id)
	if _, err := db.ExecContext(ctx,
		"UPDATE research_tags SET "+strings.Join(sets, ", ")+
			" WHERE profile_id = ? AND id = ?", args...); err != nil {
		return domain.ResearchTag{}, fmt.Errorf("update research tag: %w", err)
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
