package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

func UpdateFolder(ctx context.Context, db *sql.DB, profileID, folderID string, setParent bool, parentID *string, name *string, position *int) (domain.Folder, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Folder{}, fmt.Errorf("begin folder update: %w", err)
	}
	defer tx.Rollback()

	var folderExists bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM folders WHERE id = ? AND profile_id = ?)", folderID, profileID,
	).Scan(&folderExists); err != nil {
		return domain.Folder{}, fmt.Errorf("find folder: %w", err)
	}
	if !folderExists {
		return domain.Folder{}, ErrNotFound
	}

	if setParent && parentID != nil {
		if *parentID == folderID {
			return domain.Folder{}, errors.New("a folder cannot contain itself")
		}
		var parentExists bool
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM folders WHERE id = ? AND profile_id = ?)", *parentID, profileID,
		).Scan(&parentExists); err != nil {
			return domain.Folder{}, fmt.Errorf("find parent folder: %w", err)
		}
		if !parentExists {
			return domain.Folder{}, ErrNotFound
		}
		var cycle bool
		if err := tx.QueryRowContext(ctx, `
			WITH RECURSIVE descendants(id) AS (
				SELECT id FROM folders WHERE parent_id = ? AND profile_id = ?
				UNION SELECT f.id FROM folders f JOIN descendants d ON f.parent_id = d.id
				WHERE f.profile_id = ?
			)
			SELECT EXISTS(SELECT 1 FROM descendants WHERE id = ?)`, folderID, profileID, profileID, *parentID).Scan(&cycle); err != nil {
			return domain.Folder{}, fmt.Errorf("check folder cycle: %w", err)
		}
		if cycle {
			return domain.Folder{}, errors.New("folder nesting would create a cycle")
		}
	}

	var appendPosition int
	if setParent && position == nil {
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(position) + 1, 0) FROM folders
			WHERE profile_id = ? AND ((parent_id IS NULL AND ? IS NULL) OR parent_id = ?) AND id != ?`,
			profileID, nullable(parentID), nullable(parentID), folderID,
		).Scan(&appendPosition); err != nil {
			return domain.Folder{}, fmt.Errorf("calculate folder position: %w", err)
		}
	}

	now := time.Now().UTC()
	sets := []string{"updated_at = ?"}
	args := []any{formatTime(now)}
	if setParent {
		sets = append(sets, "parent_id = ?")
		args = append(args, nullable(parentID))
	}
	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *name)
	}
	if position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *position)
	} else if setParent {
		sets = append(sets, "position = ?")
		args = append(args, appendPosition)
	}
	result, err := tx.ExecContext(ctx, "UPDATE folders SET "+strings.Join(sets, ", ")+
		" WHERE id = ? AND profile_id = ?", append(args, folderID, profileID)...)
	if err != nil {
		return domain.Folder{}, fmt.Errorf("update folder: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return domain.Folder{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return domain.Folder{}, fmt.Errorf("commit folder update: %w", err)
	}

	items, err := ListFolders(ctx, db, profileID)
	if err != nil {
		return domain.Folder{}, err
	}
	for _, item := range items {
		if item.ID == folderID {
			return item, nil
		}
	}
	return domain.Folder{}, ErrNotFound
}

func DeleteFolder(ctx context.Context, db *sql.DB, profileID, folderID string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM folders WHERE id = ? AND profile_id = ?", folderID, profileID)
	if err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func ListTags(ctx context.Context, db *sql.DB, profileID string) ([]domain.Tag, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, color, position, created_at FROM tags
		WHERE profile_id = ? ORDER BY position, name COLLATE NOCASE`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Tag, 0)
	for rows.Next() {
		var item domain.Tag
		var color sql.NullString
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Name, &color, &item.Position, &createdAt); err != nil {
			return nil, err
		}
		item.Color = stringPointer(color)
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func CreateTag(ctx context.Context, db *sql.DB, profileID, name string, color *string) (domain.Tag, error) {
	item := domain.Tag{ID: uuid.NewString(), Name: strings.TrimSpace(name), Color: color, CreatedAt: time.Now().UTC()}
	if item.Name == "" {
		return domain.Tag{}, errors.New("tag name is required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO tags (id, profile_id, name, color, created_at) VALUES (?, ?, ?, ?, ?)`,
		item.ID, profileID, item.Name, nullable(color), formatTime(item.CreatedAt))
	if err != nil {
		return domain.Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return item, nil
}

func DeleteTag(ctx context.Context, db *sql.DB, profileID, tagID string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM tags WHERE id = ? AND profile_id = ?", tagID, profileID)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func SetEntryTags(ctx context.Context, db *sql.DB, profileID, entryID string, tagIDs []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries e JOIN subscriptions s ON s.feed_id = e.feed_id
		WHERE e.id = ? AND s.profile_id = ?)`, entryID, profileID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	uniqueTagIDs := make([]string, 0, len(tagIDs))
	seen := make(map[string]struct{}, len(tagIDs))
	for _, tagID := range tagIDs {
		if _, duplicate := seen[tagID]; duplicate {
			continue
		}
		var owned bool
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM tags WHERE id = ? AND profile_id = ?)", tagID, profileID,
		).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return ErrNotFound
		}
		seen[tagID] = struct{}{}
		uniqueTagIDs = append(uniqueTagIDs, tagID)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entry_tags WHERE entry_id = ?", entryID); err != nil {
		return err
	}
	for _, tagID := range uniqueTagIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO entry_tags (entry_id, tag_id)
			SELECT ?, id FROM tags WHERE id = ? AND profile_id = ?`, entryID, tagID, profileID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func AddEntryTagsByName(ctx context.Context, db *sql.DB, profileID, entryID string, names []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries e JOIN subscriptions s ON s.feed_id = e.feed_id
		WHERE e.id = ? AND s.profile_id = ?)`, entryID, profileID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	seen := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		key := strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		var tagID string
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM tags WHERE profile_id = ? AND name = ? COLLATE NOCASE LIMIT 1`,
			profileID, name,
		).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID = uuid.NewString()
			if _, err = tx.ExecContext(ctx, `
				INSERT INTO tags (id, profile_id, name, created_at) VALUES (?, ?, ?, ?)`,
				tagID, profileID, name, formatTime(time.Now().UTC()),
			); err != nil {
				return fmt.Errorf("create generated tag: %w", err)
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO entry_tags (entry_id, tag_id) VALUES (?, ?)`, entryID, tagID); err != nil {
			return fmt.Errorf("assign generated tag: %w", err)
		}
	}
	return tx.Commit()
}

func ListRules(ctx context.Context, db *sql.DB, profileID string) ([]domain.Rule, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, enabled, priority, conditions_json, actions_json, created_at, updated_at
		FROM rules WHERE profile_id = ? ORDER BY priority, id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Rule, 0)
	for rows.Next() {
		var item domain.Rule
		var enabled int
		var conditions, actions, createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Name, &enabled, &item.Priority, &conditions, &actions, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		item.ConditionsJSON = json.RawMessage(conditions)
		item.ActionsJSON = json.RawMessage(actions)
		item.CreatedAt, item.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func CreateRule(ctx context.Context, db *sql.DB, profileID, name string, enabled bool, priority int, conditions, actions json.RawMessage) (domain.Rule, error) {
	if !json.Valid(conditions) || !json.Valid(actions) {
		return domain.Rule{}, errors.New("rule conditions and actions must be valid JSON")
	}
	now := time.Now().UTC()
	item := domain.Rule{ID: uuid.NewString(), Name: strings.TrimSpace(name), Enabled: enabled, Priority: priority, ConditionsJSON: conditions, ActionsJSON: actions, CreatedAt: now, UpdatedAt: now}
	if item.Name == "" {
		return domain.Rule{}, errors.New("rule name is required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO rules (id, profile_id, name, enabled, priority, conditions_json, actions_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, profileID, item.Name, boolInt(enabled), priority, string(conditions), string(actions), formatTime(now), formatTime(now))
	if err != nil {
		return domain.Rule{}, fmt.Errorf("create rule: %w", err)
	}
	return item, nil
}

func DeleteRule(ctx context.Context, db *sql.DB, profileID, ruleID string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM rules WHERE id = ? AND profile_id = ?", ruleID, profileID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type ruleConditions struct {
	TitleContains  string `json:"title_contains"`
	AuthorContains string `json:"author_contains"`
	FeedID         string `json:"feed_id"`
}

type ruleActions struct {
	MarkRead  bool     `json:"mark_read"`
	Star      bool     `json:"star"`
	ReadLater bool     `json:"read_later"`
	AddTagIDs []string `json:"add_tag_ids"`
}

// ApplyRulesToFeed evaluates enabled rules against the feed's entries. When
// entryIDs is non-nil only those entries are considered; an empty non-nil
// slice short-circuits, so refresh cycles without new entries cost nothing.
func ApplyRulesToFeed(ctx context.Context, db *sql.DB, profileID, feedID string, entryIDs []string) error {
	rules, err := ListRules(ctx, db, profileID)
	if err != nil || len(rules) == 0 {
		return err
	}
	if entryIDs != nil && len(entryIDs) == 0 {
		return nil
	}
	query := "SELECT id, title, COALESCE(author, '') FROM entries WHERE feed_id = ?"
	args := []any{feedID}
	if entryIDs != nil {
		placeholders := make([]string, len(entryIDs))
		for index, id := range entryIDs {
			placeholders[index] = "?"
			args = append(args, id)
		}
		query += " AND id IN (" + strings.Join(placeholders, ",") + ")"
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type candidate struct{ id, title, author string }
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.title, &item.author); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		var conditions ruleConditions
		var actions ruleActions
		if json.Unmarshal(rule.ConditionsJSON, &conditions) != nil || json.Unmarshal(rule.ActionsJSON, &actions) != nil {
			continue
		}
		for _, item := range candidates {
			if conditions.FeedID != "" && conditions.FeedID != feedID ||
				conditions.TitleContains != "" && !strings.Contains(strings.ToLower(item.title), strings.ToLower(conditions.TitleContains)) ||
				conditions.AuthorContains != "" && !strings.Contains(strings.ToLower(item.author), strings.ToLower(conditions.AuthorContains)) {
				continue
			}
			if actions.MarkRead || actions.Star || actions.ReadLater {
				// updated_at only moves when a flag actually changes; bumping it
				// unconditionally made every rule-matched entry look locally
				// modified, which breaks sync conflict detection and re-pushes
				// the whole feed on every sync.
				_, err := tx.ExecContext(ctx, `
					INSERT INTO entry_states (profile_id, entry_id, is_read, is_starred, is_read_later, read_at, updated_at)
					VALUES (?, ?, ?, ?, ?, CASE WHEN ? = 1 THEN ? ELSE NULL END, ?)
					ON CONFLICT(profile_id, entry_id) DO UPDATE SET
						is_read = MAX(entry_states.is_read, excluded.is_read),
						is_starred = MAX(entry_states.is_starred, excluded.is_starred),
						is_read_later = MAX(entry_states.is_read_later, excluded.is_read_later),
						read_at = COALESCE(entry_states.read_at, excluded.read_at),
						updated_at = CASE
							WHEN entry_states.is_read <> MAX(entry_states.is_read, excluded.is_read)
								OR entry_states.is_starred <> MAX(entry_states.is_starred, excluded.is_starred)
								OR entry_states.is_read_later <> MAX(entry_states.is_read_later, excluded.is_read_later)
							THEN excluded.updated_at
							ELSE entry_states.updated_at
						END`,
					profileID, item.id, boolInt(actions.MarkRead), boolInt(actions.Star), boolInt(actions.ReadLater), boolInt(actions.MarkRead), now, now)
				if err != nil {
					return err
				}
			}
			for _, tagID := range actions.AddTagIDs {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_tags (entry_id, tag_id)
					SELECT ?, id FROM tags WHERE id = ? AND profile_id = ?`, item.id, tagID, profileID); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

func ListSavedFilters(ctx context.Context, db *sql.DB, profileID string) ([]domain.SavedFilter, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, query_json, position, created_at, updated_at
		FROM saved_filters WHERE profile_id = ? ORDER BY position, name COLLATE NOCASE`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.SavedFilter, 0)
	for rows.Next() {
		var item domain.SavedFilter
		var query, createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Name, &query, &item.Position, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.QueryJSON = json.RawMessage(query)
		item.CreatedAt, item.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func CreateSavedFilter(ctx context.Context, db *sql.DB, profileID, name string, query json.RawMessage) (domain.SavedFilter, error) {
	if !json.Valid(query) {
		return domain.SavedFilter{}, errors.New("saved filter query must be valid JSON")
	}
	now := time.Now().UTC()
	item := domain.SavedFilter{ID: uuid.NewString(), Name: strings.TrimSpace(name), QueryJSON: query, CreatedAt: now, UpdatedAt: now}
	if item.Name == "" {
		return domain.SavedFilter{}, errors.New("saved filter name is required")
	}
	_, err := db.ExecContext(ctx, `INSERT INTO saved_filters (id, profile_id, name, query_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, item.ID, profileID, item.Name, string(query), formatTime(now), formatTime(now))
	return item, err
}

func DeleteSavedFilter(ctx context.Context, db *sql.DB, profileID, filterID string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM saved_filters WHERE id = ? AND profile_id = ?", filterID, profileID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
