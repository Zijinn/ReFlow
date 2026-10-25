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

const taskScopeQuickTask = "quick_task"

const (
	quickTaskTextLimit = 500
	quickTaskNoteLimit = 2000
)

func scanQuickTask(row interface {
	Scan(dest ...any) error
}) (domain.QuickTask, error) {
	var task domain.QuickTask
	var paperID sql.NullString
	if err := row.Scan(&task.ID, &task.Title, &task.Notes, &task.StartDate, &task.DueDate,
		&paperID, &task.CompletedAt, &task.Position); err != nil {
		return domain.QuickTask{}, err
	}
	if paperID.Valid {
		task.PaperID = &paperID.String
	}
	task.Completed = task.CompletedAt != ""
	return task, nil
}

const quickTaskColumns = `id, title, notes, start_date, due_date, paper_id, completed_at, position`

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func requireQuickTaskPaper(ctx context.Context, q rowQueryer, profileID, paperID string) error {
	var exists bool
	if err := q.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM research_papers WHERE profile_id = ? AND id = ?)",
		profileID, paperID).Scan(&exists); err != nil {
		return fmt.Errorf("check quick task paper: %w", err)
	}
	if !exists {
		return taskValidationf(taskScopeQuickTask, "paper_id does not exist")
	}
	return nil
}

