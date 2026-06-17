package client

import (
	"fmt"
	"log"
	"reflect"
	"slices"
	"time"
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

// CreateTask adds a new task. The new task starts in TaskStateDefault (todo).
// Validation: DependsOn entries must reference existing tasks; ParentID,
// when non-empty, must reference an existing task. The repository assigns
// the ID, which is returned.
func (c *Client) CreateTask(in CreateTaskInput) (TaskID, error) {
	if err := c.validateDependencies("", in.DependsOn); err != nil {
		return "", err
	}
	if err := c.validateParent("", in.ParentID); err != nil {
		return "", err
	}
	mode := in.Mode
	if mode == "" {
		mode = TaskModeDefault
	}
	t := Task{
		Subject:       in.Subject,
		Description:   in.Description,
		State:         TaskStateDefault,
		AssignedAgent: in.AssignedAgent,
		DependsOn:     in.DependsOn,
		ParentID:      in.ParentID,
		Labels:        in.Labels,
		Mode:          mode,
	}
	if err := c.backend.Tasks().Save(&t); err != nil {
		return "", fmt.Errorf("save task: %w", err)
	}
	payload := map[string]any{"subject": in.Subject}
	if in.AssignedAgent != "" {
		payload["assigned_agent"] = in.AssignedAgent
	}
	if in.ParentID != "" {
		payload["parent_id"] = in.ParentID
	}
	if len(in.DependsOn) > 0 {
		payload["depends_on"] = append([]TaskID(nil), in.DependsOn...)
	}
	if len(in.Labels) > 0 {
		payload["labels"] = append([]string(nil), in.Labels...)
	}
	if mode != TaskModeDefault {
		payload["mode"] = string(mode)
	}
	c.emit(&Event{Kind: EventTaskCreated, TaskID: t.ID, Payload: payload})
	return t.ID, nil
}

// EditTask overwrites every mutable field of an existing task with the
// values in `in`. Callers that want partial-edit semantics must Get the
// current task first, merge their changes, and pass the merged values back.
// DependsOn replaces the existing dependency list; every referenced task
// must already exist, and the resulting graph must remain acyclic. ParentID,
// when non-empty, must reference an existing task and cannot equal `id`.
// Returns a NotFoundError if no task with the given ID exists.
func (c *Client) EditTask(id TaskID, in EditTaskInput) error {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return fmt.Errorf("load task %q: %w", id, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: id}
	}
	if err := c.validateDependencies(id, in.DependsOn); err != nil {
		return err
	}
	if err := c.validateParent(id, in.ParentID); err != nil {
		return err
	}
	mode := in.Mode
	if mode == "" {
		mode = TaskModeDefault
	}
	prev := *t
	prev.DependsOn = append([]TaskID(nil), t.DependsOn...)
	prev.Labels = append([]string(nil), t.Labels...)
	t.Subject = in.Subject
	t.Description = in.Description
	t.State = in.State
	t.AssignedAgent = in.AssignedAgent
	t.DependsOn = in.DependsOn
	t.ParentID = in.ParentID
	t.Labels = in.Labels
	t.Mode = mode
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
	if prev.ParentID != next.ParentID {
		c.emit(&Event{Kind: EventTaskParentChanged, TaskID: next.ID, Payload: map[string]any{
			"from": prev.ParentID, "to": next.ParentID,
		}})
	}
	if !equalStrings(prev.Labels, next.Labels) {
		c.emit(&Event{Kind: EventTaskLabelsChanged, TaskID: next.ID, Payload: map[string]any{
			"from": append([]string(nil), prev.Labels...),
			"to":   append([]string(nil), next.Labels...),
		}})
	}
	if prev.Mode != next.Mode {
		c.emit(&Event{Kind: EventTaskModeChanged, TaskID: next.ID, Payload: map[string]any{
			"from": string(prev.Mode), "to": string(next.Mode),
		}})
	}
}

func equalStrings(a, b []string) bool { return reflect.DeepEqual(a, b) }

func equalIDs(a, b []TaskID) bool { return reflect.DeepEqual(a, b) }

