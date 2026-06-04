package sqlite

import (
	"testing"

	"github.com/sn3d/tm/internal/client"
)

func TestPlanCommentsRepository_Add(t *testing.T) {
	b := newTempBackend(t)
	plan := client.Plan{ID: "plan-1", Subject: "with comments"}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	c := makeComment("alice", "looks good")

	if err := b.PlanComments().Add("plan-1", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected Add to assign a non-empty ID")
	}
	got, err := b.PlanComments().GetForPlan("plan-1")
	if err != nil {
		t.Fatalf("GetForPlan: %v", err)
	}
	if len(got) != 1 || got[0].ID != c.ID || got[0].Who != "alice" || got[0].Comment != "looks good" {
		t.Errorf("unexpected stored comments: %+v", got)
	}
}

func TestPlanCommentsRepository_GetForPlan(t *testing.T) {
	b := newTempBackend(t)
	plan1 := client.Plan{ID: "plan-1", Subject: "p1"}
	plan2 := client.Plan{ID: "plan-2", Subject: "p2"}
	if err := b.Plans().Save(&plan1); err != nil {
		t.Fatalf("seed plan 1: %v", err)
	}
	if err := b.Plans().Save(&plan2); err != nil {
		t.Fatalf("seed plan 2: %v", err)
	}
	seed := []client.Comment{
		makeComment("alice", "first"),
		makeComment("bob", "second"),
		makeComment("alice", "third"),
	}
	for i := range seed {
		if err := b.PlanComments().Add("plan-1", &seed[i]); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	other := makeComment("carol", "other plan")
	if err := b.PlanComments().Add("plan-2", &other); err != nil {
		t.Fatalf("Add (other plan): %v", err)
	}

	got, err := b.PlanComments().GetForPlan("plan-1")
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
	plan := client.Plan{ID: "plan-1", Subject: "no comments"}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	got, err := b.PlanComments().GetForPlan("plan-1")
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
	c := makeComment("alice", "orphan")

	if err := b.PlanComments().Add("missing-plan", &c); err == nil {
		t.Fatal("expected FK constraint error, got nil")
	}
}

func TestPlanCommentsRepository_CascadeOnPlanDelete(t *testing.T) {
	b := newTempBackend(t)
	plan := client.Plan{ID: "doomed-plan", Subject: "doomed"}
	if err := b.Plans().Save(&plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	c := makeComment("alice", "bye")
	if err := b.PlanComments().Add("doomed-plan", &c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	db := b.(*backend).plans.db
	if _, err := db.Exec(`DELETE FROM plans WHERE id = ?`, "doomed-plan"); err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	got, err := b.PlanComments().GetForPlan("doomed-plan")
	if err != nil {
		t.Fatalf("GetForPlan: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected cascade delete to remove comments, got %d", len(got))
	}
}
