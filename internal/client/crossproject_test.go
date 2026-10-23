package client

import (
	"testing"
	"time"
)

// newMultiProjectClient wires an unbound Client over a backend holding
// several projects, which is what the cross-project reads operate on.
func newMultiProjectClient(t *testing.T, tickers ...string) (*Client, *stubBackend) {
	t.Helper()
	b := newStubBackend(tickers...)
	return New(b, WithActor("alice")), b
}

func TestInboxAll_OneInboxPerProjectWithWork(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill", "infra")

	// alice has work in tm and bill, nothing in infra.
	b.tasks.store[TaskRef{Project: "tm", ID: "1"}] = &Task{
		ID: TaskRef{Project: "tm", ID: "1"}, AssignedAgent: "alice", State: TaskStateTodo}
	b.tasks.store[TaskRef{Project: "bill", ID: "1"}] = &Task{
		ID: TaskRef{Project: "bill", ID: "1"}, AssignedAgent: "alice", State: TaskStateInProgress}
	b.tasks.store[TaskRef{Project: "infra", ID: "1"}] = &Task{
		ID: TaskRef{Project: "infra", ID: "1"}, AssignedAgent: "bob", State: TaskStateTodo}

	boxes, err := c.InboxAll("alice")
	if err != nil {
		t.Fatalf("InboxAll: %v", err)
	}
	if len(boxes) != 2 {
		t.Fatalf("got %d inboxes, want 2 (projects with nothing to do are skipped)", len(boxes))
	}
	// Ordered by ticker, so the output is stable.
	if boxes[0].Project != "bill" || boxes[1].Project != "tm" {
		t.Errorf("projects = %q, %q; want bill, tm (ticker order)", boxes[0].Project, boxes[1].Project)
	}
	for _, box := range boxes {
		if len(box.Tasks) != 1 {
			t.Errorf("%s inbox has %d tasks, want 1", box.Project, len(box.Tasks))
		}
		// Each task is qualified, so output can render TICKER/#id.
		if box.Tasks[0].ID.Project != box.Project {
			t.Errorf("task %s is not in its own inbox's project %q", box.Tasks[0].ID, box.Project)
		}
	}
}

func TestInboxAll_SkipsArchivedProjects(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "old")
	now := time.Now().UTC()
	archived := b.projects.store["old"]
	archived.ArchivedAt = &now

	b.tasks.store[TaskRef{Project: "tm", ID: "1"}] = &Task{
		ID: TaskRef{Project: "tm", ID: "1"}, AssignedAgent: "alice", State: TaskStateTodo}
	b.tasks.store[TaskRef{Project: "old", ID: "1"}] = &Task{
		ID: TaskRef{Project: "old", ID: "1"}, AssignedAgent: "alice", State: TaskStateTodo}

	boxes, err := c.InboxAll("alice")
	if err != nil {
		t.Fatalf("InboxAll: %v", err)
	}
	for _, box := range boxes {
		if box.Project == "old" {
			t.Error("an archived project must not appear in the cross-project inbox")
		}
	}
}

// TestInboxAll_CursorsAreIndependent is the whole reason cursors are keyed by
// (actor, project): reading one project must not mark another's work as seen.
func TestInboxAll_CursorsAreIndependent(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")

	base := time.Now().UTC().Add(-time.Hour)
	for _, p := range []string{"tm", "bill"} {
		b.tasks.store[TaskRef{Project: p, ID: "1"}] = &Task{
			ID:            TaskRef{Project: p, ID: "1"},
			AssignedAgent: "alice",
			State:         TaskStateTodo,
			UpdatedAt:     base,
		}
	}

	// Read only tm's inbox through a bound client.
	tmClient, err := c.ForProject("tm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmClient.Inbox("alice"); err != nil {
		t.Fatalf("Inbox: %v", err)
	}

	// tm's cursor advanced; bill's did not.
	tmCursor, _ := b.cursors.Get("alice", "tm")
	if tmCursor.IsZero() {
		t.Error("tm cursor should have advanced")
	}
	billCursor, _ := b.cursors.Get("alice", "bill")
	if !billCursor.IsZero() {
		t.Errorf("bill cursor = %v, want zero; reading tm must not mark bill as seen", billCursor)
	}
}

