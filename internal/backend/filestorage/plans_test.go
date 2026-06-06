package filestorage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func newTempPlansRepo(t *testing.T) client.PlansRepository {
	t.Helper()
	b, err := NewBackend(tmpDir(t))
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	return b.Plans()
}

func TestPlansRepository_Create_AssignsID(t *testing.T) {
	repo := newTempPlansRepo(t)
	plan := client.Plan{
		Subject:       "Q2 cleanup",
		Description:   "remove deprecated",
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

func TestPlansRepository_Create_AssignsSequentialPlanIDs(t *testing.T) {
	repo := newTempPlansRepo(t)

	first := client.Plan{Subject: "a", State: client.PlanStateDraft}
	if err := repo.Save(&first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second := client.Plan{Subject: "b", State: client.PlanStateDraft}
	if err := repo.Save(&second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	if first.ID != "1" || second.ID != "2" {
		t.Errorf("expected 1, 2; got %q, %q", first.ID, second.ID)
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
		{Subject: "first", State: client.PlanStateDraft, AssignedAgent: "a"},
		{Subject: "second", State: client.PlanStateActive, AssignedAgent: "b"},
		{Subject: "third", State: client.PlanStateCompleted, AssignedAgent: "c"},
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
}

func TestPlansRepository_Save_WritesSlugFilename(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	repo := b.Plans()
	plan := client.Plan{Subject: "Cleanup Q2!", State: client.PlanStateDraft}

	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save: %v", err)
	}

	want := filepath.Join(dir, "plans", "1--cleanup-q2.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected file at %q: %v", want, err)
	}
}

func TestPlansRepository_Save_RenamesOnSubjectChange(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	repo := b.Plans()
	plan := client.Plan{Subject: "old subject", State: client.PlanStateDraft}
	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save initial: %v", err)
	}
	original := filepath.Join(dir, "plans", "1--old-subject.md")
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("expected original file: %v", err)
	}

	plan.Subject = "new subject"
	if err := repo.Save(&plan); err != nil {
		t.Fatalf("Save update: %v", err)
	}

	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Errorf("expected old file to be removed, got err=%v", err)
	}
	renamed := filepath.Join(dir, "plans", "1--new-subject.md")
	if _, err := os.Stat(renamed); err != nil {
		t.Errorf("expected renamed file at %q: %v", renamed, err)
	}
}

func TestPlansRepository_Update(t *testing.T) {
	repo := newTempPlansRepo(t)
	original := client.Plan{ID: "PLAN-X", Subject: "draft", Description: "v1", State: client.PlanStateDraft, AssignedAgent: "agent-a"}
	if err := repo.Save(&original); err != nil {
		t.Fatalf("Save (initial): %v", err)
	}
	updated := client.Plan{ID: "PLAN-X", Subject: "final", Description: "v2", State: client.PlanStateCompleted, AssignedAgent: "agent-b"}

	if err := repo.Save(&updated); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	got, err := repo.GetByID("PLAN-X")
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

// Legacy plan files written before the timestamp columns existed must parse
// cleanly with zero-valued CreatedAt/UpdatedAt.
func TestPlansRepository_ParsesLegacyFile_WithoutTimestamps(t *testing.T) {
	dir := tmpDir(t)
	b, err := NewBackend(dir)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}

	legacyPath := filepath.Join(dir, "plans", "legacy-1.md")
	legacyContent := `---
id: legacy-1
state: draft
assigned_agent: alice
---

# legacy plan
`
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, err := b.Plans().GetByID("legacy-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected legacy plan to load")
	}
	if !got.CreatedAt.IsZero() || !got.UpdatedAt.IsZero() {
		t.Errorf("expected zero timestamps for legacy plan, got CreatedAt=%v UpdatedAt=%v", got.CreatedAt, got.UpdatedAt)
	}
}
