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

type tasksRepository struct {
	dir string
}

// Save creates or updates the task file. When t.ID is empty the next
// sequential ID from the shared task/plan counter is assigned and written
// back into t.ID. CreatedAt is stamped on first save and preserved on
// updates; UpdatedAt is refreshed on every save. Both fields are written
// back into t. Existing comments on the file (if any) are preserved across
// updates.
func (tr *tasksRepository) Save(t *client.Task) error {
	if t.ID == "" {
		next, err := nextSharedNumericID(tr.dir)
		if err != nil {
			return err
		}
		t.ID = next
	}
	if t.State == "" {
		t.State = client.TaskStateDefault
	}

	existing, err := readTaskFile(tr.dir, t.ID)
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
			t.CreatedAt = prev
		}
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	tf := &taskFile{
		frontmatter: frontmatter{
			ID:            t.ID,
			State:         t.State.String(),
			AssignedAgent: t.AssignedAgent,
			DependsOn:     t.DependsOn,
			PlanID:        t.PlanID,
			CreatedAt:     t.CreatedAt.Format(time.RFC3339Nano),
			UpdatedAt:     t.UpdatedAt.Format(time.RFC3339Nano),
		},
		subject:     t.Subject,
		description: t.Description,
	}
	if existing != nil {
		tf.comments = existing.comments
	}
	return writeTaskFile(tr.dir, tf)
}

func (tr *tasksRepository) GetByID(id client.TaskID) (*client.Task, error) {
	tf, err := readTaskFile(tr.dir, id)
	if err != nil {
		return nil, err
	}
	if tf == nil {
		return nil, nil
	}
	return taskFileToTask(tf)
}

// GetAll returns every task in the repository, ordered by UpdatedAt
// descending (most recently changed first). ID breaks ties.
func (tr *tasksRepository) GetAll() ([]client.Task, error) {
	entries, err := os.ReadDir(filepath.Join(tr.dir, "tasks"))
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	tasks := make([]client.Task, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := idFromFilename(e.Name())
		tf, err := readTaskFile(tr.dir, id)
		if err != nil {
			return nil, err
		}
		if tf == nil {
			continue
		}
		t, err := taskFileToTask(tf)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if !tasks[i].UpdatedAt.Equal(tasks[j].UpdatedAt) {
			return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}

// idFromFilename strips the .md suffix and any --slug tail, returning just
// the leading ID portion. "1--foo-bar.md" → "1", "TASK-123--foo.md" →
// "TASK-123", "42.md" → "42".
func idFromFilename(name string) string {
	stem := strings.TrimSuffix(name, ".md")
	if i := strings.Index(stem, idSlugSep); i >= 0 {
		return stem[:i]
	}
	return stem
}

// parseTime parses an RFC3339Nano timestamp from frontmatter. Returns a
// zero time when s is empty so legacy files (predating timestamps) read as
// zero-valued.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func taskFileToTask(tf *taskFile) (*client.Task, error) {
	state, err := client.ParseTaskState(tf.frontmatter.State)
	if err != nil {
		return nil, fmt.Errorf("parse state for task %q: %w", tf.frontmatter.ID, err)
	}
	createdAt, err := parseTime(tf.frontmatter.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at for task %q: %w", tf.frontmatter.ID, err)
	}
	updatedAt, err := parseTime(tf.frontmatter.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at for task %q: %w", tf.frontmatter.ID, err)
	}
	return &client.Task{
		ID:            tf.frontmatter.ID,
		Subject:       tf.subject,
		Description:   tf.description,
		State:         state,
		AssignedAgent: tf.frontmatter.AssignedAgent,
		DependsOn:     tf.frontmatter.DependsOn,
		PlanID:        tf.frontmatter.PlanID,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}

// GetByPlan returns every task whose PlanID matches the given plan. Walks
// every file under tasks/ since the file layout has no plan-keyed index.
func (tr *tasksRepository) GetByPlan(planID client.PlanID) ([]client.Task, error) {
	all, err := tr.GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]client.Task, 0)
	for _, t := range all {
		if t.PlanID == planID {
			out = append(out, t)
		}
	}
	return out, nil
}
