package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

const planCommentsSchema = `
CREATE TABLE IF NOT EXISTS plan_comments (
	id      TEXT PRIMARY KEY,
	plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	who     TEXT NOT NULL DEFAULT '',
	comment TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_plan_comments_plan_id ON plan_comments(plan_id);`

type planCommentsRepository struct {
	db *sql.DB
}

// Add inserts a comment for the given plan. The repository assigns a fresh
// ULID and writes it into c.ID. The plan must exist; otherwise the
// foreign-key constraint surfaces an error.
func (cr *planCommentsRepository) Add(id client.PlanID, c *client.Comment) error {
	c.ID = ulid.Make().String()
	const q = `INSERT INTO plan_comments (id, plan_id, who, comment) VALUES (?, ?, ?, ?)`
	if _, err := cr.db.Exec(q, c.ID, id, c.Who, c.Comment); err != nil {
		return fmt.Errorf("insert comment for plan %q: %w", id, err)
	}
	return nil
}

// GetForPlan returns every comment attached to the given plan, ordered by
// ULID (which sorts by creation time). An empty slice is returned when the
// plan has no comments.
func (cr *planCommentsRepository) GetForPlan(id client.PlanID) ([]client.Comment, error) {
	const q = `SELECT id, who, comment FROM plan_comments WHERE plan_id = ? ORDER BY id`
	rows, err := cr.db.Query(q, id)
	if err != nil {
		return nil, fmt.Errorf("query comments for plan %q: %w", id, err)
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
