package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

// The 长任务 board limits reuse the research values on purpose: a board card and a
// paper row have to read in the same narrow column, so the two modules drift
// together instead of one quietly allowing a longer string than the other renders.
const (
	taskTextLimit     = 500
	taskLongTextLimit = 2000
	taskReorderLimit  = 500
	taskMapLimit      = 20000
)

// taskDateLayout is the only date form the board accepts. Dates stay TEXT exactly
// like the research table's deadline, and ” means "no date picked" rather than a
// NULL the client would have to special-case on every card.
const taskDateLayout = "2006-01-02"

// Validation scopes name which board entity a client-fixable problem belongs to, so
// the API layer can answer invalid_task_layer instead of one generic code and the
// editor that sent the request knows which field to re-open.
const (
	taskScopeThread     = "task_thread"
	taskScopeLayer      = "task_layer"
	taskScopeItem       = "task_item"
	taskScopeAssignment = "task_assignment"
	taskScopeStep       = "task_step"
)

// TaskValidationError reports a client-fixable board problem so the API layer can
// answer 400 instead of 500.
type TaskValidationError struct {
	Scope  string
	Reason string
}

func (e *TaskValidationError) Error() string { return e.Reason }

// Code is the problem code the API layer answers with, derived from the scope so a
// new entity cannot ship a validation error whose code the handler never maps.
func (e *TaskValidationError) Code() string { return "invalid_" + e.Scope }

func taskValidationf(scope, format string, args ...any) *TaskValidationError {
	return &TaskValidationError{Scope: scope, Reason: fmt.Sprintf(format, args...)}
}

// ErrTaskAssignmentExists reports placing one item in one layer twice. Left to the
// UNIQUE (item_id, layer_id) key the driver would answer 500 with a constraint
// message nobody can act on, so the collision is detected and surfaced as
// 409 assignment_exists.
var ErrTaskAssignmentExists = errors.New("task assignment already exists")

// --- validation ---

func validateTaskText(scope, field, value string, limit int) error {
	if utf8.RuneCountInString(value) > limit {
		return taskValidationf(scope, "field %q exceeds %d characters", field, limit)
	}
	return nil
}

// validateTaskRequired is the rule the board enforces on names: a thread, a layer,
// a card and a step all render their label as the only text on the row, so an empty
// one is not a display problem but an unfinished write.
func validateTaskRequired(scope, field, value string, limit int) error {
	if strings.TrimSpace(value) == "" {
		return taskValidationf(scope, "field %q must not be empty", field)
	}
	return validateTaskText(scope, field, value, limit)
}

// validateTaskDate accepts ” (no date) or a calendar-exact YYYY-MM-DD. The
// re-format check is what rejects 2026-13-01 and 2026-02-30: time.Parse rolls some
// impossible dates over instead of failing, and a rolled-over date would put a
// card's deadline on a day the user never typed.
func validateTaskDate(scope, field, value string) error {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(taskDateLayout, value)
	if err != nil || parsed.Format(taskDateLayout) != value {
		return taskValidationf(scope, "field %q must be '' or YYYY-MM-DD, got %q", field, value)
	}
	return nil
}

// validateTaskWindow keeps a placement's range from running backwards. It is only
// checked when both ends are set, because the board lets the owner type the due
// date first and add the start date later.
func validateTaskWindow(scope, startDate, dueDate string) error {
	if startDate == "" || dueDate == "" {
		return nil
	}
	if dueDate < startDate {
		return taskValidationf(scope, "due_date %q is before start_date %q", dueDate, startDate)
	}
	return nil
}

// --- derived rule predicates, in SQL ---

// taskAllStepsDoneSQL spells the 本层已收口 rule for the assignment whose id is
// reachable as assignmentID: at least one step, and no step left undone. The Go
// derivation in taskStepsClosed is the same rule; both exist because the write path
// has to reconcile closed_at for rows it never loaded, and a stored boolean would
// let the two definitions drift apart.
func taskAllStepsDoneSQL(assignmentID string) string {
	return fmt.Sprintf(`(SELECT COUNT(*) FROM task_steps s WHERE s.assignment_id = %s) > 0
		AND (SELECT COUNT(*) FROM task_steps s WHERE s.assignment_id = %s AND s.done = 0) = 0`,
		assignmentID, assignmentID)
}

// taskItemClosedSQL is the item-level 已收口 rule: at least one placement, and every
// placement closed.
func taskItemClosedSQL() string {
	return `(SELECT COUNT(*) FROM task_assignments a WHERE a.item_id = task_items.id) > 0
		AND NOT EXISTS (SELECT 1 FROM task_assignments a
			WHERE a.item_id = task_items.id AND NOT (` + taskAllStepsDoneSQL("a.id") + `))`
}

// taskExecer is the write half of *sql.DB and *sql.Tx, so one helper serves both
// the transaction that made a change and a standalone statement that is its own.
type taskExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// syncTaskClosedStamps rewrites the two closed_at display columns for the items
// selected by itemWhere so they agree with the step-derived truth: stamped when the
// last step flips to done, cleared when a step flips back. Each statement only
// matches the rows whose value genuinely has to change, so a write that left the
// close state alone never re-stamps a card's history.
//
// itemWhere is written over task_items columns; the placement statements reach the
// same set through a subquery, because task_assignments has no thread or profile
// column of its own.
func syncTaskClosedStamps(ctx context.Context, exec taskExecer, itemWhere string, args []any, now string) error {
	assignmentScope := fmt.Sprintf("item_id IN (SELECT id FROM task_items WHERE %s)", itemWhere)
	itemScope := itemWhere
	assignmentClosed := taskAllStepsDoneSQL("task_assignments.id")
	statements := []struct {
		query  string
		values []any
	}{
		{fmt.Sprintf(`UPDATE task_assignments SET closed_at = ''
			WHERE closed_at <> '' AND %s AND NOT (%s)`, assignmentScope, assignmentClosed), nil},
		{fmt.Sprintf(`UPDATE task_assignments SET closed_at = ?
			WHERE closed_at = '' AND %s AND %s`, assignmentScope, assignmentClosed), []any{now}},
		{fmt.Sprintf(`UPDATE task_items SET closed_at = ''
			WHERE closed_at <> '' AND %s AND NOT (%s)`, itemScope, taskItemClosedSQL()), nil},
		{fmt.Sprintf(`UPDATE task_items SET closed_at = ?
			WHERE closed_at = '' AND %s AND %s`, itemScope, taskItemClosedSQL()), []any{now}},
	}
	for _, statement := range statements {
		values := append(append([]any{}, statement.values...), args...)
		if _, err := exec.ExecContext(ctx, statement.query, values...); err != nil {
			return fmt.Errorf("sync task closed stamps: %w", err)
		}
	}
	return nil
}

// --- read side ---

