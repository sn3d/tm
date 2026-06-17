package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func newTempRepo(t *testing.T) client.TasksRepository {
	t.Helper()
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	return b.Tasks()
}

func TestTasksRepository_Create_AssignsID(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)
	task := client.Task{
		Subject:       "write tests",
		Description:   "cover the sqlite repo",
		State:         client.TaskStateTodo,
		AssignedAgent: "go-developer",
	}

	// Act
	err := repo.Save(&task)

	// Assert
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected Save to assign a non-empty ID")
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Subject != task.Subject || got.Description != task.Description ||
		got.State != task.State || got.AssignedAgent != task.AssignedAgent {
		t.Errorf("stored task mismatch: %+v", got)
	}
}

func TestTasksRepository_Create_AssignsUniqueIDs(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)
	first := client.Task{Subject: "a"}
	second := client.Task{Subject: "b"}

	// Act
	if err := repo.Save(&first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := repo.Save(&second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	// Assert
	if first.ID == "" || second.ID == "" {
		t.Fatalf("expected non-empty IDs, got first=%q second=%q", first.ID, second.ID)
	}
	if first.ID == second.ID {
		t.Errorf("expected unique IDs, got both %q", first.ID)
	}
}

func TestTasksRepository_GetByID(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)
	task := client.Task{ID: "fixed-id-1", Subject: "find me", State: client.TaskStateInProgress, AssignedAgent: "agent-a"}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Act
	got, err := repo.GetByID("fixed-id-1")

	// Assert
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.ID != "fixed-id-1" || got.Subject != "find me" || got.State != client.TaskStateInProgress || got.AssignedAgent != "agent-a" {
		t.Errorf("unexpected task: %+v", got)
	}
}

func TestTasksRepository_GetByID_NotFound(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)

	// Act
	got, err := repo.GetByID("does-not-exist")

	// Assert
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing task, got %+v", got)
	}
}

func TestTasksRepository_GetAll(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)
	seed := []client.Task{
		{ID: "task-1", Subject: "first", State: client.TaskStateTodo, AssignedAgent: "a"},
		{ID: "task-2", Subject: "second", State: client.TaskStateInProgress, AssignedAgent: "b"},
		{ID: "task-3", Subject: "third", State: client.TaskStateDone, AssignedAgent: "c"},
	}
	for i := range seed {
		if err := repo.Save(&seed[i]); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// Act
	got, err := repo.GetAll()

	// Assert
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(got) != len(seed) {
		t.Fatalf("expected %d tasks, got %d", len(seed), len(got))
	}
	// GetAll orders by UpdatedAt DESC, so the most recently saved task comes
	// first — i.e. the reverse of insertion order here.
	for i, task := range got {
		want := seed[len(seed)-1-i]
		if task.Subject != want.Subject || task.AssignedAgent != want.AssignedAgent {
			t.Errorf("task %d mismatch: got %+v, want %+v", i, task, want)
		}
	}
}

func TestTasksRepository_ParentID_RoundTrip(t *testing.T) {
	b := newTempBackend(t)
	parent := client.Task{ID: "parent-1", Subject: "container", State: client.TaskStateDraft, Mode: client.TaskModePlanning}
	if err := b.Tasks().Save(&parent); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	task := client.Task{ID: "task-1", Subject: "under parent", State: client.TaskStateTodo, ParentID: "parent-1"}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := b.Tasks().GetByID("task-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.ParentID != "parent-1" {
		t.Errorf("expected ParentID=parent-1, got %q", got.ParentID)
	}
}

