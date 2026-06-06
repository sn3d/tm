package filestorage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sn3d/tm/internal/client"
)

type plansRepository struct {
	dir string
}

// Save creates or updates the plan file. When p.ID is empty the next
// sequential ID from the shared task/plan counter is assigned and written
// back into p.ID. CreatedAt is stamped on first save and preserved on
// updates; UpdatedAt is refreshed on every save. Both fields are written
// back into p. Existing comments on the file (if any) are preserved across
// updates.
func (pr *plansRepository) Save(p *client.Plan) error {
	if p.ID == "" {
		next, err := nextSharedNumericID(pr.dir)
		if err != nil {
			return err
		}
		p.ID = next
	}
	if p.State == "" {
		p.State = client.PlanStateDefault
	}

	existing, err := readPlanFile(pr.dir, p.ID)
	if err != nil {
		return err
	}

	// CreatedAt resolution, in priority order:
	//   1. existing stored value (preserve across updates)
	//   2. caller-supplied non-zero value (import path)
	//   3. now (fresh insert with no caller hint)
	now := time.Now().UTC()
	if existing != nil {
		if prev, perr := parseTime(existing.frontmatter.CreatedAt); perr == nil && !prev.IsZero() {
			p.CreatedAt = prev
		}
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	pf := &planFile{
		frontmatter: planFrontmatter{
			ID:            p.ID,
			State:         p.State.String(),
			AssignedAgent: p.AssignedAgent,
			CreatedAt:     p.CreatedAt.Format(time.RFC3339Nano),
			UpdatedAt:     p.UpdatedAt.Format(time.RFC3339Nano),
		},
		subject:     p.Subject,
		description: p.Description,
	}
	if existing != nil {
		pf.comments = existing.comments
	}
	return writePlanFile(pr.dir, pf)
}

func (pr *plansRepository) GetByID(id client.PlanID) (*client.Plan, error) {
	pf, err := readPlanFile(pr.dir, id)
	if err != nil {
		return nil, err
	}
	if pf == nil {
		return nil, nil
	}
	return planFileToPlan(pf)
}

// GetAll returns every plan in the repository, ordered by UpdatedAt
// descending (most recently changed first). ID breaks ties.
func (pr *plansRepository) GetAll() ([]client.Plan, error) {
	entries, err := os.ReadDir(filepath.Join(pr.dir, "plans"))
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	plans := make([]client.Plan, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := idFromFilename(e.Name())
		pf, err := readPlanFile(pr.dir, id)
		if err != nil {
			return nil, err
		}
		if pf == nil {
			continue
		}
		p, err := planFileToPlan(pf)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *p)
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if !plans[i].UpdatedAt.Equal(plans[j].UpdatedAt) {
			return plans[i].UpdatedAt.After(plans[j].UpdatedAt)
		}
		return plans[i].ID < plans[j].ID
	})
	return plans, nil
}

func planFileToPlan(pf *planFile) (*client.Plan, error) {
	state, err := client.ParsePlanState(pf.frontmatter.State)
	if err != nil {
		return nil, fmt.Errorf("parse state for plan %q: %w", pf.frontmatter.ID, err)
	}
	createdAt, err := parseTime(pf.frontmatter.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at for plan %q: %w", pf.frontmatter.ID, err)
	}
	updatedAt, err := parseTime(pf.frontmatter.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at for plan %q: %w", pf.frontmatter.ID, err)
	}
	return &client.Plan{
		ID:            pf.frontmatter.ID,
		Subject:       pf.subject,
		Description:   pf.description,
		State:         state,
		AssignedAgent: pf.frontmatter.AssignedAgent,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}