// loadTaskBoard reads the profile's board once and assembles the threads named by
// threadIDs (all of them when threadIDs is empty). Every child query is scoped by
// profile_id rather than by an id list: this is a personal tool holding tens of
// rows that the client fetches in one document, so a flat read per level beats an
// IN-list per thread and stays clear of SQLite's host-parameter ceiling however
// long the card list grows.
func loadTaskBoard(ctx context.Context, db *sql.DB, profileID string, threadIDs []string) ([]domain.TaskThread, error) {
	threads, err := queryTaskThreads(ctx, db, profileID, threadIDs)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return []domain.TaskThread{}, nil
	}
	layersByThread, err := queryTaskLayers(ctx, db, profileID)
	if err != nil {
		return nil, err
	}
	itemsByThread, err := queryTaskItems(ctx, db, profileID)
	if err != nil {
		return nil, err
	}
	assignmentsByItem, err := queryTaskAssignments(ctx, db, profileID)
	if err != nil {
		return nil, err
	}
	stepsByAssignment, err := queryTaskSteps(ctx, db, profileID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.TaskThread, 0, len(threads))
	for _, thread := range threads {
		result = append(result, assembleTaskThread(thread,
			layersByThread[thread.ID], itemsByThread[thread.ID], assignmentsByItem, stepsByAssignment))
	}
	return result, nil
}

func queryTaskThreads(ctx context.Context, db *sql.DB, profileID string, threadIDs []string) ([]domain.TaskThread, error) {
	query := `SELECT id, name, position, archived, map_markdown FROM task_threads WHERE profile_id = ?`
	args := []any{profileID}
	if len(threadIDs) > 0 {
		query += ` AND id IN (` + taskPlaceholders(len(threadIDs)) + `)`
		for _, id := range threadIDs {
			args = append(args, id)
		}
	}
	// created_at breaks a position tie so a board seeded by two writes in the same
	// instant still reads in the order it was built rather than an arbitrary one.
	rows, err := db.QueryContext(ctx, query+" ORDER BY position, created_at", args...)
	if err != nil {
		return nil, fmt.Errorf("list task threads: %w", err)
	}
	defer rows.Close()
	threads := make([]domain.TaskThread, 0)
	for rows.Next() {
		var (
			thread   domain.TaskThread
			archived int
		)
		if err := rows.Scan(&thread.ID, &thread.Name, &thread.Position, &archived, &thread.MapMarkdown); err != nil {
			return nil, fmt.Errorf("scan task thread: %w", err)
		}
		thread.Archived = archived != 0
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func queryTaskLayers(ctx context.Context, db *sql.DB, profileID string) (map[string][]domain.TaskLayer, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, thread_id, name, position, forced_unlock
		FROM task_layers WHERE profile_id = ? ORDER BY position, created_at`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list task layers: %w", err)
	}
	defer rows.Close()
	byThread := make(map[string][]domain.TaskLayer)
	for rows.Next() {
		var (
			layer        domain.TaskLayer
			threadID     string
			forcedUnlock int
		)
		if err := rows.Scan(&layer.ID, &threadID, &layer.Name, &layer.Position, &forcedUnlock); err != nil {
			return nil, fmt.Errorf("scan task layer: %w", err)
		}
		layer.ForcedUnlock = forcedUnlock != 0
		byThread[threadID] = append(byThread[threadID], layer)
	}
	return byThread, rows.Err()
}

func queryTaskItems(ctx context.Context, db *sql.DB, profileID string) (map[string][]domain.TaskItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, thread_id, title, notes, position, paper_id, closed_at
		FROM task_items WHERE profile_id = ? ORDER BY position, created_at`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list task items: %w", err)
	}
	defer rows.Close()
	byThread := make(map[string][]domain.TaskItem)
	for rows.Next() {
		var (
			item     domain.TaskItem
			threadID string
			paperID  sql.NullString
		)
		if err := rows.Scan(&item.ID, &threadID, &item.Title, &item.Notes,
			&item.Position, &paperID, &item.ClosedAt); err != nil {
			return nil, fmt.Errorf("scan task item: %w", err)
		}
		if paperID.Valid {
			value := paperID.String
			item.PaperID = &value
		}
		// A card with no placements answers an empty list, never null, so the
		// client renders "跨 0 层" nothing rather than defending against nil.
		item.Assignments = make([]domain.TaskAssignment, 0)
		byThread[threadID] = append(byThread[threadID], item)
	}
	return byThread, rows.Err()
}

func queryTaskAssignments(ctx context.Context, db *sql.DB, profileID string) (map[string][]domain.TaskAssignment, error) {
	// The join to task_layers is what lets a placement come back already carrying
	// its layer's position: a card sitting in 第二层 and 第三层 has to list those
	// placements in the order the board shows the layers, not in insertion order.
	rows, err := db.QueryContext(ctx, `SELECT a.item_id, a.id, a.layer_id, a.position, l.position,
			a.start_date, a.due_date, a.closed_at
		FROM task_assignments a
		JOIN task_items i ON i.id = a.item_id
		JOIN task_layers l ON l.id = a.layer_id
		WHERE i.profile_id = ?
		ORDER BY l.position, a.position, a.id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list task assignments: %w", err)
	}
	defer rows.Close()
	byItem := make(map[string][]domain.TaskAssignment)
	for rows.Next() {
		var (
			itemID     string
			assignment domain.TaskAssignment
		)
		if err := rows.Scan(&itemID, &assignment.ID, &assignment.LayerID, &assignment.Position,
			&assignment.LayerPosition, &assignment.StartDate, &assignment.DueDate, &assignment.ClosedAt); err != nil {
			return nil, fmt.Errorf("scan task assignment: %w", err)
		}
		assignment.Steps = make([]domain.TaskStep, 0)
		byItem[itemID] = append(byItem[itemID], assignment)
	}
	return byItem, rows.Err()
}

func queryTaskSteps(ctx context.Context, db *sql.DB, profileID string) (map[string][]domain.TaskStep, error) {
	// Steps join through two levels to reach the profile: the schema keeps them on
	// the placement they belong to, so a step inherits its scope instead of
	// carrying a third copy of the profile id.
	rows, err := db.QueryContext(ctx, `SELECT s.assignment_id, s.id, s.name, s.done, s.position, s.done_at
		FROM task_steps s
		JOIN task_assignments a ON a.id = s.assignment_id
		JOIN task_items i ON i.id = a.item_id
		WHERE i.profile_id = ?
		ORDER BY s.position, s.id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list task steps: %w", err)
	}
	defer rows.Close()
	byAssignment := make(map[string][]domain.TaskStep)
	for rows.Next() {
		var (
			assignmentID string
			step         domain.TaskStep
			done         int
		)
		if err := rows.Scan(&assignmentID, &step.ID, &step.Name, &done, &step.Position, &step.DoneAt); err != nil {
			return nil, fmt.Errorf("scan task step: %w", err)
		}
		step.Done = done != 0
		byAssignment[assignmentID] = append(byAssignment[assignmentID], step)
	}
	return byAssignment, rows.Err()
}

func taskPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}

