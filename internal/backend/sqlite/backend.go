package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/scope"
	_ "modernc.org/sqlite"
)

// dbFilename is the default sqlite database file written inside the home
// project directory (~/.tm/projects/<sanitized-key>/tm.db).
const dbFilename = "tm.db"

type backend struct {
	tasks        *tasksRepository
	comments     *commentsRepository
	plans        *plansRepository
	planComments *planCommentsRepository
	events       *eventsRepository
	cursors      *actorCursorsRepository
}

var allSchemas = []string{
	tasksSchema,
	commentsSchema,
	plansSchema,
	planCommentsSchema,
	eventsSchema,
	actorCursorsSchema,
}

// NewBackend opens (or creates) a SQLite database at dbPath, ensures all
// schemas exist, and returns a Backend backed by it. Use ":memory:" for an
// ephemeral in-memory store.
func NewBackend(dbPath string) (client.Backend, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	// Migrations run before schema creation: the tasks schema includes an
	// index on plan_id, which would fail to create on a legacy table that
	// predates the column.
	if err := migrateTasksAddPlanID(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate tasks.plan_id: %w", err)
	}
	for _, schema := range allSchemas {
		if _, err := db.Exec(schema); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create schema: %w", err)
		}
	}
	return &backend{
		tasks:        &tasksRepository{db: db},
		comments:     &commentsRepository{db: db},
		plans:        &plansRepository{db: db},
		planComments: &planCommentsRepository{db: db},
		events:       &eventsRepository{db: db},
		cursors:      &actorCursorsRepository{db: db},
	}, nil
}

// migrateTasksAddPlanID adds the plan_id column to pre-existing tasks tables
// created before the Plan entity existed. SQLite has no `ADD COLUMN IF NOT
// EXISTS`, so we run the ALTER unconditionally and swallow:
//   - "duplicate column" when an already-migrated DB is reopened
//   - "no such table" on fresh DBs where the schema loop will create the
//     up-to-date tasks table moments later
func migrateTasksAddPlanID(db *sql.DB) error {
	_, err := db.Exec(`ALTER TABLE tasks ADD COLUMN plan_id TEXT NOT NULL DEFAULT ''`)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "no such table") {
		return nil
	}
	return err
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

func (b *backend) Plans() client.PlansRepository {
	return b.plans
}

func (b *backend) PlanComments() client.PlanCommentsRepository {
	return b.planComments
}

func (b *backend) Events() client.EventsRepository {
	return b.events
}

func (b *backend) ActorCursors() client.ActorCursorRepository {
	return b.cursors
}