func TestTasksRepository_GetByParent(t *testing.T) {
	b := newTempBackend(t)
	for _, id := range []string{"parent-1", "parent-2"} {
		if err := b.Tasks().Save(&client.Task{ID: id, Subject: id, State: client.TaskStateDraft, Mode: client.TaskModePlanning}); err != nil {
			t.Fatalf("seed parent %q: %v", id, err)
		}
	}
	seed := []client.Task{
		{ID: "t-a", Subject: "a", State: client.TaskStateTodo, ParentID: "parent-1"},
		{ID: "t-b", Subject: "b", State: client.TaskStateTodo, ParentID: "parent-1"},
		{ID: "t-c", Subject: "c", State: client.TaskStateTodo, ParentID: "parent-2"},
		{ID: "t-d", Subject: "d", State: client.TaskStateTodo}, // top-level
	}
	for i := range seed {
		if err := b.Tasks().Save(&seed[i]); err != nil {
			t.Fatalf("seed task %q: %v", seed[i].ID, err)
		}
	}

	got, err := b.Tasks().GetByParent("parent-1")
	if err != nil {
		t.Fatalf("GetByParent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks for parent-1, got %d", len(got))
	}
	for _, task := range got {
		if task.ParentID != "parent-1" {
			t.Errorf("expected ParentID=parent-1, got %q for task %q", task.ParentID, task.ID)
		}
	}

	topLevel, err := b.Tasks().GetByParent("")
	if err != nil {
		t.Fatalf("GetByParent(empty): %v", err)
	}
	if len(topLevel) != 1 || topLevel[0].ID != "t-d" {
		// parent-1 and parent-2 are also top-level by ParentID definition,
		// so 3 top-level rows is correct. Recompute the expectation.
		want := map[string]bool{"t-d": true, "parent-1": true, "parent-2": true}
		got := map[string]bool{}
		for _, t := range topLevel {
			got[t.ID] = true
		}
		for id := range want {
			if !got[id] {
				t.Errorf("missing top-level task %q (got %v)", id, topLevel)
			}
		}
	}
}

