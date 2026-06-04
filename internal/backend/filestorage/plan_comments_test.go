package filestorage

import (
	"testing"

	"github.com/sn3d/tm/internal/client"
)

func TestPlanCommentsRepository_Add(t *testing.T) {
	b := newTempBackend(t)
	plan := client.Plan{ID: "PLAN-1", Subject: "with comments", State: client.PlanStateDraft}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	c := client.Comment{Who: "alice", Comment: "looks good"}

	if err := b.PlanComments().Add("PLAN-1", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected Add to assign a non-empty ID")
	}
	got, err := b.PlanComments().GetForPlan("PLAN-1")
	if err != nil {
		t.Fatalf("GetForPlan: %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID || got[0].Who != "alice" || got[0].Comment != "looks good" {
		t.Errorf("unexpected stored comments: %+v", got)
	}
}

func TestPlanCommentsRepository_GetForPlan(t *testing.T) {
	b := newTempBackend(t)
	plan1 := client.Plan{ID: "PLAN-1", Subject: "p1", State: client.PlanStateDraft}
	plan2 := client.Plan{ID: "PLAN-2", Subject: "p2", State: client.PlanStateDraft}
	if err := b.Plans().Save(&plan1); err != nil {
		t.Fatalf("seed plan 1: %v", err)
	}
	if err := b.Plans().Save(&plan2); err != nil {
		t.Fatalf("seed plan 2: %v", err)
	}
	seed := []client.Comment{
		{Who: "alice", Comment: "first"},
		{Who: "bob", Comment: "second"},
		{Who: "alice", Comment: "third"},
	}
	for i := range seed {
		if err := b.PlanComments().Add("PLAN-1", &seed[i]); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	other := client.Comment{Who: "carol", Comment: "other plan"}
	if err := b.PlanComments().Add("PLAN-2", &other); err != nil {
		t.Fatalf("Add (other plan): %v", err)
	}

	got, err := b.PlanComments().GetForPlan("PLAN-1")
	if err != nil {
		t.Fatalf("GetForPlan: %v", err)
	}
	if len(got) != len(seed) {
		t.Fatalf("expected %d comments, got %d", len(seed), len(got))
	}
	for i, c := range got {
		if c.ID != seed[i].ID || c.Who != seed[i].Who || c.Comment != seed[i].Comment {
			t.Errorf("comment %d mismatch: got %+v, want %+v", i, c, seed[i])
		}
	}
}

func TestPlanCommentsRepository_GetForPlan_Empty(t *testing.T) {
	b := newTempBackend(t)
	plan := client.Plan{ID: "PLAN-1", Subject: "no comments", State: client.PlanStateDraft}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	got, err := b.PlanComments().GetForPlan("PLAN-1")
	if err != nil {
		t.Fatalf("GetForPlan: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 comments, got %d", len(got))
	}
}

func TestPlanCommentsRepository_Add_NonexistentPlan(t *testing.T) {
	b := newTempBackend(t)
	c := client.Comment{Who: "alice", Comment: "orphan"}

	if err := b.PlanComments().Add("missing-plan", &c); err == nil {
		t.Fatal("expected error for missing plan, got nil")
	}
}

func TestPlansRepository_Update_PreservesComments(t *testing.T) {
	b := newTempBackend(t)
	plan := client.Plan{ID: "PLAN-1", Subject: "initial", State: client.PlanStateDraft}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	c := client.Comment{Who: "alice", Comment: "important note"}
	if err := b.PlanComments().Add("PLAN-1", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	updated := client.Plan{ID: "PLAN-1", Subject: "renamed", State: client.PlanStateActive, AssignedAgent: "agent-x"}
	if err := b.Plans().Save(&updated); err != nil {
		t.Fatalf("Save (update): %v", err)
	}

	got, err := b.PlanComments().GetForPlan("PLAN-1")
	if err != nil {
		t.Fatalf("GetForPlan: %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID || got[0].Who != "alice" || got[0].Comment != "important note" {
		t.Errorf("expected comments preserved across update, got %+v", got)
	}
}
