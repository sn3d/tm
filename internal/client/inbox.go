package client

import (
	"fmt"
	"log"
	"sort"
	"time"
)

// Inbox is the per-actor view of "what needs my attention now and what
// changed since I last looked". Tasks and Plans are the current open/active
// items assigned to the actor; RecentChanges is the slice of journal events
// touching those items (or reassignments TO the actor) since LastSeenAt.
type Inbox struct {
	Actor         string
	Tasks         []Task
	Plans         []Plan
	RecentChanges []Event
	LastSeenAt    time.Time
}

// Inbox returns the actor's inbox AND advances their last-seen cursor to now.
// Calling Inbox twice in a row shows only events that arrived between the
// two calls. The cursor advance happens after the read, so a cursor-write
// failure is logged but does not invalidate the returned snapshot.
func (c *Client) Inbox(actor string) (*Inbox, error) {
	return c.inbox(actor, true)
}

// PeekInbox returns the inbox WITHOUT advancing the cursor. Use it for
// "show me what's new" without marking it as seen.
func (c *Client) PeekInbox(actor string) (*Inbox, error) {
	return c.inbox(actor, false)
}

func (c *Client) inbox(actor string, advance bool) (*Inbox, error) {
	if actor == "" {
		return nil, fmt.Errorf("inbox: actor is required")
	}

	cursors := c.backend.ActorCursors()
	lastSeen, err := cursors.Get(actor)
	if err != nil {
		return nil, fmt.Errorf("load cursor for %q: %w", actor, err)
	}

	tasks, err := c.assignedTasks(actor)
	if err != nil {
		return nil, err
	}
	plans, err := c.assignedPlans(actor)
	if err != nil {
		return nil, err
	}
	changes, err := c.recentChanges(actor, tasks, plans, lastSeen)
	if err != nil {
		return nil, err
	}

	if advance {
		if err := cursors.Set(actor, time.Now().UTC()); err != nil {
			log.Printf("inbox cursor advance failed (actor=%s): %v", actor, err)
		}
	}

	return &Inbox{
		Actor:         actor,
		Tasks:         tasks,
		Plans:         plans,
		RecentChanges: changes,
		LastSeenAt:    lastSeen,
	}, nil
}

func (c *Client) assignedTasks(actor string) ([]Task, error) {
	all, err := c.backend.Tasks().GetAll()
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	out := make([]Task, 0)
	for _, t := range all {
		if t.AssignedAgent != actor {
			continue
		}
		switch t.State.Category() {
		case CategoryOpen, CategoryActive:
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (c *Client) assignedPlans(actor string) ([]Plan, error) {
	all, err := c.backend.Plans().GetAll()
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	out := make([]Plan, 0)
	for _, p := range all {
		if p.AssignedAgent != actor {
			continue
		}
		switch p.State.Category() {
		case CategoryOpen, CategoryActive:
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// recentChanges returns events since `since` that involve the actor: either
// the event touches an item currently assigned to them, or it is a
// reassignment to them. Events authored by the actor are excluded — your own
// actions are not news. Output is oldest-first for chronological reading.
func (c *Client) recentChanges(actor string, tasks []Task, plans []Plan, since time.Time) ([]Event, error) {
	events, err := c.backend.Events().List(EventFilter{Since: since})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	taskIDs := make(map[TaskID]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}
	planIDs := make(map[PlanID]struct{}, len(plans))
	for _, p := range plans {
		planIDs[p.ID] = struct{}{}
	}

	out := make([]Event, 0, len(events))
	for _, e := range events {
		if e.Actor == actor {
			continue
		}
		if c.eventInvolvesActor(e, actor, taskIDs, planIDs) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].ID < out[j].ID
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

func (c *Client) eventInvolvesActor(e Event, actor string, taskIDs map[TaskID]struct{}, planIDs map[PlanID]struct{}) bool {
	if e.TaskID != "" {
		if _, ok := taskIDs[e.TaskID]; ok {
			return true
		}
	}
	if e.PlanID != "" {
		if _, ok := planIDs[e.PlanID]; ok {
			return true
		}
	}
	switch e.Kind {
	case EventTaskAssigned, EventPlanAssigned:
		if to, _ := e.Payload["to"].(string); to == actor {
			return true
		}
	}
	return false
}
