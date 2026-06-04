package filestorage

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
	if len(got.DependsOn) != 2 || got.DependsOn[0] != dep1.ID || got.DependsOn[1] != dep2.ID {
		t.Errorf("DependsOn round-trip: got %v, want [%q %q]", got.DependsOn, dep1.ID, dep2.ID)
	}
}

func TestTasksRepository_DependsOn_OmittedWhenEmpty(t *testing.T) {
	repo := newTempRepo(t)
	task := client.Task{Subject: "no deps", State: client.TaskStateTodo}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.DependsOn) != 0 {
		t.Errorf("DependsOn: got %v, want empty/nil", got.DependsOn)
	}
}
