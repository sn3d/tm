package client

import (
	"testing"
	"time"
)

func TestCurrentWeek(t *testing.T) {
	loc := time.FixedZone("TST", 2*3600)
	// Week of 2024-01-01 (a Monday) .. 2024-01-08 (the next Monday).
	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	nextMonday := time.Date(2024, 1, 8, 0, 0, 0, 0, loc)

	tests := []struct {
		name string
		now  time.Time
	}{
		{"monday morning", time.Date(2024, 1, 1, 0, 0, 0, 0, loc)},
		{"monday midday", time.Date(2024, 1, 1, 12, 30, 0, 0, loc)},
		{"wednesday", time.Date(2024, 1, 3, 9, 0, 0, 0, loc)},
		{"saturday", time.Date(2024, 1, 6, 23, 59, 59, 0, loc)},
		{"sunday", time.Date(2024, 1, 7, 18, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := CurrentWeek(tt.now)
			if !from.Equal(monday) {
				t.Errorf("from: got %v, want %v", from, monday)
			}
			if !to.Equal(nextMonday) {
				t.Errorf("to: got %v, want %v", to, nextMonday)
			}
		})
	}
}

func TestResolveTimesheetRange(t *testing.T) {
	loc := time.UTC
	now := time.Date(2024, 1, 3, 9, 0, 0, 0, loc) // a Wednesday
	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	nextMonday := time.Date(2024, 1, 8, 0, 0, 0, 0, loc)
	since := time.Date(2023, 12, 1, 0, 0, 0, 0, loc)
	until := time.Date(2024, 2, 1, 0, 0, 0, 0, loc)

	t.Run("default week", func(t *testing.T) {
		from, to := ResolveTimesheetRange(now, false, nil, nil)
		if !from.Equal(monday) || !to.Equal(nextMonday) {
			t.Errorf("got [%v,%v), want [%v,%v)", from, to, monday, nextMonday)
		}
	})
	t.Run("last week", func(t *testing.T) {
		from, to := ResolveTimesheetRange(now, true, nil, nil)
		if !from.Equal(monday.AddDate(0, 0, -7)) || !to.Equal(monday) {
			t.Errorf("last-week window wrong: [%v,%v)", from, to)
		}
	})
	t.Run("since overrides from only", func(t *testing.T) {
		from, to := ResolveTimesheetRange(now, false, &since, nil)
		if !from.Equal(since) || !to.Equal(nextMonday) {
			t.Errorf("got [%v,%v)", from, to)
		}
	})
	t.Run("both override", func(t *testing.T) {
		from, to := ResolveTimesheetRange(now, false, &since, &until)
		if !from.Equal(since) || !to.Equal(until) {
			t.Errorf("got [%v,%v)", from, to)
		}
	})
}

// wideRange is a window guaranteed to contain every event the stub backend
// synthesizes (its timestamps start at Unix second 1).
func wideRange() (time.Time, time.Time) {
	return time.Unix(0, 0).UTC(), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
}

