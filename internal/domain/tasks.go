package domain

// 长任务 (long-task) board: the second module of the 个人工作台, sitting beside the
// untouched 科研工作台. The shape is 主线 (thread) → 层 (layer) → 任务 (item) → 步
// (step), where a layer is a sequential unlock gate and one item can be placed in
// several layers at once through an assignment.
//
// Every status below is derived on read and never stored, so these structs carry
// both the stored columns and the computed fields the board paints from them.

// Item statuses, reported by TaskItem.Status. Exactly one applies, and the order
// the server tests them in is closed → locked → overdue → unstarted → active, so
// a task that is both overdue and sitting in a locked layer reports locked.
const (
	TaskStatusActive    = "active"
	TaskStatusLocked    = "locked"
	TaskStatusUnstarted = "unstarted"
	TaskStatusOverdue   = "overdue"
	TaskStatusClosed    = "closed"
)

// TaskStep is one checkbox inside a placement. DoneAt is a display stamp the
// storage layer writes when Done flips and clears when it flips back.
type TaskStep struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Position int    `json:"position"`
	DoneAt   string `json:"done_at"`
}

// TaskAssignment is one item placed in one layer: the 跨 N 层 chip counts these,
// and each carries its own date window and its own 本层已收口 state.
type TaskAssignment struct {
	ID string `json:"id"`
	// LayerPosition lets the client sort a card's placements by layer order
	// without a second lookup into the layer list.
	LayerPosition int        `json:"layer_position"`
	LayerID       string     `json:"layer_id"`
	Position      int        `json:"position"`
	StartDate     string     `json:"start_date"`
	DueDate       string     `json:"due_date"`
	Closed        bool       `json:"closed"`
	ClosedAt      string     `json:"closed_at"`
	Steps         []TaskStep `json:"steps"`
}

// TaskItemStats is the checklist tally behind the circular badge: total steps and
// done steps across *every* placement of the item, not just this layer's.
type TaskItemStats struct {
	TotalSteps int `json:"total_steps"`
	DoneSteps  int `json:"done_steps"`
}

// TaskItem is one board card.
type TaskItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Position int    `json:"position"`
	// PaperID links the card to a 科研工作台 paper; nil means unlinked. A patch
	// distinguishes "not mentioned" (nil, no flag) from "unlink" (explicit null).
	PaperID    *string `json:"paper_id"`
	ClosedAt   string  `json:"closed_at"`
	Closed     bool    `json:"closed"`
	LayerCount int     `json:"layer_count"`
	// Progress is 0-100 over the whole item, deliberately global across layers:
	// a task that is 2/2 in layer one and 0/3 in layer two shows 40 on both
	// cards, because it is one task, not two.
	Progress int           `json:"progress"`
	Status   string        `json:"status"`
	Stats    TaskItemStats `json:"stats"`
	// Assignments are ordered by their layer's position.
	Assignments []TaskAssignment `json:"assignments"`
}

// TaskLayer is one 层 block and its unlock state. Locked layers stay writable —
// the lock is a status the pill paints, not a gate.
type TaskLayer struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Position     int    `json:"position"`
	Unlocked     bool   `json:"unlocked"`
	ForcedUnlock bool   `json:"forced_unlock"`
	// ItemCount and ClosedCount count placements in this layer, which is what
	// the pill renders as "✓ 第一层 3/3" or "🔒 第三层 0/3".
	ItemCount   int `json:"item_count"`
	ClosedCount int `json:"closed_count"`
}

// TaskStats backs the five-cell strip and the "3/13" in the thread header. The
// five status counts are over distinct items and sum to Total.
type TaskStats struct {
	Active      int `json:"active"`
	Locked      int `json:"locked"`
	Unstarted   int `json:"unstarted"`
	Overdue     int `json:"overdue"`
	Closed      int `json:"closed"`
	Total       int `json:"total"`
	Slots       int `json:"slots"`
	SlotsClosed int `json:"slots_closed"`
}

// TaskThread is a 主线 and the whole tree under it. The board is a personal tool
// with tens of rows, so GET /tasks/threads returns everything in one document.
type TaskThread struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Archived bool   `json:"archived"`
	// MapMarkdown is the source the mind-map view was imported from. The tree
	// below stays authoritative; this is only the editable outline.
	MapMarkdown string      `json:"map_markdown"`
	Layers      []TaskLayer `json:"layers"`
	Items       []TaskItem  `json:"items"`
	Stats       TaskStats   `json:"stats"`
}

// TaskThreadPatch carries optional thread updates; nil fields are untouched.
type TaskThreadPatch struct {
	Name        *string
	Position    *int
	Archived    *bool
	MapMarkdown *string
}

// QuickTask is one 即时任务: a flat, unlayered task with its own date window.
// It intentionally does not participate in the layer unlock chain.
type QuickTask struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Notes       string  `json:"notes"`
	StartDate   string  `json:"start_date"`
	DueDate     string  `json:"due_date"`
	PaperID     *string `json:"paper_id"`
	CompletedAt string  `json:"completed_at"`
	Completed   bool    `json:"completed"`
	Position    int     `json:"position"`
}

// QuickTaskPatch carries optional quick-task updates; nil fields are untouched.
// Completed flips the stored stamp, so a client never has to send completed_at.
type QuickTaskPatch struct {
	Title     *string
	Notes     *string
	StartDate *string
	DueDate   *string
	Completed *bool
	Position  *int
}

// TaskLayerPatch carries optional layer updates; nil fields are untouched.
type TaskLayerPatch struct {
	Name         *string
	Position     *int
	ForcedUnlock *bool
}

// TaskItemPatch carries optional item updates. SetPaperID says the request
// mentioned paper_id at all; PaperID is nil then only when the client sent an
// explicit null, which unlinks the card from its paper.
type TaskItemPatch struct {
	Title      *string
	Notes      *string
	Position   *int
	SetPaperID bool
	PaperID    *string
}

// TaskAssignmentPatch carries optional placement updates; nil fields are
// untouched.
type TaskAssignmentPatch struct {
	StartDate *string
	DueDate   *string
	Position  *int
}

// TaskStepPatch carries optional step updates; nil fields are untouched.
type TaskStepPatch struct {
	Name     *string
	Done     *bool
	Position *int
}
