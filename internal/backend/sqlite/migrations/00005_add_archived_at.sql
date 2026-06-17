-- +goose Up
-- Soft-archive support: tasks can be hidden from default list/inbox views
-- without changing their workflow state. Nullable column distinguishes
-- "not archived" (NULL) from "archived at some explicit moment" — using
-- DEFAULT '' would conflate the two.
ALTER TABLE tasks ADD COLUMN archived_at TEXT NULL;

-- Index supports the common "list active tasks" query that filters
-- WHERE archived_at IS NULL.
CREATE INDEX IF NOT EXISTS idx_tasks_archived_at ON tasks(archived_at);

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_archived_at;
ALTER TABLE tasks DROP COLUMN archived_at;
