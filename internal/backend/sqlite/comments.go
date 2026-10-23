package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

type commentsRepository struct {
	db *sql.DB
}

// Add inserts a comment for the task the ref addresses. The repository
// assigns a fresh ULID and stamps CreatedAt when the caller left it zero,
// writing both back into c. The task must exist; otherwise the foreign-key
// constraint surfaces an error.
func (cr *commentsRepository) Add(ref client.TaskID, c *client.Comment) error {
	if ref.Project == "" || ref.ID == "" {
		return fmt.Errorf("add comment: incomplete task reference %q", ref)
	}
	if c.ID == "" {
		c.ID = ulid.Make().String()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	const q = `INSERT INTO task_comments (id, project_id, task_id, who, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := cr.db.Exec(q, c.ID, ref.Project, ref.ID, c.Who, c.Comment,
		c.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("insert comment for task %s: %w", ref, err)
	}
	return nil
}

// GetForTask returns every comment attached to the given task, oldest first.
// Ordering is by the explicit created_at with the ULID breaking ties: rows
// merged from several databases (see `tm migrate`) have no meaningful
// cross-source ULID order, which is why the column exists.
func (cr *commentsRepository) GetForTask(ref client.TaskID) ([]client.Comment, error) {
	if ref.Project == "" || ref.ID == "" {
		return []client.Comment{}, nil
	}
	const q = `SELECT id, who, body, created_at FROM task_comments
		WHERE project_id = ? AND task_id = ? ORDER BY created_at, id`
	rows, err := cr.db.Query(q, ref.Project, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("query comments for task %s: %w", ref, err)
	}
	defer rows.Close()

	comments := make([]client.Comment, 0)
	for rows.Next() {
		var (
			c          client.Comment
			createdStr string
		)
		if err := rows.Scan(&c.ID, &c.Who, &c.Comment, &createdStr); err != nil {
			return nil, fmt.Errorf("scan comment row: %w", err)
		}
		if c.CreatedAt, err = parseSQLTime(createdStr); err != nil {
			return nil, fmt.Errorf("parse created_at for comment %q: %w", c.ID, err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment rows: %w", err)
	}
	return comments, nil
}
