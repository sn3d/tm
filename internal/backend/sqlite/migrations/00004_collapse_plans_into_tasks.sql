-- +goose Up
-- Move every plan into the tasks table as a planning-mode root task.
-- State mapping (plan -> task): draft -> draft, active -> in_progress,
-- on_hold -> blocked, completed -> done, cancelled -> cancelled.
-- The shared numeric counter guarantees plan IDs and task IDs never overlap,
-- so insertion is collision-free.
INSERT INTO tasks (id, subject, description, state, assigned_agent, plan_id, parent_id, labels, mode, created_at, updated_at)
SELECT
    p.id,
    p.subject,
    p.description,
    CASE p.state
        WHEN 'draft'     THEN 'draft'
        WHEN 'active'    THEN 'in_progress'
        WHEN 'on_hold'   THEN 'blocked'
        WHEN 'completed' THEN 'done'
        WHEN 'cancelled' THEN 'cancelled'
        ELSE 'draft'
    END,
    p.assigned_agent,
    '',
    '',
    '[]',
    'planning',
    p.created_at,
    p.updated_at
FROM plans p;

-- Reparent existing child tasks: whatever was plan_id is now parent_id.
UPDATE tasks SET parent_id = plan_id WHERE plan_id != '' AND parent_id = '';

-- Move plan comments into the unified comments table. Each plan comment
-- now belongs to the task that absorbed its plan.
INSERT INTO comments (id, task_id, who, comment)
SELECT id, plan_id, who, comment FROM plan_comments;

-- Drop the now-redundant plan_id index and column from tasks. Going forward
-- the hierarchy lives entirely on parent_id.
DROP INDEX IF EXISTS idx_tasks_plan_id;
ALTER TABLE tasks DROP COLUMN plan_id;

-- Drop the plan tables entirely.
DROP TABLE IF EXISTS plan_comments;
DROP TABLE IF EXISTS plans;

-- Drop the plan_id column and its index from events. Historical plan_id
-- references in old journal rows are lost; the entity they pointed at now
-- exists as a task with the same numeric ID, so events that need that
-- reference can be re-derived from event payloads if needed.
DROP INDEX IF EXISTS idx_events_plan;
ALTER TABLE events DROP COLUMN plan_id;

-- +goose Down
-- Reversing the collapse is best-effort. plan_comments and plans tables are
-- recreated empty, plan_id is added back to tasks, but state mappings are
-- not reversed (in_progress/blocked/done could each come from multiple plan
-- states). Use with caution — primarily here so goose can roll back during
-- local development.
CREATE TABLE IF NOT EXISTS plans (
    id             TEXT    PRIMARY KEY,
    subject        TEXT    NOT NULL DEFAULT '',
    description    TEXT    NOT NULL DEFAULT '',
    state          TEXT    NOT NULL DEFAULT 'draft',
    assigned_agent TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL DEFAULT '',
    updated_at     TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS plan_comments (
    id      TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    who     TEXT NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_plan_comments_plan_id ON plan_comments(plan_id);

ALTER TABLE tasks ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tasks_plan_id ON tasks(plan_id);

ALTER TABLE events ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_events_plan ON events(plan_id, ts);
