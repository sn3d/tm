package client

import (
	"fmt"
	"time"
)

// TimesheetActivity is the normalized milestone label on a timesheet entry.
// It is derived from the underlying journal event: task.created maps to
// "created", task.commented to "commented", and the two meaningful
// task.state_changed transitions to "started" / "finished".
type TimesheetActivity string

const (
	ActivityCreated   TimesheetActivity = "created"
	ActivityStarted   TimesheetActivity = "started"
	ActivityFinished  TimesheetActivity = "finished"
	ActivityCommented TimesheetActivity = "commented"
)

// TimesheetEntry is one resolved line in the weekly activity log. It is built
// from a single journal Event, with the task's Subject and (for comments) the
// comment body joined in. Detail carries per-activity context: for a
// "finished" entry it holds the terminal state ("done" or "cancelled"); for a
// "commented" entry it holds the comment body; otherwise it is empty.
type TimesheetEntry struct {
	Timestamp   time.Time
	Activity    TimesheetActivity
	TaskID      TaskID
	TaskSubject string // "" if the task is no longer resolvable (hard-deleted)
	Actor       string
	Detail      string
}

// TimesheetEntryView is the canonical JSON shape for a timesheet entry,
// shared by the CLI (`--format json`) and the MCP tool so both surfaces stay
// in lockstep. Timestamp is RFC3339Nano; Detail is omitted when empty.
type TimesheetEntryView struct {
	Timestamp   string `json:"ts"`
	Activity    string `json:"activity"`
	TaskID      string `json:"task_id"`
	TaskSubject string `json:"task_subject"`
	Actor       string `json:"actor"`
	Detail      string `json:"detail,omitempty"`
}

// ViewTimesheetEntry maps a TimesheetEntry to its JSON view.
func ViewTimesheetEntry(e TimesheetEntry) TimesheetEntryView {
	return TimesheetEntryView{
		Timestamp:   e.Timestamp.Format(time.RFC3339Nano),
		Activity:    string(e.Activity),
		TaskID:      e.TaskID,
		TaskSubject: e.TaskSubject,
		Actor:       e.Actor,
		Detail:      e.Detail,
	}
}

// ViewTimesheetEntries maps a slice of entries to their JSON views, keeping the
// CLI (`--format json`) and MCP tool on one code path.
func ViewTimesheetEntries(entries []TimesheetEntry) []TimesheetEntryView {
	views := make([]TimesheetEntryView, len(entries))
	for i, e := range entries {
		views[i] = ViewTimesheetEntry(e)
	}
	return views
}

// CurrentWeek returns the [from, to) window for the Monday-start week
// containing now, in now's own location: from is Monday 00:00:00 and to is the
// following Monday 00:00:00 (exclusive). now is taken as a parameter rather
// than read internally so the range logic stays deterministic and testable.
func CurrentWeek(now time.Time) (from, to time.Time) {
	// Go's Weekday is Sunday=0..Saturday=6; remap so Monday=0..Sunday=6.
	offset := (int(now.Weekday()) + 6) % 7
	y, m, d := now.Date()
	from = time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -offset)
	to = from.AddDate(0, 0, 7)
	return from, to
}

// ResolveTimesheetRange computes the [from, to) window shared by the CLI and
// MCP entry points. It starts from the Monday-start week containing now,
// shifts back a week when lastWeek is set, then applies whichever of since /
// until the caller has already parsed (a nil pointer leaves that side at the
// week default). Parsing (RFC3339, bare dates, day-bumping) stays with each
// caller; only the default-and-override skeleton lives here so the two
// surfaces cannot drift apart.
func ResolveTimesheetRange(now time.Time, lastWeek bool, since, until *time.Time) (from, to time.Time) {
	from, to = CurrentWeek(now)
	if lastWeek {
		from = from.AddDate(0, 0, -7)
		to = to.AddDate(0, 0, -7)
	}
	if since != nil {
		from = *since
	}
	if until != nil {
		to = *until
	}
	return from, to
}

