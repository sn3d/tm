package client

import (
	"fmt"
	"log"
	"reflect"
)

type Client struct {
	backend Backend
	actor   string
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithActor sets the actor recorded on every event the Client emits.
// An empty string falls back to ActorSystem.
func WithActor(actor string) Option {
	return func(c *Client) {
		if actor == "" {
			actor = ActorSystem
		}
		c.actor = actor
	}
}

func New(backend Backend, opts ...Option) *Client {
	c := &Client{backend: backend, actor: ActorSystem}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Actor returns the identity this Client emits events under. Callers (e.g.
// the MCP inbox handler) need it to compute "my inbox" when no per-call
// actor override is supplied.
func (c *Client) Actor() string {
	return c.actor
}

// As returns a sibling Client sharing the same backend but emitting journal
// events under the given actor. Empty actor falls back to ActorSystem. Used
// by MCP handlers to attribute a single call to a per-request identity
// without rebuilding the backend.
func (c *Client) As(actor string) *Client {
	if actor == "" {
		actor = ActorSystem
	}
	return &Client{backend: c.backend, actor: actor}
}

// Create adds a new task with the given subject, description, and assigned
// agent. The new task starts in TaskStateDefault (todo). Pass an empty
// assignedAgent to leave it unassigned. dependsOn lists existing task IDs
// that the new task depends on; every referenced task must already exist.
// When planID is non-empty the referenced plan must exist; pass "" for a
// standalone task. The repository assigns the ID, which is returned.
func (c *Client) CreateTask(subject, description, assignedAgent string, dependsOn []TaskID, planID PlanID) (TaskID, error) {
	if err := c.validateDependencies("", dependsOn); err != nil {
		return "", err
	}
	if err := c.validatePlan(planID); err != nil {
		return "", err
	}
	t := Task{
		Subject:       subject,
		Description:   description,
		State:         TaskStateDefault,
		AssignedAgent: assignedAgent,
		DependsOn:     dependsOn,
		PlanID:        planID,
	}
	if err := c.backend.Tasks().Save(&t); err != nil {
		return "", fmt.Errorf("save task: %w", err)
	}
	payload := map[string]any{"subject": subject}
	if assignedAgent != "" {
		payload["assigned_agent"] = assignedAgent
	}
	if planID != "" {
		payload["plan_id"] = planID
	}
	if len(dependsOn) > 0 {
		payload["depends_on"] = append([]TaskID(nil), dependsOn...)
	}
	c.emit(&Event{Kind: EventTaskCreated, TaskID: t.ID, Payload: payload})
	return t.ID, nil
}

// Edit overwrites the mutable fields of an existing task with the values
// provided. Callers that want partial-edit semantics ("change only some
// fields") must Get the current task first and pass the merged values back.
// dependsOn replaces the existing dependency list; every referenced task
// must already exist, and the resulting graph must remain acyclic. Returns
// a NotFoundError if no task with the given ID exists.
func (c *Client) EditTask(id TaskID, subject, description string, state TaskState, assignedAgent string, dependsOn []TaskID, planID PlanID) error {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return fmt.Errorf("load task %q: %w", id, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: id}
	}
	if err := c.validateDependencies(id, dependsOn); err != nil {
		return err
	}
	if err := c.validatePlan(planID); err != nil {
		return err
	}
	prev := *t
	prev.DependsOn = append([]TaskID(nil), t.DependsOn...)
	t.Subject = subject
	t.Description = description
	t.State = state
	t.AssignedAgent = assignedAgent
	t.DependsOn = dependsOn
	t.PlanID = planID
	if err := c.backend.Tasks().Save(t); err != nil {
		return fmt.Errorf("save task %q: %w", id, err)
	}
	c.emitTaskEditEvents(prev, *t)
	return nil
}

// emitTaskEditEvents fans an EditTask out into per-field events. The generic
// task.edited event covers subject/description; the rest are dedicated kinds
// so subscribers (e.g. "wake me when something is assigned to me") can match
// without inspecting payloads.
func (c *Client) emitTaskEditEvents(prev, next Task) {
	if prev.Subject != next.Subject || prev.Description != next.Description {
		from := map[string]any{}
		to := map[string]any{}
		if prev.Subject != next.Subject {
			from["subject"] = prev.Subject
			to["subject"] = next.Subject
		}
		if prev.Description != next.Description {
			from["description"] = prev.Description
			to["description"] = next.Description
		}
		c.emit(&Event{Kind: EventTaskEdited, TaskID: next.ID, Payload: map[string]any{"from": from, "to": to}})
	}
	if prev.State != next.State {
		c.emit(&Event{Kind: EventTaskStateChanged, TaskID: next.ID, Payload: map[string]any{
			"from": string(prev.State), "to": string(next.State),
		}})
	}
	if prev.AssignedAgent != next.AssignedAgent {
		c.emit(&Event{Kind: EventTaskAssigned, TaskID: next.ID, Payload: map[string]any{
			"from": prev.AssignedAgent, "to": next.AssignedAgent,
		}})
	}
	if !equalIDs(prev.DependsOn, next.DependsOn) {
		c.emit(&Event{Kind: EventTaskDependsOnChanged, TaskID: next.ID, Payload: map[string]any{
			"from": append([]TaskID(nil), prev.DependsOn...),
			"to":   append([]TaskID(nil), next.DependsOn...),
		}})
	}
	if prev.PlanID != next.PlanID {
		c.emit(&Event{Kind: EventTaskPlanChanged, TaskID: next.ID, Payload: map[string]any{
			"from": prev.PlanID, "to": next.PlanID,
		}})
	}
}

func equalIDs(a, b []TaskID) bool { return reflect.DeepEqual(a, b) }

// validatePlan checks that a non-empty planID refers to an existing plan.
// An empty planID is treated as "standalone" and always passes.
func (c *Client) validatePlan(planID PlanID) error {
	if planID == "" {
		return nil
	}
	p, err := c.backend.Plans().GetByID(planID)
	if err != nil {
		return fmt.Errorf("check plan %q: %w", planID, err)
	}
	if p == nil {
		return &NotFoundError{Resource: "plan", ID: planID}
	}
	return nil
}

// validateDependencies enforces three invariants for a (selfID, deps) pair:
//   - no entry equals selfID (self-dependency)
//   - every dep refers to an existing task
//   - adding the (selfID -> deps) edges keeps the graph acyclic. For Create,
//     selfID is "" and cycle detection is skipped since the new task isn't
//     yet reachable from anything.
func (c *Client) validateDependencies(selfID TaskID, deps []TaskID) error {
	if len(deps) == 0 {
		return nil
	}
	seen := make(map[TaskID]struct{}, len(deps))
	for _, d := range deps {
		if d == "" {
			return fmt.Errorf("invalid dependency: empty task ID")
		}
		if d == selfID {
			return fmt.Errorf("task %q cannot depend on itself", selfID)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("duplicate dependency %q", d)
		}
		seen[d] = struct{}{}
		t, err := c.backend.Tasks().GetByID(d)
		if err != nil {
			return fmt.Errorf("check dependency %q: %w", d, err)
		}
		if t == nil {
			return &NotFoundError{Resource: "task", ID: d}
		}
	}
	if selfID == "" {
		return nil
	}
	return c.detectCycle(selfID, deps)
}

// detectCycle returns an error if introducing edges selfID -> deps[i] would
// create a cycle. It walks the existing dependency graph from each dep; if
// any walk reaches selfID, the new edge closes a loop.
func (c *Client) detectCycle(selfID TaskID, deps []TaskID) error {
	visited := map[TaskID]struct{}{}
	var walk func(TaskID) error
	walk = func(id TaskID) error {
		if id == selfID {
			return fmt.Errorf("dependency cycle: task %q would depend on itself transitively", selfID)
		}
		if _, ok := visited[id]; ok {
			return nil
		}
		visited[id] = struct{}{}
		t, err := c.backend.Tasks().GetByID(id)
		if err != nil {
			return fmt.Errorf("walk dependency %q: %w", id, err)
		}
		if t == nil {
			return nil
		}
		for _, next := range t.DependsOn {
			if err := walk(next); err != nil {
				return err
			}
		}
		return nil
	}
	for _, d := range deps {
		if err := walk(d); err != nil {
			return err
		}
	}
	return nil
}

// ListTasks returns all tasks.
func (c *Client) ListTasks() ([]Task, error) {
	return c.backend.Tasks().GetAll()
}

// GetTask returns the task with the given ID, or a NotFoundError if it doesn't exist.
func (c *Client) GetTask(id TaskID) (*Task, error) {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, &NotFoundError{Resource: "task", ID: id}
	}
	return t, nil
}

// GetTaskComments returns all comments attached to the given task, or a
// NotFoundError if the task doesn't exist.
func (c *Client) GetTaskComments(id TaskID) ([]Comment, error) {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("load task %q: %w", id, err)
	}
	if t == nil {
		return nil, &NotFoundError{Resource: "task", ID: id}
	}
	comments, err := c.backend.Comments().GetForTask(id)
	if err != nil {
		return nil, fmt.Errorf("load comments for task %q: %w", id, err)
	}
	return comments, nil
}

