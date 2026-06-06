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

func TestTasksRepository_PlanID_RoundTrip(t *testing.T) {
	b := newTempBackend(t)
	plan := client.Plan{ID: "PLAN-1", Subject: "container"}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	task := client.Task{ID: "task-1", Subject: "in plan", State: client.TaskStateTodo, PlanID: "PLAN-1"}
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
	if got.PlanID != "PLAN-1" {
		t.Errorf("expected PlanID=PLAN-1, got %q", got.PlanID)
	}
}

func TestTasksRepository_GetByPlan(t *testing.T) {
	b := newTempBackend(t)
	for _, id := range []string{"PLAN-1", "PLAN-2"} {
		if err := b.Plans().Save(&client.Plan{ID: id, Subject: id}); err != nil {
			t.Fatalf("seed plan %q: %v", id, err)
		}
	}
	seed := []client.Task{
		{ID: "t-a", Subject: "a", State: client.TaskStateTodo, PlanID: "PLAN-1"},
		{ID: "t-b", Subject: "b", State: client.TaskStateTodo, PlanID: "PLAN-1"},
		{ID: "t-c", Subject: "c", State: client.TaskStateTodo, PlanID: "PLAN-2"},
		{ID: "t-d", Subject: "d", State: client.TaskStateTodo}, // standalone
	}
	for i := range seed {
		if err := b.Tasks().Save(&seed[i]); err != nil {
			t.Fatalf("seed task %q: %v", seed[i].ID, err)
		}
	}

	got, err := b.Tasks().GetByPlan("PLAN-1")
	if err != nil {
		t.Fatalf("GetByPlan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks for PLAN-1, got %d", len(got))
	}
	for _, task := range got {
		if task.PlanID != "PLAN-1" {
			t.Errorf("expected PlanID=PLAN-1, got %q for task %q", task.PlanID, task.ID)
		}
	}

	standalone, err := b.Tasks().GetByPlan("")
	if err != nil {
		t.Fatalf("GetByPlan(empty): %v", err)
	}
	if len(standalone) != 1 || standalone[0].ID != "t-d" {
		t.Errorf("expected standalone task t-d, got %+v", standalone)
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
	gotP, err := b.Plans().GetByID("legacy-p")
	if err != nil {
		t.Fatalf("GetByID legacy plan: %v", err)
	}
	if gotP == nil || !gotP.CreatedAt.IsZero() || !gotP.UpdatedAt.IsZero() {
		t.Errorf("expected legacy plan with zero timestamps, got %+v", gotP)
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
	const want = 3
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
	const want = 3
	if version != want {
		t.Errorf("expected goose version %d after legacy reconciliation + later migrations, got %d", want, version)
	}
}

func TestNewBackend_MigratesTasksAddPlanID(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-migration-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	// Seed a pre-plan-id tasks table directly so NewBackend has to migrate it.
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
	if got.PlanID != "" {
		t.Errorf("expected PlanID empty after migration, got %q", got.PlanID)
	}
	// Re-opening should be a no-op (the duplicate-column error is swallowed).
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
