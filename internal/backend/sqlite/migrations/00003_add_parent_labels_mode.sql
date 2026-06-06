-- +goose Up
ALTER TABLE tasks ADD COLUMN parent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN labels    TEXT NOT NULL DEFAULT '[]';
ALTER TABLE tasks ADD COLUMN mode      TEXT NOT NULL DEFAULT 'standard';

CREATE INDEX IF NOT EXISTS idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_mode      ON tasks(mode);

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_mode;
DROP INDEX IF EXISTS idx_tasks_parent_id;

ALTER TABLE tasks DROP COLUMN mode;
ALTER TABLE tasks DROP COLUMN labels;
ALTER TABLE tasks DROP COLUMN parent_id;
