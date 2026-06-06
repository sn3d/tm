package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func newTempPlansRepo(t *testing.T) client.PlansRepository {
	t.Helper()
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tm-test-%d.db", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(dbPath) })

	b, err := NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	return b.Plans()
}

func TestPlansRepository_Create_AssignsID(t *testing.T) {
	repo := newTempPlansRepo(t)
	plan := client.Plan{
		Subject:       "Q2 cleanup",
		Description:   "remove deprecated code",
		State:         client.PlanStateDraft,
		AssignedAgent: "alice",
	}

	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if plan.ID == "" {
		t.Fatal("expected Save to assign a non-empty ID")
	}
	got, err := repo.GetByID(plan.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected plan, got nil")
	}
	if got.Subject != plan.Subject || got.Description != plan.Description ||
		got.State != plan.State || got.AssignedAgent != plan.AssignedAgent {
		t.Errorf("stored plan mismatch: %+v", got)
	}
}

func TestPlansRepository_Create_AssignsUniqueIDs(t *testing.T) {
	repo := newTempPlansRepo(t)
	first := client.Plan{Subject: "a"}
	second := client.Plan{Subject: "b"}

	if err := repo.Save(&first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := repo.Save(&second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	if first.ID == "" || second.ID == "" {
		t.Fatalf("expected non-empty IDs, got first=%q second=%q", first.ID, second.ID)
	}
	if first.ID == second.ID {
		t.Errorf("expected unique IDs, got both %q", first.ID)
	}
}

func TestPlansRepository_GetByID_NotFound(t *testing.T) {
	repo := newTempPlansRepo(t)

	got, err := repo.GetByID("does-not-exist")

	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing plan, got %+v", got)
	}
}

func TestPlansRepository_GetAll(t *testing.T) {
	repo := newTempPlansRepo(t)
	seed := []client.Plan{
		{ID: "plan-1", Subject: "first", State: client.PlanStateDraft, AssignedAgent: "a"},
		{ID: "plan-2", Subject: "second", State: client.PlanStateActive, AssignedAgent: "b"},
		{ID: "plan-3", Subject: "third", State: client.PlanStateCompleted, AssignedAgent: "c"},
	}
	for i := range seed {
		if err := repo.Save(&seed[i]); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(got) != len(seed) {
		t.Fatalf("expected %d plans, got %d", len(seed), len(got))
	}
	// GetAll orders by UpdatedAt DESC, so the most recently saved plan comes
	// first — i.e. the reverse of insertion order here.
	for i, p := range got {
		want := seed[len(seed)-1-i]
		if p.Subject != want.Subject || p.AssignedAgent != want.AssignedAgent {
			t.Errorf("plan %d mismatch: got %+v, want %+v", i, p, want)
		}
	}
}

func TestPlansRepository_Update(t *testing.T) {
	repo := newTempPlansRepo(t)
	original := client.Plan{ID: "update-target", Subject: "draft", Description: "v1", State: client.PlanStateDraft, AssignedAgent: "agent-a"}
	if err := repo.Save(&original); err != nil {
		t.Fatalf("Save (initial): %v", err)
	}
	updated := client.Plan{ID: "update-target", Subject: "final", Description: "v2", State: client.PlanStateCompleted, AssignedAgent: "agent-b"}

	if err := repo.Save(&updated); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	got, err := repo.GetByID("update-target")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected plan, got nil")
	}
	if got.Subject != updated.Subject || got.Description != updated.Description ||
		got.State != updated.State || got.AssignedAgent != updated.AssignedAgent {
		t.Errorf("update did not persist: got %+v, want %+v", got, updated)
	}
	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 row after update, got %d", len(all))
	}
}

func TestPlansRepository_Save_StampsTimestamps(t *testing.T) {
	repo := newTempPlansRepo(t)
	plan := client.Plan{Subject: "stamps"}
	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save (initial): %v", err)
	}
	if plan.CreatedAt.IsZero() || plan.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps stamped, got CreatedAt=%v UpdatedAt=%v", plan.CreatedAt, plan.UpdatedAt)
	}
	created, firstUpdated := plan.CreatedAt, plan.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	plan.Subject = "stamps v2"
	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	if !plan.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt should be preserved: got %v, want %v", plan.CreatedAt, created)
	}
	if !plan.UpdatedAt.After(firstUpdated) {
		t.Errorf("UpdatedAt should advance: got %v, was %v", plan.UpdatedAt, firstUpdated)
	}
}

// A caller-supplied non-zero CreatedAt on first insert (e.g. importing data
// from another system) must be honored rather than overwritten with now.
func TestPlansRepository_Save_HonorsCallerSuppliedCreatedAt(t *testing.T) {
	repo := newTempPlansRepo(t)
	want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	plan := client.Plan{Subject: "imported", CreatedAt: want}
	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !plan.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt overwritten: got %v, want %v", plan.CreatedAt, want)
	}
	got, err := repo.GetByID(plan.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt did not round-trip: got %v, want %v", got.CreatedAt, want)
	}
}
