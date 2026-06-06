package client

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stubBackend is an in-memory Backend used only by these tests. It exists
// because Client.Create/Update need a real backend to look up dependency
// targets and walk the dependency graph for cycle detection.
type stubBackend struct {
	tasks        *stubTasks
	comments     *stubComments
	plans        *stubPlans
	planComments *stubPlanComments
	events       *stubEvents
	cursors      *stubActorCursors
}

func newStubBackend() *stubBackend {
	return &stubBackend{
		tasks:        &stubTasks{store: map[TaskID]*Task{}},
		comments:     &stubComments{},
		plans:        &stubPlans{store: map[PlanID]*Plan{}},
		planComments: &stubPlanComments{store: map[PlanID][]Comment{}},
		events:       &stubEvents{},
		cursors:      &stubActorCursors{store: map[string]time.Time{}},
	}
}

func (b *stubBackend) Tasks() TasksRepository                 { return b.tasks }
func (b *stubBackend) Comments() CommentsRepository           { return b.comments }
func (b *stubBackend) Plans() PlansRepository                 { return b.plans }
func (b *stubBackend) PlanComments() PlanCommentsRepository   { return b.planComments }
func (b *stubBackend) Events() EventsRepository               { return b.events }
func (b *stubBackend) ActorCursors() ActorCursorRepository    { return b.cursors }

type stubActorCursors struct {
	store map[string]time.Time
	setN  int
}

func (s *stubActorCursors) Get(actor string) (time.Time, error) {
	return s.store[actor], nil
}

func (s *stubActorCursors) Set(actor string, ts time.Time) error {
	s.setN++
	s.store[actor] = ts
	return nil
}

type stubEvents struct {
	appended []Event
	next     int
}

func (s *stubEvents) Append(e *Event) error {
	s.next++
	if e.ID == "" {
		e.ID = "EV-" + strconv.Itoa(s.next)
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Unix(int64(s.next), 0).UTC()
	}
	s.appended = append(s.appended, *e)
	return nil
}

