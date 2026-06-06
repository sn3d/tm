package client

import (
	"testing"
	"time"
)

func TestInbox_EmptyActorErrors(t *testing.T) {
	c := New(newStubBackend())
	if _, err := c.Inbox(""); err == nil {
		t.Fatal("Inbox(\"\"): expected error, got nil")
	}
	if _, err := c.PeekInbox(""); err == nil {
		t.Fatal("PeekInbox(\"\"): expected error, got nil")
	}
}

func TestInbox_TasksFilteredByAssigneeAndCategory(t *testing.T) {
	b := newStubBackend()
	for _, ts := range []struct {
		id, assignee string
		state        TaskState
	}{
		{"T-1", "alice", TaskStateTodo},
		{"T-2", "alice", TaskStateInProgress},
		{"T-3", "alice", TaskStateBlocked},
		{"T-4", "alice", TaskStateInReview},
		{"T-5", "alice", TaskStateDone},
		{"T-6", "alice", TaskStateCancelled},
		{"T-7", "bob", TaskStateTodo},
		{"T-8", "", TaskStateTodo},
	} {
		b.tasks.store[ts.id] = &Task{ID: ts.id, AssignedAgent: ts.assignee, State: ts.state}
	}

	box, err := New(b).Inbox("alice")
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	got := map[string]bool{}
	for _, t := range box.Tasks {
		got[t.ID] = true
	}
	want := []string{"T-1", "T-2", "T-3", "T-4"}
	for _, id := range want {
		if !got[id] {
			t.Errorf("missing task %q", id)
		}
	}
	for _, id := range []string{"T-5", "T-6", "T-7", "T-8"} {
		if got[id] {
			t.Errorf("unexpected task %q in inbox", id)
		}
	}
}

func TestInbox_TasksSortedNewestFirstByUpdatedAt(t *testing.T) {
	b := newStubBackend()
	base := time.Unix(1000, 0).UTC()
	for i, id := range []string{"T-1", "T-5", "T-3"} {
		b.tasks.store[id] = &Task{
			ID:            id,
			AssignedAgent: "alice",
			State:         TaskStateTodo,
			UpdatedAt:     base.Add(time.Duration(i) * time.Second),
		}
	}
	box, _ := New(b).Inbox("alice")
	if len(box.Tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(box.Tasks))
	}
	if box.Tasks[0].ID != "T-3" || box.Tasks[1].ID != "T-5" || box.Tasks[2].ID != "T-1" {
		t.Errorf("sort order: got %v, want [T-3 T-5 T-1]",
			[]string{box.Tasks[0].ID, box.Tasks[1].ID, box.Tasks[2].ID})
	}
}

func TestInbox_ResumableIsTasksUpdatedSinceLastSeen(t *testing.T) {
	b := newStubBackend()
	cutoff := time.Unix(1000, 0).UTC()
	b.cursors.store["alice"] = cutoff

	// Stale: updated before cutoff — assigned, active, but no news.
	b.tasks.store["T-stale"] = &Task{
		ID: "T-stale", AssignedAgent: "alice", State: TaskStateInProgress,
		UpdatedAt: cutoff.Add(-time.Second),
	}
	// Fresh: updated after cutoff — should appear in Resumable.
	b.tasks.store["T-fresh"] = &Task{
		ID: "T-fresh", AssignedAgent: "alice", State: TaskStateBlocked,
		UpdatedAt: cutoff.Add(time.Second),
	}
	// Not assigned to alice — must not appear even if fresh.
	b.tasks.store["T-other"] = &Task{
		ID: "T-other", AssignedAgent: "bob", State: TaskStateInProgress,
		UpdatedAt: cutoff.Add(time.Second),
	}

	box, err := New(b).PeekInbox("alice")
	if err != nil {
		t.Fatalf("PeekInbox: %v", err)
	}
	if len(box.Resumable) != 1 {
		t.Fatalf("want 1 resumable, got %d (%v)", len(box.Resumable), box.Resumable)
	}
	if box.Resumable[0].ID != "T-fresh" {
		t.Errorf("got resumable %q, want T-fresh", box.Resumable[0].ID)
	}
}

func TestInbox_PlansFilteredByAssigneeAndCategory(t *testing.T) {
	b := newStubBackend()
	for _, ps := range []struct {
		id, assignee string
		state        PlanState
	}{
		{"P-1", "alice", PlanStateDraft},
		{"P-2", "alice", PlanStateActive},
		{"P-3", "alice", PlanStateOnHold},
		{"P-4", "alice", PlanStateCompleted},
		{"P-5", "alice", PlanStateCancelled},
		{"P-6", "bob", PlanStateDraft},
	} {
		b.plans.store[ps.id] = &Plan{ID: ps.id, AssignedAgent: ps.assignee, State: ps.state}
	}

	box, _ := New(b).Inbox("alice")
	got := map[string]bool{}
	for _, p := range box.Plans {
		got[p.ID] = true
	}
	for _, id := range []string{"P-1", "P-2", "P-3"} {
		if !got[id] {
			t.Errorf("missing plan %q", id)
		}
	}
	for _, id := range []string{"P-4", "P-5", "P-6"} {
		if got[id] {
			t.Errorf("unexpected plan %q in inbox", id)
		}
	}
}