// assembleTaskThread fills every derived field of one 主线: the layer unlock chain,
// the per-layer tallies, each card's status and progress, and the thread stats.
// None of it is stored, so the same rules recompute from the step rows on every
// read and a board can never disagree with its own checklist.
func assembleTaskThread(thread domain.TaskThread, layers []domain.TaskLayer, items []domain.TaskItem,
	assignmentsByItem map[string][]domain.TaskAssignment, stepsByAssignment map[string][]domain.TaskStep,
) domain.TaskThread {
	thread.Layers = make([]domain.TaskLayer, 0, len(layers))
	thread.Items = make([]domain.TaskItem, 0, len(items))

	// Pass 1: attach steps, settle the two closings that depend on steps alone, and
	// tally each layer's placements while doing it.
	for index := range items {
		item := items[index]
		placements := assignmentsByItem[item.ID]
		merged := make([]domain.TaskAssignment, 0, len(placements))
		for _, assignment := range placements {
			steps := stepsByAssignment[assignment.ID]
			if steps == nil {
				steps = make([]domain.TaskStep, 0)
			}
			assignment.Steps = steps
			assignment.Closed = taskStepsClosed(steps)
			item.Stats.TotalSteps += len(steps)
			for _, step := range steps {
				if step.Done {
					item.Stats.DoneSteps++
				}
			}
			merged = append(merged, assignment)
		}
		sort.SliceStable(merged, func(left, right int) bool {
			if merged[left].LayerPosition != merged[right].LayerPosition {
				return merged[left].LayerPosition < merged[right].LayerPosition
			}
			return merged[left].Position < merged[right].Position
		})
		item.Assignments = merged
		item.LayerCount = len(merged)
		item.Closed = len(merged) > 0
		for _, assignment := range merged {
			if !assignment.Closed {
				item.Closed = false
				break
			}
		}
		// Progress is the whole item's ratio, across every layer, so the same task
		// in two layers shows the same number on both cards.
		item.Progress = taskProgress(item.Stats.DoneSteps, item.Stats.TotalSteps)
		items[index] = item
	}

	// Pass 2: walk the layers in order and decide each unlock. totalByLayer and
	// closedByLayer are built here from the placements pass 1 settled, so a layer
	// with no placements counts as fully closed and an empty predecessor never
	// blocks the chain forever.
	totalByLayer := make(map[string]int, len(layers))
	closedByLayer := make(map[string]int, len(layers))
	for _, item := range items {
		for _, assignment := range item.Assignments {
			totalByLayer[assignment.LayerID]++
			if assignment.Closed {
				closedByLayer[assignment.LayerID]++
			}
		}
	}
	unlocked := make(map[string]bool, len(layers))
	for index, layer := range layers {
		layer.ItemCount = totalByLayer[layer.ID]
		layer.ClosedCount = closedByLayer[layer.ID]
		switch {
		case layer.ForcedUnlock:
			// The escape hatch wins without rewriting history: the owner opens a
			// layer whose predecessor is legitimately stuck.
			layer.Unlocked = true
		case index == 0:
			layer.Unlocked = true
		default:
			previous := layers[index-1]
			layer.Unlocked = closedByLayer[previous.ID] == totalByLayer[previous.ID]
		}
		unlocked[layer.ID] = layer.Unlocked
		thread.Layers = append(thread.Layers, layer)
	}

	// Pass 3: each card's status, tested in the contract's fixed order, so a card
	// that is both overdue and parked in a locked layer reports locked.
	today := time.Now().UTC().Format(taskDateLayout)
	for _, item := range items {
		item.Status = deriveTaskItemStatus(item, unlocked, today)
		thread.Stats.Total++
		for _, assignment := range item.Assignments {
			thread.Stats.Slots++
			if assignment.Closed {
				thread.Stats.SlotsClosed++
			}
		}
		switch item.Status {
		case domain.TaskStatusActive:
			thread.Stats.Active++
		case domain.TaskStatusLocked:
			thread.Stats.Locked++
		case domain.TaskStatusUnstarted:
			thread.Stats.Unstarted++
		case domain.TaskStatusOverdue:
			thread.Stats.Overdue++
		case domain.TaskStatusClosed:
			thread.Stats.Closed++
		}
		thread.Items = append(thread.Items, item)
	}
	return thread
}

// deriveTaskItemStatus applies the status precedence: closed, then locked, then
// overdue, then unstarted, with active as everything else.
func deriveTaskItemStatus(item domain.TaskItem, unlocked map[string]bool, today string) string {
	if item.Closed {
		return domain.TaskStatusClosed
	}
	anyUnlocked := false
	stepsInUnlocked := 0
	for _, assignment := range item.Assignments {
		if unlocked[assignment.LayerID] {
			anyUnlocked = true
			stepsInUnlocked += len(assignment.Steps)
		}
	}
	// Locked needs a placement to be locked in: a brand-new card nobody has dropped
	// on a layer yet is unstarted below, not locked.
	if len(item.Assignments) > 0 && !anyUnlocked {
		return domain.TaskStatusLocked
	}
	// Overdue is judged across every placement, locked ones included: a date the
	// owner typed does not stop being late because the layer holding it is shut.
	for _, assignment := range item.Assignments {
		if assignment.DueDate != "" && assignment.DueDate < today {
			return domain.TaskStatusOverdue
		}
	}
	if stepsInUnlocked == 0 {
		return domain.TaskStatusUnstarted
	}
	return domain.TaskStatusActive
}

func taskStepsClosed(steps []domain.TaskStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if !step.Done {
			return false
		}
	}
	return true
}

// taskProgress rounds the done/total percentage of an item's whole checklist. The
// 2/2 plus 0/3 card is 40 on both of its cells, not 100 and 0, because the board
// tracks one task and the split numbers would read as two.
func taskProgress(done, total int) int {
	if total == 0 {
		return 0
	}
	return int(math.Round(float64(done) / float64(total) * 100))
}

// ListTaskThreads returns the whole 长任务 board in one document.
func ListTaskThreads(ctx context.Context, db *sql.DB, profileID string) ([]domain.TaskThread, error) {
	return loadTaskBoard(ctx, db, profileID, nil)
}

// GetTaskThread returns one thread with its full tree.
func GetTaskThread(ctx context.Context, db *sql.DB, profileID, id string) (domain.TaskThread, error) {
	if err := requireTaskThread(ctx, db, profileID, id); err != nil {
		return domain.TaskThread{}, err
	}
	threads, err := loadTaskBoard(ctx, db, profileID, []string{id})
	if err != nil {
		return domain.TaskThread{}, err
	}
	if len(threads) == 0 {
		return domain.TaskThread{}, ErrNotFound
	}
	return threads[0], nil
}

