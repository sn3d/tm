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
	for i, task := range got {
		if task.Subject != seed[i].Subject || task.AssignedAgent != seed[i].AssignedAgent {
			t.Errorf("task %d mismatch: got %+v, want %+v", i, task, seed[i])
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
