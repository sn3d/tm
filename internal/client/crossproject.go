package client

import (
	"fmt"
	"time"
)

// InboxAll returns one inbox per non-archived project in the instance, in
// ticker order, skipping projects where the actor has nothing to do. Each
// inbox carries its own Project and its own cursor, so glancing at one
// project never marks another's work as seen.
//
// Scope is the instance, not every configured instance. `--all` spans the
// resolved instance; reaching another takes `--instance work --all`. That
// keeps one DB connection per command and preserves the instance as a real
// isolation boundary (work vs personal).
func (c *Client) InboxAll(actor string) ([]*Inbox, error) {
	if actor == "" {
		return nil, fmt.Errorf("inbox: actor is required")
	}
	projects, err := c.backend.Projects().GetAll(false)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]*Inbox, 0, len(projects))
	for _, p := range projects {
		// Peek semantics per project would leave cursors unadvanced; the
		// caller's advance choice applies uniformly, so InboxAll advances
		// each project it actually reports on.
		box, err := c.inboxFor(actor, p.Ticker, true)
		if err != nil {
			return nil, fmt.Errorf("inbox for project %s: %w", p.Ticker, err)
		}
		if len(box.Tasks) == 0 && len(box.RecentChanges) == 0 {
			continue
		}
		out = append(out, box)
	}
	return out, nil
}

// PeekInboxAll is InboxAll without advancing any cursor.
func (c *Client) PeekInboxAll(actor string) ([]*Inbox, error) {
	if actor == "" {
		return nil, fmt.Errorf("inbox: actor is required")
	}
	projects, err := c.backend.Projects().GetAll(false)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]*Inbox, 0, len(projects))
	for _, p := range projects {
		box, err := c.inboxFor(actor, p.Ticker, false)
		if err != nil {
			return nil, fmt.Errorf("inbox for project %s: %w", p.Ticker, err)
		}
		if len(box.Tasks) == 0 && len(box.RecentChanges) == 0 {
			continue
		}
		out = append(out, box)
	}
	return out, nil
}

// JournalAll returns journal events across every project in the instance,
// newest first. Each Event carries its own Project, so output can render
// `TM/#42` and the reader can tell which repo an entry came from.
//
// An explicit filter.Project is honoured (callers can narrow after asking
// for the cross-project view); otherwise the filter deliberately leaves the
// project unset so the repository scans the whole instance.
func (c *Client) JournalAll(filter EventFilter) ([]Event, error) {
	events, err := c.backend.Events().List(filter)
	if err != nil {
		return nil, fmt.Errorf("list events across projects: %w", err)
	}
	return events, nil
}

// TimesheetAll builds the activity log across every project in the instance.
// Same window and milestone rules as Timesheet; entries carry fully-qualified
// task refs so a weekly report can attribute work per repo.
func (c *Client) TimesheetAll(from, to time.Time, actor string) ([]TimesheetEntry, error) {
	if !to.After(from) {
		return nil, fmt.Errorf("timesheet: window end %s is not after start %s",
			to.Format(time.RFC3339), from.Format(time.RFC3339))
	}
	events, err := c.JournalAll(timesheetFilter(from, to, actor, ""))
	if err != nil {
		return nil, err
	}
	return c.entriesFromEvents(events), nil
}
