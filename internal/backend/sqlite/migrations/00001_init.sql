-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS tasks (
    id             TEXT    PRIMARY KEY,
    subject        TEXT    NOT NULL DEFAULT '',
    description    TEXT    NOT NULL DEFAULT '',
    state          TEXT    NOT NULL DEFAULT 'todo',
    assigned_agent TEXT    NOT NULL DEFAULT '',
    plan_id        TEXT    NOT NULL DEFAULT ''
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_tasks_plan_id ON tasks(plan_id);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS task_deps (
    task_id       TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    PRIMARY KEY (task_id, depends_on_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_task_deps_task_id ON task_deps(task_id);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS comments (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    who     TEXT NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_comments_task_id ON comments(task_id);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS plans (
    id             TEXT    PRIMARY KEY,
    subject        TEXT    NOT NULL DEFAULT '',
    description    TEXT    NOT NULL DEFAULT '',
    state          TEXT    NOT NULL DEFAULT 'draft',
    assigned_agent TEXT    NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS plan_comments (
    id      TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    who     TEXT NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_plan_comments_plan_id ON plan_comments(plan_id);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS events (
    id      TEXT PRIMARY KEY,
    ts      TEXT NOT NULL,
    actor   TEXT NOT NULL DEFAULT '',
    kind    TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    plan_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}'
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_events_ts    ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_task  ON events(task_id, ts);
CREATE INDEX IF NOT EXISTS idx_events_plan  ON events(plan_id, ts);
CREATE INDEX IF NOT EXISTS idx_events_actor ON events(actor, ts);

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS actor_cursors (
    actor        TEXT PRIMARY KEY,
    last_seen_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS actor_cursors;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS plan_comments;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS task_deps;
DROP TABLE IF EXISTS tasks;
