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
	for i, p := range got {
		if p.Subject != seed[i].Subject || p.AssignedAgent != seed[i].AssignedAgent {
			t.Errorf("plan %d mismatch: got %+v, want %+v", i, p, seed[i])
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
