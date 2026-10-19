-- 长任务 board: the second module of the 个人工作台, next to the untouched 科研工作台.
-- The shape is 主线 (thread) → 层 (layer) → 任务 (item) → 步 (step).
--
-- A layer is a *sequential unlock gate*, not a folder: layer N unlocks when every
-- assignment of layer N-1 has closed (or when the user forces it). The lock is a
-- status the board paints, never a write gate, so nothing here blocks inserting a
-- task into a locked layer — the reference board keeps work queued up in 第三层.
--
-- Items reach layers through task_assignments instead of a layer_id column because
-- one task genuinely sits in two layers at once (the 跨 2 层 chip), and each of
-- those placements has its own 10/12 – 10/31 date window and its own 完成 button.
--
-- Nothing derived is stored as truth: locked/unlocked, every closed/overdue
-- status and each percentage is computed on read from the step rows. The two
-- closed_at columns are display stamps only — the storage layer rewrites them to
-- agree with the steps after each write, and the boolean never reads them.
--
-- profile_id rides on thread, layer and item the way research_papers carries it,
-- so every query in the storage layer is a plain "WHERE profile_id = ?" scope
-- check instead of a join back to the thread. Assignments and steps are children
-- of one item/layer pair and inherit their scope through the FK.
CREATE TABLE task_threads (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    -- Archived threads keep their rows (and their history) but leave the board;
    -- a delete would throw away finished work the owner wants to be able to
    -- reopen, which is the whole point of the flag.
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE TABLE task_layers (
    id TEXT PRIMARY KEY,
    -- Dropping a thread drops its layers, and dropping a layer drops the
    -- placements below it: a placement that names no layer would render as a
    -- card floating outside every 层 block.
    thread_id TEXT NOT NULL REFERENCES task_threads(id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    -- position is the sequence the unlock chain walks, so it is data, not a
    -- created_at tie-break.
    position INTEGER NOT NULL DEFAULT 0,
    -- The escape hatch for a layer whose predecessor is legitimately stuck: a
    -- forced unlock opens it without pretending the previous layer closed.
    forced_unlock INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE INDEX idx_task_layers_thread ON task_layers(thread_id, position);

CREATE TABLE task_items (
    id TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL REFERENCES task_threads(id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    -- A board card can be pinned to a 科研工作台 paper. Nullable rather than
    -- DEFAULT '' because the FK action below is SET NULL: deleting the paper
    -- must unlink the card, not delete it, since the task may outlive the paper.
    paper_id TEXT REFERENCES research_papers(id) ON DELETE SET NULL,
    closed_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE INDEX idx_task_items_thread ON task_items(thread_id, position);

CREATE TABLE task_assignments (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES task_items(id) ON DELETE CASCADE,
    layer_id TEXT NOT NULL REFERENCES task_layers(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    -- Date windows are free text ('' or YYYY-MM-DD) exactly like the research
    -- table's deadline/submission_date: the board renders "10/12 – 10/31" and
    -- nothing here does date arithmetic beyond comparing to today.
    start_date TEXT NOT NULL DEFAULT '',
    due_date TEXT NOT NULL DEFAULT '',
    closed_at TEXT NOT NULL DEFAULT '',
    -- One placement per (item, layer): a second row for the same pairing would
    -- give the same card two conflicting 本层 progress numbers.
    UNIQUE (item_id, layer_id)
) STRICT;

CREATE INDEX idx_task_assignments_layer ON task_assignments(layer_id);

CREATE TABLE task_steps (
    id TEXT PRIMARY KEY,
    -- Steps belong to a placement, not to the item: the same task in two layers
    -- ticks a different checklist in each, and 本层已收口 means *this* layer's
    -- steps are all done.
    assignment_id TEXT NOT NULL REFERENCES task_assignments(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    done INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL DEFAULT 0,
    done_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE INDEX idx_task_steps_assignment ON task_steps(assignment_id, position);
