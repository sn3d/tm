package filestorage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sn3d/tm/internal/client"
	"gopkg.in/yaml.v3"
)

// legacyPlanFile is the in-memory shape of a pre-collapse plan markdown file.
// Defined here (rather than via the old plansRepository, which is gone)
// because migrate.go is the only remaining reader of that on-disk format.
type legacyPlanFile struct {
	frontmatter legacyPlanFrontmatter
	subject     string
	description string
	comments    []client.Comment
}

type legacyPlanFrontmatter struct {
	ID            string `yaml:"id"`
	State         string `yaml:"state"`
	AssignedAgent string `yaml:"assigned_agent"`
	CreatedAt     string `yaml:"created_at,omitempty"`
	UpdatedAt     string `yaml:"updated_at,omitempty"`
}

// collapsePlansIntoTasks is the filestorage analogue of the sqlite
// 00004_collapse_plans_into_tasks migration. For each file under dir/plans/
// it writes an equivalent task file under dir/tasks/ with mode=planning,
// state remapped, and comments copied. Existing task files whose frontmatter
// has a non-empty plan_id get their parent_id field set to that plan_id
// (so children reparent under their planning-mode root). The plans/
// directory is removed once every plan file has been absorbed.
//
// The migration is idempotent: it skips plans whose ID already exists as a
// task (the previous run absorbed it), so a half-completed migration on a
// crashed backend resumes cleanly on next open.
func collapsePlansIntoTasks(dir string) error {
	plansDir := filepath.Join(dir, "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list plans dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := idFromFilename(e.Name())
		if err := absorbPlan(dir, id); err != nil {
			return err
		}
	}

	if err := reparentTasks(dir); err != nil {
		return err
	}

	// All plan files have been absorbed. Remove the directory so subsequent
	// opens skip the migration entirely.
	if err := os.RemoveAll(plansDir); err != nil {
		return fmt.Errorf("remove plans dir after collapse: %w", err)
	}
	return nil
}

// absorbPlan reads one plan file and writes it as a planning-mode task with
// the same ID, copying comments and remapping the state. If a task file with
// the same ID already exists (e.g. a previous run absorbed this plan but
// crashed before removing plans/), the absorb is skipped silently.
func absorbPlan(dir, id string) error {
	taskPath, err := findTaskPath(dir, id)
	if err != nil {
		return err
	}
	if taskPath != "" {
		// Already absorbed.
		return nil
	}

	pf, err := readLegacyPlanFile(dir, id)
	if err != nil {
		return err
	}
	if pf == nil {
		return nil
	}

	state := planStateToTaskState(pf.frontmatter.State)
	tf := &taskFile{
		frontmatter: frontmatter{
			ID:            pf.frontmatter.ID,
			State:         state,
			AssignedAgent: pf.frontmatter.AssignedAgent,
			Mode:          string(client.TaskModePlanning),
			CreatedAt:     pf.frontmatter.CreatedAt,
			UpdatedAt:     pf.frontmatter.UpdatedAt,
		},
		subject:     pf.subject,
		description: pf.description,
		comments:    pf.comments,
	}
	if err := writeTaskFile(dir, tf); err != nil {
		return fmt.Errorf("write absorbed plan %q: %w", id, err)
	}
	return nil
}

// readLegacyPlanFile locates and parses a pre-collapse plan file by ID. It
// reuses the shared frontmatter regex and comments parsing in file.go since
// the markdown layout is identical between plans and tasks; only the
// frontmatter schema differs.
func readLegacyPlanFile(dir, id string) (*legacyPlanFile, error) {
	plansDir := filepath.Join(dir, "plans")
	matches, err := filepath.Glob(filepath.Join(plansDir, id+idSlugSep+"*.md"))
	if err != nil {
		return nil, fmt.Errorf("glob plan %q: %w", id, err)
	}
	path := ""
	if len(matches) > 0 {
		path = matches[0]
	} else {
		bare := filepath.Join(plansDir, id+".md")
		if _, err := os.Stat(bare); err == nil {
			path = bare
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat plan %q: %w", id, err)
		}
	}
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %q: %w", id, err)
	}
	m := frontmatterRE.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("plan %q: missing frontmatter", id)
	}
	var fm legacyPlanFrontmatter
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return nil, fmt.Errorf("plan %q: unmarshal frontmatter: %w", id, err)
	}
	subject, description, commentsSection := splitBody(string(m[2]))
	comments, err := parseComments(commentsSection)
	if err != nil {
		return nil, fmt.Errorf("plan %q: %w", id, err)
	}
	return &legacyPlanFile{
		frontmatter: fm,
		subject:     subject,
		description: description,
		comments:    comments,
	}, nil
}

// reparentTasks walks every task file and, if its frontmatter has a non-empty
// plan_id but an empty parent_id, copies plan_id into parent_id and clears
// plan_id. The reparent runs after absorbPlan so the planning-mode root task
// the children point at exists by the time we touch them.
func reparentTasks(dir string) error {
	tasksDir := filepath.Join(dir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list tasks dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := idFromFilename(e.Name())
		tf, err := readTaskFile(dir, id)
		if err != nil {
			return err
		}
		if tf == nil {
			continue
		}
		if tf.frontmatter.PlanID == "" || tf.frontmatter.ParentID != "" {
			continue
		}
		tf.frontmatter.ParentID = tf.frontmatter.PlanID
		tf.frontmatter.PlanID = ""
		// Refresh updated_at to mark the migration touch so resumable
		// inboxes can surface "your task moved under a planning root."
		tf.frontmatter.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := writeTaskFile(dir, tf); err != nil {
			return fmt.Errorf("reparent task %q: %w", id, err)
		}
	}
	return nil
}

// planStateToTaskState mirrors the SQL CASE in the 00004 migration.
func planStateToTaskState(s string) string {
	switch s {
	case "draft":
		return string(client.TaskStateDraft)
	case "active":
		return string(client.TaskStateInProgress)
	case "on_hold":
		return string(client.TaskStateBlocked)
	case "completed":
		return string(client.TaskStateDone)
	case "cancelled":
		return string(client.TaskStateCancelled)
	default:
		return string(client.TaskStateDraft)
	}
}
