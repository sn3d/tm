package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigration00006_IsAtomic is the safety property the design calls out:
// 00006 rebuilds the tasks table (create / copy / drop / rename) because
// SQLite cannot add a primary-key column in place, so a failure part-way
// through MUST leave the original table intact.
//
// The failure is induced the way a real one would arrive — a constraint
// violation inside the migration — by seeding a task id that the rebuilt
// table's composite primary key cannot accept twice. goose runs each
// migration in one transaction, so the whole thing must roll back.
func TestMigration00006_IsAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "-Users-me-unravela-tm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "tm.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// A v5 database whose `tasks` table has NO primary key, so it can hold
	// two rows with the same id. The rebuilt table's composite PK rejects the
	// duplicate, failing the migration mid-flight.
	if _, err := db.Exec(`
		CREATE TABLE tasks (id TEXT, subject TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'todo',
			assigned_agent TEXT NOT NULL DEFAULT '', parent_id TEXT NOT NULL DEFAULT '',
			labels TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'standard',
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
			archived_at TEXT NULL);
		CREATE TABLE task_deps (task_id TEXT NOT NULL, depends_on_id TEXT NOT NULL);
		CREATE TABLE comments (id TEXT PRIMARY KEY, task_id TEXT NOT NULL,
			who TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '');
		CREATE TABLE events (id TEXT PRIMARY KEY, ts TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
			task_id TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '{}');
		CREATE TABLE actor_cursors (actor TEXT PRIMARY KEY, last_seen_at TEXT NOT NULL);
		CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL, is_applied INTEGER NOT NULL,
			tstamp TIMESTAMP DEFAULT (datetime('now')));
		INSERT INTO goose_db_version (version_id, is_applied) VALUES (0,1),(1,1),(2,1),(3,1),(4,1),(5,1);
		INSERT INTO tasks (id, subject, description) VALUES ('1', 'keep me', 'important body');
		INSERT INTO tasks (id, subject, description) VALUES ('1', 'duplicate', 'also important');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	// The migration must fail...
	if _, err := NewBackend(dbPath); err == nil {
		t.Fatal("expected the migration to fail on the duplicate primary key")
	}

	// ...and the ORIGINAL data must survive untouched. This is the whole
	// point: a half-applied rebuild that dropped `tasks` would be data loss.
	check, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()

	var taskCount int
	if err := check.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&taskCount); err != nil {
		t.Fatalf("the original tasks table must still exist: %v", err)
	}
	if taskCount != 2 {
		t.Errorf("original tasks table holds %d rows, want 2 (rollback must preserve them)", taskCount)
	}

	// The descriptions must still be in the old column, not half-moved.
	var subject, description string
	if err := check.QueryRow(
		`SELECT subject, description FROM tasks WHERE subject = 'keep me'`).Scan(&subject, &description); err != nil {
		t.Fatalf("original row lost: %v", err)
	}
	if description != "important body" {
		t.Errorf("description = %q, want it preserved in the original column", description)
	}

	// And the new tables must NOT exist — a partial migration is the failure
	// mode this test exists to rule out.
	for _, table := range []string{"projects", "task_descriptions", "task_comments", "tasks_new"} {
		var n int
		if err := check.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("table %q exists after a failed migration; the rebuild was not atomic", table)
		}
	}

	// The schema version must still be 5, so a later fixed run retries 00006.
	var version int64
	if err := check.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Errorf("goose version = %d after a failed migration, want 5", version)
	}
}

// TestMigration00006_FailureMessageIsUseful checks the error surfaces rather
// than being swallowed, so a user whose upgrade fails learns why.
func TestMigration00006_FailureMessageIsUseful(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "-Users-me-unravela-tm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "tm.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE tasks (id TEXT, subject TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'todo',
			assigned_agent TEXT NOT NULL DEFAULT '', parent_id TEXT NOT NULL DEFAULT '',
			labels TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'standard',
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
			archived_at TEXT NULL);
		CREATE TABLE task_deps (task_id TEXT NOT NULL, depends_on_id TEXT NOT NULL);
		CREATE TABLE comments (id TEXT PRIMARY KEY, task_id TEXT NOT NULL,
			who TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '');
		CREATE TABLE events (id TEXT PRIMARY KEY, ts TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
			task_id TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '{}');
		CREATE TABLE actor_cursors (actor TEXT PRIMARY KEY, last_seen_at TEXT NOT NULL);
		CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL, is_applied INTEGER NOT NULL,
			tstamp TIMESTAMP DEFAULT (datetime('now')));
		INSERT INTO goose_db_version (version_id, is_applied) VALUES (0,1),(1,1),(2,1),(3,1),(4,1),(5,1);
		INSERT INTO tasks (id, subject) VALUES ('1', 'a');
		INSERT INTO tasks (id, subject) VALUES ('1', 'b');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	_, err = NewBackend(dbPath)
	if err == nil {
		t.Fatal("expected an error")
	}
	// The migration name should appear so the user knows which step failed.
	if !strings.Contains(err.Error(), "00006") {
		t.Errorf("error should name the failing migration, got: %v", err)
	}
}