// validateParent checks that a non-empty parentID refers to an existing task
// and is not selfID (a task cannot be its own parent). An empty parentID is
// treated as "top-level" and always passes. Transitive cycles (A→B→A) are
// not checked here — commit 2 will add that once parent_id becomes the
// authoritative hierarchy field.
func (c *Client) validateParent(selfID, parentID TaskID) error {
	if parentID == "" {
		return nil
	}
	if parentID == selfID {
		return fmt.Errorf("task %q cannot be its own parent", selfID)
	}
	p, err := c.backend.Tasks().GetByID(parentID)
	if err != nil {
		return fmt.Errorf("check parent %q: %w", parentID, err)
	}
	if p == nil {
		return &NotFoundError{Resource: "task", ID: parentID}
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

// ListTasks returns tasks filtered by archived state. Pass
// ArchivedFilterDefault (or ArchivedActive) to hide archived rows, ArchivedOnly
// to show only archived rows, ArchivedAll to include both. The repository
// always returns every row; filtering happens here so backends stay dumb.
func (c *Client) ListTasks(filter ArchivedFilter) ([]Task, error) {
	all, err := c.backend.Tasks().GetAll()
	if err != nil {
		return nil, err
	}
	return applyArchivedFilter(all, filter), nil
}

// applyArchivedFilter keeps rows that match the requested archive state. The
// returned slice is a fresh allocation; the input is untouched.
func applyArchivedFilter(in []Task, f ArchivedFilter) []Task {
	out := make([]Task, 0, len(in))
	for _, t := range in {
		if f.keep(t) {
			out = append(out, t)
		}
	}
	return out
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

// GetTasksByLabel returns all tasks whose Labels slice contains the given
// label, narrowed by the archived filter. Empty label returns no results —
// callers wanting "all tasks" should use ListTasks. Filtering is done
// client-side since labels are an in-memory slice on each task; no backend
// index is maintained.
func (c *Client) GetTasksByLabel(label string, filter ArchivedFilter) ([]Task, error) {
	if label == "" {
		return nil, nil
	}
	all, err := c.backend.Tasks().GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(all))
	for _, t := range all {
		if !slices.Contains(t.Labels, label) {
			continue
		}
		if !filter.keep(t) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// GetTasksByParent returns all tasks whose ParentID matches the given id,
// narrowed by the archived filter. An empty id is treated as "top-level" and
// returns tasks with no parent; no existence check is performed in that case.
// For a non-empty id the parent task must exist, otherwise a NotFoundError is
// returned.
func (c *Client) GetTasksByParent(id TaskID, filter ArchivedFilter) ([]Task, error) {
	if id != "" {
		p, err := c.backend.Tasks().GetByID(id)
		if err != nil {
			return nil, fmt.Errorf("load parent task %q: %w", id, err)
		}
		if p == nil {
			return nil, &NotFoundError{Resource: "task", ID: id}
		}
	}
	tasks, err := c.backend.Tasks().GetByParent(id)
	if err != nil {
		return nil, fmt.Errorf("load tasks for parent %q: %w", id, err)
	}
	return applyArchivedFilter(tasks, filter), nil
}

// ArchiveTask marks a task as soft-hidden. When cascade is true the entire
// descendant tree (via ParentID) is archived in the same call; cascade=false
// archives only the named task. Returns the number of descendants archived
// (NOT counting the named task itself). No-ops (and emits no event) if the
// task is already archived. The task's State is untouched — archive is a
// visibility signal, not a workflow signal, and dependents of an archived
// task remain blocked on it.
func (c *Client) ArchiveTask(id TaskID, cascade bool) (int, error) {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return 0, fmt.Errorf("load task %q: %w", id, err)
	}
	if t == nil {
		return 0, &NotFoundError{Resource: "task", ID: id}
	}
	if t.ArchivedAt != nil {
		return 0, nil
	}

	now := time.Now().UTC()
	t.ArchivedAt = &now
	if err := c.backend.Tasks().Save(t); err != nil {
		return 0, fmt.Errorf("save task %q: %w", id, err)
	}

	cascaded := 0
	if cascade {
		descendants, err := c.collectDescendants(id)
		if err != nil {
			return 0, err
		}
		for i := range descendants {
			if descendants[i].ArchivedAt != nil {
				continue
			}
			descendants[i].ArchivedAt = &now
			if err := c.backend.Tasks().Save(&descendants[i]); err != nil {
				return 0, fmt.Errorf("save descendant task %q: %w", descendants[i].ID, err)
			}
			cascaded++
		}
	}

	c.emit(&Event{Kind: EventTaskArchived, TaskID: id, Payload: map[string]any{
		"archived_at":   now.Format(time.RFC3339Nano),
		"cascade_count": cascaded,
	}})
	return cascaded, nil
}

// UnarchiveTask clears ArchivedAt on the named task only. Intentionally does
// not cascade — reviving a tree requires explicit per-task action. No-op (and
// no event) if the task is not archived.
func (c *Client) UnarchiveTask(id TaskID) error {
	t, err := c.backend.Tasks().GetByID(id)
	if err != nil {
		return fmt.Errorf("load task %q: %w", id, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: id}
	}
	if t.ArchivedAt == nil {
		return nil
	}
	t.ArchivedAt = nil
	if err := c.backend.Tasks().Save(t); err != nil {
		return fmt.Errorf("save task %q: %w", id, err)
	}
	c.emit(&Event{Kind: EventTaskUnarchived, TaskID: id, Payload: map[string]any{}})
	return nil
}

// collectDescendants returns every task reachable downward from root via
// ParentID (children, grandchildren, ...), BFS-ordered. The root itself is
// NOT included. A visited set guards against any pathological cycle even
// though the parent graph is supposed to be a forest.
func (c *Client) collectDescendants(root TaskID) ([]Task, error) {
	var out []Task
	visited := map[TaskID]struct{}{root: {}}
	queue := []TaskID{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		children, err := c.backend.Tasks().GetByParent(parent)
		if err != nil {
			return nil, fmt.Errorf("load children of %q: %w", parent, err)
		}
		for _, child := range children {
			if _, seen := visited[child.ID]; seen {
				continue
			}
			visited[child.ID] = struct{}{}
			out = append(out, child)
			queue = append(queue, child.ID)
		}
	}
	return out, nil
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
		log.Printf("journal append failed (kind=%s task=%s): %v",
			e.Kind, e.TaskID, err)
	}
}

type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with ID %s not found", e.Resource, e.ID)
}
