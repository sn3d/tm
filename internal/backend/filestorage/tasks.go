package filestorage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sn3d/tm/internal/client"
)

type tasksRepository struct {
	dir string
}

// Save creates or updates the task file. When t.ID is empty the next
// sequential ID from the shared task/plan counter is assigned and written
// back into t.ID. Existing comments on the file (if any) are preserved
// across updates.
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

	tf := &taskFile{
		frontmatter: frontmatter{
			ID:            t.ID,
			State:         t.State.String(),
			AssignedAgent: t.AssignedAgent,
			DependsOn:     t.DependsOn,
			PlanID:        t.PlanID,
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

// GetAll returns every task in the repository, sorted by numeric ID. Files
// with non-numeric IDs (e.g. legacy or manually placed) sort before numeric
// ones in lexicographic order.
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
		return idLess(tasks[i].ID, tasks[j].ID)
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

// idLess orders task IDs: numeric ascending, then non-numeric lexicographic
// (non-numeric coming after — this only matters if someone manually places
// a non-numeric file in tasks/).
func idLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		return ai < bi
	case aerr == nil:
		return true
	case berr == nil:
		return false
	default:
		return a < b
	}
}

func taskFileToTask(tf *taskFile) (*client.Task, error) {
	state, err := client.ParseTaskState(tf.frontmatter.State)
	if err != nil {
		return nil, fmt.Errorf("parse state for task %q: %w", tf.frontmatter.ID, err)
	}
	return &client.Task{
		ID:            tf.frontmatter.ID,
		Subject:       tf.subject,
		Description:   tf.description,
		State:         state,
		AssignedAgent: tf.frontmatter.AssignedAgent,
		DependsOn:     tf.frontmatter.DependsOn,
		PlanID:        tf.frontmatter.PlanID,
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