func TestInbox_RecentChangesExcludeOwnActions(t *testing.T) {
	b := newStubBackend()
	b.tasks.store["T-1"] = &Task{ID: "T-1", AssignedAgent: "alice", State: TaskStateTodo}
	// Alice's own action; should NOT appear.
	_ = b.events.Append(&Event{Kind: EventTaskStateChanged, TaskID: "T-1", Actor: "alice"})
	// Bob's action on alice's task; SHOULD appear.
	_ = b.events.Append(&Event{Kind: EventTaskStateChanged, TaskID: "T-1", Actor: "bob"})

	box, _ := New(b).Inbox("alice")
	if len(box.RecentChanges) != 1 {
		t.Fatalf("want 1 recent change, got %d", len(box.RecentChanges))
	}
	if box.RecentChanges[0].Actor != "bob" {
		t.Errorf("got actor %q, want bob", box.RecentChanges[0].Actor)
	}
}

func TestInbox_RecentChangesIncludeReassignToMe(t *testing.T) {
	b := newStubBackend()
	// Task currently belongs to bob (so not in alice's assigned list), but
	// the journal records a reassignment whose payload `to` is alice.
	b.tasks.store["T-99"] = &Task{ID: "T-99", AssignedAgent: "bob", State: TaskStateTodo}
	_ = b.events.Append(&Event{
		Kind:    EventTaskAssigned,
		TaskID:  "T-99",
		Actor:   "bob",
		Payload: map[string]any{"from": "", "to": "alice"},
	})

	box, _ := New(b).Inbox("alice")
	if len(box.RecentChanges) != 1 {
		t.Fatalf("want 1 recent change, got %d", len(box.RecentChanges))
	}
	if box.RecentChanges[0].TaskID != "T-99" {
		t.Errorf("got task %q, want T-99", box.RecentChanges[0].TaskID)
	}
}

func TestInbox_RecentChangesRespectSince(t *testing.T) {
	b := newStubBackend()
	b.tasks.store["T-1"] = &Task{ID: "T-1", AssignedAgent: "alice", State: TaskStateTodo}
	// Three events at synthetic timestamps t=1,2,3.
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})

	// Pre-seed alice's cursor to the second event's timestamp; only event #3
	// should show up.
	b.cursors.store["alice"] = time.Unix(2, 0).UTC()

	box, _ := New(b).Inbox("alice")
	if len(box.RecentChanges) != 1 {
		t.Fatalf("want 1 recent change after cursor, got %d", len(box.RecentChanges))
	}
}

func TestInbox_RecentChangesChronological(t *testing.T) {
	b := newStubBackend()
	b.tasks.store["T-1"] = &Task{ID: "T-1", AssignedAgent: "alice", State: TaskStateTodo}
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})

	box, _ := New(b).Inbox("alice")
	if len(box.RecentChanges) != 3 {
		t.Fatalf("want 3, got %d", len(box.RecentChanges))
	}
	for i := 1; i < len(box.RecentChanges); i++ {
		if box.RecentChanges[i].Timestamp.Before(box.RecentChanges[i-1].Timestamp) {
			t.Errorf("event %d (%v) before event %d (%v)", i, box.RecentChanges[i].Timestamp, i-1, box.RecentChanges[i-1].Timestamp)
		}
	}
}

func TestInbox_AdvancesCursor(t *testing.T) {
	b := newStubBackend()
	b.tasks.store["T-1"] = &Task{ID: "T-1", AssignedAgent: "alice", State: TaskStateTodo}
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})

	c := New(b)
	if _, err := c.Inbox("alice"); err != nil {
		t.Fatalf("first Inbox: %v", err)
	}
	if b.cursors.setN != 1 {
		t.Fatalf("want 1 Set call, got %d", b.cursors.setN)
	}
	cur, _ := b.cursors.Get("alice")
	if cur.IsZero() {
		t.Fatal("cursor not advanced past zero")
	}

	// A second Inbox immediately after should see no new changes — the cursor
	// is now past everything in the journal.
	box2, _ := c.Inbox("alice")
	if len(box2.RecentChanges) != 0 {
		t.Errorf("want 0 changes after cursor advance, got %d", len(box2.RecentChanges))
	}
}

func TestInbox_LastSeenAtIsPreviousCursor(t *testing.T) {
	b := newStubBackend()
	b.tasks.store["T-1"] = &Task{ID: "T-1", AssignedAgent: "alice", State: TaskStateTodo}
	pre := time.Unix(42, 0).UTC()
	b.cursors.store["alice"] = pre

	box, _ := New(b).Inbox("alice")
	if !box.LastSeenAt.Equal(pre) {
		t.Errorf("LastSeenAt: got %v, want %v", box.LastSeenAt, pre)
	}
}

func TestPeekInbox_DoesNotAdvanceCursor(t *testing.T) {
	b := newStubBackend()
	b.tasks.store["T-1"] = &Task{ID: "T-1", AssignedAgent: "alice", State: TaskStateTodo}
	_ = b.events.Append(&Event{Kind: EventTaskCommented, TaskID: "T-1", Actor: "bob"})

	c := New(b)
	if _, err := c.PeekInbox("alice"); err != nil {
		t.Fatalf("PeekInbox: %v", err)
	}
	if b.cursors.setN != 0 {
		t.Errorf("Peek should not call Set; setN=%d", b.cursors.setN)
	}
	cur, _ := b.cursors.Get("alice")
	if !cur.IsZero() {
		t.Errorf("cursor should remain zero after Peek, got %v", cur)
	}
}
