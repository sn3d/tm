package filestorage

import (
	"testing"

	"github.com/sn3d/tm/internal/client"
)

// TestSharedCounter_AssignsInterleavedIDs verifies that creating tasks and
// plans without explicit IDs draws from one shared sequence: 1, 2, 3, 4...
// in creation order, regardless of entity type.
func TestSharedCounter_AssignsInterleavedIDs(t *testing.T) {
	b := newTempBackend(t)

	t1 := client.Task{Subject: "task one", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&t1); err != nil {
		t.Fatalf("save task one: %v", err)
	}
	if t1.ID != "1" {
		t.Errorf("first task: want ID 1, got %q", t1.ID)
	}

	p1 := client.Plan{Subject: "plan one", State: client.PlanStateDraft}
	if err := b.Plans().Save(&p1); err != nil {
		t.Fatalf("save plan one: %v", err)
	}
	if p1.ID != "2" {
		t.Errorf("first plan: want ID 2, got %q", p1.ID)
	}

	t2 := client.Task{Subject: "task two", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&t2); err != nil {
		t.Fatalf("save task two: %v", err)
	}
	if t2.ID != "3" {
		t.Errorf("second task: want ID 3, got %q", t2.ID)
	}

	p2 := client.Plan{Subject: "plan two", State: client.PlanStateDraft}
	if err := b.Plans().Save(&p2); err != nil {
		t.Fatalf("save plan two: %v", err)
	}
	if p2.ID != "4" {
		t.Errorf("second plan: want ID 4, got %q", p2.ID)
	}

	for _, want := range []struct {
		id  client.TaskID
		sub string
	}{{"1", "task one"}, {"3", "task two"}} {
		got, err := b.Tasks().GetByID(want.id)
		if err != nil {
			t.Fatalf("GetByID(task %s): %v", want.id, err)
		}
		if got == nil || got.Subject != want.sub {
			t.Errorf("task lookup %s: got %+v, want subject %q", want.id, got, want.sub)
		}
	}
	for _, want := range []struct {
		id  client.PlanID
		sub string
	}{{"2", "plan one"}, {"4", "plan two"}} {
		got, err := b.Plans().GetByID(want.id)
		if err != nil {
			t.Fatalf("GetByID(plan %s): %v", want.id, err)
		}
		if got == nil || got.Subject != want.sub {
			t.Errorf("plan lookup %s: got %+v, want subject %q", want.id, got, want.sub)
		}
	}
}

// TestSharedCounter_IgnoresNonNumericIDs verifies that pre-existing
// non-numeric IDs (legacy PLAN-N, JIRA-style TASK-123) don't influence the
// next shared numeric ID.
func TestSharedCounter_IgnoresNonNumericIDs(t *testing.T) {
	b := newTempBackend(t)

	if err := b.Plans().Save(&client.Plan{ID: "PLAN-99", Subject: "legacy plan", State: client.PlanStateDraft}); err != nil {
		t.Fatalf("seed legacy plan: %v", err)
	}
	if err := b.Tasks().Save(&client.Task{ID: "TASK-500", Subject: "legacy task", State: client.TaskStateTodo}); err != nil {
		t.Fatalf("seed legacy task: %v", err)
	}

	auto := client.Task{Subject: "auto", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&auto); err != nil {
		t.Fatalf("save auto task: %v", err)
	}
	if auto.ID != "1" {
		t.Errorf("expected non-numeric IDs to be ignored: want 1, got %q", auto.ID)
	}
}
