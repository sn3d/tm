package client

import (
	"reflect"
	"testing"
)

// kinds returns just the EventKind values from events, in order.
func kinds(events []Event) []EventKind {
	out := make([]EventKind, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

func TestCreateTask_EmitsCreatedEvent(t *testing.T) {
	b := newStubBackend()
	c := New(b, WithActor("alice"))
	id, err := c.CreateTask(CreateTaskInput{Subject: "subj", Description: "desc", AssignedAgent: "agent-x"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(b.events.appended) != 1 {
		t.Fatalf("want 1 event, got %d", len(b.events.appended))
	}
	e := b.events.appended[0]
	if e.Kind != EventTaskCreated {
		t.Errorf("kind: %s", e.Kind)
	}
	if e.TaskID != id {
		t.Errorf("task id: got %q want %q", e.TaskID, id)
	}
	if e.Actor != "alice" {
		t.Errorf("actor: %q", e.Actor)
	}
	if e.Payload["subject"] != "subj" {
		t.Errorf("payload subject: %v", e.Payload["subject"])
	}
	if e.Payload["assigned_agent"] != "agent-x" {
		t.Errorf("payload assigned_agent: %v", e.Payload["assigned_agent"])
	}
	if _, present := e.Payload["plan_id"]; present {
		t.Error("plan_id should be omitted when empty")
	}
}

func TestCreateTask_PayloadOmitsEmptyDependsOn(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	if _, err := c.CreateTask(CreateTaskInput{Subject: "s"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, present := b.events.appended[0].Payload["depends_on"]; present {
		t.Error("depends_on should be omitted when empty")
	}
}

func TestEditTask_EmitsOnlyChangedFieldEvents(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, _ := c.CreateTask(CreateTaskInput{Subject: "subj", AssignedAgent: "alice"})
	b.events.appended = nil // reset to focus on Edit events

	// Change only state.
	if err := c.EditTask(id, EditTaskInput{Subject: "subj", State: TaskStateInProgress, AssignedAgent: "alice"}); err != nil {
		t.Fatalf("EditTask: %v", err)
	}
	got := kinds(b.events.appended)
	want := []EventKind{EventTaskStateChanged}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kinds: got %v, want %v", got, want)
	}
	e := b.events.appended[0]
	if e.Payload["from"] != "todo" || e.Payload["to"] != "in_progress" {
		t.Errorf("state payload: %+v", e.Payload)
	}
}

func TestEditTask_EmitsAssignedAndPlanAndDeps(t *testing.T) {
	b := newStubBackend()
	b.seedPlan("PLAN-1")
	b.seedPlan("PLAN-2")
	b.seed("dep-a")
	b.seed("dep-b")

	c := New(b)
	id, _ := c.CreateTask(CreateTaskInput{Subject: "subj", AssignedAgent: "alice", DependsOn: []TaskID{"dep-a"}, PlanID: "PLAN-1"})
	b.events.appended = nil

	if err := c.EditTask(id, EditTaskInput{Subject: "subj", State: TaskStateTodo, AssignedAgent: "bob", DependsOn: []TaskID{"dep-b"}, PlanID: "PLAN-2"}); err != nil {
		t.Fatalf("EditTask: %v", err)
	}
	got := kinds(b.events.appended)
	want := []EventKind{EventTaskAssigned, EventTaskDependsOnChanged, EventTaskPlanChanged}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kinds: got %v, want %v", got, want)
	}
}

func TestEditTask_NoEventsWhenNothingChanged(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, _ := c.CreateTask(CreateTaskInput{Subject: "subj", Description: "desc", AssignedAgent: "alice"})
	b.events.appended = nil

	if err := c.EditTask(id, EditTaskInput{Subject: "subj", Description: "desc", State: TaskStateTodo, AssignedAgent: "alice"}); err != nil {
		t.Fatalf("EditTask: %v", err)
	}
	if len(b.events.appended) != 0 {
		t.Errorf("expected no events, got %+v", b.events.appended)
	}
}

func TestEditTask_EditedEventCarriesOnlyDiffFields(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, _ := c.CreateTask(CreateTaskInput{Subject: "subj", Description: "desc"})
	b.events.appended = nil

	if err := c.EditTask(id, EditTaskInput{Subject: "new-subj", Description: "desc", State: TaskStateTodo}); err != nil {
		t.Fatalf("EditTask: %v", err)
	}
	if len(b.events.appended) != 1 || b.events.appended[0].Kind != EventTaskEdited {
		t.Fatalf("expected single task.edited, got %+v", b.events.appended)
	}
	p := b.events.appended[0].Payload
	from := p["from"].(map[string]any)
	to := p["to"].(map[string]any)
	if _, has := from["description"]; has {
		t.Error("from should not include unchanged description")
	}
	if from["subject"] != "subj" || to["subject"] != "new-subj" {
		t.Errorf("diff payload: %+v", p)
	}
}

func TestAddTaskComment_EmitsCommentEventWithoutBody(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, _ := c.CreateTask(CreateTaskInput{Subject: "s"})
	b.events.appended = nil

	if err := c.AddTaskComment(id, "alice", "sensitive body should not leak"); err != nil {
		t.Fatalf("AddTaskComment: %v", err)
	}
	if len(b.events.appended) != 1 || b.events.appended[0].Kind != EventTaskCommented {
		t.Fatalf("expected single task.commented, got %+v", b.events.appended)
	}
	e := b.events.appended[0]
	if e.Payload["who"] != "alice" {
		t.Errorf("payload who: %v", e.Payload["who"])
	}
	if _, present := e.Payload["comment"]; present {
		t.Error("payload must not contain comment body")
	}
	if _, present := e.Payload["body"]; present {
		t.Error("payload must not contain body")
	}
}

func TestCreatePlan_EmitsCreatedEvent(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, err := c.CreatePlan("subj", "", "alice")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if len(b.events.appended) != 1 {
		t.Fatalf("want 1 event, got %d", len(b.events.appended))
	}
	e := b.events.appended[0]
	if e.Kind != EventPlanCreated || e.PlanID != id {
		t.Errorf("unexpected event: %+v", e)
	}
}

func TestEditPlan_EmitsFieldEvents(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, _ := c.CreatePlan("subj", "desc", "alice")
	b.events.appended = nil

	if err := c.EditPlan(id, "new-subj", "desc", PlanStateActive, "bob"); err != nil {
		t.Fatalf("EditPlan: %v", err)
	}
	got := kinds(b.events.appended)
	want := []EventKind{EventPlanEdited, EventPlanStateChanged, EventPlanAssigned}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kinds: got %v, want %v", got, want)
	}
}

func TestAddPlanComment_EmitsCommentEventWithoutBody(t *testing.T) {
	b := newStubBackend()
	c := New(b)
	id, _ := c.CreatePlan("s", "", "")
	b.events.appended = nil

	if err := c.AddPlanComment(id, "alice", "private body"); err != nil {
		t.Fatalf("AddPlanComment: %v", err)
	}
	if len(b.events.appended) != 1 || b.events.appended[0].Kind != EventPlanCommented {
		t.Fatalf("expected single plan.commented, got %+v", b.events.appended)
	}
	if _, present := b.events.appended[0].Payload["comment"]; present {
		t.Error("payload must not contain comment body")
	}
}

func TestListEvents_PassesThroughToBackend(t *testing.T) {
	b := newStubBackend()
	c := New(b, WithActor("alice"))
	_, _ = c.CreateTask(CreateTaskInput{Subject: "s"})
	_, _ = c.CreatePlan("p", "", "")

	got, err := c.ListEvents(EventFilter{Kinds: []EventKind{EventTaskCreated}})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || got[0].Kind != EventTaskCreated {
		t.Errorf("filter not applied: %+v", got)
	}
}