// GetTaskLayer returns one layer with its unlock state and tallies.
func GetTaskLayer(ctx context.Context, db *sql.DB, profileID, id string) (domain.TaskLayer, error) {
	threadID, err := taskLayerThreadID(ctx, db, profileID, id)
	if err != nil {
		return domain.TaskLayer{}, err
	}
	return GetTaskThreadLayer(ctx, db, profileID, threadID, id)
}

// GetTaskThreadLayer returns one layer out of a thread the caller already resolved,
// which is how the write paths answer without locating the owner twice.
func GetTaskThreadLayer(ctx context.Context, db *sql.DB, profileID, threadID, layerID string) (domain.TaskLayer, error) {
	thread, err := GetTaskThread(ctx, db, profileID, threadID)
	if err != nil {
		return domain.TaskLayer{}, err
	}
	for _, layer := range thread.Layers {
		if layer.ID == layerID {
			return layer, nil
		}
	}
	return domain.TaskLayer{}, ErrNotFound
}

// GetTaskItem returns one card with its placements, steps and status.
func GetTaskItem(ctx context.Context, db *sql.DB, profileID, id string) (domain.TaskItem, error) {
	threadID, err := taskItemThreadID(ctx, db, profileID, id)
	if err != nil {
		return domain.TaskItem{}, err
	}
	thread, err := GetTaskThread(ctx, db, profileID, threadID)
	if err != nil {
		return domain.TaskItem{}, err
	}
	for _, item := range thread.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.TaskItem{}, ErrNotFound
}

// GetTaskAssignment returns one placement with its steps.
func GetTaskAssignment(ctx context.Context, db *sql.DB, profileID, id string) (domain.TaskAssignment, error) {
	threadID, err := taskAssignmentThreadID(ctx, db, profileID, id)
	if err != nil {
		return domain.TaskAssignment{}, err
	}
	thread, err := GetTaskThread(ctx, db, profileID, threadID)
	if err != nil {
		return domain.TaskAssignment{}, err
	}
	for _, item := range thread.Items {
		for _, assignment := range item.Assignments {
			if assignment.ID == id {
				return assignment, nil
			}
		}
	}
	return domain.TaskAssignment{}, ErrNotFound
}

// GetTaskStep returns one step. A step has no derived field, but it is still read
// through its placement and card, because the step row carries no profile column of
// its own and that join is what makes a step id from another board answer 404
// instead of leaking someone else's checklist.
func GetTaskStep(ctx context.Context, db *sql.DB, profileID, id string) (domain.TaskStep, error) {
	var (
		step domain.TaskStep
		done int
	)
	err := db.QueryRowContext(ctx, `SELECT s.id, s.name, s.done, s.position, s.done_at
		FROM task_steps s
		JOIN task_assignments a ON a.id = s.assignment_id
		JOIN task_items i ON i.id = a.item_id
		WHERE i.profile_id = ? AND s.id = ?`, profileID, id).
		Scan(&step.ID, &step.Name, &done, &step.Position, &step.DoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskStep{}, ErrNotFound
	}
	if err != nil {
		return domain.TaskStep{}, fmt.Errorf("get task step: %w", err)
	}
	step.Done = done != 0
	return step, nil
}

// ResolveTaskThreadID reports the 主线 a board row belongs to, so a write that
// arrived by entity id can still publish the thread the client should refresh.
func ResolveTaskThreadID(ctx context.Context, db *sql.DB, profileID, entity, id string) (string, error) {
	switch entity {
	case "thread":
		if err := requireTaskThread(ctx, db, profileID, id); err != nil {
			return "", err
		}
		return id, nil
	case "layer":
		return taskLayerThreadID(ctx, db, profileID, id)
	case "item":
		return taskItemThreadID(ctx, db, profileID, id)
	case "assignment":
		return taskAssignmentThreadID(ctx, db, profileID, id)
	case "step":
		return taskStepThreadID(ctx, db, profileID, id)
	default:
		return "", fmt.Errorf("resolve task thread: unknown board entity %q", entity)
	}
}

