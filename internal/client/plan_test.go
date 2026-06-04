package client

import (
	"errors"
	"testing"
)

func TestCreatePlan_AssignsID(t *testing.T) {
	c := New(newStubBackend())
	id, err := c.CreatePlan("plan subject", "desc", "alice")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty plan ID")
	}
}

func TestCreatePlan_StartsDraft(t *testing.T) {
	c := New(newStubBackend())
	id, err := c.CreatePlan("s", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	got, err := c.GetPlan(id)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != PlanStateDraft {
		t.Errorf("expected PlanStateDraft, got %v", got.State)
	}
}

func TestGetPlan_NotFound(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.GetPlan("missing")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" || nfe.ID != "missing" {
		t.Errorf("unexpected NotFoundError: %+v", nfe)
	}
}

func TestListPlans(t *testing.T) {
	c := New(newStubBackend())
	if _, err := c.CreatePlan("a", "", ""); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if _, err := c.CreatePlan("b", "", ""); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	plans, err := c.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(plans))
	}
}

func TestUpdatePlan(t *testing.T) {
	c := New(newStubBackend())
	id, err := c.CreatePlan("initial", "d1", "alice")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := c.EditPlan(id, "updated", "d2", PlanStateActive, "bob"); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	got, err := c.GetPlan(id)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Subject != "updated" || got.Description != "d2" || got.State != PlanStateActive || got.AssignedAgent != "bob" {
		t.Errorf("update did not persist: %+v", got)
	}
}

func TestUpdatePlan_NotFound(t *testing.T) {
	c := New(newStubBackend())
	err := c.EditPlan("missing", "s", "", PlanStateDraft, "")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" {
		t.Errorf("expected resource=plan, got %q", nfe.Resource)
	}
}

func TestCreate_PlanMustExist(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.CreateTask("s", "", "", nil, "missing-plan")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" || nfe.ID != "missing-plan" {
		t.Errorf("unexpected NotFoundError: %+v", nfe)
	}
}

func TestCreate_WithExistingPlan(t *testing.T) {
	b := newStubBackend()
	b.seedPlan("PLAN-1")
	c := New(b)

	id, err := c.CreateTask("s", "", "", nil, "PLAN-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := c.GetTask(id)
	if got.PlanID != "PLAN-1" {
		t.Errorf("expected PlanID=PLAN-1, got %q", got.PlanID)
	}
}

func TestUpdate_PlanMustExist(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	c := New(b)

	err := c.EditTask("1", "s", "", TaskStateTodo, "", nil, "missing-plan")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" {
		t.Errorf("expected resource=plan, got %q", nfe.Resource)
	}
}

func TestUpdate_ClearPlan(t *testing.T) {
	b := newStubBackend()
	b.seedPlan("PLAN-1")
	c := New(b)
	id, err := c.CreateTask("s", "", "", nil, "PLAN-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := c.EditTask(id, "s", "", TaskStateTodo, "", nil, ""); err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	got, _ := c.GetTask(id)
	if got.PlanID != "" {
		t.Errorf("expected PlanID cleared, got %q", got.PlanID)
	}
}

func TestGetTasksByPlan(t *testing.T) {
	b := newStubBackend()
	b.seedPlan("PLAN-1")
	b.seedPlan("PLAN-2")
	c := New(b)

	if _, err := c.CreateTask("a", "", "", nil, "PLAN-1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := c.CreateTask("b", "", "", nil, "PLAN-1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := c.CreateTask("c", "", "", nil, "PLAN-2"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := c.CreateTask("d", "", "", nil, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tasks, err := c.GetTasksByPlan("PLAN-1")
	if err != nil {
		t.Fatalf("GetTasksByPlan: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks for PLAN-1, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.PlanID != "PLAN-1" {
			t.Errorf("task %q has wrong PlanID %q", task.ID, task.PlanID)
		}
	}
}

func TestGetTasksByPlan_PlanNotFound(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.GetTasksByPlan("missing")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" {
		t.Errorf("expected resource=plan, got %q", nfe.Resource)
	}
}

func TestGetTasksByPlan_StandaloneSkipsValidation(t *testing.T) {
	b := newStubBackend()
	b.seedPlan("PLAN-1")
	c := New(b)

	if _, err := c.CreateTask("standalone-a", "", "", nil, ""); err != nil {
		t.Fatalf("Create standalone-a: %v", err)
	}
	if _, err := c.CreateTask("in-plan", "", "", nil, "PLAN-1"); err != nil {
		t.Fatalf("Create in-plan: %v", err)
	}
	if _, err := c.CreateTask("standalone-b", "", "", nil, ""); err != nil {
		t.Fatalf("Create standalone-b: %v", err)
	}

	tasks, err := c.GetTasksByPlan("")
	if err != nil {
		t.Fatalf("GetTasksByPlan(\"\"): %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 standalone tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.PlanID != "" {
			t.Errorf("task %q has non-empty PlanID %q", task.ID, task.PlanID)
		}
	}
}

func TestAddPlanComment(t *testing.T) {
	c := New(newStubBackend())
	id, err := c.CreatePlan("s", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if err := c.AddPlanComment(id, "alice", "good plan"); err != nil {
		t.Fatalf("AddPlanComment: %v", err)
	}
	comments, err := c.GetPlanComments(id)
	if err != nil {
		t.Fatalf("GetPlanComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Who != "alice" || comments[0].Comment != "good plan" {
		t.Errorf("unexpected comments: %+v", comments)
	}
}

func TestAddPlanComment_PlanNotFound(t *testing.T) {
	c := New(newStubBackend())
	err := c.AddPlanComment("missing", "alice", "hi")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" {
		t.Errorf("expected resource=plan, got %q", nfe.Resource)
	}
}

func TestGetPlanComments_PlanNotFound(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.GetPlanComments("missing")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.Resource != "plan" {
		t.Errorf("expected resource=plan, got %q", nfe.Resource)
	}
}

func TestParsePlanState_Valid(t *testing.T) {
	for _, s := range PlanStates {
		got, err := ParsePlanState(string(s))
		if err != nil {
			t.Errorf("ParsePlanState(%q): unexpected error %v", s, err)
		}
		if got != s {
			t.Errorf("ParsePlanState(%q) = %q, want %q", s, got, s)
		}
	}
}

func TestParsePlanState_Invalid(t *testing.T) {
	for _, s := range []string{"", "todo", "in_progress", "blocked"} {
		if _, err := ParsePlanState(s); err == nil {
			t.Errorf("ParsePlanState(%q): expected error, got nil", s)
		}
	}
}

func TestPlanStateCategory(t *testing.T) {
	tests := []struct {
		state PlanState
		want  Category
	}{
		{PlanStateDraft, CategoryOpen},
		{PlanStateActive, CategoryActive},
		{PlanStateOnHold, CategoryActive},
		{PlanStateCompleted, CategoryDone},
		{PlanStateCancelled, CategoryCancelled},
	}
	for _, tt := range tests {
		if got := tt.state.Category(); got != tt.want {
			t.Errorf("%s.Category() = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestPlanStateDefault(t *testing.T) {
	if !PlanStateDefault.Valid() {
		t.Errorf("PlanStateDefault %q is not a valid PlanState", PlanStateDefault)
	}
}
