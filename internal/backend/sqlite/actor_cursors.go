package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const actorCursorsSchema = `
CREATE TABLE IF NOT EXISTS actor_cursors (
	actor        TEXT PRIMARY KEY,
	last_seen_at TEXT NOT NULL
);
`

type actorCursorsRepository struct {
	db *sql.DB
}

func (r *actorCursorsRepository) Get(actor string) (time.Time, error) {
	var raw string
	err := r.db.QueryRow(`SELECT last_seen_at FROM actor_cursors WHERE actor = ?`, actor).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("query cursor for %q: %w", actor, err)
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cursor for %q: %w", actor, err)
	}
	return ts, nil
}

func (r *actorCursorsRepository) Set(actor string, ts time.Time) error {
	const q = `INSERT INTO actor_cursors (actor, last_seen_at) VALUES (?, ?)
		ON CONFLICT(actor) DO UPDATE SET last_seen_at = excluded.last_seen_at`
	if _, err := r.db.Exec(q, actor, ts.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("upsert cursor for %q: %w", actor, err)
	}
	return nil
}
