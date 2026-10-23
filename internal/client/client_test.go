package client

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stubBackend is an in-memory Backend used only by these tests. It exists
// because Client.Create/Update need a real backend to look up dependency
// targets and walk the dependency graph for cycle detection.
//
// It is project-aware in the same way the sqlite backend is: rows are keyed
// by the full TaskRef, and per-project ID counters are independent, so a test
// can assert that TM/#1 and BILL/#1 coexist.
type stubBackend struct {
	projects     *stubProjects
	tasks        *stubTasks
	descriptions *stubDescriptions
	comments     *stubComments
	events       *stubEvents
	cursors      *stubActorCursors
}

// testProject is the project every single-project test operates in.
const testProject = "tm"

func newStubBackend(tickers ...string) *stubBackend {
	if len(tickers) == 0 {
		tickers = []string{testProject}
	}
	projects := &stubProjects{store: map[string]*Project{}}
	for _, tk := range tickers {
		projects.store[tk] = &Project{Ticker: tk, DisplayName: strings.ToUpper(tk)}
	}
	return &stubBackend{
		projects:     projects,
		tasks:        &stubTasks{store: map[TaskID]*Task{}, next: map[string]int{}},
		descriptions: &stubDescriptions{store: map[TaskID]string{}},
		comments:     &stubComments{store: map[TaskID][]Comment{}},
		events:       &stubEvents{},
		cursors:      &stubActorCursors{store: map[cursorKey]time.Time{}},
	}
}

// newTestClient wires a Client bound to testProject, which is what almost
// every test in this file wants.
func newTestClient(opts ...Option) (*Client, *stubBackend) {
	b := newStubBackend()
	opts = append([]Option{WithProject(testProject)}, opts...)
	return New(b, opts...), b
}

// ref builds a reference in the default test project.
func ref(id string) TaskID { return TaskRef{Project: testProject, ID: id} }

func (b *stubBackend) Projects() ProjectsRepository         { return b.projects }
func (b *stubBackend) Tasks() TasksRepository               { return b.tasks }
func (b *stubBackend) Descriptions() DescriptionsRepository { return b.descriptions }
func (b *stubBackend) Comments() CommentsRepository         { return b.comments }
func (b *stubBackend) Events() EventsRepository             { return b.events }
func (b *stubBackend) ActorCursors() ActorCursorRepository  { return b.cursors }

type stubProjects struct {
	store map[string]*Project
}

func (s *stubProjects) Save(p *Project) error {
	canon, err := CanonicalTicker(p.Ticker)
	if err != nil {
		return err
	}
	p.Ticker = canon
	cp := *p
	s.store[canon] = &cp
	return nil
}

func (s *stubProjects) GetByTicker(ticker string) (*Project, error) {
	canon, err := CanonicalTicker(ticker)
	if err != nil {
		return nil, err
	}
	p, ok := s.store[canon]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *stubProjects) GetAll(includeArchived bool) ([]Project, error) {
	out := make([]Project, 0, len(s.store))
	for _, p := range s.store {
		if p.ArchivedAt != nil && !includeArchived {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ticker < out[j].Ticker })
	return out, nil
}

func (s *stubProjects) TaskCounts() (map[string]int, error) {
	return map[string]int{}, nil
}

// cursorKey mirrors the sqlite backend's (actor, project) primary key, so a
// test can prove that reading one project's inbox leaves another's cursor
// alone.
type cursorKey struct {
	actor   string
	project string
}

type stubActorCursors struct {
	store map[cursorKey]time.Time
	setN  int
}

func (s *stubActorCursors) Get(actor, project string) (time.Time, error) {
	return s.store[cursorKey{actor, project}], nil
}

func (s *stubActorCursors) Set(actor, project string, ts time.Time) error {
	s.setN++
	s.store[cursorKey{actor, project}] = ts
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
	if e.Project == "" && !e.TaskID.IsZero() {
		e.Project = e.TaskID.Project
	}
	s.appended = append(s.appended, *e)
	return nil
}