// AddTaskComment appends a new comment authored by `who` to the given task.
// Returns a NotFoundError if the task doesn't exist.
func (c *Client) AddTaskComment(id TaskID, who string, comment string) error {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return fmt.Errorf("load task %q: %w", id, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: id}
	}
	cm := Comment{Who: who, Comment: comment}
	if err := c.backend.Comments().Add(id, &cm); err != nil {
		return fmt.Errorf("add comment to task %q: %w", id, err)
	}
	c.emit(&Event{Kind: EventTaskCommented, TaskID: id, Payload: map[string]any{
		"comment_id": cm.ID, "who": who,
	}})
	return nil
}

// CreatePlan adds a new plan. The new plan starts in PlanStateDefault (draft).
// Pass an empty assignedAgent to leave it unassigned. The repository assigns
// the ID, which is returned.
func (c *Client) CreatePlan(subject, description, assignedAgent string) (PlanID, error) {
	p := Plan{
		Subject:       subject,
		Description:   description,
		State:         PlanStateDefault,
		AssignedAgent: assignedAgent,
	}
	if err := c.backend.Plans().Save(&p); err != nil {
		return "", fmt.Errorf("save plan: %w", err)
	}
	payload := map[string]any{"subject": subject}
	if assignedAgent != "" {
		payload["assigned_agent"] = assignedAgent
	}
	c.emit(&Event{Kind: EventPlanCreated, PlanID: p.ID, Payload: payload})
	return p.ID, nil
}

