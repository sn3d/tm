-- +goose Up
ALTER TABLE tasks ADD COLUMN created_at TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN created_at TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at);
CREATE INDEX IF NOT EXISTS idx_plans_updated_at ON plans(updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_plans_updated_at;
DROP INDEX IF EXISTS idx_tasks_updated_at;

ALTER TABLE plans DROP COLUMN updated_at;
ALTER TABLE plans DROP COLUMN created_at;
ALTER TABLE tasks DROP COLUMN updated_at;
ALTER TABLE tasks DROP COLUMN created_at;