func TestTimesheet_Milestones(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject), WithActor("alice"))

	id, err := c.CreateTask(CreateTaskInput{Subject: "Wire up billing"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := c.EditTask(id, EditTaskInput{Subject: "Wire up billing", State: TaskStateInProgress}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := c.AddTaskComment(id, "alice", "spoke to finance"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if err := c.EditTask(id, EditTaskInput{Subject: "Wire up billing", State: TaskStateDone}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	from, to := wideRange()
	entries, err := c.Timesheet(from, to, "")
	if err != nil {
		t.Fatalf("Timesheet: %v", err)
	}

	want := []struct {
		activity TimesheetActivity
		detail   string
	}{
		{ActivityCreated, ""},
		{ActivityStarted, ""},
		{ActivityCommented, "spoke to finance"},
		{ActivityFinished, "done"},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i, w := range want {
		e := entries[i]
		if e.Activity != w.activity {
			t.Errorf("entry %d activity: got %q, want %q", i, e.Activity, w.activity)
		}
		if e.Detail != w.detail {
			t.Errorf("entry %d detail: got %q, want %q", i, e.Detail, w.detail)
		}
		if e.TaskSubject != "Wire up billing" {
			t.Errorf("entry %d subject: got %q", i, e.TaskSubject)
		}
	}

	// Oldest-first ordering.
	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp.Before(entries[i-1].Timestamp) {
			t.Errorf("entries not oldest-first at %d: %+v", i, entries)
		}
	}
}

func TestTimesheet_CancelledIsFinished(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject), WithActor("alice"))
	id, _ := c.CreateTask(CreateTaskInput{Subject: "s"})
	if err := c.EditTask(id, EditTaskInput{Subject: "s", State: TaskStateCancelled}); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	from, to := wideRange()
	entries, _ := c.Timesheet(from, to, "")
	var fin *TimesheetEntry
	for i := range entries {
		if entries[i].Activity == ActivityFinished {
			fin = &entries[i]
		}
	}
	if fin == nil {
		t.Fatalf("no finished entry: %+v", entries)
	}
	if fin.Detail != "cancelled" {
		t.Errorf("detail: got %q, want %q", fin.Detail, "cancelled")
	}
}

func TestTimesheet_IgnoresNonMilestoneTransitions(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject), WithActor("alice"))
	id, _ := c.CreateTask(CreateTaskInput{Subject: "s"})
	if err := c.EditTask(id, EditTaskInput{Subject: "s", State: TaskStateBlocked}); err != nil {
		t.Fatalf("block: %v", err)
	}
	if err := c.EditTask(id, EditTaskInput{Subject: "s", State: TaskStateInReview}); err != nil {
		t.Fatalf("review: %v", err)
	}

	from, to := wideRange()
	entries, _ := c.Timesheet(from, to, "")
	for _, e := range entries {
		if e.Activity == ActivityStarted || e.Activity == ActivityFinished {
			t.Errorf("unexpected milestone from blocked/in_review: %+v", e)
		}
	}
	// Only the "created" entry should survive.
	if len(entries) != 1 || entries[0].Activity != ActivityCreated {
		t.Errorf("expected only a created entry, got %+v", entries)
	}
}

func TestTimesheet_ActorFilter(t *testing.T) {
	b := newStubBackend()
	alice := New(b, WithProject(testProject), WithActor("alice"))
	bob := New(b, WithProject(testProject), WithActor("bob"))

	if _, err := alice.CreateTask(CreateTaskInput{Subject: "by alice"}); err != nil {
		t.Fatalf("alice create: %v", err)
	}
	if _, err := bob.CreateTask(CreateTaskInput{Subject: "by bob"}); err != nil {
		t.Fatalf("bob create: %v", err)
	}

	from, to := wideRange()
	entries, err := alice.Timesheet(from, to, "alice")
	if err != nil {
		t.Fatalf("Timesheet: %v", err)
	}
	if len(entries) != 1 || entries[0].Actor != "alice" {
		t.Errorf("actor filter failed: %+v", entries)
	}

	all, _ := alice.Timesheet(from, to, "")
	if len(all) != 2 {
		t.Errorf("unfiltered: got %d, want 2: %+v", len(all), all)
	}
}

func TestTimesheet_EmptyWindow(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject), WithActor("alice"))
	if _, err := c.CreateTask(CreateTaskInput{Subject: "s"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// A window in the far future contains no events.
	from := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	entries, err := c.Timesheet(from, to, "")
	if err != nil {
		t.Fatalf("Timesheet: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %+v", entries)
	}
}

func TestTimesheet_InvertedWindowErrors(t *testing.T) {
	c := New(newStubBackend(), WithProject(testProject), WithActor("alice"))
	from := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // before from
	if _, err := c.Timesheet(from, to, ""); err == nil {
		t.Error("expected error for inverted window, got nil")
	}
	// Equal bounds are also empty/invalid (half-open window contains nothing).
	if _, err := c.Timesheet(from, from, ""); err == nil {
		t.Error("expected error for zero-width window, got nil")
	}
}

func TestTimesheet_MissingTaskResolvesEmptySubject(t *testing.T) {
	b := newStubBackend()
	// Append a bare state-changed event whose task is not in the store.
	_ = b.events.Append(&Event{
		Kind:    EventTaskStateChanged,
		TaskID:  ref("ghost"),
		Actor:   "alice",
		Payload: map[string]any{"from": "todo", "to": "in_progress"},
	})
	c := New(b, WithProject(testProject), WithActor("alice"))

	from, to := wideRange()
	entries, err := c.Timesheet(from, to, "")
	if err != nil {
		t.Fatalf("Timesheet: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
	}
	if entries[0].Activity != ActivityStarted || entries[0].TaskSubject != "" {
		t.Errorf("expected started entry with empty subject, got %+v", entries[0])
	}
}
