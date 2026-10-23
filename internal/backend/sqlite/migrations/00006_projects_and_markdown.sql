-- +goose Up
-- Projects become first-class rows, tasks gain a composite (project_id, id)
-- primary key, and all markdown moves into dedicated tables.
--
-- Existing data is attributed to a single placeholder project whose ticker
-- the application derives from the database's directory name (see
-- placeholderTicker in migrate.go) and injects via a scratch table. That
-- keeps upgrading zero-touch: every existing database works immediately
-- after the binary is updated, with no mandatory command to run first.
--
-- SQLite cannot add a primary-key column in place, so `tasks` is rebuilt:
-- create new, copy, drop, rename. goose runs each migration inside one
-- transaction, so a failure anywhere below leaves the original table intact.
--
-- ORDERING MATTERS: task_descriptions / task_comments / task_deps carry
-- composite foreign keys referencing tasks(project_id, id). Those columns
-- only exist after the rebuilt table is renamed into place, so the swap
-- happens first and the markdown tables are created against the new shape.

-- +goose StatementBegin
CREATE TABLE projects (
    ticker       TEXT PRIMARY KEY,          -- canonical lowercase
    display_name TEXT NOT NULL,             -- 'TM', 'WebApp' — user casing
    name         TEXT NOT NULL DEFAULT '',  -- 'Task Manager for Agents'
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    archived_at  TEXT NULL,
    CHECK (length(ticker) BETWEEN 1 AND 15),
    CHECK (ticker = lower(ticker))
);
-- +goose StatementEnd

-- Insert the placeholder project for the data already in this database.
--
-- tm_migration_placeholder is a single-row scratch table the application
-- creates immediately before running goose, carrying the ticker derived from
-- the directory name. Crucially the application creates it ONLY when the
-- database already holds tasks: a brand-new database must come up with no
-- projects at all, since inventing one named after its directory would
-- fabricate a project the user never asked for (and for the default instance
-- at ~/.tm/local.db that directory is ".tm").
--
-- So the INSERT is driven entirely by the scratch table's existence. The
-- SELECT yields no rows when it is absent, leaving `projects` empty.
-- +goose StatementBegin
INSERT INTO projects (ticker, display_name, name, created_at, updated_at)
SELECT
    ticker,
    display_name,
    '',
    strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM tm_migration_placeholder
LIMIT 1;
-- +goose StatementEnd

