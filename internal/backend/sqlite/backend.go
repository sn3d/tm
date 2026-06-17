package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/scope"
	_ "modernc.org/sqlite"
)

// dbFilename is the default sqlite database file written inside the home
// project directory (~/.tm/projects/<sanitized-key>/tm.db).
const dbFilename = "tm.db"

type backend struct {
	tasks    *tasksRepository
	comments *commentsRepository
	events   *eventsRepository
	cursors  *actorCursorsRepository
}

// NewBackend opens (or creates) a SQLite database at dbPath, runs any
// pending migrations, and returns a Backend backed by it. Use ":memory:"
// for an ephemeral in-memory store.
func NewBackend(dbPath string) (client.Backend, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &backend{
		tasks:    &tasksRepository{db: db},
		comments: &commentsRepository{db: db},
		events:   &eventsRepository{db: db},
		cursors:  &actorCursorsRepository{db: db},
	}, nil
}

// NewBackendFromOptions is the map-keyed variant used by app.NewClient.
// The sqlite database always lives under ~/.tm/projects/<sanitized-key>/tm.db.
// The key is the discovered project_root when set (so subdirectories of an
// initialized project share state), or cwd as a fallback.
//
// Options:
//
//	project_root absolute path to use as the project root key. Set by
//	             DefaultConfig after walking up from cwd. Without it, cwd
//	             is used as the key.
//	db_path      explicit override for the database file path. When set,
//	             project_root is ignored. Use ":memory:" for an ephemeral
//	             in-memory store. Useful for tests.
func NewBackendFromOptions(opts map[string]string) (client.Backend, error) {
	if dbPath := opts["db_path"]; dbPath != "" {
		return NewBackend(dbPath)
	}
	dir, err := scope.HomeProjectDir(opts["project_root"])
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	return NewBackend(filepath.Join(dir, dbFilename))
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
