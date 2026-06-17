package client

import (
	"errors"
	"testing"
)

// seedTask is a shorter helper for archive tests. It inserts a Task literal
// directly into the store so we can build hierarchies without going through
// Create (and its validation).
func (b *stubBackend) seedTask(t Task) {
	cp := t
	b.tasks.store[t.ID] = &cp
}

func TestArchiveTask_SetsArchivedAtAndEmitsEvent(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "T-1", State: TaskStateTodo})
	c := New(b)

	cascaded, err := c.ArchiveTask("T-1", true)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}
	if cascaded != 0 {
		t.Errorf("cascade_count: got %d, want 0 (no children)", cascaded)
	}
	stored, _ := b.tasks.GetByID("T-1")
	if stored.ArchivedAt == nil {
		t.Error("ArchivedAt should be non-nil after archive")
	}
	var archEvent *Event
	for i, ev := range b.events.appended {
		if ev.Kind == EventTaskArchived {
			archEvent = &b.events.appended[i]
		}
	}
	if archEvent == nil {
		t.Fatal("expected EventTaskArchived")
	}
	if archEvent.TaskID != "T-1" {
		t.Errorf("event TaskID: got %q, want T-1", archEvent.TaskID)
	}
	if archEvent.Payload["cascade_count"].(int) != 0 {
		t.Errorf("payload cascade_count: got %v, want 0", archEvent.Payload["cascade_count"])
	}
	if _, ok := archEvent.Payload["archived_at"].(string); !ok {
		t.Errorf("payload archived_at: not a string (%v)", archEvent.Payload["archived_at"])
	}
}

func TestArchiveTask_CascadeArchivesDescendants(t *testing.T) {
	b := newStubBackend()
	// root → child → grandchild, plus an unrelated sibling-of-root that should NOT be archived.
	b.seedTask(Task{ID: "root", State: TaskStateTodo})
	b.seedTask(Task{ID: "child", State: TaskStateTodo, ParentID: "root"})
	b.seedTask(Task{ID: "grand", State: TaskStateTodo, ParentID: "child"})
	b.seedTask(Task{ID: "unrelated", State: TaskStateTodo})
	c := New(b)

	cascaded, err := c.ArchiveTask("root", true)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}
	if cascaded != 2 {
		t.Errorf("cascade_count: got %d, want 2 (child + grand)", cascaded)
	}
	for _, id := range []TaskID{"root", "child", "grand"} {
		got, _ := b.tasks.GetByID(id)
		if got.ArchivedAt == nil {
			t.Errorf("task %q should be archived", id)
		}
	}
	unrelated, _ := b.tasks.GetByID("unrelated")
	if unrelated.ArchivedAt != nil {
		t.Errorf("unrelated task should NOT be archived")
	}
}

func TestArchiveTask_NoCascade(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "root"})
	b.seedTask(Task{ID: "child", ParentID: "root"})
	c := New(b)

	cascaded, err := c.ArchiveTask("root", false)
	if err != nil {
		t.Fatalf("ArchiveTask: %v", err)
	}
	if cascaded != 0 {
		t.Errorf("cascade_count: got %d, want 0 (cascade=false)", cascaded)
	}
	root, _ := b.tasks.GetByID("root")
	if root.ArchivedAt == nil {
		t.Error("root should be archived")
	}
	child, _ := b.tasks.GetByID("child")
	if child.ArchivedAt != nil {
		t.Error("child should NOT be archived with cascade=false")
	}
}

func TestArchiveTask_AlreadyArchivedIsNoop(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "T-1"})
	c := New(b)

	if _, err := c.ArchiveTask("T-1", false); err != nil {
		t.Fatalf("first archive: %v", err)
	}
	eventsBefore := len(b.events.appended)
	if _, err := c.ArchiveTask("T-1", false); err != nil {
		t.Fatalf("second archive: %v", err)
	}
	if len(b.events.appended) != eventsBefore {
		t.Errorf("re-archiving should not emit a second event (events: %d -> %d)",
			eventsBefore, len(b.events.appended))
	}
}

