package client

import (
	"fmt"
	"time"
)

type TaskID = string

type Task struct {
	ID            TaskID
	Subject       string
	Description   string
	State         TaskState
	AssignedAgent string
	DependsOn     []TaskID
	ParentID      TaskID // empty string => top-level task; self-reference forming the hierarchy
	Labels        []string
	Mode          TaskMode
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// ArchivedAt marks a task as soft-hidden when non-nil. Archive is a
	// visibility signal independent of State: a `todo` task can be archived
	// (parked) and a `done` task can stay un-archived (recently shipped).
	// Dependents of an archived task remain blocked on it — archive does not
	// change workflow, only what appears in the default lists and inbox.
	ArchivedAt *time.Time
}

// TaskMode is a render/filter hint on a task. It does not change workflow,
// state transitions, or any backend behavior — only how UIs treat the row.
// `standard` is the default; `planning` marks a task that represents a plan
// (typically with children via ParentID) so TUI / filters can render it
// distinctly.
type TaskMode string

const (
	TaskModeStandard TaskMode = "standard"
	TaskModePlanning TaskMode = "planning"
)

// TaskModeDefault is assigned to newly created tasks when no mode is set.
const TaskModeDefault = TaskModeStandard

// TaskModes is the canonical ordered list of valid task modes.
var TaskModes = []TaskMode{
	TaskModeStandard,
	TaskModePlanning,
}

func (m TaskMode) String() string { return string(m) }

func (m TaskMode) Valid() bool {
	switch m {
	case TaskModeStandard, TaskModePlanning:
		return true
	}
	return false
}

// ParseTaskMode converts a string into a TaskMode. The empty string maps to
// TaskModeDefault so callers can pass "" to mean "leave default".
func ParseTaskMode(s string) (TaskMode, error) {
	if s == "" {
		return TaskModeDefault, nil
	}
	m := TaskMode(s)
	if !m.Valid() {
		return "", fmt.Errorf("invalid task mode: %q", s)
	}
	return m, nil
}

// CreateTaskInput is the parameter bag for Client.CreateTask. Optional
// fields take their zero value's natural meaning: empty AssignedAgent =
// unassigned, empty ParentID = top-level, nil DependsOn = no deps, nil
// Labels = no labels, empty Mode = TaskModeDefault.
type CreateTaskInput struct {
	Subject       string
	Description   string
	AssignedAgent string
	DependsOn     []TaskID
	ParentID      TaskID
	Labels        []string
	Mode          TaskMode
}

// EditTaskInput is the parameter bag for Client.EditTask. Edit semantics are
// REPLACE: every field is written verbatim (e.g. passing a nil Labels clears
// the label list). Callers wanting partial-edit semantics must read the
// task, merge their changes, and pass the merged values.
type EditTaskInput struct {
	Subject       string
	Description   string
	State         TaskState
	AssignedAgent string
	DependsOn     []TaskID
	ParentID      TaskID
	Labels        []string
	Mode          TaskMode
}

type TasksRepository interface {
	// Save inserts or updates a task. When t.ID is empty the repository
	// assigns a new ID and writes it back into t.ID. When t.ID is set the
	// existing row is replaced. The repository stamps t.CreatedAt on insert
	// (preserved across updates) and t.UpdatedAt on every save, writing both
	// back into the struct.
	Save(t *Task) (err error)
	GetByID(id TaskID) (t *Task, err error)
	// GetAll returns every task ordered by UpdatedAt descending (most
	// recently changed first), with ID as a tiebreaker.
	GetAll() (t []Task, err error)
	// GetByParent returns tasks whose ParentID matches the given id, ordered
	// like GetAll. Pass "" to list top-level tasks (no parent).
	GetByParent(parentID TaskID) (t []Task, err error)
}

// TaskState is the lifecycle state of a task.
type TaskState string

const (
	// TaskStateDraft is the initial "shaping it" state — typically used by
	// planning-mode tasks before they're handed off, but available to any
	// task. Category: open.
	TaskStateDraft      TaskState = "draft"
	TaskStateTodo       TaskState = "todo"
	TaskStateInProgress TaskState = "in_progress"
	TaskStateBlocked    TaskState = "blocked"
	TaskStateInReview   TaskState = "in_review"
	TaskStateDone       TaskState = "done"
	TaskStateCancelled  TaskState = "cancelled"
)

// TaskStateDefault is assigned to newly created tasks.
const TaskStateDefault = TaskStateTodo

// TaskStates is the canonical ordered list of valid task states.
var TaskStates = []TaskState{
	TaskStateDraft,
	TaskStateTodo,
	TaskStateInProgress,
	TaskStateBlocked,
	TaskStateInReview,
	TaskStateDone,
	TaskStateCancelled,
}

func (s TaskState) String() string { return string(s) }

func (s TaskState) Valid() bool {
	switch s {
	case TaskStateDraft, TaskStateTodo, TaskStateInProgress, TaskStateBlocked,
		TaskStateInReview, TaskStateDone, TaskStateCancelled:
		return true
	}
	return false
}

func (s TaskState) Category() Category {
	switch s {
	case TaskStateDraft, TaskStateTodo:
		return CategoryOpen
	case TaskStateInProgress, TaskStateBlocked, TaskStateInReview:
		return CategoryActive
	case TaskStateDone:
		return CategoryDone
	case TaskStateCancelled:
		return CategoryCancelled
	}
	return CategoryOpen
}

// ParseTaskState converts a string into a TaskState, returning an error for
// values that are not in TaskStates.
func ParseTaskState(s string) (TaskState, error) {
	ts := TaskState(s)
	if !ts.Valid() {
		return "", fmt.Errorf("invalid task state: %q", s)
	}
	return ts, nil
}

// ArchivedFilter narrows list queries by archive state.
type ArchivedFilter string

const (
	ArchivedActive ArchivedFilter = "active"   // exclude archived (default)
	ArchivedOnly   ArchivedFilter = "archived" // only archived
	ArchivedAll    ArchivedFilter = "all"      // include both
)

// ArchivedFilterDefault is what callers pass when they have no opinion.
const ArchivedFilterDefault = ArchivedActive

// ParseArchivedFilter parses the user-facing string form. The empty string
// maps to the default so "no flag set" reads as "active only".
func ParseArchivedFilter(s string) (ArchivedFilter, error) {
	if s == "" {
		return ArchivedFilterDefault, nil
	}
	f := ArchivedFilter(s)
	switch f {
	case ArchivedActive, ArchivedOnly, ArchivedAll:
		return f, nil
	}
	return "", fmt.Errorf("invalid archived filter: %q (want active|archived|all)", s)
}

// keep applies an ArchivedFilter predicate to a single task.
func (f ArchivedFilter) keep(t Task) bool {
	switch f {
	case ArchivedOnly:
		return t.ArchivedAt != nil
	case ArchivedAll:
		return true
	default:
		return t.ArchivedAt == nil
	}
}
