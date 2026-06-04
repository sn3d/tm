package filestorage

import (
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

type planCommentsRepository struct {
	dir string
}

// Add appends a comment to the given plan file. The repository assigns a
// fresh ULID and writes it back into c.ID. Returns an error if the plan
// does not exist.
func (cr *planCommentsRepository) Add(id client.PlanID, c *client.Comment) error {
	pf, err := readPlanFile(cr.dir, id)
	if err != nil {
		return err
	}
	if pf == nil {
		return fmt.Errorf("plan %q not found", id)
	}
	c.ID = ulid.Make().String()
	pf.comments = append(pf.comments, *c)
	return writePlanFile(cr.dir, pf)
}

// GetForPlan returns every comment attached to the given plan, in insertion
// order. Returns an empty slice when the plan has no comments or does not
// exist (matching the sqlite backend's behavior).
func (cr *planCommentsRepository) GetForPlan(id client.PlanID) ([]client.Comment, error) {
	pf, err := readPlanFile(cr.dir, id)
	if err != nil {
		return nil, err
	}
	if pf == nil {
		return []client.Comment{}, nil
	}
	out := make([]client.Comment, len(pf.comments))
	copy(out, pf.comments)
	return out, nil
}