// ListQuickTasks returns the flat 即时任务 list. Open tasks come first, then
// completed ones, and each group keeps the order the owner chose.
func ListQuickTasks(ctx context.Context, db *sql.DB, profileID string) ([]domain.QuickTask, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+quickTaskColumns+`
		FROM quick_tasks WHERE profile_id = ?
		ORDER BY CASE WHEN completed_at = '' THEN 0 ELSE 1 END, position, created_at`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list quick tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.QuickTask, 0)
	for rows.Next() {
		task, err := scanQuickTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan quick task: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func GetQuickTask(ctx context.Context, db *sql.DB, profileID, id string) (domain.QuickTask, error) {
	task, err := scanQuickTask(db.QueryRowContext(ctx,
		`SELECT `+quickTaskColumns+` FROM quick_tasks WHERE profile_id = ? AND id = ?`, profileID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.QuickTask{}, ErrNotFound
	}
	if err != nil {
		return domain.QuickTask{}, fmt.Errorf("get quick task: %w", err)
	}
	return task, nil
}

func nextQuickTaskPosition(ctx context.Context, tx *sql.Tx, profileID string) (int, error) {
	var min sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MIN(position) FROM quick_tasks WHERE profile_id = ?`, profileID).Scan(&min); err != nil {
		return 0, fmt.Errorf("quick task position: %w", err)
	}
	if !min.Valid {
		return 0, nil
	}
	return int(min.Int64) - 1, nil
}

// CreateQuickTask adds one flat task. Position is MIN-1 so a new task lands on
// top, matching the research table's "new row first" convention.
func CreateQuickTask(ctx context.Context, db *sql.DB, profileID, title, notes, startDate, dueDate string, paperID *string) (domain.QuickTask, error) {
	title = strings.TrimSpace(title)
	if err := validateTaskRequired(taskScopeQuickTask, "title", title, quickTaskTextLimit); err != nil {
		return domain.QuickTask{}, err
	}
	notes = strings.TrimSpace(notes)
	if err := validateTaskText(taskScopeQuickTask, "notes", notes, quickTaskNoteLimit); err != nil {
		return domain.QuickTask{}, err
	}
	if err := validateTaskDate(taskScopeQuickTask, "start_date", startDate); err != nil {
		return domain.QuickTask{}, err
	}
	if err := validateTaskDate(taskScopeQuickTask, "due_date", dueDate); err != nil {
		return domain.QuickTask{}, err
	}
	if err := validateTaskWindow(taskScopeQuickTask, startDate, dueDate); err != nil {
		return domain.QuickTask{}, err
	}
	now := formatTime(time.Now().UTC())
	id := uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.QuickTask{}, fmt.Errorf("begin quick task create: %w", err)
	}
	defer tx.Rollback()
	var linkedPaper any
	if paperID != nil {
		trimmed := strings.TrimSpace(*paperID)
		if trimmed != "" {
			if err := requireQuickTaskPaper(ctx, tx, profileID, trimmed); err != nil {
				return domain.QuickTask{}, err
			}
			linkedPaper = trimmed
		}
	}
	position, err := nextQuickTaskPosition(ctx, tx, profileID)
	if err != nil {
		return domain.QuickTask{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO quick_tasks
		(id, profile_id, title, notes, start_date, due_date, paper_id, completed_at, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
		id, profileID, title, notes, startDate, dueDate, linkedPaper, position, now, now); err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return domain.QuickTask{}, taskValidationf(taskScopeQuickTask, "paper_id does not exist")
		}
		return domain.QuickTask{}, fmt.Errorf("create quick task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.QuickTask{}, fmt.Errorf("commit quick task create: %w", err)
	}
	return GetQuickTask(ctx, db, profileID, id)
}

func UpdateQuickTask(ctx context.Context, db *sql.DB, profileID, id string, patch domain.QuickTaskPatch) (domain.QuickTask, error) {
	current, err := GetQuickTask(ctx, db, profileID, id)
	if err != nil {
		return domain.QuickTask{}, err
	}
	sets := []string{}
	args := []any{}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if err := validateTaskRequired(taskScopeQuickTask, "title", title, quickTaskTextLimit); err != nil {
			return domain.QuickTask{}, err
		}
		sets = append(sets, "title = ?")
		args = append(args, title)
	}
	if patch.Notes != nil {
		notes := strings.TrimSpace(*patch.Notes)
		if err := validateTaskText(taskScopeQuickTask, "notes", notes, quickTaskNoteLimit); err != nil {
			return domain.QuickTask{}, err
		}
		sets = append(sets, "notes = ?")
		args = append(args, notes)
	}
	startDate, dueDate := current.StartDate, current.DueDate
	if patch.StartDate != nil {
		if err := validateTaskDate(taskScopeQuickTask, "start_date", *patch.StartDate); err != nil {
			return domain.QuickTask{}, err
		}
		startDate = *patch.StartDate
		sets = append(sets, "start_date = ?")
		args = append(args, startDate)
	}
	if patch.DueDate != nil {
		if err := validateTaskDate(taskScopeQuickTask, "due_date", *patch.DueDate); err != nil {
			return domain.QuickTask{}, err
		}
		dueDate = *patch.DueDate
		sets = append(sets, "due_date = ?")
		args = append(args, dueDate)
	}
	if err := validateTaskWindow(taskScopeQuickTask, startDate, dueDate); err != nil {
		return domain.QuickTask{}, err
	}
	if patch.SetPaperID {
		var linked any
		if patch.PaperID != nil {
			trimmed := strings.TrimSpace(*patch.PaperID)
			if trimmed != "" {
				if err := requireQuickTaskPaper(ctx, db, profileID, trimmed); err != nil {
					return domain.QuickTask{}, err
				}
				linked = trimmed
			}
		}
		sets = append(sets, "paper_id = ?")
		args = append(args, linked)
	}
	if patch.Completed != nil {
		if *patch.Completed {
			if current.CompletedAt == "" {
				sets = append(sets, "completed_at = ?")
				args = append(args, formatTime(time.Now().UTC()))
			}
		} else if current.CompletedAt != "" {
			sets = append(sets, "completed_at = ?")
			args = append(args, "")
		}
	}
	if patch.Position != nil {
		// New quick tasks are positioned with MIN(position)-1, so negative
		// positions are the normal representation; do not reject a client that
		// echoes the read model back.
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, formatTime(time.Now().UTC()), profileID, id)
		if _, err := db.ExecContext(ctx, `UPDATE quick_tasks SET `+joinComma(sets)+
			` WHERE profile_id = ? AND id = ?`, args...); err != nil {
			return domain.QuickTask{}, fmt.Errorf("update quick task: %w", err)
		}
	}
	return GetQuickTask(ctx, db, profileID, id)
}

func DeleteQuickTask(ctx context.Context, db *sql.DB, profileID, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM quick_tasks WHERE profile_id = ? AND id = ?`, profileID, id)
	if err != nil {
		return fmt.Errorf("delete quick task: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}
