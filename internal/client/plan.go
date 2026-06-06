package client

import (
	"fmt"
	"time"
)

type PlanID = string

type Plan struct {
	ID            PlanID
	Subject       string
	Description   string
	State         PlanState
	AssignedAgent string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PlansRepository interface {
	// Save inserts a new plan or updates an existing one. When p.ID is empty
	// the repository assigns a fresh ID and writes it back into p.ID. The
	// repository stamps p.CreatedAt on insert (preserved across updates) and
	// p.UpdatedAt on every save, writing both back into the struct.
	Save(p *Plan) (err error)
	GetByID(id PlanID) (p *Plan, err error)
	// GetAll returns every plan ordered by UpdatedAt descending (most
	// recently changed first), with ID as a tiebreaker.
	GetAll() (p []Plan, err error)
}

type PlanCommentsRepository interface {
	// Add inserts a comment for the given plan. The repository assigns
	// c.ID and writes it back into the value pointed to.
	Add(id PlanID, c *Comment) (err error)
	GetForPlan(id PlanID) (comments []Comment, err error)
}

// PlanState is the lifecycle state of a plan.
type PlanState string

const (
	PlanStateDraft     PlanState = "draft"
	PlanStateActive    PlanState = "active"
	PlanStateOnHold    PlanState = "on_hold"
	PlanStateCompleted PlanState = "completed"
	PlanStateCancelled PlanState = "cancelled"
)

// PlanStateDefault is assigned to newly created plans.
const PlanStateDefault = PlanStateDraft

// PlanStates is the canonical ordered list of valid plan states.
var PlanStates = []PlanState{
	PlanStateDraft,
	PlanStateActive,
	PlanStateOnHold,
	PlanStateCompleted,
	PlanStateCancelled,
}

func (s PlanState) String() string { return string(s) }

func (s PlanState) Valid() bool {
	switch s {
	case PlanStateDraft, PlanStateActive, PlanStateOnHold,
		PlanStateCompleted, PlanStateCancelled:
		return true
	}
	return false
}

func (s PlanState) Category() Category {
	switch s {
	case PlanStateDraft:
		return CategoryOpen
	case PlanStateActive, PlanStateOnHold:
		return CategoryActive
	case PlanStateCompleted:
		return CategoryDone
	case PlanStateCancelled:
		return CategoryCancelled
	}
	return CategoryOpen
}

// ParsePlanState converts a string into a PlanState, returning an error for
// values that are not in PlanStates.
func ParsePlanState(s string) (PlanState, error) {
	ps := PlanState(s)
	if !ps.Valid() {
		return "", fmt.Errorf("invalid plan state: %q", s)
	}
	return ps, nil
}