// EditPlan overwrites the mutable fields of an existing plan. Returns a
// NotFoundError if no plan with the given ID exists.
func (c *Client) EditPlan(id PlanID, subject, description string, state PlanState, assignedAgent string) error {
	p, err := c.backend.Plans().GetByID(id)
	if err != nil {
		return fmt.Errorf("load plan %q: %w", id, err)
	}
	if p == nil {
		return &NotFoundError{Resource: "plan", ID: id}
	}
	prev := *p
	p.Subject = subject
	p.Description = description
	p.State = state
	p.AssignedAgent = assignedAgent
	if err := c.backend.Plans().Save(p); err != nil {
		return fmt.Errorf("save plan %q: %w", id, err)
	}
	c.emitPlanEditEvents(prev, *p)
	return nil
}

func (c *Client) emitPlanEditEvents(prev, next Plan) {
	if prev.Subject != next.Subject || prev.Description != next.Description {
		from := map[string]any{}
		to := map[string]any{}
		if prev.Subject != next.Subject {
			from["subject"] = prev.Subject
			to["subject"] = next.Subject
		}
		if prev.Description != next.Description {
			from["description"] = prev.Description
			to["description"] = next.Description
		}
		c.emit(&Event{Kind: EventPlanEdited, PlanID: next.ID, Payload: map[string]any{"from": from, "to": to}})
	}
	if prev.State != next.State {
		c.emit(&Event{Kind: EventPlanStateChanged, PlanID: next.ID, Payload: map[string]any{
			"from": string(prev.State), "to": string(next.State),
		}})
	}
	if prev.AssignedAgent != next.AssignedAgent {
		c.emit(&Event{Kind: EventPlanAssigned, PlanID: next.ID, Payload: map[string]any{
			"from": prev.AssignedAgent, "to": next.AssignedAgent,
		}})
	}
}