func TestPeekInboxAll_DoesNotAdvanceCursors(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")
	for _, p := range []string{"tm", "bill"} {
		b.tasks.store[TaskRef{Project: p, ID: "1"}] = &Task{
			ID: TaskRef{Project: p, ID: "1"}, AssignedAgent: "alice", State: TaskStateTodo}
	}

	if _, err := c.PeekInboxAll("alice"); err != nil {
		t.Fatalf("PeekInboxAll: %v", err)
	}
	if b.cursors.setN != 0 {
		t.Errorf("peek advanced %d cursors, want 0", b.cursors.setN)
	}
}

func TestInboxAll_AdvancesEachReportedProject(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")
	for _, p := range []string{"tm", "bill"} {
		b.tasks.store[TaskRef{Project: p, ID: "1"}] = &Task{
			ID: TaskRef{Project: p, ID: "1"}, AssignedAgent: "alice", State: TaskStateTodo}
	}

	if _, err := c.InboxAll("alice"); err != nil {
		t.Fatalf("InboxAll: %v", err)
	}
	for _, p := range []string{"tm", "bill"} {
		if ts, _ := b.cursors.Get("alice", p); ts.IsZero() {
			t.Errorf("cursor for %q did not advance", p)
		}
	}
}

func TestInboxAll_RequiresActor(t *testing.T) {
	c, _ := newMultiProjectClient(t, "tm")
	if _, err := c.InboxAll(""); err == nil {
		t.Error("InboxAll with no actor should error")
	}
	if _, err := c.PeekInboxAll(""); err == nil {
		t.Error("PeekInboxAll with no actor should error")
	}
}

func TestJournalAll_SpansEveryProject(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")

	_ = b.events.Append(&Event{Kind: EventTaskCreated, TaskID: TaskRef{Project: "tm", ID: "1"}, Actor: "alice"})
	_ = b.events.Append(&Event{Kind: EventTaskCreated, TaskID: TaskRef{Project: "bill", ID: "1"}, Actor: "bob"})

	events, err := c.JournalAll(EventFilter{})
	if err != nil {
		t.Fatalf("JournalAll: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 across both projects", len(events))
	}
	// Each event carries its own project so output can render TICKER/#id.
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Project] = true
		if e.TaskID.Project != e.Project {
			t.Errorf("event task %s disagrees with its project %q", e.TaskID, e.Project)
		}
	}
	if !seen["tm"] || !seen["bill"] {
		t.Errorf("expected events from both projects, got %v", seen)
	}
}

func TestJournalAll_HonoursExplicitProjectFilter(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")
	_ = b.events.Append(&Event{Kind: EventTaskCreated, TaskID: TaskRef{Project: "tm", ID: "1"}, Actor: "alice"})
	_ = b.events.Append(&Event{Kind: EventTaskCreated, TaskID: TaskRef{Project: "bill", ID: "1"}, Actor: "bob"})

	// A caller may narrow after asking for the cross-project view.
	events, err := c.JournalAll(EventFilter{Project: "bill"})
	if err != nil {
		t.Fatalf("JournalAll: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Project != "bill" {
		t.Errorf("event project = %q, want bill", events[0].Project)
	}
}

func TestTimesheetAll_SpansEveryProject(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")

	from := time.Now().UTC().Add(-time.Hour)
	to := time.Now().UTC().Add(time.Hour)
	mid := time.Now().UTC()

	for _, p := range []string{"tm", "bill"} {
		taskRef := TaskRef{Project: p, ID: "1"}
		b.tasks.store[taskRef] = &Task{ID: taskRef, Subject: p + " work"}
		_ = b.events.Append(&Event{
			Kind: EventTaskCreated, TaskID: taskRef, Actor: "alice",
			Timestamp: mid, Payload: map[string]any{"subject": p + " work"},
		})
	}

	entries, err := c.TimesheetAll(from, to, "")
	if err != nil {
		t.Fatalf("TimesheetAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 across both projects", len(entries))
	}
	// Entries carry fully-qualified refs so a weekly report can attribute
	// work per repo.
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.TaskID.Project] = true
		if e.TaskSubject == "" {
			t.Errorf("entry %s lost its subject", e.TaskID)
		}
	}
	if !seen["tm"] || !seen["bill"] {
		t.Errorf("expected entries from both projects, got %v", seen)
	}
}

func TestTimesheetAll_RejectsInvertedWindow(t *testing.T) {
	c, _ := newMultiProjectClient(t, "tm")
	now := time.Now().UTC()
	if _, err := c.TimesheetAll(now, now.Add(-time.Hour), ""); err == nil {
		t.Error("TimesheetAll should reject a window whose end precedes its start")
	}
}