func requireTaskThread(ctx context.Context, db *sql.DB, profileID, id string) error {
	var exists bool
	if err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM task_threads WHERE profile_id = ? AND id = ?)", profileID, id).
		Scan(&exists); err != nil {
		return fmt.Errorf("check task thread: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func taskLayerThreadID(ctx context.Context, db *sql.DB, profileID, id string) (string, error) {
	return scanTaskOwnerID(ctx, db,
		"SELECT thread_id FROM task_layers WHERE profile_id = ? AND id = ?", id, profileID, "task layer")
}

func taskItemThreadID(ctx context.Context, db *sql.DB, profileID, id string) (string, error) {
	return scanTaskOwnerID(ctx, db,
		"SELECT thread_id FROM task_items WHERE profile_id = ? AND id = ?", id, profileID, "task item")
}

func taskAssignmentThreadID(ctx context.Context, db *sql.DB, profileID, id string) (string, error) {
	return scanTaskOwnerID(ctx, db, `SELECT i.thread_id FROM task_assignments a
		JOIN task_items i ON i.id = a.item_id WHERE i.profile_id = ? AND a.id = ?`,
		id, profileID, "task assignment")
}

func taskStepThreadID(ctx context.Context, db *sql.DB, profileID, id string) (string, error) {
	return scanTaskOwnerID(ctx, db, `SELECT i.thread_id FROM task_steps s
		JOIN task_assignments a ON a.id = s.assignment_id
		JOIN task_items i ON i.id = a.item_id WHERE i.profile_id = ? AND s.id = ?`,
		id, profileID, "task step")
}

func scanTaskOwnerID(ctx context.Context, db *sql.DB, query, id, profileID, label string) (string, error) {
	var owner string
	err := db.QueryRowContext(ctx, query, profileID, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("locate %s: %w", label, err)
	}
	return owner, nil
}

// taskAssignmentItemID resolves the card a placement belongs to, scoped by profile
// so an id from another board answers 404 rather than touching a stranger's rows.
func taskAssignmentItemID(ctx context.Context, db *sql.DB, profileID, assignmentID string) (string, error) {
	var itemID string
	err := db.QueryRowContext(ctx, `SELECT a.item_id FROM task_assignments a
		JOIN task_items i ON i.id = a.item_id WHERE i.profile_id = ? AND a.id = ?`, profileID, assignmentID).
		Scan(&itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("locate task assignment item: %w", err)
	}
	return itemID, nil
}

func taskStepAssignmentID(ctx context.Context, db *sql.DB, profileID, stepID string) (string, error) {
	var assignmentID string
	err := db.QueryRowContext(ctx, `SELECT s.assignment_id FROM task_steps s
		JOIN task_assignments a ON a.id = s.assignment_id
		JOIN task_items i ON i.id = a.item_id
		WHERE i.profile_id = ? AND s.id = ?`, profileID, stepID).Scan(&assignmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("locate task step assignment: %w", err)
	}
	return assignmentID, nil
}

// --- thread writes ---

// CreateTaskThread appends a 主线 to the board. A thread with no layers is a legal
// state: the owner names the thread first and adds 层 to it afterwards.
func CreateTaskThread(ctx context.Context, db *sql.DB, profileID, name string) (domain.TaskThread, error) {
	name = strings.TrimSpace(name)
	if err := validateTaskRequired(taskScopeThread, "name", name, taskTextLimit); err != nil {
		return domain.TaskThread{}, err
	}
	now := formatTime(time.Now().UTC())
	id := uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskThread{}, fmt.Errorf("begin task thread create: %w", err)
	}
	defer tx.Rollback()
	position, err := nextTaskPosition(ctx, tx, "task_threads", "profile_id = ?", profileID)
	if err != nil {
		return domain.TaskThread{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_threads
		(id, profile_id, name, position, archived, map_markdown, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, '', ?, ?)`, id, profileID, name, position, now, now); err != nil {
		return domain.TaskThread{}, fmt.Errorf("create task thread: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskThread{}, fmt.Errorf("commit task thread create: %w", err)
	}
	return GetTaskThread(ctx, db, profileID, id)
}

// UpdateTaskThread applies a partial patch: a nil field is left alone, so the board
// renames, reorders, or archives a thread in whichever request the control makes.
func UpdateTaskThread(ctx context.Context, db *sql.DB, profileID, id string, patch domain.TaskThreadPatch) (domain.TaskThread, error) {
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		if err := validateTaskRequired(taskScopeThread, "name", trimmed, taskTextLimit); err != nil {
			return domain.TaskThread{}, err
		}
		patch.Name = &trimmed
	}
	if patch.Position != nil && *patch.Position < 0 {
		return domain.TaskThread{}, taskValidationf(taskScopeThread, "position must not be negative")
	}
	if err := requireTaskThread(ctx, db, profileID, id); err != nil {
		return domain.TaskThread{}, err
	}
	sets := []string{}
	args := []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	if patch.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolToInt(*patch.Archived))
	}
	if patch.MapMarkdown != nil {
		trimmed := strings.TrimSpace(*patch.MapMarkdown)
		if err := validateTaskText(taskScopeThread, "map_markdown", trimmed, taskMapLimit); err != nil {
			return domain.TaskThread{}, err
		}
		sets = append(sets, "map_markdown = ?")
		args = append(args, trimmed)
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, formatTime(time.Now().UTC()))
		args = append(args, profileID, id)
		if _, err := db.ExecContext(ctx,
			"UPDATE task_threads SET "+joinComma(sets)+" WHERE profile_id = ? AND id = ?", args...); err != nil {
			return domain.TaskThread{}, fmt.Errorf("update task thread: %w", err)
		}
	}
	return GetTaskThread(ctx, db, profileID, id)
}

// DeleteTaskThread drops a board and everything under it: the cascade from thread to
// layer and item takes the placements, and each placement takes its steps.
func DeleteTaskThread(ctx context.Context, db *sql.DB, profileID, id string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM task_threads WHERE profile_id = ? AND id = ?", profileID, id)
	if err != nil {
		return fmt.Errorf("delete task thread: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- layer writes ---

// CreateTaskLayer appends a 层 to one thread. A missing name is filled in here
// rather than left to the client: the board adds a layer with one click, and the
// label that appears is the server's ordering rule, so the client can send {} and
// still get 第2层 instead of an empty pill.
func CreateTaskLayer(ctx context.Context, db *sql.DB, profileID, threadID string, name *string) (domain.TaskLayer, error) {
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if err := validateTaskRequired(taskScopeLayer, "name", trimmed, taskTextLimit); err != nil {
			return domain.TaskLayer{}, err
		}
		name = &trimmed
	}
	if err := requireTaskThread(ctx, db, profileID, threadID); err != nil {
		return domain.TaskLayer{}, err
	}
	now := formatTime(time.Now().UTC())
	id := uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskLayer{}, fmt.Errorf("begin task layer create: %w", err)
	}
	defer tx.Rollback()
	position, err := nextTaskPosition(ctx, tx, "task_layers", "profile_id = ? AND thread_id = ?", profileID, threadID)
	if err != nil {
		return domain.TaskLayer{}, err
	}
	label := fmt.Sprintf("第%d层", position+1)
	if name != nil {
		label = *name
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_layers
		(id, thread_id, profile_id, name, position, forced_unlock, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)`, id, threadID, profileID, label, position, now, now); err != nil {
		return domain.TaskLayer{}, fmt.Errorf("create task layer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskLayer{}, fmt.Errorf("commit task layer create: %w", err)
	}
	return GetTaskThreadLayer(ctx, db, profileID, threadID, id)
}

// UpdateTaskLayer applies a partial layer patch. Moving a layer changes which
// predecessor gates it, and unlock is recomputed on read, so a PATCH that only
// mentions position re-chains the whole board by itself.
func UpdateTaskLayer(ctx context.Context, db *sql.DB, profileID, id string, patch domain.TaskLayerPatch) (domain.TaskLayer, error) {
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		if err := validateTaskRequired(taskScopeLayer, "name", trimmed, taskTextLimit); err != nil {
			return domain.TaskLayer{}, err
		}
		patch.Name = &trimmed
	}
	if patch.Position != nil && *patch.Position < 0 {
		return domain.TaskLayer{}, taskValidationf(taskScopeLayer, "position must not be negative")
	}
	threadID, err := taskLayerThreadID(ctx, db, profileID, id)
	if err != nil {
		return domain.TaskLayer{}, err
	}
	sets := []string{}
	args := []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	if patch.ForcedUnlock != nil {
		sets = append(sets, "forced_unlock = ?")
		args = append(args, boolToInt(*patch.ForcedUnlock))
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, formatTime(time.Now().UTC()))
		args = append(args, profileID, id)
		if _, err := db.ExecContext(ctx,
			"UPDATE task_layers SET "+joinComma(sets)+" WHERE profile_id = ? AND id = ?", args...); err != nil {
			return domain.TaskLayer{}, fmt.Errorf("update task layer: %w", err)
		}
	}
	return GetTaskThreadLayer(ctx, db, profileID, threadID, id)
}