// A pre-existing tasks table without created_at/updated_at columns must
// migrate cleanly: legacy rows survive with empty timestamps and new saves
// stamp them.
func TestNewBackend_MigratesTasksAddTimestamps(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-ts-migration-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	{
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		const oldSchema = `
			CREATE TABLE tasks (
				id             TEXT PRIMARY KEY,
				subject        TEXT NOT NULL DEFAULT '',
				description    TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'todo',
				assigned_agent TEXT NOT NULL DEFAULT '',
				plan_id        TEXT NOT NULL DEFAULT ''
			);
			CREATE TABLE plans (
				id             TEXT PRIMARY KEY,
				subject        TEXT NOT NULL DEFAULT '',
				description    TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'draft',
				assigned_agent TEXT NOT NULL DEFAULT ''
			);`
		if _, err := db.Exec(oldSchema); err != nil {
			t.Fatalf("create old schema: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO tasks (id, subject) VALUES (?, ?)`, "legacy-t", "pre-ts"); err != nil {
			t.Fatalf("insert legacy task: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO plans (id, subject) VALUES (?, ?)`, "legacy-p", "pre-ts"); err != nil {
			t.Fatalf("insert legacy plan: %v", err)
		}
		_ = db.Close()
	}

	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	gotT, err := b.Tasks().GetByID("legacy-t")
	if err != nil {
		t.Fatalf("GetByID legacy task: %v", err)
	}
	if gotT == nil || !gotT.CreatedAt.IsZero() || !gotT.UpdatedAt.IsZero() {
		t.Errorf("expected legacy task with zero timestamps, got %+v", gotT)
	}
	// The plan rows are collapsed into tasks by the 00004 migration. The
	// former plan id should now resolve as a planning-mode task with the
	// same id, and still carry the zero timestamps that the original plan
	// row had.
	gotP, err := b.Tasks().GetByID("legacy-p")
	if err != nil {
		t.Fatalf("GetByID legacy plan-as-task: %v", err)
	}
	if gotP == nil || !gotP.CreatedAt.IsZero() || !gotP.UpdatedAt.IsZero() {
		t.Errorf("expected legacy plan-as-task with zero timestamps, got %+v", gotP)
	}
	if gotP != nil && gotP.Mode != client.TaskModePlanning {
		t.Errorf("expected collapsed plan to have mode=planning, got %q", gotP.Mode)
	}

	// Saving a legacy row should stamp both timestamps.
	if err := b.Tasks().Save(gotT); err != nil {
		t.Fatalf("Save legacy task: %v", err)
	}
	if gotT.CreatedAt.IsZero() || gotT.UpdatedAt.IsZero() {
		t.Errorf("expected timestamps stamped after save, got %+v", gotT)
	}

	if _, err := NewBackend(dbPath); err != nil {
		t.Errorf("second NewBackend should not error, got %v", err)
	}
}

// Goose tracks applied migrations in goose_db_version. A fresh database
// should end up at the latest migration version after both 00001_init and
// 00002_add_timestamps have been applied.
func TestNewBackend_FreshDBAtLatestVersion(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-fresh-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	if _, err := NewBackend(dbPath); err != nil {
		t.Fatalf("NewBackend: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var version int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("query goose_db_version: %v", err)
	}
	const want = 5
	if version != want {
		t.Errorf("expected goose version %d, got %d", want, version)
	}
}

// A legacy database (tasks/plans present, no goose tracking) should be
// stamped at version 1 and then later migrations should apply on top.
func TestNewBackend_LegacyDBMigratedToLatest(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-legacy-stamp-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	{
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		const oldSchema = `
			CREATE TABLE tasks (
				id             TEXT PRIMARY KEY,
				subject        TEXT NOT NULL DEFAULT '',
				description    TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'todo',
				assigned_agent TEXT NOT NULL DEFAULT ''
			);`
		if _, err := db.Exec(oldSchema); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
		_ = db.Close()
	}

	if _, err := NewBackend(dbPath); err != nil {
		t.Fatalf("NewBackend: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var version int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("query goose_db_version: %v", err)
	}
	const want = 5
	if version != want {
		t.Errorf("expected goose version %d after legacy reconciliation + later migrations, got %d", want, version)
	}
}

// A pre-collapse database (goose at version 3, plans + plan_comments rows
// present, tasks with plan_id) must collapse cleanly on open:
//   - plans become tasks with mode=planning, state remapped.
//   - tasks with plan_id get parent_id set to that plan and lose plan_id.
//   - plan_comments rows become comments on the now-task.
//   - plans and plan_comments tables are dropped.
func TestNewBackend_CollapsesPreV4Database(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-collapse-v4-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	// Stage 1: open the DB and let migrations through 00003 run, then
	// roll the goose version back to 3 so 00004 hasn't run yet. That gives
	// us a fully-shaped pre-collapse schema we can seed plan rows into.
	{
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			t.Fatalf("PRAGMA: %v", err)
		}
		if err := runMigrations(db); err != nil {
			t.Fatalf("runMigrations: %v", err)
		}
		// Roll back to version 3 so 00004 can be exercised below by the
		// next NewBackend call. goose's API doesn't expose a partial-down,
		// so we patch the version_id directly.
		if _, err := db.Exec(`DELETE FROM goose_db_version WHERE version_id >= 4`); err != nil {
			t.Fatalf("rollback goose version: %v", err)
		}
		// Re-create the plans / plan_comments tables and the plan_id columns
		// on tasks and events — those are dropped by 00004 so a fresh-on-v4
		// DB doesn't have them, but a pre-v4 DB must. Also drop archived_at
		// (added by 00005, which we're rolling past) so the schema looks
		// genuinely like v3.
		const reshape = `
			DROP INDEX IF EXISTS idx_tasks_archived_at;
			ALTER TABLE tasks DROP COLUMN archived_at;
			CREATE TABLE plans (
				id             TEXT PRIMARY KEY,
				subject        TEXT NOT NULL DEFAULT '',
				description    TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'draft',
				assigned_agent TEXT NOT NULL DEFAULT '',
				created_at     TEXT NOT NULL DEFAULT '',
				updated_at     TEXT NOT NULL DEFAULT ''
			);
			CREATE TABLE plan_comments (
				id      TEXT PRIMARY KEY,
				plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
				who     TEXT NOT NULL DEFAULT '',
				comment TEXT NOT NULL DEFAULT ''
			);
			ALTER TABLE tasks ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
			ALTER TABLE events ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
		`
		if _, err := db.Exec(reshape); err != nil {
			t.Fatalf("reshape pre-v4: %v", err)
		}
		// Seed: one plan in 'active' state, one task pointing at it, one
		// comment on the plan.
		if _, err := db.Exec(`INSERT INTO plans (id, subject, state, assigned_agent) VALUES (?, ?, ?, ?)`,
			"7", "Old plan", "active", "lead"); err != nil {
			t.Fatalf("seed plan: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO tasks (id, subject, state, assigned_agent, plan_id, labels, mode) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"8", "child", "todo", "alice", "7", "[]", "standard"); err != nil {
			t.Fatalf("seed child task: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO plan_comments (id, plan_id, who, comment) VALUES (?, ?, ?, ?)`,
			"C-1", "7", "bob", "old comment"); err != nil {
			t.Fatalf("seed plan comment: %v", err)
		}
		_ = db.Close()
	}

	// Stage 2: open via NewBackend. This should re-run goose, which will
	// see version 3 and apply 00004 to collapse.
	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend (collapse): %v", err)
	}

	planAsTask, err := b.Tasks().GetByID("7")
	if err != nil {
		t.Fatalf("GetByID 7: %v", err)
	}
	if planAsTask == nil {
		t.Fatal("expected absorbed plan id=7 as task, got nil")
	}
	if planAsTask.Mode != client.TaskModePlanning {
		t.Errorf("expected mode=planning, got %q", planAsTask.Mode)
	}
	if planAsTask.State != client.TaskStateInProgress {
		t.Errorf("expected state=in_progress (from plan.active), got %q", planAsTask.State)
	}

	child, err := b.Tasks().GetByID("8")
	if err != nil {
		t.Fatalf("GetByID 8: %v", err)
	}
	if child == nil {
		t.Fatal("expected child task 8, got nil")
	}
	if child.ParentID != "7" {
		t.Errorf("expected ParentID=7 on child, got %q", child.ParentID)
	}

	comments, err := b.Comments().GetForTask("7")
	if err != nil {
		t.Fatalf("GetForTask 7: %v", err)
	}
	if len(comments) != 1 || comments[0].Comment != "old comment" {
		t.Errorf("expected one absorbed plan comment, got %+v", comments)
	}

	// The plans and plan_comments tables should be gone.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen for schema check: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, name := range []string{"plans", "plan_comments"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master %q: %v", name, err)
		}
		if n != 0 {
			t.Errorf("expected table %q to be dropped, still present", name)
		}
	}
}

// A pre-plan-id tasks table must migrate cleanly through the plan-id ADD
// (00001 legacy reconcile) and the plan-id DROP (00004 collapse) so the
// legacy row survives both. Post-collapse the task has no PlanID field at
// all; ParentID is the hierarchy field and should be empty for the legacy
// row since it never participated in a plan.
func TestNewBackend_MigratesLegacyTasks(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-migration-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	{
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		const oldSchema = `
			CREATE TABLE tasks (
				id             TEXT PRIMARY KEY,
				subject        TEXT NOT NULL DEFAULT '',
				description    TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'todo',
				assigned_agent TEXT NOT NULL DEFAULT ''
			);`
		if _, err := db.Exec(oldSchema); err != nil {
			t.Fatalf("create old schema: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO tasks (id, subject) VALUES (?, ?)`, "legacy-1", "pre-migration"); err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
		_ = db.Close()
	}

	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	got, err := b.Tasks().GetByID("legacy-1")
	if err != nil {
		t.Fatalf("GetByID after migration: %v", err)
	}
	if got == nil {
		t.Fatal("expected legacy task to survive migration")
	}
	if got.ParentID != "" {
		t.Errorf("expected ParentID empty after migration, got %q", got.ParentID)
	}
	if _, err := NewBackend(dbPath); err != nil {
		t.Errorf("second NewBackend should not error, got %v", err)
	}
}

func TestTasksRepository_Update(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)
	original := client.Task{ID: "update-target", Subject: "draft", Description: "v1", State: client.TaskStateTodo, AssignedAgent: "agent-a"}
	if err := repo.Save(&original); err != nil {
		t.Fatalf("Save (initial): %v", err)
	}
	updated := client.Task{ID: "update-target", Subject: "final", Description: "v2", State: client.TaskStateDone, AssignedAgent: "agent-b"}

	// Act
	err := repo.Save(&updated)

	// Assert
	if err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	got, err := repo.GetByID("update-target")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Subject != updated.Subject || got.Description != updated.Description ||
		got.State != updated.State || got.AssignedAgent != updated.AssignedAgent {
		t.Errorf("update did not persist: got %+v, want %+v", got, updated)
	}
	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 row after update, got %d", len(all))
	}
}

// Save stamps CreatedAt and UpdatedAt on first save, then preserves CreatedAt
// while refreshing UpdatedAt on subsequent saves.
func TestTasksRepository_Save_StampsTimestamps(t *testing.T) {
	repo := newTempRepo(t)
	task := client.Task{Subject: "stamps"}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save (initial): %v", err)
	}
	if task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps stamped, got CreatedAt=%v UpdatedAt=%v", task.CreatedAt, task.UpdatedAt)
	}
	created, firstUpdated := task.CreatedAt, task.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	task.Subject = "stamps v2"
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	if !task.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt should be preserved: got %v, want %v", task.CreatedAt, created)
	}
	if !task.UpdatedAt.After(firstUpdated) {
		t.Errorf("UpdatedAt should advance: got %v, was %v", task.UpdatedAt, firstUpdated)
	}

	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.CreatedAt.Equal(task.CreatedAt) || !got.UpdatedAt.Equal(task.UpdatedAt) {
		t.Errorf("timestamps did not round-trip: got %+v, want CreatedAt=%v UpdatedAt=%v",
			got, task.CreatedAt, task.UpdatedAt)
	}
}

