package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/sn3d/tm/internal/client"
	_ "modernc.org/sqlite"
)

type backend struct {
	db           *sql.DB
	projects     *projectsRepository
	tasks        *tasksRepository
	descriptions *descriptionsRepository
	comments     *commentsRepository
	events       *eventsRepository
	cursors      *actorCursorsRepository
}

// NewBackend opens (or creates) a SQLite database at dbPath, runs any
// pending migrations, and returns a Backend backed by it. Use ":memory:"
// for an ephemeral in-memory store.
//
// One Backend is one *instance*: it holds every project in that database,
// which is what lets the cross-project reads work on a single connection.
func NewBackend(dbPath string) (client.Backend, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := runMigrations(db, dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &backend{
		db:           db,
		projects:     &projectsRepository{db: db},
		tasks:        &tasksRepository{db: db},
		descriptions: &descriptionsRepository{db: db},
		comments:     &commentsRepository{db: db},
		events:       &eventsRepository{db: db},
		cursors:      &actorCursorsRepository{db: db},
	}, nil
}

// NewBackendFromOptions is the map-keyed variant used by app.NewClient.
//
// Options:
//
//	db_path  path to the instance's database file, as resolved from
//	         ~/.tm/config.yaml. ":memory:" gives an ephemeral store, which
//	         is what tests use.
func NewBackendFromOptions(opts map[string]string) (client.Backend, error) {
	dbPath := opts["db_path"]
	if dbPath == "" {
		return nil, fmt.Errorf("sqlite: no db_path configured; run 'tm init' to create an instance")
	}
	return NewBackend(dbPath)
}

// Close releases the underlying connection. Commands are short-lived so this
// is mostly for tests and the migrate command, which opens many databases.
func (b *backend) Close() error {
	return b.db.Close()
}

func (b *backend) Projects() client.ProjectsRepository {
	return b.projects
}

func (b *backend) Tasks() client.TasksRepository {
	return b.tasks
}

func (b *backend) Descriptions() client.DescriptionsRepository {
	return b.descriptions
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
