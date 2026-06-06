package filestorage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func tmpDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("tm-git-test-%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newTempRepo(t *testing.T) client.TasksRepository {
	t.Helper()
	b, err := NewBackend(tmpDir(t))
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
		Description:   "cover the git backend",
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
	first := client.Task{Subject: "a", State: client.TaskStateTodo}
	second := client.Task{Subject: "b", State: client.TaskStateTodo}

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
		{ID: "1", Subject: "first", State: client.TaskStateTodo, AssignedAgent: "a"},
		{ID: "2", Subject: "second", State: client.TaskStateInProgress, AssignedAgent: "b"},
		{ID: "3", Subject: "third", State: client.TaskStateDone, AssignedAgent: "c"},
	}
	for i := range seed {
		if err := repo.Save(&seed[i]); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// Act
	got, err := repo.GetAll()

	// Assert: GetAll orders by UpdatedAt DESC, so the most recently saved
	// task comes first — i.e. the reverse of insertion order here.
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(got) != len(seed) {
		t.Fatalf("expected %d tasks, got %d", len(seed), len(got))
	}
	for i, task := range got {
		want := seed[len(seed)-1-i]
		if task.Subject != want.Subject || task.AssignedAgent != want.AssignedAgent {
			t.Errorf("task %d mismatch: got %+v, want %+v", i, task, want)
		}
	}
}

func TestTasksRepository_Create_AssignsSequentialNumericIDs(t *testing.T) {
	// Arrange
	repo := newTempRepo(t)

	// Act
	first := client.Task{Subject: "a", State: client.TaskStateTodo}
	if err := repo.Save(&first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second := client.Task{Subject: "b", State: client.TaskStateTodo}
	if err := repo.Save(&second); err != nil {
		t.Fatalf("Save second: %v", err)
	}
	third := client.Task{Subject: "c", State: client.TaskStateTodo}
	if err := repo.Save(&third); err != nil {
		t.Fatalf("Save third: %v", err)
	}

	// Assert
	if first.ID != "1" || second.ID != "2" || third.ID != "3" {
		t.Errorf("expected sequential IDs 1,2,3; got %q,%q,%q", first.ID, second.ID, third.ID)
	}
}

func TestTasksRepository_Save_WritesSlugFilename(t *testing.T) {
	// Arrange
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	repo := b.Tasks()
	task := client.Task{Subject: "Implement the login flow!", State: client.TaskStateTodo}

	// Act
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Assert
	want := filepath.Join(dir, "tasks", "1--implement-the-login-flow.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected file at %q: %v", want, err)
	}
}

func TestTasksRepository_Save_RenamesOnSubjectChange(t *testing.T) {
	// Arrange
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	repo := b.Tasks()
	task := client.Task{Subject: "old subject", State: client.TaskStateTodo}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save initial: %v", err)
	}
	original := filepath.Join(dir, "tasks", "1--old-subject.md")
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("expected original file: %v", err)
	}

	// Act
	task.Subject = "new subject"
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save update: %v", err)
	}

	// Assert
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Errorf("expected old file to be removed, got err=%v", err)
	}
	renamed := filepath.Join(dir, "tasks", "1--new-subject.md")
	if _, err := os.Stat(renamed); err != nil {
		t.Errorf("expected renamed file at %q: %v", renamed, err)
	}
}

func TestTasksRepository_JiraStyleID_RoundTrips(t *testing.T) {
	// Arrange
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	repo := b.Tasks()
	task := client.Task{ID: "TASK-123", Subject: "imported from jira", State: client.TaskStateTodo}

	// Act
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Assert: filename preserves the dash in the ID
	want := filepath.Join(dir, "tasks", "TASK-123--imported-from-jira.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected file at %q: %v", want, err)
	}
	got, err := repo.GetByID("TASK-123")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil || got.ID != "TASK-123" || got.Subject != "imported from jira" {
		t.Errorf("round-trip failed: %+v", got)
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
}

// Legacy task files written before the parent_id field existed must still
// parse cleanly.
func TestTasksRepository_ParsesLegacyFile_WithoutParentID(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}

	legacyPath := filepath.Join(dir, "tasks", "legacy-1.md")
	legacyContent := `---
id: legacy-1
state: todo
assigned_agent: alice
---

# legacy task

some description
`
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, err := b.Tasks().GetByID("legacy-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected legacy task to load")
	}
	if got.ParentID != "" {
		t.Errorf("expected empty ParentID for legacy task, got %q", got.ParentID)
	}
	if got.Subject != "legacy task" || got.AssignedAgent != "alice" {
		t.Errorf("legacy task did not parse correctly: %+v", got)
	}
}

// Top-level tasks (no ParentID) must not emit a parent_id frontmatter field.
func TestTasksRepository_Save_TopLevelOmitsParentIDFromFile(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	task := client.Task{ID: "task-1", Subject: "standalone", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(dir, "tasks", "task-1--standalone.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}
	if bytes.Contains(raw, []byte("parent_id")) {
		t.Errorf("top-level task file should not emit parent_id, got:\n%s", string(raw))
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
}

// Save without an explicit State backfills the default so the round-trip
// through Load succeeds.
func TestTasksRepository_Save_DefaultsEmptyState(t *testing.T) {
	repo := newTempRepo(t)
	task := client.Task{Subject: "no state"}
	if err := repo.Save(&task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != client.TaskStateDefault {
		t.Errorf("expected default state %q, got %q", client.TaskStateDefault, got.State)
	}
}

// A file whose YAML carries a state name outside the canonical set surfaces
// as a parse error rather than silently coercing.
func TestTasksRepository_RejectsUnknownStateOnLoad(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	path := filepath.Join(dir, "tasks", "corrupt.md")
	content := `---
id: corrupt
state: bogus
assigned_agent: alice
---

# corrupt task
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if _, err := b.Tasks().GetByID("corrupt"); err == nil {
		t.Fatal("expected error loading task with unknown state, got nil")
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
	// Re-save first; it should now be the most recently updated.
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

// Legacy task files written before the timestamp columns existed must parse
// cleanly with zero-valued CreatedAt/UpdatedAt.
func TestTasksRepository_ParsesLegacyFile_WithoutTimestamps(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}

	legacyPath := filepath.Join(dir, "tasks", "legacy-ts.md")
	legacyContent := `---
id: legacy-ts
state: todo
assigned_agent: alice
---

# legacy task
`
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, err := b.Tasks().GetByID("legacy-ts")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected legacy task to load")
	}
	if !got.CreatedAt.IsZero() || !got.UpdatedAt.IsZero() {
		t.Errorf("expected zero timestamps for legacy task, got CreatedAt=%v UpdatedAt=%v", got.CreatedAt, got.UpdatedAt)
	}
}
