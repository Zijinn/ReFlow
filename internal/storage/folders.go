package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

func ListFolders(ctx context.Context, db *sql.DB, profileID string) ([]domain.Folder, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, profile_id, parent_id, name, position, created_at, updated_at
		FROM folders WHERE profile_id = ? ORDER BY position, name COLLATE NOCASE, id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Folder, 0)
	for rows.Next() {
		var item domain.Folder
		var parentID sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.ProfileID, &parentID, &item.Name, &item.Position, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan folder: %w", err)
		}
		item.ParentID = stringPointer(parentID)
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func EnsureFolder(ctx context.Context, db *sql.DB, profileID string, parentID *string, name string) (domain.Folder, error) {
	var item domain.Folder
	var storedParent sql.NullString
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx, `
		SELECT id, profile_id, parent_id, name, position, created_at, updated_at
		FROM folders WHERE profile_id = ? AND name = ?
			AND ((parent_id IS NULL AND ? IS NULL) OR parent_id = ?)
		LIMIT 1`, profileID, name, nullable(parentID), nullable(parentID)).Scan(
		&item.ID, &item.ProfileID, &storedParent, &item.Name, &item.Position, &createdAt, &updatedAt,
	)
	if err == nil {
		item.ParentID = stringPointer(storedParent)
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		return item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Folder{}, fmt.Errorf("find folder: %w", err)
	}

	now := time.Now().UTC()
	item = domain.Folder{
		ID:        uuid.NewString(),
		ProfileID: profileID,
		ParentID:  parentID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO folders (id, profile_id, parent_id, name, position, created_at, updated_at)
		VALUES (?, ?, ?, ?,
			COALESCE((SELECT MAX(position) + 1 FROM folders
				WHERE profile_id = ? AND ((parent_id IS NULL AND ? IS NULL) OR parent_id = ?)), 0),
			?, ?)`,
		item.ID, profileID, nullable(parentID), name, profileID, nullable(parentID), nullable(parentID), formatTime(now), formatTime(now))
	if err != nil {
		return domain.Folder{}, fmt.Errorf("create folder: %w", err)
	}
	return item, nil
}