// DeleteTaskLayer drops a 层 with its placements and their steps. There is no rule
// about removing the only layer or one that still has open work: the board is the
// owner's, and a cascade that refused to run would only teach them to delete a row
// at a time.
func DeleteTaskLayer(ctx context.Context, db *sql.DB, profileID, id string) error {
	threadID, err := taskLayerThreadID(ctx, db, profileID, id)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin task layer delete: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM task_layers WHERE profile_id = ? AND id = ?", profileID, id)
	if err != nil {
		return fmt.Errorf("delete task layer: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	// The placements this cascade removed can have been the only open ones of
	// their cards, so every item of the thread needs its display stamp reconciled.
	// The layer's own id is gone by now, which is why the whole thread is the scope.
	if err := syncTaskClosedStamps(ctx, tx, "profile_id = ? AND thread_id = ?",
		[]any{profileID, threadID}, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// --- item writes ---

// CreateTaskItem appends a card to one thread. A layerID also mints the placement in
// the same transaction: dropping a card onto a 层 block is one user action, and two
// round trips would let a half-created card show up in between.
func CreateTaskItem(ctx context.Context, db *sql.DB, profileID, threadID, title string, layerID string) (domain.TaskItem, error) {
	title = strings.TrimSpace(title)
	if err := validateTaskRequired(taskScopeItem, "title", title, taskTextLimit); err != nil {
		return domain.TaskItem{}, err
	}
	if err := requireTaskThread(ctx, db, profileID, threadID); err != nil {
		return domain.TaskItem{}, err
	}
	if layerID != "" {
		owner, err := taskLayerThreadID(ctx, db, profileID, layerID)
		if err != nil {
			return domain.TaskItem{}, err
		}
		if owner != threadID {
			return domain.TaskItem{}, taskValidationf(taskScopeItem,
				"layer %s belongs to another thread", layerID)
		}
	}
	now := formatTime(time.Now().UTC())
	id := uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskItem{}, fmt.Errorf("begin task item create: %w", err)
	}
	defer tx.Rollback()
	position, err := nextTaskPosition(ctx, tx, "task_items", "profile_id = ? AND thread_id = ?", profileID, threadID)
	if err != nil {
		return domain.TaskItem{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_items
		(id, thread_id, profile_id, title, notes, position, paper_id, closed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, NULL, '', ?, ?)`,
		id, threadID, profileID, title, position, now, now); err != nil {
		return domain.TaskItem{}, fmt.Errorf("create task item: %w", err)
	}
	if layerID != "" {
		// The first placement of a card starts at 0 within the card, and it is the
		// only one this call can make, so no sibling lookup is needed.
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_assignments
			(id, item_id, layer_id, position, start_date, due_date, closed_at)
			VALUES (?, ?, ?, 0, '', '', '')`, uuid.NewString(), id, layerID); err != nil {
			return domain.TaskItem{}, fmt.Errorf("assign task item to layer: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskItem{}, fmt.Errorf("commit task item create: %w", err)
	}
	return GetTaskItem(ctx, db, profileID, id)
}

// UpdateTaskItem applies a partial card patch. paper_id is tri-state: the route says
// whether the field was mentioned at all, and a mention carrying an explicit null
// unlinks the card instead of leaving the old paper attached.
func UpdateTaskItem(ctx context.Context, db *sql.DB, profileID, id string, patch domain.TaskItemPatch) (domain.TaskItem, error) {
	if patch.Title != nil {
		trimmed := strings.TrimSpace(*patch.Title)
		if err := validateTaskRequired(taskScopeItem, "title", trimmed, taskTextLimit); err != nil {
			return domain.TaskItem{}, err
		}
		patch.Title = &trimmed
	}
	if patch.Notes != nil {
		if err := validateTaskText(taskScopeItem, "notes", *patch.Notes, taskLongTextLimit); err != nil {
			return domain.TaskItem{}, err
		}
	}
	if patch.Position != nil && *patch.Position < 0 {
		return domain.TaskItem{}, taskValidationf(taskScopeItem, "position must not be negative")
	}
	if patch.SetPaperID && patch.PaperID != nil {
		if err := requireResearchPaper(ctx, db, profileID, *patch.PaperID); err != nil {
			return domain.TaskItem{}, err
		}
	}
	if _, err := taskItemThreadID(ctx, db, profileID, id); err != nil {
		return domain.TaskItem{}, err
	}
	sets := []string{}
	args := []any{}
	if patch.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *patch.Title)
	}
	if patch.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *patch.Notes)
	}
	if patch.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	if patch.SetPaperID {
		sets = append(sets, "paper_id = ?")
		args = append(args, taskNullableString(patch.PaperID))
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, formatTime(time.Now().UTC()))
		args = append(args, profileID, id)
		if _, err := db.ExecContext(ctx,
			"UPDATE task_items SET "+joinComma(sets)+" WHERE profile_id = ? AND id = ?", args...); err != nil {
			return domain.TaskItem{}, fmt.Errorf("update task item: %w", err)
		}
	}
	return GetTaskItem(ctx, db, profileID, id)
}

