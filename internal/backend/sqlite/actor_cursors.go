package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type actorCursorsRepository struct {
	db *sql.DB
}

// Get returns the actor's last-seen timestamp in one project. An unseen
// (actor, project) pair yields the zero time so every event reads as new.
func (r *actorCursorsRepository) Get(actor, project string) (time.Time, error) {
	var raw string
	err := r.db.QueryRow(
		`SELECT last_seen_at FROM actor_cursors WHERE actor = ? AND project_id = ?`,
		actor, project).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("query cursor for %q in %q: %w", actor, project, err)
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cursor for %q in %q: %w", actor, project, err)
	}
	return ts, nil
}

// Set overwrites the actor's last-seen timestamp for one project. Cursors are
// per (actor, project) so glancing at one project never marks another
// project's work as seen.
func (r *actorCursorsRepository) Set(actor, project string, ts time.Time) error {
	const q = `INSERT INTO actor_cursors (actor, project_id, last_seen_at) VALUES (?, ?, ?)
		ON CONFLICT(actor, project_id) DO UPDATE SET last_seen_at = excluded.last_seen_at`
	if _, err := r.db.Exec(q, actor, project, ts.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("upsert cursor for %q in %q: %w", actor, project, err)
	}
	return nil
}
