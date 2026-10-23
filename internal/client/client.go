package client

import (
	"fmt"
	"log"
	"reflect"
	"slices"
	"time"
)

// Client is the facade agents and the CLI invoke. It is bound to one project
// — the cwd's mapped project, or whatever --project / TM_PROJECT selected —
// so the single-project methods (CreateTask, ListTasks, Inbox, ...) need no
// project argument and read exactly as they did before projects existed.
//
// The backend underneath spans the whole instance, which is what lets
// InboxAll / JournalAll read across every project on one connection.
type Client struct {
	backend Backend
	actor   string
	project string // canonical lowercase ticker
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

// WithProject binds the Client to a project. The ticker is canonicalised; an
// invalid or empty ticker leaves the Client unbound, in which case every
// single-project operation errors with an actionable message rather than
// silently writing to a guessed project.
func WithProject(ticker string) Option {
	return func(c *Client) {
		if ticker == "" {
			return
		}
		if canon, err := CanonicalTicker(ticker); err == nil {
			c.project = canon
		}
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

// Project returns the canonical ticker this Client is bound to, or "" when
// unbound.
func (c *Client) Project() string {
	return c.project
}

// As returns a sibling Client sharing the same backend and project but
// emitting journal events under the given actor. Empty actor falls back to
// ActorSystem. Used by MCP handlers to attribute a single call to a
// per-request identity without rebuilding the backend.
func (c *Client) As(actor string) *Client {
	if actor == "" {
		actor = ActorSystem
	}
	return &Client{backend: c.backend, actor: actor, project: c.project}
}

// ForProject returns a sibling Client bound to a different project in the
// same instance, sharing the backend connection. Used by the MCP `project`
// parameter and the CLI `--project` flag, and by the cross-project reads as
// they walk the instance.
func (c *Client) ForProject(ticker string) (*Client, error) {
	canon, err := CanonicalTicker(ticker)
	if err != nil {
		return nil, err
	}
	return &Client{backend: c.backend, actor: c.actor, project: canon}, nil
}

// Projects exposes the instance's project registry: `tm project` subcommands
// and the `tm init` picker read it directly.
func (c *Client) Projects() ProjectsRepository {
	return c.backend.Projects()
}

// requireProject returns the bound project or an actionable error. Every
// single-project method funnels through here so an unmapped directory
// produces one consistent, agent-readable message instead of a nil-ticker
// query that quietly returns nothing.
func (c *Client) requireProject() (string, error) {
	if c.project == "" {
		return "", fmt.Errorf("no TM project selected; run 'tm init' here or in a parent directory, or pass --project TICKER")
	}
	return c.project, nil
}

// ref builds a fully-qualified reference to a task in this Client's project
// from a bare numeric ID.
func (c *Client) ref(id string) (TaskRef, error) {
	project, err := c.requireProject()
	if err != nil {
		return TaskRef{}, err
	}
	return TaskRef{Project: project, ID: id}, nil
}

// ParseRef resolves any documented reference form against this Client's
// project, so a bare `42` means "task 42 here". Entry points (CLI args, MCP
// id arguments) use this to turn user input into a TaskRef.
func (c *Client) ParseRef(s string) (TaskRef, error) {
	return ParseTaskRef(s, c.project)
}

// CreateTask adds a new task to the Client's project. The new task starts in
// TaskStateDefault (todo). Validation: DependsOn entries must reference
// existing tasks in this project; ParentID, when set, must reference an
// existing task. The repository assigns the number, and the full ref is
// returned.
func (c *Client) CreateTask(in CreateTaskInput) (TaskID, error) {
	project, err := c.requireProject()
	if err != nil {
		return TaskRef{}, err
	}
	if err := c.validateSameProject(in.DependsOn, in.ParentID); err != nil {
		return TaskRef{}, err
	}
	if err := c.validateDependencies(TaskRef{}, in.DependsOn); err != nil {
		return TaskRef{}, err
	}
	if err := c.validateParent(TaskRef{}, in.ParentID); err != nil {
		return TaskRef{}, err
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
	if err := c.backend.Tasks().Save(project, &t); err != nil {
		return TaskRef{}, fmt.Errorf("save task: %w", err)
	}
	payload := map[string]any{"subject": in.Subject}
	if in.AssignedAgent != "" {
		payload["assigned_agent"] = in.AssignedAgent
	}
	if !in.ParentID.IsZero() {
		payload["parent_id"] = in.ParentID.String()
	}
	if len(in.DependsOn) > 0 {
		payload["depends_on"] = FormatTaskRefs(in.DependsOn)
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

// validateSameProject enforces the design's non-goal: parent and dependency
// links are same-project only. Catching it here keeps the error actionable
// instead of surfacing as a silent "task not found" from a project-scoped
// lookup. Relaxable later without a migration.
func (c *Client) validateSameProject(deps []TaskID, parent TaskID) error {
	project, err := c.requireProject()
	if err != nil {
		return err
	}
	if !parent.IsZero() && parent.Project != project {
		return fmt.Errorf("parent %s is in another project; parent links must stay within %s", parent, project)
	}
	for _, d := range deps {
		if !d.IsZero() && d.Project != project {
			return fmt.Errorf("dependency %s is in another project; dependencies must stay within %s", d, project)
		}
	}
	return nil
}

// EditTask overwrites every mutable field of an existing task with the
// values in `in`. Callers that want partial-edit semantics must Get the
// current task first, merge their changes, and pass the merged values back.
// DependsOn replaces the existing dependency list; every referenced task
// must already exist, and the resulting graph must remain acyclic. ParentID,
// when set, must reference an existing task and cannot equal `ref`.
// Returns a NotFoundError if no task with the given ref exists.
func (c *Client) EditTask(ref TaskID, in EditTaskInput) error {
	project, err := c.requireProject()
	if err != nil {
		return err
	}
	if ref.Project != project {
		return fmt.Errorf("task %s is not in the selected project %s", ref, project)
	}
	t, err := c.backend.Tasks().GetByID(ref)
	if err != nil {
		return fmt.Errorf("load task %s: %w", ref, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: ref.String()}
	}
	if err := c.validateSameProject(in.DependsOn, in.ParentID); err != nil {
		return err
	}
	if err := c.validateDependencies(ref, in.DependsOn); err != nil {
		return err
	}
	if err := c.validateParent(ref, in.ParentID); err != nil {
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
	if err := c.backend.Tasks().Save(project, t); err != nil {
		return fmt.Errorf("save task %s: %w", ref, err)
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
			"from": FormatTaskRefs(prev.DependsOn),
			"to":   FormatTaskRefs(next.DependsOn),
		}})
	}
	if prev.ParentID != next.ParentID {
		c.emit(&Event{Kind: EventTaskParentChanged, TaskID: next.ID, Payload: map[string]any{
			"from": prev.ParentID.String(), "to": next.ParentID.String(),
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

// validateParent checks that a non-zero parentRef refers to an existing task
// and is not selfRef (a task cannot be its own parent). A zero parentRef is
// treated as "top-level" and always passes.
func (c *Client) validateParent(selfRef, parentRef TaskID) error {
	if parentRef.IsZero() {
		return nil
	}
	if parentRef == selfRef {
		return fmt.Errorf("task %s cannot be its own parent", selfRef)
	}
	p, err := c.backend.Tasks().GetByID(parentRef)
	if err != nil {
		return fmt.Errorf("check parent %s: %w", parentRef, err)
	}
	if p == nil {
		return &NotFoundError{Resource: "task", ID: parentRef.String()}
	}
	return nil
}

// validateDependencies enforces three invariants for a (selfRef, deps) pair:
//   - no entry equals selfRef (self-dependency)
//   - every dep refers to an existing task
//   - adding the (selfRef -> deps) edges keeps the graph acyclic. For Create,
//     selfRef is the zero ref and cycle detection is skipped since the new
//     task isn't yet reachable from anything.
func (c *Client) validateDependencies(selfRef TaskID, deps []TaskID) error {
	if len(deps) == 0 {
		return nil
	}
	seen := make(map[TaskID]struct{}, len(deps))
	for _, d := range deps {
		if d.IsZero() {
			return fmt.Errorf("invalid dependency: empty task reference")
		}
		if d == selfRef {
			return fmt.Errorf("task %s cannot depend on itself", selfRef)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("duplicate dependency %s", d)
		}
		seen[d] = struct{}{}
		t, err := c.backend.Tasks().GetByID(d)
		if err != nil {
			return fmt.Errorf("check dependency %s: %w", d, err)
		}
		if t == nil {
			return &NotFoundError{Resource: "task", ID: d.String()}
		}
	}
	if selfRef.IsZero() {
		return nil
	}
	return c.detectCycle(selfRef, deps)
}

// detectCycle returns an error if introducing edges selfRef -> deps[i] would
// create a cycle. It walks the existing dependency graph from each dep; if
// any walk reaches selfRef, the new edge closes a loop.
func (c *Client) detectCycle(selfRef TaskID, deps []TaskID) error {
	visited := map[TaskID]struct{}{}
	var walk func(TaskID) error
	walk = func(ref TaskID) error {
		if ref == selfRef {
			return fmt.Errorf("dependency cycle: task %s would depend on itself transitively", selfRef)
		}
		if _, ok := visited[ref]; ok {
			return nil
		}
		visited[ref] = struct{}{}
		t, err := c.backend.Tasks().GetByID(ref)
		if err != nil {
			return fmt.Errorf("walk dependency %s: %w", ref, err)
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

// ListTasks returns the project's tasks filtered by archived state. Pass
// ArchivedFilterDefault (or ArchivedActive) to hide archived rows, ArchivedOnly
// to show only archived rows, ArchivedAll to include both. The repository
// always returns every row; filtering happens here so backends stay dumb.
//
// Description is not populated on the returned tasks — see TasksRepository.
func (c *Client) ListTasks(filter ArchivedFilter) ([]Task, error) {
	project, err := c.requireProject()
	if err != nil {
		return nil, err
	}
	all, err := c.backend.Tasks().GetAll(project)
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

// GetTask returns the task the ref addresses, or a NotFoundError if it
// doesn't exist. Description IS populated.
func (c *Client) GetTask(ref TaskID) (*Task, error) {
	t, err := c.backend.Tasks().GetByID(ref)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, &NotFoundError{Resource: "task", ID: ref.String()}
	}
	return t, nil
}

// GetTaskComments returns all comments attached to the given task, or a
// NotFoundError if the task doesn't exist.
func (c *Client) GetTaskComments(ref TaskID) ([]Comment, error) {
	t, err := c.backend.Tasks().GetByID(ref)
	if err != nil {
		return nil, fmt.Errorf("load task %s: %w", ref, err)
	}
	if t == nil {
		return nil, &NotFoundError{Resource: "task", ID: ref.String()}
	}
	comments, err := c.backend.Comments().GetForTask(ref)
	if err != nil {
		return nil, fmt.Errorf("load comments for task %s: %w", ref, err)
	}
	return comments, nil
}

// AddTaskComment appends a new comment authored by `who` to the given task.
// Returns a NotFoundError if the task doesn't exist.
func (c *Client) AddTaskComment(ref TaskID, who string, comment string) error {
	t, err := c.backend.Tasks().GetByID(ref)
	if err != nil {
		return fmt.Errorf("load task %s: %w", ref, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: ref.String()}
	}
	cm := Comment{Who: who, Comment: comment}
	if err := c.backend.Comments().Add(ref, &cm); err != nil {
		return fmt.Errorf("add comment to task %s: %w", ref, err)
	}
	c.emit(&Event{Kind: EventTaskCommented, TaskID: ref, Payload: map[string]any{
		"comment_id": cm.ID, "who": who,
	}})
	return nil
}

// GetTasksByLabel returns all tasks in the project whose Labels slice
// contains the given label, narrowed by the archived filter. Empty label
// returns no results — callers wanting "all tasks" should use ListTasks.
// Filtering is done client-side since labels are an in-memory slice on each
// task; no backend index is maintained.
func (c *Client) GetTasksByLabel(label string, filter ArchivedFilter) ([]Task, error) {
	if label == "" {
		return nil, nil
	}
	project, err := c.requireProject()
	if err != nil {
		return nil, err
	}
	all, err := c.backend.Tasks().GetAll(project)
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

// GetTasksByParent returns all tasks whose ParentID matches the given ref,
// narrowed by the archived filter. A zero ref is treated as "top-level" and
// returns the project's tasks with no parent; no existence check is performed
// in that case. For a non-zero ref the parent task must exist, otherwise a
// NotFoundError is returned.
func (c *Client) GetTasksByParent(ref TaskID, filter ArchivedFilter) ([]Task, error) {
	project, err := c.requireProject()
	if err != nil {
		return nil, err
	}
	lookup := ref
	if ref.IsZero() {
		// Top-level listing still needs a project to scope the query.
		lookup = TaskRef{Project: project}
	} else {
		p, err := c.backend.Tasks().GetByID(ref)
		if err != nil {
			return nil, fmt.Errorf("load parent task %s: %w", ref, err)
		}
		if p == nil {
			return nil, &NotFoundError{Resource: "task", ID: ref.String()}
		}
	}
	tasks, err := c.backend.Tasks().GetByParent(lookup)
	if err != nil {
		return nil, fmt.Errorf("load tasks for parent %s: %w", ref, err)
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
func (c *Client) ArchiveTask(ref TaskID, cascade bool) (int, error) {
	project, err := c.requireProject()
	if err != nil {
		return 0, err
	}
	t, err := c.backend.Tasks().GetByID(ref)
	if err != nil {
		return 0, fmt.Errorf("load task %s: %w", ref, err)
	}
	if t == nil {
		return 0, &NotFoundError{Resource: "task", ID: ref.String()}
	}
	if t.ArchivedAt != nil {
		return 0, nil
	}

	now := time.Now().UTC()
	t.ArchivedAt = &now
	if err := c.backend.Tasks().Save(project, t); err != nil {
		return 0, fmt.Errorf("save task %s: %w", ref, err)
	}

	cascaded := 0
	if cascade {
		descendants, err := c.collectDescendants(ref)
		if err != nil {
			return 0, err
		}
		for i := range descendants {
			if descendants[i].ArchivedAt != nil {
				continue
			}
			descendants[i].ArchivedAt = &now
			// Descendants come back from GetByParent, which does not join
			// the description. Re-read it so saving the archive flag does
			// not blank the body.
			body, err := c.backend.Descriptions().Get(descendants[i].ID)
			if err != nil {
				return 0, fmt.Errorf("load description for %s: %w", descendants[i].ID, err)
			}
			descendants[i].Description = body
			if err := c.backend.Tasks().Save(project, &descendants[i]); err != nil {
				return 0, fmt.Errorf("save descendant task %s: %w", descendants[i].ID, err)
			}
			cascaded++
		}
	}

	c.emit(&Event{Kind: EventTaskArchived, TaskID: ref, Payload: map[string]any{
		"archived_at":   now.Format(time.RFC3339Nano),
		"cascade_count": cascaded,
	}})
	return cascaded, nil
}

// UnarchiveTask clears ArchivedAt on the named task only. Intentionally does
// not cascade — reviving a tree requires explicit per-task action. No-op (and
// no event) if the task is not archived.
func (c *Client) UnarchiveTask(ref TaskID) error {
	project, err := c.requireProject()
	if err != nil {
		return err
	}
	t, err := c.backend.Tasks().GetByID(ref)
	if err != nil {
		return fmt.Errorf("load task %s: %w", ref, err)
	}
	if t == nil {
		return &NotFoundError{Resource: "task", ID: ref.String()}
	}
	if t.ArchivedAt == nil {
		return nil
	}
	t.ArchivedAt = nil
	if err := c.backend.Tasks().Save(project, t); err != nil {
		return fmt.Errorf("save task %s: %w", ref, err)
	}
	c.emit(&Event{Kind: EventTaskUnarchived, TaskID: ref, Payload: map[string]any{}})
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
			return nil, fmt.Errorf("load children of %s: %w", parent, err)
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

// ListEvents returns journal entries matching the filter (newest first),
// scoped to the Client's project unless the filter names one explicitly.
func (c *Client) ListEvents(filter EventFilter) ([]Event, error) {
	if filter.Project == "" {
		project, err := c.requireProject()
		if err != nil {
			return nil, err
		}
		filter.Project = project
	}
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
	if e.Project == "" {
		// Prefer the task's own project so an event always lands in the
		// journal of the project it describes, even if the Client was
		// rebound between the mutation and the emit.
		if !e.TaskID.IsZero() {
			e.Project = e.TaskID.Project
		} else {
			e.Project = c.project
		}
	}
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

// ParseRefs resolves a list of reference strings against this Client's
// project. Used by the --depends-on flag and the MCP depends_on argument.
func (c *Client) ParseRefs(in []string) ([]TaskRef, error) {
	return ParseTaskRefs(in, c.project)
}

// Backend exposes the underlying repositories. It exists for the import path
// in `tm migrate`, which has to write rows verbatim — preserving IDs,
// timestamps and comment ULIDs — rather than go through the Client's
// validate-and-emit methods. Ordinary callers should not reach for this.
func (c *Client) Backend() Backend {
	return c.backend
}
