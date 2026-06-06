package filestorage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/scope"
)

type backend struct {
	dir      string
	tasks    *tasksRepository
	comments *commentsRepository
	events   *eventsRepository
	cursors  *actorCursorsRepository
}

// NewBackend returns a Backend that stores each task as a single markdown
// file (with YAML frontmatter and inline comment blocks) under dir/tasks/.
// On open, any legacy dir/plans/ directory is collapsed into tasks/ as
// planning-mode entries by collapsePlansIntoTasks.
func NewBackend(dir string) (client.Backend, error) {
	tasksDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create tasks dir: %w", err)
	}
	if err := collapsePlansIntoTasks(dir); err != nil {
		return nil, fmt.Errorf("collapse plans into tasks: %w", err)
	}
	b := &backend{dir: dir}
	b.tasks = &tasksRepository{dir: dir}
	b.comments = &commentsRepository{dir: dir}
	b.events = &eventsRepository{dir: dir}
	b.cursors = &actorCursorsRepository{dir: dir}
	return b, nil
}

// NewBackendFromOptions is the map-keyed variant used by app.NewClient.
// Filestorage always stores under <project_root>/.tm/. The project_root is
// either provided explicitly (set by DefaultConfig after walking up from cwd)
// or discovered by walking up from cwd looking for a taskmanager.yaml or
// .tm/ marker; missing marker errors with a hint to run "tm init".
func NewBackendFromOptions(opts map[string]string) (client.Backend, error) {
	dir, err := scope.CwdProjectDir("filestorage", opts["project_root"])
	if err != nil {
		return nil, err
	}
	return NewBackend(dir)
}

// sanitizePath is kept as a thin shim so existing tests in this package can
// still reach it; the implementation lives in internal/scope.
func sanitizePath(p string) string {
	return scope.SanitizePath(p)
}

func (b *backend) Tasks() client.TasksRepository {
	return b.tasks
}

func (b *backend) Comments() client.CommentsRepository {
	return b.comments
}

func (b *backend) Events() client.EventsRepository {
	return b.events
}

func (b *backend) ActorCursors() client.ActorCursorRepository {
	return b.cursors
}