func (s *stubEvents) List(filter EventFilter) ([]Event, error) {
	out := make([]Event, 0, len(s.appended))
	for i := len(s.appended) - 1; i >= 0; i-- {
		e := s.appended[i]
		if filter.TaskID != "" && e.TaskID != filter.TaskID {
			continue
		}
		if filter.PlanID != "" && e.PlanID != filter.PlanID {
			continue
		}
		if filter.Actor != "" && e.Actor != filter.Actor {
			continue
		}
		if len(filter.Kinds) > 0 {
			match := false
			for _, k := range filter.Kinds {
				if e.Kind == k {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if !filter.Since.IsZero() && !e.Timestamp.After(filter.Since) {
			continue
		}
		out = append(out, e)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

type stubTasks struct {
	store map[TaskID]*Task
	next  int
}

func (s *stubTasks) Save(t *Task) error {
	if t.ID == "" {
		s.next++
		t.ID = strconv.Itoa(s.next)
	}
	cp := *t
	if t.DependsOn != nil {
		cp.DependsOn = append([]TaskID(nil), t.DependsOn...)
	}
	s.store[t.ID] = &cp
	return nil
}

func (s *stubTasks) GetByID(id TaskID) (*Task, error) {
	t, ok := s.store[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	if t.DependsOn != nil {
		cp.DependsOn = append([]TaskID(nil), t.DependsOn...)
	}
	return &cp, nil
}

func (s *stubTasks) GetAll() ([]Task, error) {
	out := make([]Task, 0, len(s.store))
	for _, t := range s.store {
		out = append(out, *t)
	}
	return out, nil
}

func (s *stubTasks) GetByPlan(planID PlanID) ([]Task, error) {
	out := make([]Task, 0)
	for _, t := range s.store {
		if t.PlanID == planID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (s *stubTasks) GetByParent(parentID TaskID) ([]Task, error) {
	out := make([]Task, 0)
	for _, t := range s.store {
		if t.ParentID == parentID {
			out = append(out, *t)
		}
	}
	return out, nil
}

type stubComments struct{}

func (stubComments) Add(TaskID, *Comment) error           { return nil }
func (stubComments) GetForTask(TaskID) ([]Comment, error) { return nil, nil }

type stubPlans struct {
	store map[PlanID]*Plan
	next  int
}

func (s *stubPlans) Save(p *Plan) error {
	if p.ID == "" {
		s.next++
		p.ID = "PLAN-" + strconv.Itoa(s.next)
	}
	cp := *p
	s.store[p.ID] = &cp
	return nil
}

func (s *stubPlans) GetByID(id PlanID) (*Plan, error) {
	p, ok := s.store[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *stubPlans) GetAll() ([]Plan, error) {
	out := make([]Plan, 0, len(s.store))
	for _, p := range s.store {
		out = append(out, *p)
	}
	return out, nil
}

type stubPlanComments struct {
	store map[PlanID][]Comment
	next  int
}

func (s *stubPlanComments) Add(id PlanID, c *Comment) error {
	s.next++
	c.ID = "C-" + strconv.Itoa(s.next)
	s.store[id] = append(s.store[id], *c)
	return nil
}

func (s *stubPlanComments) GetForPlan(id PlanID) ([]Comment, error) {
	out := make([]Comment, len(s.store[id]))
	copy(out, s.store[id])
	return out, nil
}

// seed inserts a task directly so tests can construct a graph without going
// through Client.Create (which itself enforces validation we want to test).
func (b *stubBackend) seed(id TaskID, deps ...TaskID) {
	b.tasks.store[id] = &Task{ID: id, DependsOn: append([]TaskID(nil), deps...)}
}

// seedPlan inserts a plan directly so tests can reference an existing plan
// without going through Client.CreatePlan.
func (b *stubBackend) seedPlan(id PlanID) {
	b.plans.store[id] = &Plan{ID: id}
}

func TestCreate_NoDeps(t *testing.T) {
	c := New(newStubBackend())
	id, err := c.CreateTask(CreateTaskInput{Subject: "s", Description: "d", AssignedAgent: "alice"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty ID")
	}
}

func TestCreate_DepMustExist(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{"missing"}})
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.ID != "missing" {
		t.Errorf("error ID: got %q, want %q", nfe.ID, "missing")
	}
}

func TestCreate_AcceptsExistingDeps(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	b.seed("2")
	c := New(b)

	id, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{"1", "2"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := c.GetTask(id)
	if len(got.DependsOn) != 2 || got.DependsOn[0] != "1" || got.DependsOn[1] != "2" {
		t.Errorf("DependsOn: got %v, want [1 2]", got.DependsOn)
	}
}

func TestCreate_RejectsDuplicateDeps(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	c := New(b)

	_, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{"1", "1"}})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-dependency error, got %v", err)
	}
}

func TestCreate_RejectsEmptyDep(t *testing.T) {
	c := New(newStubBackend())
	_, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{""}})
	if err == nil {
		t.Fatal("expected error for empty dependency, got nil")
	}
}

func TestUpdate_RejectsSelfDependency(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	c := New(b)

	err := c.EditTask("1", EditTaskInput{Subject: "s", State: TaskStateTodo, DependsOn: []TaskID{"1"}})
	if err == nil || !strings.Contains(err.Error(), "cannot depend on itself") {
		t.Fatalf("expected self-dependency error, got %v", err)
	}
}

func TestUpdate_DetectsDirectCycle(t *testing.T) {
	b := newStubBackend()
	b.seed("1") // no deps
	b.seed("2", "1")
	c := New(b)

	// Adding 1 -> 2 closes the loop (1 -> 2 -> 1).
	err := c.EditTask("1", EditTaskInput{Subject: "s", State: TaskStateTodo, DependsOn: []TaskID{"2"}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestUpdate_DetectsTransitiveCycle(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	b.seed("2", "1")
	b.seed("3", "2")
	c := New(b)

	// 1 -> 3 -> 2 -> 1 is a cycle.
	err := c.EditTask("1", EditTaskInput{Subject: "s", State: TaskStateTodo, DependsOn: []TaskID{"3"}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected transitive cycle error, got %v", err)
	}
}

func TestUpdate_AllowsBreakingExistingDependency(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	b.seed("2", "1")
	c := New(b)

	if err := c.EditTask("2", EditTaskInput{Subject: "s", State: TaskStateTodo}); err != nil {
		t.Fatalf("clearing deps should succeed, got %v", err)
	}
	got, _ := c.GetTask("2")
	if len(got.DependsOn) != 0 {
		t.Errorf("expected DependsOn cleared, got %v", got.DependsOn)
	}
}

func TestUpdate_PreservesDepsWhenUnchanged(t *testing.T) {
	b := newStubBackend()
	b.seed("1")
	b.seed("2", "1")
	c := New(b)

	if err := c.EditTask("2", EditTaskInput{Subject: "new subject", State: TaskStateTodo, DependsOn: []TaskID{"1"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := c.GetTask("2")
	if got.Subject != "new subject" {
		t.Errorf("Subject not updated")
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0] != "1" {
		t.Errorf("DependsOn: got %v, want [1]", got.DependsOn)
	}
}
