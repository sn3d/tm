package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sn3d/tm/internal/client"
)

type descriptionsRepository struct {
	db *sql.DB
}

// Get returns the task's description body, or "" when no row exists. Rows are
// created lazily, so "no row" and "empty body" are the same answer — which
// is why this returns a plain string rather than (string, bool).
func (dr *descriptionsRepository) Get(ref client.TaskID) (string, error) {
	if ref.Project == "" || ref.ID == "" {
		return "", nil
	}
	var body string
	err := dr.db.QueryRow(
		`SELECT body FROM task_descriptions WHERE project_id = ? AND task_id = ?`,
		ref.Project, ref.ID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query description for task %s: %w", ref, err)
	}
	return body, nil
}

// Set writes (or clears) the description. Writing "" deletes the row rather
// than storing an empty body, keeping the lazy-row invariant intact so a
// task with no description never costs a row.
func (dr *descriptionsRepository) Set(ref client.TaskID, body string) error {
	if ref.Project == "" || ref.ID == "" {
		return fmt.Errorf("set description: incomplete task reference %q", ref)
	}
	tx, err := dr.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for description %s: %w", ref, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveDescriptionTx(tx, ref, body, time.Now().UTC()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit description %s: %w", ref, err)
	}
	return nil
}

// saveDescriptionTx is the shared write path, used both by this repository
// and by tasksRepository.Save so a task and its body are always written in
// one transaction.
func saveDescriptionTx(tx *sql.Tx, ref client.TaskID, body string, updatedAt time.Time) error {
	if body == "" {
		if _, err := tx.Exec(
			`DELETE FROM task_descriptions WHERE project_id = ? AND task_id = ?`,
			ref.Project, ref.ID); err != nil {
			return fmt.Errorf("clear description for task %s: %w", ref, err)
		}
		return nil
	}
	const upsert = `
		INSERT INTO task_descriptions (project_id, task_id, body, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id, task_id) DO UPDATE SET
			body       = excluded.body,
			updated_at = excluded.updated_at`
	if _, err := tx.Exec(upsert, ref.Project, ref.ID, body,
		updatedAt.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("upsert description for task %s: %w", ref, err)
	}
	return nil
}
