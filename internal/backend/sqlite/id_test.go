package sqlite

import (
	"testing"

	"github.com/sn3d/tm/internal/client"
)

// TestNumericCounter_AssignsSequentialIDs verifies that creating tasks
// without explicit IDs draws from one shared sequence: 1, 2, 3, 4... in
// creation order. The counter is named "shared" historically because it
// once spanned the plans table too; post-collapse it scans only tasks.
func TestNumericCounter_AssignsSequentialIDs(t *testing.T) {
	b := newTempBackend(t)

	for i, sub := range []string{"task one", "task two", "task three", "task four"} {
		tk := client.Task{Subject: sub, State: client.TaskStateTodo}
		if err := b.Tasks().Save(&tk); err != nil {
			t.Fatalf("save %s: %v", sub, err)
		}
		want := []string{"1", "2", "3", "4"}[i]
		if tk.ID != want {
			t.Errorf("task %d (%q): want ID %s, got %q", i+1, sub, want, tk.ID)
		}
	}
}

// TestNumericCounter_IgnoresNonNumericIDs verifies that pre-existing
// non-numeric IDs (ULIDs, legacy PLAN-N) don't influence the next shared
// numeric ID — important for DBs that mix legacy ULID rows with new
// numeric rows.
func TestNumericCounter_IgnoresNonNumericIDs(t *testing.T) {
	b := newTempBackend(t)

	if err := b.Tasks().Save(&client.Task{ID: "01KSHXYS3XDE12P1R7R8V183VJ", Subject: "legacy ulid task", State: client.TaskStateTodo}); err != nil {
		t.Fatalf("seed ulid task: %v", err)
	}
	if err := b.Tasks().Save(&client.Task{ID: "PLAN-42", Subject: "legacy prefixed task", State: client.TaskStateTodo}); err != nil {
		t.Fatalf("seed prefixed task: %v", err)
	}

	auto := client.Task{Subject: "auto", State: client.TaskStateTodo}
	if err := b.Tasks().Save(&auto); err != nil {
		t.Fatalf("save auto task: %v", err)
	}
	if auto.ID != "1" {
		t.Errorf("expected non-numeric IDs to be ignored: want 1, got %q", auto.ID)
	}
}