// DeleteTaskItem drops a card and, through the cascade, its placements and their
// steps. Papers are untouched: the link is the card's, not the paper's.
func DeleteTaskItem(ctx context.Context, db *sql.DB, profileID, id string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM task_items WHERE profile_id = ? AND id = ?", profileID, id)
	if err != nil {
		return fmt.Errorf("delete task item: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderTaskItems rewrites card order inside one thread to match orderedIDs, which
// is how a dragged card sticks. Ids of another thread or profile are ignored, so a
// stale drag list can never move somebody else's card.
func ReorderTaskItems(ctx context.Context, db *sql.DB, profileID, threadID string, orderedIDs []string) error {
	if len(orderedIDs) > taskReorderLimit {
		return taskValidationf(taskScopeItem, "reorder accepts at most %d item ids", taskReorderLimit)
	}
	if err := requireTaskThread(ctx, db, profileID, threadID); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin task item reorder: %w", err)
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	for position, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx,
			"UPDATE task_items SET position = ?, updated_at = ? WHERE profile_id = ? AND thread_id = ? AND id = ?",
			position, now, profileID, threadID, id); err != nil {
			return fmt.Errorf("reorder task item: %w", err)
		}
	}
	return tx.Commit()
}

// --- assignment writes ---

// CreateTaskAssignment places an existing card in an existing layer. A locked layer
// accepts the placement: the board lets the owner queue work for a layer that has
// not opened yet, so rejecting it would turn a status into a permission.
func CreateTaskAssignment(ctx context.Context, db *sql.DB, profileID, itemID, layerID, startDate, dueDate string) (domain.TaskAssignment, error) {
	if err := validateTaskDate(taskScopeAssignment, "start_date", startDate); err != nil {
		return domain.TaskAssignment{}, err
	}
	if err := validateTaskDate(taskScopeAssignment, "due_date", dueDate); err != nil {
		return domain.TaskAssignment{}, err
	}
	if err := validateTaskWindow(taskScopeAssignment, startDate, dueDate); err != nil {
		return domain.TaskAssignment{}, err
	}
	itemThread, err := taskItemThreadID(ctx, db, profileID, itemID)
	if err != nil {
		return domain.TaskAssignment{}, err
	}
	layerThread, err := taskLayerThreadID(ctx, db, profileID, layerID)
	if err != nil {
		return domain.TaskAssignment{}, err
	}
	if itemThread != layerThread {
		return domain.TaskAssignment{}, taskValidationf(taskScopeAssignment,
			"item and layer belong to different threads")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("begin task assignment create: %w", err)
	}
	defer tx.Rollback()
	var taken bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM task_assignments WHERE item_id = ? AND layer_id = ?)", itemID, layerID).
		Scan(&taken); err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("check task assignment: %w", err)
	}
	if taken {
		return domain.TaskAssignment{}, ErrTaskAssignmentExists
	}
	position, err := nextTaskPosition(ctx, tx, "task_assignments", "item_id = ?", itemID)
	if err != nil {
		return domain.TaskAssignment{}, err
	}
	id := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_assignments
		(id, item_id, layer_id, position, start_date, due_date, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, '')`, id, itemID, layerID, position, startDate, dueDate); err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("create task assignment: %w", err)
	}
	// A new placement has no steps, so it is not closed: the card it hangs on can
	// have gone from 已收口 back to open, and its stamp has to follow.
	if err := syncTaskClosedStamps(ctx, tx, "id = ?", []any{itemID}, formatTime(time.Now().UTC())); err != nil {
		return domain.TaskAssignment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("commit task assignment create: %w", err)
	}
	return GetTaskAssignment(ctx, db, profileID, id)
}

// UpdateTaskAssignment applies a partial placement patch. The date window is
// re-checked across both ends even when the patch mentions one, because a card
// already due on 10/31 must not accept a start date of 11/02.
func UpdateTaskAssignment(ctx context.Context, db *sql.DB, profileID, id string, patch domain.TaskAssignmentPatch) (domain.TaskAssignment, error) {
	if patch.StartDate != nil {
		if err := validateTaskDate(taskScopeAssignment, "start_date", *patch.StartDate); err != nil {
			return domain.TaskAssignment{}, err
		}
	}
	if patch.DueDate != nil {
		if err := validateTaskDate(taskScopeAssignment, "due_date", *patch.DueDate); err != nil {
			return domain.TaskAssignment{}, err
		}
	}
	if patch.Position != nil && *patch.Position < 0 {
		return domain.TaskAssignment{}, taskValidationf(taskScopeAssignment, "position must not be negative")
	}
	var currentStart, currentDue string
	err := db.QueryRowContext(ctx, `SELECT a.start_date, a.due_date FROM task_assignments a
		JOIN task_items i ON i.id = a.item_id WHERE i.profile_id = ? AND a.id = ?`, profileID, id).
		Scan(&currentStart, &currentDue)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskAssignment{}, ErrNotFound
	}
	if err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("read task assignment: %w", err)
	}
	startDate, dueDate := currentStart, currentDue
	if patch.StartDate != nil {
		startDate = *patch.StartDate
	}
	if patch.DueDate != nil {
		dueDate = *patch.DueDate
	}
	if err := validateTaskWindow(taskScopeAssignment, startDate, dueDate); err != nil {
		return domain.TaskAssignment{}, err
	}
	sets := []string{}
	args := []any{}
	if patch.StartDate != nil {
		sets = append(sets, "start_date = ?")
		args = append(args, *patch.StartDate)
	}
	if patch.DueDate != nil {
		sets = append(sets, "due_date = ?")
		args = append(args, *patch.DueDate)
	}
	if patch.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	if len(sets) > 0 {
		// The profile scope was settled by the read above, which answers ErrNotFound
		// for a placement outside it; this statement only needs its own locator.
		args = append(args, id)
		if _, err := db.ExecContext(ctx,
			"UPDATE task_assignments SET "+joinComma(sets)+" WHERE id = ?", args...); err != nil {
			return domain.TaskAssignment{}, fmt.Errorf("update task assignment: %w", err)
		}
	}
	return GetTaskAssignment(ctx, db, profileID, id)
}

// DeleteTaskAssignment lifts one placement; the card stays on the board with its
// other placements untouched.
func DeleteTaskAssignment(ctx context.Context, db *sql.DB, profileID, id string) error {
	itemID, err := taskAssignmentItemID(ctx, db, profileID, id)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin task assignment delete: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM task_assignments WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete task assignment: %w", err)
	}
	if err := syncTaskClosedStamps(ctx, tx, "id = ?", []any{itemID}, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// CompleteTaskAssignment is the card-level 完成 button: it closes this placement's
// steps in one click so the board stops asking for a tick per checkbox. A placement
// with no steps has nothing to close and stays open rather than being stamped
// closed, because 本层已收口 means the checklist ran out, not the button.
func CompleteTaskAssignment(ctx context.Context, db *sql.DB, profileID, id string) (domain.TaskAssignment, error) {
	itemID, err := taskAssignmentItemID(ctx, db, profileID, id)
	if err != nil {
		return domain.TaskAssignment{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("begin task assignment complete: %w", err)
	}
	defer tx.Rollback()
	if err := tickTaskSteps(ctx, tx, "assignment_id = ?", id); err != nil {
		return domain.TaskAssignment{}, err
	}
	if err := syncTaskClosedStamps(ctx, tx, "id = ?", []any{itemID}, formatTime(time.Now().UTC())); err != nil {
		return domain.TaskAssignment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskAssignment{}, fmt.Errorf("commit task assignment complete: %w", err)
	}
	return GetTaskAssignment(ctx, db, profileID, id)
}

// CloseTaskLayer is the layer-wide 完成 action: it closes every placement of one
// 层, which in turn opens the next layer through the chain the read side walks.
func CloseTaskLayer(ctx context.Context, db *sql.DB, profileID, layerID string) (domain.TaskLayer, error) {
	threadID, err := taskLayerThreadID(ctx, db, profileID, layerID)
	if err != nil {
		return domain.TaskLayer{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskLayer{}, fmt.Errorf("begin task layer close: %w", err)
	}
	defer tx.Rollback()
	if err := tickTaskSteps(ctx, tx,
		"assignment_id IN (SELECT id FROM task_assignments WHERE layer_id = ?)", layerID); err != nil {
		return domain.TaskLayer{}, err
	}
	// Only this layer's cards had a placement ticked, but the reconciliation is
	// cheap and covers a card that closed here while still sitting in another open
	// layer as well.
	if err := syncTaskClosedStamps(ctx, tx,
		"profile_id = ? AND id IN (SELECT item_id FROM task_assignments WHERE layer_id = ?)",
		[]any{profileID, layerID}, formatTime(time.Now().UTC())); err != nil {
		return domain.TaskLayer{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskLayer{}, fmt.Errorf("commit task layer close: %w", err)
	}
	return GetTaskThreadLayer(ctx, db, profileID, threadID, layerID)
}

// --- step writes ---

// CreateTaskStep appends a checkbox to one placement.
func CreateTaskStep(ctx context.Context, db *sql.DB, profileID, assignmentID, name string) (domain.TaskStep, error) {
	name = strings.TrimSpace(name)
	if err := validateTaskRequired(taskScopeStep, "name", name, taskTextLimit); err != nil {
		return domain.TaskStep{}, err
	}
	itemID, err := taskAssignmentItemID(ctx, db, profileID, assignmentID)
	if err != nil {
		return domain.TaskStep{}, err
	}
	now := formatTime(time.Now().UTC())
	id := uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskStep{}, fmt.Errorf("begin task step create: %w", err)
	}
	defer tx.Rollback()
	position, err := nextTaskPosition(ctx, tx, "task_steps", "assignment_id = ?", assignmentID)
	if err != nil {
		return domain.TaskStep{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_steps
		(id, assignment_id, name, done, position, done_at, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, '', ?, ?)`, id, assignmentID, name, position, now, now); err != nil {
		return domain.TaskStep{}, fmt.Errorf("create task step: %w", err)
	}
	// Adding the first step is what makes a placement closable at all, and adding
	// an undone step to a closed one takes its 本层已收口 chip away.
	if err := syncTaskClosedStamps(ctx, tx, "id = ?", []any{itemID}, now); err != nil {
		return domain.TaskStep{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskStep{}, fmt.Errorf("commit task step create: %w", err)
	}
	return GetTaskStep(ctx, db, profileID, id)
}