-- Stage the markdown and dependency data in FK-free scratch tables while the
-- old `tasks` still exists, then replay it after the swap.
-- +goose StatementBegin
CREATE TABLE tm_stage_descriptions (
    project_id TEXT NOT NULL,
    task_id    TEXT NOT NULL,
    body       TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO tm_stage_descriptions (project_id, task_id, body, updated_at)
SELECT
    (SELECT ticker FROM projects LIMIT 1),
    t.id,
    t.description,
    CASE WHEN t.updated_at = '' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') ELSE t.updated_at END
FROM tasks t
WHERE t.description IS NOT NULL AND t.description != '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE tm_stage_comments (
    id         TEXT NOT NULL,
    project_id TEXT NOT NULL,
    task_id    TEXT NOT NULL,
    who        TEXT NOT NULL,
    body       TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO tm_stage_comments (id, project_id, task_id, who, body)
SELECT c.id, (SELECT ticker FROM projects LIMIT 1), c.task_id, c.who, c.comment
FROM comments c;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE tm_stage_deps (
    project_id    TEXT NOT NULL,
    task_id       TEXT NOT NULL,
    depends_on_id TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO tm_stage_deps (project_id, task_id, depends_on_id)
SELECT (SELECT ticker FROM projects LIMIT 1), d.task_id, d.depends_on_id
FROM task_deps d;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE tm_stage_cursors (
    actor        TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO tm_stage_cursors (actor, project_id, last_seen_at)
SELECT a.actor, (SELECT ticker FROM projects LIMIT 1), a.last_seen_at
FROM actor_cursors a;
-- +goose StatementEnd

-- Rebuild tasks with the composite primary key. The composite key means no
-- existing ID needs rewriting: each old database's tasks keep their numbers
-- and simply gain a project_id, which is what makes folding many databases
-- together tractable. `description` is dropped here — it moves to
-- task_descriptions, replayed from the staging table below.
-- +goose StatementBegin
CREATE TABLE tasks_new (
    project_id     TEXT NOT NULL REFERENCES projects(ticker) ON DELETE CASCADE,
    id             TEXT NOT NULL,
    subject        TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL DEFAULT 'todo',
    assigned_agent TEXT NOT NULL DEFAULT '',
    parent_id      TEXT NOT NULL DEFAULT '',
    labels         TEXT NOT NULL DEFAULT '[]',
    mode           TEXT NOT NULL DEFAULT 'standard',
    created_at     TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL DEFAULT '',
    archived_at    TEXT NULL,
    PRIMARY KEY (project_id, id)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO tasks_new (project_id, id, subject, state, assigned_agent, parent_id, labels, mode, created_at, updated_at, archived_at)
SELECT
    (SELECT ticker FROM projects LIMIT 1),
    t.id, t.subject, t.state, t.assigned_agent, t.parent_id, t.labels, t.mode,
    t.created_at, t.updated_at, t.archived_at
FROM tasks t;
-- +goose StatementEnd

-- events.project_id is deliberately NOT a foreign key: the journal is
-- append-only history and must survive project deletion.
ALTER TABLE events ADD COLUMN project_id TEXT NOT NULL DEFAULT '';

-- +goose StatementBegin
UPDATE events SET project_id = (SELECT ticker FROM projects LIMIT 1);
-- +goose StatementEnd

-- Swap the rebuilt table into place, dropping what it replaced.
DROP INDEX IF EXISTS idx_tasks_updated_at;
DROP INDEX IF EXISTS idx_tasks_parent_id;
DROP INDEX IF EXISTS idx_tasks_mode;
DROP INDEX IF EXISTS idx_tasks_archived_at;
DROP INDEX IF EXISTS idx_task_deps_task_id;
DROP INDEX IF EXISTS idx_comments_task_id;

DROP TABLE comments;
DROP TABLE task_deps;
DROP TABLE actor_cursors;
DROP TABLE tasks;

ALTER TABLE tasks_new RENAME TO tasks;

-- With the new `tasks` in place, the composite foreign keys below resolve.

-- Descriptions live in their own table, one row per task, created lazily — a
-- task with no description gets no row and the repository returns "". List
-- and board queries never need prose, so the hot table stays narrow.
-- +goose StatementBegin
CREATE TABLE task_descriptions (
    project_id TEXT NOT NULL,
    task_id    TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, task_id),
    FOREIGN KEY (project_id, task_id)
        REFERENCES tasks(project_id, id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO task_descriptions (project_id, task_id, body, updated_at)
SELECT project_id, task_id, body, updated_at FROM tm_stage_descriptions;
-- +goose StatementEnd

-- task_comments is the existing comments table renamed, with comment -> body
-- plus project_id and created_at.
--
-- created_at is derived from the comment's own ULID, whose first 10
-- characters are a Crockford-base32 millisecond timestamp. Decoding that is
-- impractical in pure SQL, so the column is seeded empty here and filled by
-- backfillCommentTimestamps (migrate.go) straight after goose finishes.
-- Ordering by ULID works within one database, but rows merged from several
-- databases have no meaningful cross-source ULID order, which is why the
-- explicit column is worth having now.
-- +goose StatementBegin
CREATE TABLE task_comments (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    task_id    TEXT NOT NULL,
    who        TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (project_id, task_id)
        REFERENCES tasks(project_id, id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO task_comments (id, project_id, task_id, who, body, created_at)
SELECT id, project_id, task_id, who, body, '' FROM tm_stage_comments;
-- +goose StatementEnd

-- Deps gain project_id. Same-project by definition.
-- +goose StatementBegin
CREATE TABLE task_deps (
    project_id    TEXT NOT NULL,
    task_id       TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    PRIMARY KEY (project_id, task_id, depends_on_id),
    FOREIGN KEY (project_id, task_id)
        REFERENCES tasks(project_id, id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO task_deps (project_id, task_id, depends_on_id)
SELECT project_id, task_id, depends_on_id FROM tm_stage_deps;
-- +goose StatementEnd

-- Cursors become per (actor, project). A global cursor would mean glancing at
-- one project marks every other project's work as seen, silently losing an
-- agent's inbox across repos. Existing single-row data is attributed to the
-- placeholder project — without that, an actor's first post-upgrade inbox
-- would show every historical event as new.
-- +goose StatementBegin
CREATE TABLE actor_cursors (
    actor        TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (actor, project_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO actor_cursors (actor, project_id, last_seen_at)
SELECT actor, project_id, last_seen_at FROM tm_stage_cursors;
-- +goose StatementEnd

CREATE INDEX idx_tasks_project_updated  ON tasks(project_id, updated_at);
CREATE INDEX idx_tasks_project_parent   ON tasks(project_id, parent_id);
CREATE INDEX idx_tasks_project_mode     ON tasks(project_id, mode);
CREATE INDEX idx_tasks_project_archived ON tasks(project_id, archived_at);
CREATE INDEX idx_task_deps_task         ON task_deps(project_id, task_id);
CREATE INDEX idx_task_comments_task     ON task_comments(project_id, task_id, id);
CREATE INDEX idx_events_project_ts      ON events(project_id, ts);

DROP TABLE tm_stage_descriptions;
DROP TABLE tm_stage_comments;
DROP TABLE tm_stage_deps;
DROP TABLE tm_stage_cursors;
DROP TABLE IF EXISTS tm_migration_placeholder;

-- +goose Down
-- Best-effort, matching the precedent set by 00004: structure reverses, and
-- the description / comment split collapses back into columns. The project
-- attribution is lost (there is only one column to collapse into), so a
-- multi-project database must not be rolled back.
-- +goose StatementBegin
CREATE TABLE tasks_old (
    id             TEXT PRIMARY KEY,
    subject        TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL DEFAULT 'todo',
    assigned_agent TEXT NOT NULL DEFAULT '',
    parent_id      TEXT NOT NULL DEFAULT '',
    labels         TEXT NOT NULL DEFAULT '[]',
    mode           TEXT NOT NULL DEFAULT 'standard',
    created_at     TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL DEFAULT '',
    archived_at    TEXT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT OR IGNORE INTO tasks_old (id, subject, description, state, assigned_agent, parent_id, labels, mode, created_at, updated_at, archived_at)
SELECT t.id, t.subject,
    COALESCE((SELECT d.body FROM task_descriptions d WHERE d.project_id = t.project_id AND d.task_id = t.id), ''),
    t.state, t.assigned_agent, t.parent_id, t.labels, t.mode, t.created_at, t.updated_at, t.archived_at
FROM tasks t;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE comments (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    who     TEXT NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT OR IGNORE INTO comments (id, task_id, who, comment)
SELECT id, task_id, who, body FROM task_comments;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE task_deps_old (
    task_id       TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    PRIMARY KEY (task_id, depends_on_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT OR IGNORE INTO task_deps_old (task_id, depends_on_id)
SELECT task_id, depends_on_id FROM task_deps;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE actor_cursors_old (
    actor        TEXT PRIMARY KEY,
    last_seen_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT OR IGNORE INTO actor_cursors_old (actor, last_seen_at)
SELECT actor, last_seen_at FROM actor_cursors;
-- +goose StatementEnd

DROP INDEX IF EXISTS idx_events_project_ts;
DROP INDEX IF EXISTS idx_task_comments_task;
DROP INDEX IF EXISTS idx_task_deps_task;
DROP INDEX IF EXISTS idx_tasks_project_archived;
DROP INDEX IF EXISTS idx_tasks_project_mode;
DROP INDEX IF EXISTS idx_tasks_project_parent;
DROP INDEX IF EXISTS idx_tasks_project_updated;

DROP TABLE task_descriptions;
DROP TABLE task_comments;
DROP TABLE task_deps;
DROP TABLE actor_cursors;
DROP TABLE tasks;
DROP TABLE projects;

ALTER TABLE tasks_old RENAME TO tasks;
ALTER TABLE task_deps_old RENAME TO task_deps;
ALTER TABLE actor_cursors_old RENAME TO actor_cursors;

ALTER TABLE events DROP COLUMN project_id;

CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_id  ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_mode       ON tasks(mode);
CREATE INDEX IF NOT EXISTS idx_tasks_archived_at ON tasks(archived_at);
CREATE INDEX IF NOT EXISTS idx_task_deps_task_id ON task_deps(task_id);
CREATE INDEX IF NOT EXISTS idx_comments_task_id  ON comments(task_id);
