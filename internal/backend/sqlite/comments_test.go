package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func makeComment(who, body string) client.Comment {
	return client.Comment{Who: who, Comment: body}
}

func newTempBackend(t *testing.T) client.Backend {
	t.Helper()
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	return b
}

func TestCommentsRepository_Add(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task := client.Task{ID: "task-1", Subject: "with comments"}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	c := makeComment("alice", "looks good")

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
	task1 := client.Task{ID: "task-1", Subject: "t1"}
	task2 := client.Task{ID: "task-2", Subject: "t2"}
	if err := b.Tasks().Save(&task1); err != nil {
		t.Fatalf("seed task 1: %v", err)
	}
	if err := b.Tasks().Save(&task2); err != nil {
		t.Fatalf("seed task 2: %v", err)
	}
	seed := []client.Comment{
		makeComment("alice", "first"),
		makeComment("bob", "second"),
		makeComment("alice", "third"),
	}
	for i := range seed {
		if err := b.Comments().Add("task-1", &seed[i]); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	other := makeComment("carol", "other task")
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
	task := client.Task{ID: "task-1", Subject: "no comments"}
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
	c := makeComment("alice", "orphan")

	// Act
	err := b.Comments().Add("missing-task", &c)

	// Assert
	if err == nil {
		t.Fatal("expected FK constraint error, got nil")
	}
}

func TestCommentsRepository_CascadeOnTaskDelete(t *testing.T) {
	// Arrange
	b := newTempBackend(t)
	task := client.Task{ID: "doomed-task", Subject: "doomed"}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	c := makeComment("alice", "bye")
	if err := b.Comments().Add("doomed-task", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Act
	db := b.(*backend).tasks.db
	if _, err := db.Exec(`DELETE FROM tasks WHERE id = ?`, "doomed-task"); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	// Assert
	got, err := b.Comments().GetForTask("doomed-task")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected cascade delete to remove comments, got %d", len(got))
	}
}