// UpdateTaskStep applies a partial step patch. done_at is stamped when a step flips
// to done and cleared when it flips back, and a patch that repeats the value already
// stored leaves the original stamp alone rather than rewriting when it happened.
func UpdateTaskStep(ctx context.Context, db *sql.DB, profileID, id string, patch domain.TaskStepPatch) (domain.TaskStep, error) {
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		if err := validateTaskRequired(taskScopeStep, "name", trimmed, taskTextLimit); err != nil {
			return domain.TaskStep{}, err
		}
		patch.Name = &trimmed
	}
	if patch.Position != nil && *patch.Position < 0 {
		return domain.TaskStep{}, taskValidationf(taskScopeStep, "position must not be negative")
	}
	var (
		currentDone int
	)
	err := db.QueryRowContext(ctx, `SELECT s.done FROM task_steps s
		JOIN task_assignments a ON a.id = s.assignment_id
		JOIN task_items i ON i.id = a.item_id
		WHERE i.profile_id = ? AND s.id = ?`, profileID, id).Scan(&currentDone)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskStep{}, ErrNotFound
	}
	if err != nil {
		return domain.TaskStep{}, fmt.Errorf("read task step: %w", err)
	}
	assignmentID, err := taskStepAssignmentID(ctx, db, profileID, id)
	if err != nil {
		return domain.TaskStep{}, err
	}
	itemID, err := taskAssignmentItemID(ctx, db, profileID, assignmentID)
	if err != nil {
		return domain.TaskStep{}, err
	}
	now := formatTime(time.Now().UTC())
	sets := []string{}
	args := []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	flipped := patch.Done != nil && *patch.Done != (currentDone != 0)
	if flipped {
		sets = append(sets, "done = ?", "done_at = ?")
		if *patch.Done {
			args = append(args, 1, now)
		} else {
			args = append(args, 0, "")
		}
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, now, id)
		if _, err := db.ExecContext(ctx,
			"UPDATE task_steps SET "+joinComma(sets)+" WHERE id = ?", args...); err != nil {
			return domain.TaskStep{}, fmt.Errorf("update task step: %w", err)
		}
	}
	if flipped {
		// The tick is what the closed chip and the percentage are read from, so a
		// flip reconciles the placement's and the card's display stamps with it.
		if err := syncTaskClosedStamps(ctx, db, "id = ?", []any{itemID}, now); err != nil {
			return domain.TaskStep{}, err
		}
	}
	return GetTaskStep(ctx, db, profileID, id)
}

// DeleteTaskStep drops one checkbox. This is the write that can close a placement by
// subtraction, since removing the only undone step leaves everything else done, so
// it reconciles the stamps like any other step change.
func DeleteTaskStep(ctx context.Context, db *sql.DB, profileID, id string) error {
	assignmentID, err := taskStepAssignmentID(ctx, db, profileID, id)
	if err != nil {
		return err
	}
	itemID, err := taskAssignmentItemID(ctx, db, profileID, assignmentID)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin task step delete: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM task_steps WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete task step: %w", err)
	}
	if err := syncTaskClosedStamps(ctx, tx, "id = ?", []any{itemID}, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// --- shared write helpers ---

// tickTaskSteps marks every undone step selected by where as done. Steps already
// done keep the done_at they were originally ticked at, so closing a layer in one
// click does not rewrite the history of work finished last week.
func tickTaskSteps(ctx context.Context, exec taskExecer, where string, arg any) error {
	now := formatTime(time.Now().UTC())
	if _, err := exec.ExecContext(ctx,
		"UPDATE task_steps SET done = 1, done_at = ?, updated_at = ? WHERE done = 0 AND "+where,
		now, now, arg); err != nil {
		return fmt.Errorf("complete task steps: %w", err)
	}
	return nil
}

// nextTaskPosition returns MAX(position)+1 of the sibling set selected by where, or
// 0 when the set is empty. New rows always append: unlike the research list, where
// the row the user just made is the one they are looking for and lands on top, a
// board card the owner adds is work queued behind what is already on the board.
func nextTaskPosition(ctx context.Context, exec queryer, table, where string, args ...any) (int, error) {
	var position int
	if err := exec.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(position)+1, 0) FROM "+table+" WHERE "+where, args...).Scan(&position); err != nil {
		return 0, fmt.Errorf("compute task %s position: %w", table, err)
	}
	return position, nil
}

// requireResearchPaper checks a card's optional paper link against the profile that
// owns it. A dead id would render as a chip pointing at nothing, and one of someone
// else's papers would leak a title, so both are refused before the write.
func requireResearchPaper(ctx context.Context, db *sql.DB, profileID, paperID string) error {
	var exists bool
	if err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM research_papers WHERE profile_id = ? AND id = ?)", profileID, paperID).
		Scan(&exists); err != nil {
		return fmt.Errorf("check research paper: %w", err)
	}
	if !exists {
		return taskValidationf(taskScopeItem, "paper %s is not in this profile's research workspace", paperID)
	}
	return nil
}

func taskNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
