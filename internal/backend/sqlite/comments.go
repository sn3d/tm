package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

const commentsSchema = `
CREATE TABLE IF NOT EXISTS comments (
	id      TEXT PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	who     TEXT NOT NULL DEFAULT '',
	comment TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_comments_task_id ON comments(task_id);`

type commentsRepository struct {
	db *sql.DB
}

// Add inserts a comment for the given task. The repository assigns a fresh
// ULID and writes it into c.ID. The task must exist; otherwise the
// foreign-key constraint surfaces an error.
func (cr *commentsRepository) Add(id client.TaskID, c *client.Comment) error {
	c.ID = ulid.Make().String()
	const q = `INSERT INTO comments (id, task_id, who, comment) VALUES (?, ?, ?, ?)`
	if _, err := cr.db.Exec(q, c.ID, id, c.Who, c.Comment); err != nil {
		return fmt.Errorf("insert comment for task %q: %w", id, err)
	}
	return nil
}

// GetForTask returns every comment attached to the given task, ordered by
// ULID (which sorts by creation time). An empty slice is returned when the
// task has no comments.
func (cr *commentsRepository) GetForTask(id client.TaskID) ([]client.Comment, error) {
	const q = `SELECT id, who, comment FROM comments WHERE task_id = ? ORDER BY id`
	rows, err := cr.db.Query(q, id)
	if err != nil {
		return nil, fmt.Errorf("query comments for task %q: %w", id, err)
	}
	defer rows.Close()

	comments := make([]client.Comment, 0)
	for rows.Next() {
		var c client.Comment
		if err := rows.Scan(&c.ID, &c.Who, &c.Comment); err != nil {
			return nil, fmt.Errorf("scan comment row: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment rows: %w", err)
	}
	return comments, nil
}
