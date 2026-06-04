package filestorage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func newTempBackend(t *testing.T) client.Backend {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("tm-git-test-%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	return b
}

func TestCommentsRepository_Add(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task := client.Task{ID: "task-1", Subject: "with comments", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	c := client.Comment{Who: "alice", Comment: "looks good"}

	// Act
	err := b.Comments().Add("task-1", &c)

	// Assert
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected Add to assign a non-empty ID")
	}
	got, err := b.Comments().GetForTask("task-1")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID || got[0].Who != "alice" || got[0].Comment != "looks good" {
		t.Errorf("unexpected stored comments: %+v", got)
	}
}

func TestCommentsRepository_GetForTask(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task1 := client.Task{ID: "task-1", Subject: "t1", State: client.TaskStateTodo}
	task2 := client.Task{ID: "task-2", Subject: "t2", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&task1); err != nil {
		t.Fatalf("seed task 1: %v", err)
	}
	if err := b.Tasks().Save(&task2); err != nil {
		t.Fatalf("seed task 2: %v", err)
	}
	seed := []client.Comment{
		{Who: "alice", Comment: "first"},
		{Who: "bob", Comment: "second"},
		{Who: "alice", Comment: "third"},
	}
	for i := range seed {
		if err := b.Comments().Add("task-1", &seed[i]); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	other := client.Comment{Who: "carol", Comment: "other task"}
	if err := b.Comments().Add("task-2", &other); err != nil {
		t.Fatalf("Add (other task): %v", err)
	}

	// Act
	got, err := b.Comments().GetForTask("task-1")

	// Assert
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != len(seed) {
		t.Fatalf("expected %d comments, got %d", len(seed), len(got))
	}
	for i, c := range got {
		if c.ID != seed[i].ID || c.Who != seed[i].Who || c.Comment != seed[i].Comment {
			t.Errorf("comment %d mismatch: got %+v, want %+v", i, c, seed[i])
		}
	}
}

func TestCommentsRepository_GetForTask_Empty(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task := client.Task{ID: "task-1", Subject: "no comments", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	// Act
	got, err := b.Comments().GetForTask("task-1")

	// Assert
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 comments, got %d", len(got))
	}
}

func TestCommentsRepository_Add_NonexistentTask(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	c := client.Comment{Who: "alice", Comment: "orphan"}

	// Act
	err := b.Comments().Add("missing-task", &c)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing task, got nil")
	}
}

func TestTasksRepository_Update_PreservesComments(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task := client.Task{ID: "task-1", Subject: "initial", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	c := client.Comment{Who: "alice", Comment: "important note"}
	if err := b.Comments().Add("task-1", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Act
	updated := client.Task{ID: "task-1", Subject: "renamed", State: client.TaskStateInProgress, AssignedAgent: "agent-x"}
	if err := b.Tasks().Save(&updated); err != nil {
		t.Fatalf("Save (update): %v", err)
	}

	// Assert
	got, err := b.Comments().GetForTask("task-1")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID || got[0].Who != "alice" || got[0].Comment != "important note" {
		t.Errorf("expected comments preserved across update, got %+v", got)
	}
}

func TestCommentsRepository_MultiLineBody(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task := client.Task{ID: "task-1", Subject: "with multi-line comment", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	body := "First paragraph.\n\nSecond paragraph with `code`.\n\n- bullet\n- another bullet"
	c := client.Comment{Who: "alice", Comment: body}

	// Act
	if err := b.Comments().Add("task-1", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Assert
	got, err := b.Comments().GetForTask("task-1")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(got))
	}
	if got[0].Comment != body {
		t.Errorf("multi-line body did not round-trip:\ngot:  %q\nwant: %q", got[0].Comment, body)
	}
}