func (s *stubEvents) List(filter EventFilter) ([]Event, error) {
	out := make([]Event, 0, len(s.appended))
	for i := len(s.appended) - 1; i >= 0; i-- {
		e := s.appended[i]
		if filter.Project != "" && e.Project != filter.Project {
			continue
		}
		if !filter.TaskID.IsZero() && e.TaskID != filter.TaskID {
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
		if !filter.Until.IsZero() && !e.Timestamp.Before(filter.Until) {
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
	// next is a per-project counter, matching the real backend: numbering
	// restarts per project, so TM/#1 and BILL/#1 can both exist.
	next map[string]int
}

func (s *stubTasks) Save(project string, t *Task) error {
	canon, err := CanonicalTicker(project)
	if err != nil {
		return err
	}
	if t.ID.ID == "" {
		s.next[canon]++
		t.ID = TaskRef{Project: canon, ID: strconv.Itoa(s.next[canon])}
	} else {
		t.ID = TaskRef{Project: canon, ID: t.ID.ID}
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	t.UpdatedAt = time.Now().UTC()
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

func (s *stubTasks) GetAll(project string) ([]Task, error) {
	canon, err := CanonicalTicker(project)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(s.store))
	for _, t := range s.store {
		if t.ID.Project != canon {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}

func (s *stubTasks) GetByParent(parentRef TaskID) ([]Task, error) {
	out := make([]Task, 0)
	for _, t := range s.store {
		if t.ID.Project != parentRef.Project {
			continue
		}
		if t.ParentID.ID == parentRef.ID {
			out = append(out, *t)
		}
	}
	return out, nil
}

// stubDescriptions mirrors the real repository's lazy-row behaviour: a task
// with no description has no entry and reads back "".
type stubDescriptions struct {
	store map[TaskID]string
}

func (s *stubDescriptions) Get(ref TaskID) (string, error) {
	return s.store[ref], nil
}

func (s *stubDescriptions) Set(ref TaskID, body string) error {
	if body == "" {
		delete(s.store, ref)
		return nil
	}
	s.store[ref] = body
	return nil
}

type stubComments struct {
	store map[TaskID][]Comment
	next  int
}

func (s *stubComments) Add(id TaskID, c *Comment) error {
	if s.store == nil {
		s.store = map[TaskID][]Comment{}
	}
	if c.ID == "" {
		s.next++
		c.ID = "C-" + strconv.Itoa(s.next)
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	s.store[id] = append(s.store[id], *c)
	return nil
}

func (s *stubComments) GetForTask(id TaskID) ([]Comment, error) {
	return append([]Comment(nil), s.store[id]...), nil
}

// seed inserts a task directly so tests can construct a graph without going
// through Client.CreateTask (which itself enforces validation we want to
// test).
func (b *stubBackend) seed(id TaskID, deps ...TaskID) {
	b.tasks.store[id] = &Task{ID: id, DependsOn: append([]TaskID(nil), deps...)}
}

func TestCreate_NoDeps(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject))
	id, err := c.CreateTask(CreateTaskInput{Subject: "s", Description: "d", AssignedAgent: "alice"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == (TaskRef{}) {
		t.Error("expected non-empty ID")
	}
}

func TestCreate_DepMustExist(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject))
	_, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{ref("missing")}})
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nfe.ID != ref("missing").String() {
		t.Errorf("error ID: got %q, want %q", nfe.ID, "missing")
	}
}

func TestCreate_AcceptsExistingDeps(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1"))
	b.seed(ref("2"))
	c := New(b, WithProject(testProject))

	id, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{ref("1"), ref("2")}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := c.GetTask(id)
	if len(got.DependsOn) != 2 || got.DependsOn[0] != ref("1") || got.DependsOn[1] != ref("2") {
		t.Errorf("DependsOn: got %v, want [1 2]", got.DependsOn)
	}
}

func TestCreate_RejectsDuplicateDeps(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1"))
	c := New(b, WithProject(testProject))

	_, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{ref("1"), ref("1")}})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-dependency error, got %v", err)
	}
}

func TestCreate_RejectsEmptyDep(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject))
	_, err := c.CreateTask(CreateTaskInput{Subject: "s", DependsOn: []TaskID{ref("")}})
	if err == nil {
		t.Fatal("expected error for empty dependency, got nil")
	}
}

func TestUpdate_RejectsSelfDependency(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1"))
	c := New(b, WithProject(testProject))

	err := c.EditTask(ref("1"), EditTaskInput{Subject: "s", State: TaskStateTodo, DependsOn: []TaskID{ref("1")}})
	if err == nil || !strings.Contains(err.Error(), "cannot depend on itself") {
		t.Fatalf("expected self-dependency error, got %v", err)
	}
}

func TestUpdate_DetectsDirectCycle(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1")) // no deps
	b.seed(ref("2"), ref("1"))
	c := New(b, WithProject(testProject))

	// Adding 1 -> 2 closes the loop (1 -> 2 -> 1).
	err := c.EditTask(ref("1"), EditTaskInput{Subject: "s", State: TaskStateTodo, DependsOn: []TaskID{ref("2")}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestUpdate_DetectsTransitiveCycle(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1"))
	b.seed(ref("2"), ref("1"))
	b.seed(ref("3"), ref("2"))
	c := New(b, WithProject(testProject))

	// 1 -> 3 -> 2 -> 1 is a cycle.
	err := c.EditTask(ref("1"), EditTaskInput{Subject: "s", State: TaskStateTodo, DependsOn: []TaskID{ref("3")}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected transitive cycle error, got %v", err)
	}
}

func TestUpdate_AllowsBreakingExistingDependency(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1"))
	b.seed(ref("2"), ref("1"))
	c := New(b, WithProject(testProject))

	if err := c.EditTask(ref("2"), EditTaskInput{Subject: "s", State: TaskStateTodo}); err != nil {
		t.Fatalf("clearing deps should succeed, got %v", err)
	}
	got, _ := c.GetTask(ref("2"))
	if len(got.DependsOn) != 0 {
		t.Errorf("expected DependsOn cleared, got %v", got.DependsOn)
	}
}

func TestUpdate_PreservesDepsWhenUnchanged(t *testing.T) {
	b := newStubBackend()
	b.seed(ref("1"))
	b.seed(ref("2"), ref("1"))
	c := New(b, WithProject(testProject))

	if err := c.EditTask(ref("2"), EditTaskInput{Subject: "new subject", State: TaskStateTodo, DependsOn: []TaskID{ref("1")}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := c.GetTask(ref("2"))
	if got.Subject != "new subject" {
		t.Errorf("Subject not updated")
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0] != ref("1") {
		t.Errorf("DependsOn: got %v, want [1]", got.DependsOn)
	}
}
