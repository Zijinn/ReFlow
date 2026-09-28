package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

const maxAnnotationsPerEntry = 500

var annotationStyles = map[string]struct{}{"highlight": {}, "underline": {}, "wavy": {}}

// AnnotationValidationError reports a client-fixable annotation problem so the
// API layer can answer 400 instead of 500.
type AnnotationValidationError struct{ Reason string }

func (e *AnnotationValidationError) Error() string { return e.Reason }

func ListEntryAnnotations(ctx context.Context, db *sql.DB, profileID, entryID string) ([]domain.EntryAnnotation, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, entry_id, style, quote, prefix, suffix, note, created_at, updated_at
		FROM entry_annotations
		WHERE profile_id = ? AND entry_id = ?
		ORDER BY created_at, id`, profileID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	annotations := make([]domain.EntryAnnotation, 0)
	for rows.Next() {
		var annotation domain.EntryAnnotation
		var createdAt, updatedAt string
		if err := rows.Scan(&annotation.ID, &annotation.EntryID, &annotation.Style, &annotation.Quote,
			&annotation.Prefix, &annotation.Suffix, &annotation.Note, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		annotation.CreatedAt = parseTime(createdAt)
		annotation.UpdatedAt = parseTime(updatedAt)
		annotations = append(annotations, annotation)
	}
	return annotations, rows.Err()
}

func CreateEntryAnnotation(ctx context.Context, db *sql.DB, profileID, entryID string, annotation domain.EntryAnnotation) (domain.EntryAnnotation, error) {
	if _, valid := annotationStyles[annotation.Style]; !valid {
		return domain.EntryAnnotation{}, &AnnotationValidationError{Reason: fmt.Sprintf("unsupported annotation style %q", annotation.Style)}
	}
	if strings.TrimSpace(annotation.Quote) == "" {
		return domain.EntryAnnotation{}, &AnnotationValidationError{Reason: "annotation quote must not be empty"}
	}
	var entryExists int
	if err := db.QueryRowContext(ctx, `
		SELECT 1 FROM entries e JOIN subscriptions s ON s.feed_id = e.feed_id
		WHERE e.id = ? AND s.profile_id = ?`, entryID, profileID).Scan(&entryExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EntryAnnotation{}, ErrNotFound
		}
		return domain.EntryAnnotation{}, err
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entry_annotations WHERE profile_id = ? AND entry_id = ?", profileID, entryID).Scan(&count); err != nil {
		return domain.EntryAnnotation{}, err
	}
	if count >= maxAnnotationsPerEntry {
		return domain.EntryAnnotation{}, &AnnotationValidationError{Reason: fmt.Sprintf("annotation limit of %d per entry reached", maxAnnotationsPerEntry)}
	}
	now := time.Now().UTC()
	annotation.ID = uuid.NewString()
	annotation.EntryID = entryID
	annotation.CreatedAt = now
	annotation.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO entry_annotations (id, profile_id, entry_id, style, quote, prefix, suffix, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		annotation.ID, profileID, entryID, annotation.Style, annotation.Quote,
		annotation.Prefix, annotation.Suffix, annotation.Note, formatTime(now), formatTime(now))
	if err != nil {
		return domain.EntryAnnotation{}, err
	}
	return annotation, nil
}

func UpdateEntryAnnotation(ctx context.Context, db *sql.DB, profileID, entryID, annotationID string, patch domain.EntryAnnotationPatch) (domain.EntryAnnotation, error) {
	if patch.Style == nil && patch.Note == nil {
		return domain.EntryAnnotation{}, &AnnotationValidationError{Reason: "annotation patch must change style or note"}
	}
	if patch.Style != nil {
		if _, valid := annotationStyles[*patch.Style]; !valid {
			return domain.EntryAnnotation{}, &AnnotationValidationError{Reason: fmt.Sprintf("unsupported annotation style %q", *patch.Style)}
		}
	}
	sets := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if patch.Style != nil {
		sets = append(sets, "style = ?")
		args = append(args, *patch.Style)
	}
	if patch.Note != nil {
		sets = append(sets, "note = ?")
		args = append(args, *patch.Note)
	}
	now := time.Now().UTC()
	sets = append(sets, "updated_at = ?")
	args = append(args, formatTime(now), profileID, entryID, annotationID)
	result, err := db.ExecContext(ctx, "UPDATE entry_annotations SET "+strings.Join(sets, ", ")+" WHERE profile_id = ? AND entry_id = ? AND id = ?", args...)
	if err != nil {
		return domain.EntryAnnotation{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.EntryAnnotation{}, err
	}
	if affected == 0 {
		return domain.EntryAnnotation{}, ErrNotFound
	}
	return getEntryAnnotation(ctx, db, profileID, entryID, annotationID)
}

func DeleteEntryAnnotation(ctx context.Context, db *sql.DB, profileID, entryID, annotationID string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM entry_annotations WHERE profile_id = ? AND entry_id = ? AND id = ?", profileID, entryID, annotationID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func getEntryAnnotation(ctx context.Context, db *sql.DB, profileID, entryID, annotationID string) (domain.EntryAnnotation, error) {
	var annotation domain.EntryAnnotation
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx, `
		SELECT id, entry_id, style, quote, prefix, suffix, note, created_at, updated_at
		FROM entry_annotations WHERE profile_id = ? AND entry_id = ? AND id = ?`,
		profileID, entryID, annotationID).Scan(&annotation.ID, &annotation.EntryID, &annotation.Style,
		&annotation.Quote, &annotation.Prefix, &annotation.Suffix, &annotation.Note, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EntryAnnotation{}, ErrNotFound
	}
	if err != nil {
		return domain.EntryAnnotation{}, err
	}
	annotation.CreatedAt = parseTime(createdAt)
	annotation.UpdatedAt = parseTime(updatedAt)
	return annotation, nil
}