// TestUnboundClient_SingleProjectOpsError proves the guard: a Client with no
// project must refuse every single-project operation rather than silently
// writing to a guessed project.
func TestUnboundClient_SingleProjectOpsError(t *testing.T) {
	c := New(newStubBackend("tm"), WithActor("alice"))

	if _, err := c.CreateTask(CreateTaskInput{Subject: "s"}); err == nil {
		t.Error("CreateTask on an unbound client should error")
	}
	if _, err := c.ListTasks(ArchivedActive); err == nil {
		t.Error("ListTasks on an unbound client should error")
	}
	if _, err := c.Inbox("alice"); err == nil {
		t.Error("Inbox on an unbound client should error")
	}
	if _, err := c.ListEvents(EventFilter{}); err == nil {
		t.Error("ListEvents on an unbound client should error")
	}
	if _, err := c.GetTasksByLabel("plan", ArchivedActive); err == nil {
		t.Error("GetTasksByLabel on an unbound client should error")
	}
}

func TestForProject_RebindsSameBackend(t *testing.T) {
	c, b := newMultiProjectClient(t, "tm", "bill")

	tmClient, err := c.ForProject("TM")
	if err != nil {
		t.Fatalf("ForProject: %v", err)
	}
	if tmClient.Project() != "tm" {
		t.Errorf("Project() = %q, want the canonical %q", tmClient.Project(), "tm")
	}
	// The actor carries over.
	if tmClient.Actor() != "alice" {
		t.Errorf("Actor() = %q, want %q", tmClient.Actor(), "alice")
	}

	// Writes through the rebound client land in the shared backend.
	id, err := tmClient.CreateTask(CreateTaskInput{Subject: "via rebind"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, ok := b.tasks.store[id]; !ok {
		t.Error("the rebound client should share the original backend")
	}
	if id.Project != "tm" {
		t.Errorf("created task project = %q, want tm", id.Project)
	}

	if _, err := c.ForProject("bad_ticker"); err == nil {
		t.Error("ForProject with an invalid ticker should error")
	}
}

// TestCreateTask_RejectsCrossProjectLinks covers the design's non-goal:
// parent and dependency links are same-project only, and the failure must be
// an actionable error rather than a silent "task not found".
func TestCreateTask_RejectsCrossProjectLinks(t *testing.T) {
	b := newStubBackend("tm", "bill")
	c := New(b, WithProject("tm"))

	otherRef := TaskRef{Project: "bill", ID: "1"}
	b.tasks.store[otherRef] = &Task{ID: otherRef, Subject: "in bill"}

	_, err := c.CreateTask(CreateTaskInput{Subject: "s", ParentID: otherRef})
	if err == nil {
		t.Fatal("a cross-project parent should be rejected")
	}
	if !contains(err.Error(), "another project") {
		t.Errorf("error should explain the cross-project restriction, got: %v", err)
	}

	_, err = c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{otherRef}})
	if err == nil {
		t.Fatal("a cross-project dependency should be rejected")
	}
	if !contains(err.Error(), "another project") {
		t.Errorf("error should explain the cross-project restriction, got: %v", err)
	}
}

// TestEditTask_RejectsTaskFromAnotherProject guards the bound-project check
// on the write path.
func TestEditTask_RejectsTaskFromAnotherProject(t *testing.T) {
	b := newStubBackend("tm", "bill")
	c := New(b, WithProject("tm"))

	otherRef := TaskRef{Project: "bill", ID: "1"}
	b.tasks.store[otherRef] = &Task{ID: otherRef, Subject: "in bill"}

	err := c.EditTask(otherRef, EditTaskInput{Subject: "hijacked", State: TaskStateTodo})
	if err == nil {
		t.Fatal("editing a task from another project should be rejected")
	}
	// And the task is untouched.
	if b.tasks.store[otherRef].Subject != "in bill" {
		t.Error("the other project's task was modified")
	}
}

func TestEmit_TagsEventWithTaskProject(t *testing.T) {
	b := newStubBackend("tm")
	c := New(b, WithProject("tm"), WithActor("alice"))

	if _, err := c.CreateTask(CreateTaskInput{Subject: "s"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(b.events.appended) != 1 {
		t.Fatalf("got %d events, want 1", len(b.events.appended))
	}
	if b.events.appended[0].Project != "tm" {
		t.Errorf("event project = %q, want tm", b.events.appended[0].Project)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
