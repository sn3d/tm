package client

import (
	"testing"
	"time"
)

func TestStubEvents_AppendAssignsIDAndTimestamp(t *testing.T) {
	s := &stubEvents{}
	e := &Event{Kind: EventTaskCreated, TaskID: "1"}
	if err := s.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if e.ID == "" {
		t.Error("expected Append to assign ID")
	}
	if e.Timestamp.IsZero() {
		t.Error("expected Append to assign Timestamp")
	}
}

func TestStubEvents_ListNewestFirst(t *testing.T) {
	s := &stubEvents{}
	for i := 0; i < 3; i++ {
		_ = s.Append(&Event{Kind: EventTaskCreated, TaskID: "t"})
	}
	got, err := s.List(EventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}
	if !got[0].Timestamp.After(got[2].Timestamp) {
		t.Error("expected newest first")
	}
}

func TestStubEvents_ListFilters(t *testing.T) {
	s := &stubEvents{}
	_ = s.Append(&Event{Kind: EventTaskCreated, TaskID: "1", Actor: "alice"})
	_ = s.Append(&Event{Kind: EventTaskAssigned, TaskID: "1", Actor: "bob"})
	_ = s.Append(&Event{Kind: EventTaskCreated, TaskID: "2", Actor: "alice"})
	_ = s.Append(&Event{Kind: EventPlanCreated, PlanID: "PLAN-1", Actor: "carol"})

	tests := []struct {
		name   string
		filter EventFilter
		want   int
	}{
		{"by task", EventFilter{TaskID: "1"}, 2},
		{"by plan", EventFilter{PlanID: "PLAN-1"}, 1},
		{"by actor", EventFilter{Actor: "alice"}, 2},
		{"by kind", EventFilter{Kinds: []EventKind{EventTaskAssigned}}, 1},
		{"by kinds OR", EventFilter{Kinds: []EventKind{EventTaskAssigned, EventTaskCreated}}, 3},
		{"limit", EventFilter{Limit: 2}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(tt.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d, want %d (%+v)", len(got), tt.want, got)
			}
		})
	}
}

func TestStubEvents_ListSinceIsExclusive(t *testing.T) {
	s := &stubEvents{}
	_ = s.Append(&Event{Kind: EventTaskCreated, TaskID: "1"})
	cutoff := s.appended[0].Timestamp
	_ = s.Append(&Event{Kind: EventTaskCreated, TaskID: "2"})

	got, err := s.List(EventFilter{Since: cutoff})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].TaskID != "2" {
		t.Errorf("expected only event with TaskID=2, got %+v", got)
	}
}

// sanity check that ActorSystem fallback works when no option is supplied.
func TestClient_DefaultActorIsSystem(t *testing.T) {
	c := New(newStubBackend())
	if c.actor != ActorSystem {
		t.Errorf("default actor: got %q, want %q", c.actor, ActorSystem)
	}
	_, _ = c.CreateTask(CreateTaskInput{Subject: "s"})
	evs := c.backend.(*stubBackend).events.appended
	if len(evs) != 1 || evs[0].Actor != ActorSystem {
		t.Errorf("expected actor=system on emitted event, got %+v", evs)
	}
}

func TestClient_WithActorOverrides(t *testing.T) {
	c := New(newStubBackend(), WithActor("alice"))
	_, _ = c.CreateTask(CreateTaskInput{Subject: "s"})
	evs := c.backend.(*stubBackend).events.appended
	if evs[0].Actor != "alice" {
		t.Errorf("got actor=%q, want %q", evs[0].Actor, "alice")
	}
}

func TestClient_WithActorEmptyFallsBackToSystem(t *testing.T) {
	c := New(newStubBackend(), WithActor(""))
	if c.actor != ActorSystem {
		t.Errorf("empty actor should fall back to %q, got %q", ActorSystem, c.actor)
	}
}

func TestClient_EmitsTimestampedEvents(t *testing.T) {
	c := New(newStubBackend())
	_, _ = c.CreateTask(CreateTaskInput{Subject: "s"})
	evs := c.backend.(*stubBackend).events.appended
	if evs[0].Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if time.Since(evs[0].Timestamp) > time.Hour {
		// stub uses synthetic ts, so don't compare to wall clock — just sanity.
	}
}
