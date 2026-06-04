package sqlite

import (
	"testing"
	"time"
)

func TestActorCursors_GetUnseenReturnsZero(t *testing.T) {
	b := newTempBackend(t)
	got, err := b.ActorCursors().Get("nobody")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("want zero time for unseen actor, got %v", got)
	}
}

func TestActorCursors_SetThenGetRoundTrips(t *testing.T) {
	b := newTempBackend(t)
	ts := time.Date(2026, 5, 29, 12, 34, 56, 789000000, time.UTC)
	if err := b.ActorCursors().Set("alice", ts); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := b.ActorCursors().Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Equal(ts) {
		t.Errorf("round-trip: got %v, want %v", got, ts)
	}
}

func TestActorCursors_MultipleActorsIndependent(t *testing.T) {
	b := newTempBackend(t)
	a := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	if err := b.ActorCursors().Set("alice", a); err != nil {
		t.Fatalf("Set alice: %v", err)
	}
	if err := b.ActorCursors().Set("carol", c); err != nil {
		t.Fatalf("Set carol: %v", err)
	}

	gotA, _ := b.ActorCursors().Get("alice")
	gotC, _ := b.ActorCursors().Get("carol")
	if !gotA.Equal(a) {
		t.Errorf("alice: got %v, want %v", gotA, a)
	}
	if !gotC.Equal(c) {
		t.Errorf("carol: got %v, want %v", gotC, c)
	}
}

func TestActorCursors_OverwriteReplaces(t *testing.T) {
	b := newTempBackend(t)
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := b.ActorCursors().Set("alice", older); err != nil {
		t.Fatalf("Set older: %v", err)
	}
	if err := b.ActorCursors().Set("alice", newer); err != nil {
		t.Fatalf("Set newer: %v", err)
	}
	got, _ := b.ActorCursors().Get("alice")
	if !got.Equal(newer) {
		t.Errorf("expected overwrite to newer: got %v, want %v", got, newer)
	}
}
