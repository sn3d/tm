package sqlite

import (
	"testing"

	"github.com/sn3d/tm/internal/client"
)

func TestTasksRepository_DependsOn_RoundTrip(t *testing.T) {
	repo := newTempRepo(t)

	dep1 := client.Task{Subject: "d1", State: client.TaskStateTodo}
	if err := repo.Save(&dep1); err != nil {
		t.Fatalf("seed dep1: %v", err)
	}
	dep2 := client.Task{Subject: "d2", State: client.TaskStateTodo}
	if err := repo.Save(&dep2); err != nil {
		t.Fatalf("seed dep2: %v", err)
	}

	task := client.Task{
		Subject:   "blocked",
		State:     client.TaskStateTodo,
		DependsOn: []client.TaskID{dep1.ID, dep2.ID},
	}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil task")
	}
	// sqlite loadDeps orders by depends_on_id; both seeded IDs are ULIDs so
	// the expected order matches insertion order only when dep1.ID < dep2.ID
	// in ULID lex order, which is always true since dep1 is created first.
	if len(got.DependsOn) != 2 || got.DependsOn[0] != dep1.ID || got.DependsOn[1] != dep2.ID {
		t.Errorf("DependsOn round-trip: got %v, want [%q %q]", got.DependsOn, dep1.ID, dep2.ID)
	}
}

func TestTasksRepository_DependsOn_UpdateReplacesList(t *testing.T) {
	repo := newTempRepo(t)
	d1 := client.Task{Subject: "d1", State: client.TaskStateTodo}
	_ = repo.Save(&d1)
	d2 := client.Task{Subject: "d2", State: client.TaskStateTodo}
	_ = repo.Save(&d2)
	d3 := client.Task{Subject: "d3", State: client.TaskStateTodo}
	_ = repo.Save(&d3)

	task := client.Task{
		Subject:   "t",
		State:     client.TaskStateTodo,
		DependsOn: []client.TaskID{d1.ID, d2.ID},
	}
	_ = repo.Save(&task)

	// Replace with a single different dep.
	task.DependsOn = []client.TaskID{d3.ID}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save replace: %v", err)
	}
	got, _ := repo.GetByID(task.ID)
	if len(got.DependsOn) != 1 || got.DependsOn[0] != d3.ID {
		t.Errorf("expected deps replaced with [%q], got %v", d3.ID, got.DependsOn)
	}
}

func TestTasksRepository_DependsOn_EmptyAfterClear(t *testing.T) {
	repo := newTempRepo(t)
	d1 := client.Task{Subject: "d1", State: client.TaskStateTodo}
	_ = repo.Save(&d1)
	task := client.Task{Subject: "t", State: client.TaskStateTodo, DependsOn: []client.TaskID{d1.ID}}
	_ = repo.Save(&task)

	task.DependsOn = nil
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save clear: %v", err)
	}
	got, _ := repo.GetByID(task.ID)
	if len(got.DependsOn) != 0 {
		t.Errorf("expected deps cleared, got %v", got.DependsOn)
	}
}
