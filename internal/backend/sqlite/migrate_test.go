package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

// buildV5DB creates a database at schema version 5 (pre-projects) with real
// data in every table 00006 has to move.

// TestMigration00006_MovesAllData builds a schema-version-5 database by hand
// (bypassing our Go code entirely, so the test cannot be fooled by a bug
// shared with the writer) and verifies 00006 relocates every kind of row it
// is responsible for.
func TestMigration00006_MovesAllData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "-Users-me-unravela-tm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "tm.db")

	// Hand-build a v5 schema database, bypassing our Go code entirely.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	schema := `
CREATE TABLE tasks (id TEXT PRIMARY KEY, subject TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'todo', assigned_agent TEXT NOT NULL DEFAULT '', parent_id TEXT NOT NULL DEFAULT '',
  labels TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'standard', created_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '', archived_at TEXT NULL);
CREATE TABLE task_deps (task_id TEXT NOT NULL, depends_on_id TEXT NOT NULL, PRIMARY KEY (task_id, depends_on_id),
  FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE);
CREATE TABLE comments (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  who TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '');
CREATE TABLE events (id TEXT PRIMARY KEY, ts TEXT NOT NULL, actor TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
  task_id TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '{}');
CREATE TABLE actor_cursors (actor TEXT PRIMARY KEY, last_seen_at TEXT NOT NULL);
CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT, version_id INTEGER NOT NULL, is_applied INTEGER NOT NULL, tstamp TIMESTAMP DEFAULT (datetime('now')));
INSERT INTO goose_db_version (version_id, is_applied) VALUES (0,1),(1,1),(2,1),(3,1),(4,1),(5,1);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO tasks (id, subject, description, state, created_at, updated_at) VALUES
		('1','first','# Body one','todo',?,?), ('2','second','','in_progress',?,?), ('3','third','Body three','done',?,?)`,
		now, now, now, now, now, now); err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET parent_id='1' WHERE id='2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO task_deps (task_id, depends_on_id) VALUES ('3','1')`); err != nil {
		t.Fatal(err)
	}

	cid := ulid.Make().String()
	wantCommentTime := ulid.Time(ulid.MustParseStrict(cid).Time()).UTC()
	if _, err := db.Exec(`INSERT INTO comments (id, task_id, who, comment) VALUES (?, '1', 'zdenko', 'a comment')`, cid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, ts, actor, kind, task_id, payload) VALUES (?, ?, 'zdenko','task.created','1','{}')`, ulid.Make().String(), now); err != nil {
		t.Fatal(err)
	}
	cursorTS := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO actor_cursors (actor, last_seen_at) VALUES ('zdenko', ?)`, cursorTS); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Now open through NewBackend, which runs 00006.
	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend (migrate): %v", err)
	}

	ps, err := b.Projects().GetAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Ticker != "tm" {
		t.Fatalf("projects = %+v, want one ticker tm", ps)
	}

	ref := client.TaskRef{Project: "tm", ID: "1"}
	got, err := b.Tasks().GetByID(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("task tm/#1 missing after migration")
	}
	if got.Description != "# Body one" {
		t.Errorf("description = %q, want %q", got.Description, "# Body one")
	}
	if got.Subject != "first" {
		t.Errorf("subject = %q", got.Subject)
	}

	// Task 2 keeps its parent, re-qualified.
	t2, err := b.Tasks().GetByID(client.TaskRef{Project: "tm", ID: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if t2.ParentID != ref {
		t.Errorf("parent = %+v, want %+v", t2.ParentID, ref)
	}

	// Task 3 keeps its dependency.
	t3, err := b.Tasks().GetByID(client.TaskRef{Project: "tm", ID: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(t3.DependsOn) != 1 || t3.DependsOn[0] != ref {
		t.Errorf("deps = %+v", t3.DependsOn)
	}

	// Task 2 had no description -> no row -> "".
	if t2.Description != "" {
		t.Errorf("task 2 description = %q, want empty", t2.Description)
	}

	// Comment moved with ULID-derived created_at.
	cs, err := b.Comments().GetForTask(ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("comments = %d, want 1", len(cs))
	}
	if cs[0].Comment != "a comment" {
		t.Errorf("body = %q", cs[0].Comment)
	}
	if !cs[0].CreatedAt.Equal(wantCommentTime) {
		t.Errorf("created_at = %v, want ULID time %v", cs[0].CreatedAt, wantCommentTime)
	}

	// Cursor attributed to the placeholder project.
	cur, err := b.ActorCursors().Get("zdenko", "tm")
	if err != nil {
		t.Fatal(err)
	}
	if cur.IsZero() {
		t.Error("cursor lost; actor's first inbox would show all history as new")
	}

	// Events backfilled with project.
	evs, err := b.Events().List(client.EventFilter{Project: "tm"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	if evs[0].TaskID != ref {
		t.Errorf("event task = %+v, want %+v", evs[0].TaskID, ref)
	}
}

// TestMigration00006_FreshDatabaseHasNoProjects guards a bug this suite
// caught: 00006 used to seed a placeholder project named after the database's
// directory unconditionally. For the default instance at ~/.tm/local.db that
// directory is ".tm", so every fresh install came up with a fabricated "tm"
// project — which then collided with the first `tm migrate` import and made it
// report "already imported".
//
// A brand-new database must come up with an EMPTY registry; `tm init` creates
// the first real project.
func TestMigration00006_FreshDatabaseHasNoProjects(t *testing.T) {
	b := newEmptyBackend(t)

	projects, err := b.Projects().GetAll(true)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("a fresh database must have no projects, got %+v", projects)
	}
}

// TestMigration00006_DotTmDirectoryDoesNotInventProject is the specific
// shape of the bug above: a database inside a directory literally named
// ".tm" (the real default instance path) must not gain a project.
func TestMigration00006_DotTmDirectoryDoesNotInventProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".tm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := NewBackend(filepath.Join(dir, "local.db"))
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	projects, err := b.Projects().GetAll(true)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("~/.tm/local.db must not gain a project from its directory name, got %+v", projects)
	}
}

// TestMigration00006_ExistingDataStillGetsPlaceholder is the other half: a
// database that DOES hold tasks must still get its zero-touch placeholder, or
// upgrading would orphan every existing row.
func TestMigration00006_ExistingDataStillGetsPlaceholder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "-Users-me-unravela-billing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "tm.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE tasks (id TEXT PRIMARY KEY, subject TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'todo',
			assigned_agent TEXT NOT NULL DEFAULT '', parent_id TEXT NOT NULL DEFAULT '',
			labels TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'standard',
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
			archived_at TEXT NULL);
		CREATE TABLE task_deps (task_id TEXT NOT NULL, depends_on_id TEXT NOT NULL,
			PRIMARY KEY (task_id, depends_on_id),
			FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE);
		CREATE TABLE comments (id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			who TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '');
		CREATE TABLE events (id TEXT PRIMARY KEY, ts TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
			task_id TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '{}');
		CREATE TABLE actor_cursors (actor TEXT PRIMARY KEY, last_seen_at TEXT NOT NULL);
		CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL, is_applied INTEGER NOT NULL,
			tstamp TIMESTAMP DEFAULT (datetime('now')));
		INSERT INTO goose_db_version (version_id, is_applied) VALUES (0,1),(1,1),(2,1),(3,1),(4,1),(5,1);
		INSERT INTO tasks (id, subject) VALUES ('1', 'existing work');
	`); err != nil {
		t.Fatalf("seed v5 database: %v", err)
	}
	_ = db.Close()

	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	projects, err := b.Projects().GetAll(true)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("a database with tasks must get exactly one placeholder, got %+v", projects)
	}
	if projects[0].Ticker != "billing" {
		t.Errorf("placeholder ticker = %q, want %q derived from the directory", projects[0].Ticker, "billing")
	}
	// And the existing row is attributed to it.
	got, err := b.Tasks().GetByID(client.TaskRef{Project: "billing", ID: "1"})
	if err != nil || got == nil {
		t.Fatalf("existing task should be attributed to the placeholder: %v (got %+v)", err, got)
	}
	if got.Subject != "existing work" {
		t.Errorf("subject = %q", got.Subject)
	}
}