// GetPlan returns the plan with the given ID, or a NotFoundError if it doesn't exist.
func (c *Client) GetPlan(id PlanID) (*Plan, error) {
	p, err := c.backend.Plans().GetByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, &NotFoundError{Resource: "plan", ID: id}
	}
	return p, nil
}

// ListPlans returns all plans.
func (c *Client) ListPlans() ([]Plan, error) {
	return c.backend.Plans().GetAll()
}

// GetTasksByPlan returns all tasks associated with the given plan. An empty
// id is treated as "standalone" and returns tasks with no plan; validation
// is skipped in that case. For a non-empty id the plan must exist, otherwise
// a NotFoundError is returned.
func (c *Client) GetTasksByPlan(id PlanID) ([]Task, error) {
	if id != "" {
		p, err := c.backend.Plans().GetByID(id)
		if err != nil {
			return nil, fmt.Errorf("load plan %q: %w", id, err)
		}
		if p == nil {
			return nil, &NotFoundError{Resource: "plan", ID: id}
		}
	}
	tasks, err := c.backend.Tasks().GetByPlan(id)
	if err != nil {
		return nil, fmt.Errorf("load tasks for plan %q: %w", id, err)
	}
	return tasks, nil
}

// AddPlanComment appends a new comment authored by `who` to the given plan.
// Returns a NotFoundError if the plan doesn't exist.
func (c *Client) AddPlanComment(id PlanID, who string, comment string) error {
	p, err := c.backend.Plans().GetByID(id)
	if err != nil {
		return fmt.Errorf("load plan %q: %w", id, err)
	}
	if p == nil {
		return &NotFoundError{Resource: "plan", ID: id}
	}
	cm := Comment{Who: who, Comment: comment}
	if err := c.backend.PlanComments().Add(id, &cm); err != nil {
		return fmt.Errorf("add comment to plan %q: %w", id, err)
	}
	c.emit(&Event{Kind: EventPlanCommented, PlanID: id, Payload: map[string]any{
		"comment_id": cm.ID, "who": who,
	}})
	return nil
}

// GetPlanComments returns all comments attached to the given plan, or a
// NotFoundError if the plan doesn't exist.
func (c *Client) GetPlanComments(id PlanID) ([]Comment, error) {
	p, err := c.backend.Plans().GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("load plan %q: %w", id, err)
	}
	if p == nil {
		return nil, &NotFoundError{Resource: "plan", ID: id}
	}
	comments, err := c.backend.PlanComments().GetForPlan(id)
	if err != nil {
		return nil, fmt.Errorf("load comments for plan %q: %w", id, err)
	}
	return comments, nil
}

// ListEvents returns journal entries matching the filter (newest first).
func (c *Client) ListEvents(filter EventFilter) ([]Event, error) {
	events, err := c.backend.Events().List(filter)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return events, nil
}

// emit sends an event to the backend journal. Failures are logged and
// swallowed so the audit log never breaks the mutation that just succeeded.
func (c *Client) emit(e *Event) {
	e.Actor = c.actor
	if err := c.backend.Events().Append(e); err != nil {
		log.Printf("journal append failed (kind=%s task=%s plan=%s): %v",
			e.Kind, e.TaskID, e.PlanID, err)
	}
}

type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with ID %s not found", e.Resource, e.ID)
}
