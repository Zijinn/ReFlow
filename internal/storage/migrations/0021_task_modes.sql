-- 任务管理的两种任务：即时任务（quick_tasks）与长期任务（现有的主线 → 层 → 任务）。
-- 即时任务不挂层、不参与解锁链，只有自己的日期窗口和完成时间；时间线把它们和
-- 长期任务的 assignment 放在同一条周/月轴上读。
CREATE TABLE quick_tasks (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    -- 和 task_assignments 一样是 '' 或日历日 YYYY-MM-DD，日期比较只在显示层做。
    start_date TEXT NOT NULL DEFAULT '',
    due_date TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE INDEX idx_quick_tasks_profile ON quick_tasks(profile_id, position);

-- 脑图的 Markdown 源。长期任务树仍然是权威结构；这一列只保存用户写下的
-- Markdown，供脑图视图重新载入和再导出，不参与状态推导。
ALTER TABLE task_threads ADD COLUMN map_markdown TEXT NOT NULL DEFAULT '';
