package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

// testProject is the project every single-project repository test uses. It
// matches the placeholder ticker migration 00006 derives for a database whose
// directory is named after it, so a freshly-created test database already has
// this project row.
const testProject = "tm"

// ref builds a reference in testProject from a bare id.
func ref(id string) client.TaskID {
	return client.TaskRef{Project: testProject, ID: id}
}

// projectRef builds a reference in an explicitly named project, for the
// multi-project tests.
func projectRef(project, id string) client.TaskID {
	return client.TaskRef{Project: project, ID: id}
}

// newTempDBPath returns a path under a per-test temp dir whose *directory* is
// named so that placeholderTicker derives testProject from it. That keeps the
// fixtures aligned with what a real upgrade produces.
func newTempDBPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "-Users-test-unravela-"+testProject)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return filepath.Join(dir, fmt.Sprintf("tm-%d.db", time.Now().UnixNano()))
}

// newTempBackend opens a fresh migrated database and returns the Backend with
// testProject created.
//
// A brand-new database deliberately comes up with NO projects — migration
// 00006 only invents a placeholder for a database that already holds tasks —
// so the test project is created explicitly here, which is also what
// `tm init` does on a real first run.
func newTempBackend(t *testing.T) client.Backend {
	t.Helper()
	b := newEmptyBackend(t)
	ensureProject(t, b, testProject)
	return b
}

// newEmptyBackend opens a fresh migrated database WITHOUT creating any
// project, for tests that assert on the empty-registry state.
func newEmptyBackend(t *testing.T) client.Backend {
	t.Helper()
	b, err := NewBackend(newTempDBPath(t))
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	return b
}

// newTempRepo returns the tasks repository of a fresh database.
func newTempRepo(t *testing.T) client.TasksRepository {
	t.Helper()
	return newTempBackend(t).Tasks()
}

// ensureProject inserts a project so tasks can reference it. Used by the
// multi-project tests; testProject itself already exists via the migration
// placeholder.
func ensureProject(t *testing.T, b client.Backend, ticker string) {
	t.Helper()
	p := &client.Project{Ticker: ticker, DisplayName: ticker}
	if err := b.Projects().Save(p); err != nil {
		t.Fatalf("save project %q: %v", ticker, err)
	}
}

// saveTask is a brevity helper for the common "save into testProject" call.
func saveTask(t *testing.T, repo client.TasksRepository, task *client.Task) {
	t.Helper()
	if err := repo.Save(testProject, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// timeAt builds a deterministic UTC timestamp from a unix second, for tests
// that need a stable ordering.
func timeAt(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}