// Timesheet returns the activity-log entries in [from, to) (from inclusive,
// to exclusive), oldest-first, restricted to the created / started / finished
// / commented milestones. actor == "" reports all actors; a non-empty actor
// narrows to that actor. Task subjects and comment bodies are joined in;
// entries whose task or comment can no longer be resolved keep empty strings
// rather than failing the whole report.
func (c *Client) Timesheet(from, to time.Time, actor string) ([]TimesheetEntry, error) {
	if !to.After(from) {
		return nil, fmt.Errorf("timesheet: window end %s is not after start %s", to.Format(time.RFC3339), from.Format(time.RFC3339))
	}
	filter := EventFilter{
		Actor: actor,
		Kinds: []EventKind{EventTaskCreated, EventTaskStateChanged, EventTaskCommented},
		// Since is an exclusive lower bound, so nudge back by 1ns to keep an
		// event landing exactly on the window start (e.g. Monday 00:00:00).
		Since: from.Add(-time.Nanosecond),
		Until: to,
	}
	events, err := c.ListEvents(filter)
	if err != nil {
		return nil, err
	}

	// ListEvents returns newest-first; flip to oldest-first so the log reads
	// chronologically (same idiom as cmd/journal/list.go).
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}

	subjects := map[TaskID]string{}    // per-task subject cache
	comments := map[TaskID][]Comment{} // per-task comment cache

	out := make([]TimesheetEntry, 0, len(events))
	for _, e := range events {
		entry, ok := c.eventToEntry(e, subjects, comments)
		if !ok {
			continue // non-milestone state transition
		}
		out = append(out, entry)
	}
	return out, nil
}

// eventToEntry maps a single journal event to a timesheet entry. It returns
// (_, false) for state transitions that are not milestones (e.g. blocked,
// in_review), which the caller skips.
func (c *Client) eventToEntry(e Event, subjects map[TaskID]string, comments map[TaskID][]Comment) (TimesheetEntry, bool) {
	entry := TimesheetEntry{Timestamp: e.Timestamp, TaskID: e.TaskID, Actor: e.Actor}

	switch e.Kind {
	case EventTaskCreated:
		entry.Activity = ActivityCreated
	case EventTaskStateChanged:
		to, _ := e.Payload["to"].(string)
		switch TaskState(to) {
		case TaskStateInProgress:
			entry.Activity = ActivityStarted
		case TaskStateDone, TaskStateCancelled:
			entry.Activity = ActivityFinished
			entry.Detail = to
		default:
			return TimesheetEntry{}, false
		}
	case EventTaskCommented:
		entry.Activity = ActivityCommented
		commentID, _ := e.Payload["comment_id"].(string)
		entry.Detail = c.resolveCommentText(e.TaskID, commentID, comments)
	default:
		return TimesheetEntry{}, false
	}

	entry.TaskSubject = c.resolveSubject(e.TaskID, subjects)
	return entry, true
}

// resolveSubject returns the task's subject, caching lookups per task ID. A
// task that can no longer be loaded resolves to "" (cached) so the timesheet
// still lists the activity.
func (c *Client) resolveSubject(id TaskID, cache map[TaskID]string) string {
	if subject, seen := cache[id]; seen {
		return subject
	}
	subject := ""
	if t, err := c.GetTask(id); err == nil {
		subject = t.Subject
	}
	cache[id] = subject
	return subject
}

// resolveCommentText returns the body of the comment with the given ID on the
// task, caching the task's comments per task ID. A missing task or comment
// resolves to "".
func (c *Client) resolveCommentText(id TaskID, commentID string, cache map[TaskID][]Comment) string {
	list, seen := cache[id]
	if !seen {
		list, _ = c.GetTaskComments(id) // error -> nil, treated as no comments
		cache[id] = list
	}
	for _, cm := range list {
		if cm.ID == commentID {
			return cm.Comment
		}
	}
	return ""
}
