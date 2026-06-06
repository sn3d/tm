package sqlite

import (
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func TestEventsRepository_AppendAssignsIDAndTimestamp(t *testing.T) {
	b := newTempBackend(t)
	e := &client.Event{Kind: client.EventTaskCreated, TaskID: "1", Actor: "alice", Payload: map[string]any{"subject": "s"}}
	if err := b.Events().Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if e.ID == "" {
		t.Error("expected non-empty ID")
	}
	if e.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestEventsRepository_RoundTrip(t *testing.T) {
	b := newTempBackend(t)
	in := &client.Event{
		Kind:    client.EventTaskAssigned,
		TaskID:  "task-1",
		Actor:   "bob",
		Payload: map[string]any{"from": "alice", "to": "bob"},
	}
	if err := b.Events().Append(in); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := b.Events().List(client.EventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].ID != in.ID || got[0].TaskID != "task-1" || got[0].Actor != "bob" {
		t.Errorf("roundtrip mismatch: %+v", got[0])
	}
	if got[0].Payload["from"] != "alice" || got[0].Payload["to"] != "bob" {
		t.Errorf("payload mismatch: %+v", got[0].Payload)
	}
}

func TestEventsRepository_NewestFirst(t *testing.T) {
	b := newTempBackend(t)
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		e := &client.Event{Kind: client.EventTaskCreated, TaskID: "t", Actor: "a", Timestamp: base.Add(time.Duration(i) * time.Second)}
		if err := b.Events().Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, _ := b.Events().List(client.EventFilter{})
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if !got[0].Timestamp.After(got[2].Timestamp) {
		t.Errorf("expected newest first, got %+v", got)
	}
}

func TestEventsRepository_Filters(t *testing.T) {
	b := newTempBackend(t)
	seed := []*client.Event{
		{Kind: client.EventTaskCreated, TaskID: "1", Actor: "alice"},
		{Kind: client.EventTaskAssigned, TaskID: "1", Actor: "bob"},
		{Kind: client.EventTaskCreated, TaskID: "2", Actor: "alice"},
		{Kind: client.EventTaskParentChanged, TaskID: "3", Actor: "carol"},
	}
	for _, e := range seed {
		if err := b.Events().Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	tests := []struct {
		name   string
		filter client.EventFilter
		want   int
	}{
		{"by task", client.EventFilter{TaskID: "1"}, 2},
		{"by actor", client.EventFilter{Actor: "alice"}, 2},
		{"by kind", client.EventFilter{Kinds: []client.EventKind{client.EventTaskAssigned}}, 1},
		{"by kinds OR", client.EventFilter{Kinds: []client.EventKind{client.EventTaskCreated, client.EventTaskAssigned}}, 3},
		{"limit", client.EventFilter{Limit: 2}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := b.Events().List(tt.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestEventsRepository_SinceIsExclusive(t *testing.T) {
	b := newTempBackend(t)
	first := &client.Event{Kind: client.EventTaskCreated, TaskID: "1", Actor: "a", Timestamp: time.Now().UTC()}
	if err := b.Events().Append(first); err != nil {
		t.Fatalf("Append: %v", err)
	}
	second := &client.Event{Kind: client.EventTaskCreated, TaskID: "2", Actor: "a", Timestamp: first.Timestamp.Add(time.Second)}
	if err := b.Events().Append(second); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := b.Events().List(client.EventFilter{Since: first.Timestamp})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].TaskID != "2" {
		t.Errorf("expected only second event, got %+v", got)
	}
}

func TestEventsRepository_EmptyOnFreshBackend(t *testing.T) {
	b := newTempBackend(t)
	got, err := b.Events().List(client.EventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}
