-- A quick task can point back to the paper whose 下一步行动 created it. The
-- link is optional (manual tasks stay unlinked) and SET NULL so deleting the
-- paper does not delete work the owner still has to do.
ALTER TABLE quick_tasks ADD COLUMN paper_id TEXT REFERENCES research_papers(id) ON DELETE SET NULL;

CREATE INDEX idx_quick_tasks_paper ON quick_tasks(profile_id, paper_id);
