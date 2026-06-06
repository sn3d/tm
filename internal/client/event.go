package client

import "time"

// EventKind identifies the type of an audit-log Event. Names follow the
// "<entity>.<verb>" convention so list filters can match a single entity or
// a single verb cheaply.
type EventKind string

const (
	EventTaskCreated          EventKind = "task.created"
	EventTaskEdited           EventKind = "task.edited"
	EventTaskStateChanged     EventKind = "task.state_changed"
	EventTaskAssigned         EventKind = "task.assigned"
	EventTaskDependsOnChanged EventKind = "task.depends_on_changed"
	EventTaskPlanChanged      EventKind = "task.plan_changed"
	EventTaskParentChanged    EventKind = "task.parent_changed"
	EventTaskLabelsChanged    EventKind = "task.labels_changed"
	EventTaskModeChanged      EventKind = "task.mode_changed"
	EventTaskCommented        EventKind = "task.commented"

	EventPlanCreated      EventKind = "plan.created"
	EventPlanEdited       EventKind = "plan.edited"
	EventPlanStateChanged EventKind = "plan.state_changed"
	EventPlanAssigned     EventKind = "plan.assigned"
	EventPlanCommented    EventKind = "plan.commented"
)

// ActorSystem is the fallback Actor recorded when no caller-supplied actor
// is configured (e.g. a unit test that wires the client directly).
const ActorSystem = "system"

// Event is one entry in the append-only audit journal. The current-state
// row (in tasks/plans) remains authoritative; events are a side-effect log
// recording who did what, when.
type Event struct {
	ID        string         // ULID, sortable by time
	Timestamp time.Time
	Actor     string
	Kind      EventKind
	TaskID    TaskID         // empty for plan-only events
	PlanID    PlanID         // empty for task-only events
	Payload   map[string]any // kind-specific; see emit_* helpers in client
}

// EventFilter narrows the result of EventsRepository.List. Zero-value fields
// are ignored; non-zero fields are AND-combined. Kinds is OR-combined.
type EventFilter struct {
	TaskID TaskID
	PlanID PlanID
	Actor  string
	Kinds  []EventKind
	Since  time.Time // exclusive lower bound
	Limit  int       // 0 = no limit
}

// EventsRepository is the persistence interface for the journal. Backends
// implement it alongside the existing per-entity repositories.
type EventsRepository interface {
	// Append assigns e.ID (ULID) and e.Timestamp (now) when empty, then
	// persists the event. Backends must not mutate any other field.
	Append(e *Event) error
	// List returns events matching the filter, newest first.
	List(filter EventFilter) ([]Event, error)
}
