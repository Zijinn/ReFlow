-- 即时任务的自由标签。标签只参与展示与提示，不做跨任务关联，因此用 JSON 数组
-- 存在任务行里，避免为一份展示字段再建一张多对多表。
ALTER TABLE quick_tasks ADD COLUMN tags TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags));
