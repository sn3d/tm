package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/sn3d/tm/internal/client"
)

const plansSchema = `
CREATE TABLE IF NOT EXISTS plans (
	id             TEXT    PRIMARY KEY,
	subject        TEXT    NOT NULL DEFAULT '',
	description    TEXT    NOT NULL DEFAULT '',
	state          TEXT    NOT NULL DEFAULT 'draft',
	assigned_agent TEXT    NOT NULL DEFAULT ''
);`

type plansRepository struct {
	db *sql.DB
}

// Save inserts a new plan or updates an existing one. When p.ID is empty the
// next sequential ID from the shared task/plan counter is assigned and
// written back into p.ID. The read+write runs in a transaction so concurrent
// callers don't collide on the next ID.
func (pr *plansRepository) Save(p *client.Plan) error {
	tx, err := pr.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for plan %q: %w", p.ID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if p.ID == "" {
		next, err := nextSharedNumericID(tx)
		if err != nil {
			return err
		}
		p.ID = next
	}
	if p.State == "" {
		p.State = client.PlanStateDefault
	}

	const upsertPlan = `
		INSERT INTO plans (id, subject, description, state, assigned_agent)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			subject        = excluded.subject,
			description    = excluded.description,
			state          = excluded.state,
			assigned_agent = excluded.assigned_agent`
	if _, err := tx.Exec(upsertPlan, p.ID, p.Subject, p.Description, string(p.State), p.AssignedAgent); err != nil {
		return fmt.Errorf("upsert plan %q: %w", p.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit plan %q: %w", p.ID, err)
	}
	return nil
}

// GetByID returns the plan with the given ID, or (nil, nil) when no such row exists.
func (pr *plansRepository) GetByID(id client.PlanID) (*client.Plan, error) {
	const q = `SELECT id, subject, description, state, assigned_agent FROM plans WHERE id = ?`
	var (
		p     client.Plan
		state string
	)
	err := pr.db.QueryRow(q, id).Scan(&p.ID, &p.Subject, &p.Description, &state, &p.AssignedAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query plan %q: %w", id, err)
	}
	parsedState, err := client.ParsePlanState(state)
	if err != nil {
		return nil, fmt.Errorf("plan %q: %w", id, err)
	}
	p.State = parsedState
	return &p, nil
}

// GetAll returns every plan in the repository, ordered by ID.
func (pr *plansRepository) GetAll() ([]client.Plan, error) {
	const q = `SELECT id, subject, description, state, assigned_agent FROM plans ORDER BY id`
	rows, err := pr.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query plans: %w", err)
	}
	defer rows.Close()

	plans := make([]client.Plan, 0)
	for rows.Next() {
		var (
			p     client.Plan
			state string
		)
		if err := rows.Scan(&p.ID, &p.Subject, &p.Description, &state, &p.AssignedAgent); err != nil {
			return nil, fmt.Errorf("scan plan row: %w", err)
		}
		parsedState, err := client.ParsePlanState(state)
		if err != nil {
			return nil, fmt.Errorf("plan %q: %w", p.ID, err)
		}
		p.State = parsedState
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan rows: %w", err)
	}
	return plans, nil
}
