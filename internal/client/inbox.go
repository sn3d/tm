package client

import (
	"fmt"
	"log"
	"sort"
	"time"
)

// Inbox is the per-actor view of "what needs my attention now and what
// changed since I last looked". Tasks are the current open/active items
// assigned to the actor; RecentChanges is the slice of journal events
// touching those items (or reassignments TO the actor) since LastSeenAt.
// Resumable is the subset of Tasks whose UpdatedAt is after LastSeenAt —
// i.e. tasks the agent owns that changed since the last heartbeat (typically
// a reply on a blocked task or a reassignment back).
//
// Handoff model — no unread bits, no per-event read state. Work moves
// between actors via two coupled changes on the task: set the next state
// (e.g. in_review, blocked) AND reassign to whoever should act next. The
// task then leaves the previous owner's inbox (different assignee) and
// appears in the new owner's inbox; on the new owner's next heartbeat it
// also lands in Resumable because UpdatedAt advanced past their LastSeenAt.
// Terminal states (done, cancelled) drop the task from any inbox via the
// category filter.
type Inbox struct {
	Actor         string
	Tasks         []Task
	Resumable     []Task
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
	changes, err := c.recentChanges(actor, tasks, lastSeen)
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
		Resumable:     resumableTasks(tasks, lastSeen),
		RecentChanges: changes,
		LastSeenAt:    lastSeen,
	}, nil
}

// resumableTasks picks the tasks the actor owns that changed since their
// last heartbeat. Order is inherited from `tasks` (UpdatedAt desc, ID tie).
func resumableTasks(tasks []Task, lastSeen time.Time) []Task {
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.UpdatedAt.After(lastSeen) {
			out = append(out, t)
		}
	}
	return out
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
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// recentChanges returns events since `since` that involve the actor: either
// the event touches an item currently assigned to them, or it is a
// reassignment to them. Events authored by the actor are excluded — your own
// actions are not news. Output is oldest-first for chronological reading.
func (c *Client) recentChanges(actor string, tasks []Task, since time.Time) ([]Event, error) {
	events, err := c.backend.Events().List(EventFilter{Since: since})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	taskIDs := make(map[TaskID]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.ID] = struct{}{}
	}

	out := make([]Event, 0, len(events))
	for _, e := range events {
		if e.Actor == actor {
			continue
		}
		if c.eventInvolvesActor(e, actor, taskIDs) {
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

func (c *Client) eventInvolvesActor(e Event, actor string, taskIDs map[TaskID]struct{}) bool {
	if e.TaskID != "" {
		if _, ok := taskIDs[e.TaskID]; ok {
			return true
		}
	}
	if e.Kind == EventTaskAssigned {
		if to, _ := e.Payload["to"].(string); to == actor {
			return true
		}
	}
	return false
}
