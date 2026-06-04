package client

import "fmt"

type TaskID = string

type Task struct {
	ID            TaskID
	Subject       string
	Description   string
	State         TaskState
	AssignedAgent string
	DependsOn     []TaskID
	PlanID        PlanID // empty string => standalone task
}

type TasksRepository interface {
	// Save inserts or updates a task. When t.ID == 0 the repository assigns
	// a new ID and writes it back into t.ID. When t.ID != 0 the existing
	// row is replaced.
	Save(t *Task) (err error)
	GetByID(id TaskID) (t *Task, err error)
	GetAll() (t []Task, err error)
	GetByPlan(planID PlanID) (t []Task, err error)
}

// TaskState is the lifecycle state of a task.
type TaskState string

const (
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
	case TaskStateTodo, TaskStateInProgress, TaskStateBlocked,
		TaskStateInReview, TaskStateDone, TaskStateCancelled:
		return true
	}
	return false
}

func (s TaskState) Category() Category {
	switch s {
	case TaskStateTodo:
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