func TestUnarchiveTask_ClearsArchivedAtAndEmitsEvent(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "T-1"})
	c := New(b)

	if _, err := c.ArchiveTask("T-1", false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := c.UnarchiveTask("T-1"); err != nil {
		t.Fatalf("UnarchiveTask: %v", err)
	}
	got, _ := b.tasks.GetByID("T-1")
	if got.ArchivedAt != nil {
		t.Error("ArchivedAt should be nil after unarchive")
	}
	var found bool
	for _, ev := range b.events.appended {
		if ev.Kind == EventTaskUnarchived && ev.TaskID == "T-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected EventTaskUnarchived")
	}
}

func TestUnarchiveTask_DoesNotCascade(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "root"})
	b.seedTask(Task{ID: "child", ParentID: "root"})
	c := New(b)

	if _, err := c.ArchiveTask("root", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := c.UnarchiveTask("root"); err != nil {
		t.Fatalf("unarchive root: %v", err)
	}
	child, _ := b.tasks.GetByID("child")
	if child.ArchivedAt == nil {
		t.Error("child should remain archived (unarchive must not cascade)")
	}
}

func TestUnarchiveTask_NotArchivedIsNoop(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "T-1"})
	c := New(b)

	if err := c.UnarchiveTask("T-1"); err != nil {
		t.Fatalf("UnarchiveTask: %v", err)
	}
	if len(b.events.appended) != 0 {
		t.Errorf("expected no events; got %d", len(b.events.appended))
	}
}

func TestArchiveTask_NotFound(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.ArchiveTask("missing", false)
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
}

func TestUnarchiveTask_NotFound(t *testing.T) {
	c := New(newStubBackend())
	err := c.UnarchiveTask("missing")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
}

func TestListTasks_ArchivedFilter(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "A", State: TaskStateTodo})
	b.seedTask(Task{ID: "B", State: TaskStateTodo})
	c := New(b)
	if _, err := c.ArchiveTask("B", false); err != nil {
		t.Fatalf("archive B: %v", err)
	}

	tests := []struct {
		name   string
		filter ArchivedFilter
		wantN  int
	}{
		{"active hides archived", ArchivedActive, 1},
		{"archived shows only archived", ArchivedOnly, 1},
		{"all shows both", ArchivedAll, 2},
		{"default is active", ArchivedFilterDefault, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.ListTasks(tt.filter)
			if err != nil {
				t.Fatalf("ListTasks: %v", err)
			}
			if len(got) != tt.wantN {
				t.Errorf("len: got %d, want %d", len(got), tt.wantN)
			}
		})
	}
}

func TestGetTasksByParent_ArchivedFilter(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "root"})
	b.seedTask(Task{ID: "A", ParentID: "root"})
	b.seedTask(Task{ID: "B", ParentID: "root"})
	c := New(b)
	if _, err := c.ArchiveTask("A", false); err != nil {
		t.Fatalf("archive A: %v", err)
	}

	active, err := c.GetTasksByParent("root", ArchivedActive)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(active) != 1 || active[0].ID != "B" {
		t.Errorf("active under root: got %+v, want [B]", taskIDs(active))
	}
	all, err := c.GetTasksByParent("root", ArchivedAll)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("all under root: got %d, want 2", len(all))
	}
}

func TestGetTasksByLabel_ArchivedFilter(t *testing.T) {
	b := newStubBackend()
	b.seedTask(Task{ID: "A", Labels: []string{"bug"}})
	b.seedTask(Task{ID: "B", Labels: []string{"bug"}})
	c := New(b)
	if _, err := c.ArchiveTask("A", false); err != nil {
		t.Fatalf("archive A: %v", err)
	}

	active, err := c.GetTasksByLabel("bug", ArchivedActive)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(active) != 1 || active[0].ID != "B" {
		t.Errorf("active bug-labeled: got %+v, want [B]", taskIDs(active))
	}
	only, err := c.GetTasksByLabel("bug", ArchivedOnly)
	if err != nil {
		t.Fatalf("only: %v", err)
	}
	if len(only) != 1 || only[0].ID != "A" {
		t.Errorf("only archived bug-labeled: got %+v, want [A]", taskIDs(only))
	}
}

func TestParseArchivedFilter(t *testing.T) {
	tests := []struct {
		in      string
		want    ArchivedFilter
		wantErr bool
	}{
		{"", ArchivedFilterDefault, false},
		{"active", ArchivedActive, false},
		{"archived", ArchivedOnly, false},
		{"all", ArchivedAll, false},
		{"bogus", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseArchivedFilter(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func taskIDs(ts []Task) []TaskID {
	out := make([]TaskID, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}