// GetAll orders by UpdatedAt DESC. Touching an older task should push it to
// the front of the list.
func TestTasksRepository_GetAll_OrdersByUpdatedAtDesc(t *testing.T) {
	repo := newTempRepo(t)
	first := client.Task{Subject: "first"}
	if err := repo.Save(&first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second := client.Task{Subject: "second"}
	if err := repo.Save(&second); err != nil {
		t.Fatalf("Save second: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := repo.Save(&first); err != nil {
		t.Fatalf("re-save first: %v", err)
	}

	got, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(got) != 2 || got[0].Subject != "first" || got[1].Subject != "second" {
		t.Errorf("expected [first, second], got %+v", got)
	}
}

// A caller-supplied non-zero CreatedAt on first insert (e.g. importing data
// from another system) must be honored rather than overwritten with now.
func TestTasksRepository_Save_HonorsCallerSuppliedCreatedAt(t *testing.T) {
	repo := newTempRepo(t)
	want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	task := client.Task{Subject: "imported", CreatedAt: want}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !task.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt overwritten: got %v, want %v", task.CreatedAt, want)
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt did not round-trip: got %v, want %v", got.CreatedAt, want)
	}
}

func TestTasksRepository_ArchivedAt_NilRoundTrips(t *testing.T) {
	repo := newTempRepo(t)
	task := client.Task{Subject: "active"}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ArchivedAt != nil {
		t.Errorf("ArchivedAt: got %v, want nil for never-archived row", got.ArchivedAt)
	}
}

func TestTasksRepository_ArchivedAt_SetRoundTrips(t *testing.T) {
	repo := newTempRepo(t)
	want := time.Date(2026, 6, 17, 14, 23, 11, 0, time.UTC)
	task := client.Task{Subject: "archived", ArchivedAt: &want}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("ArchivedAt: got nil, want non-nil")
	}
	if !got.ArchivedAt.Equal(want) {
		t.Errorf("ArchivedAt: got %v, want %v", got.ArchivedAt, want)
	}
}

func TestTasksRepository_ArchivedAt_ClearReverts(t *testing.T) {
	repo := newTempRepo(t)
	when := time.Date(2026, 6, 17, 14, 23, 11, 0, time.UTC)
	task := client.Task{Subject: "first archive then unarchive", ArchivedAt: &when}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save archived: %v", err)
	}
	// Now unarchive: same row, ArchivedAt cleared.
	task.ArchivedAt = nil
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save unarchived: %v", err)
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ArchivedAt != nil {
		t.Errorf("ArchivedAt should be nil after clear, got %v", got.ArchivedAt)
	}
}
