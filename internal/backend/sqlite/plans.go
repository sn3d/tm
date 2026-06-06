package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sn3d/tm/internal/client"
)

type plansRepository struct {
	db *sql.DB
}

// Save inserts a new plan or updates an existing one. When p.ID is empty the
// next sequential ID from the shared task/plan counter is assigned and
// written back into p.ID. CreatedAt is stamped on insert and preserved on
// update; UpdatedAt is refreshed on every save. Both timestamps are written
// back into p. The read+write runs in a transaction so concurrent callers
// don't collide on the next ID.
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

	// CreatedAt resolution, in priority order:
	//   1. existing stored value (preserve across updates)
	//   2. caller-supplied non-zero value (import path, or legacy row
	//      whose stored column is '' getting a real timestamp)
	//   3. now (fresh insert with no caller hint)
	now := time.Now().UTC()
	existing, err := loadPlanCreatedAt(tx, p.ID)
	if err != nil {
		return err
	}
	if existing != nil && !existing.IsZero() {
		p.CreatedAt = *existing
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	const upsertPlan = `
		INSERT INTO plans (id, subject, description, state, assigned_agent, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			subject        = excluded.subject,
			description    = excluded.description,
			state          = excluded.state,
			assigned_agent = excluded.assigned_agent,
			updated_at     = excluded.updated_at`
	if _, err := tx.Exec(upsertPlan, p.ID, p.Subject, p.Description, string(p.State), p.AssignedAgent,
		p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("upsert plan %q: %w", p.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit plan %q: %w", p.ID, err)
	}
	return nil
}

// loadPlanCreatedAt returns the stored created_at for a plan ID within the
// current transaction. Returns (nil, nil) when no row exists.
func loadPlanCreatedAt(tx *sql.Tx, id client.PlanID) (*time.Time, error) {
	var createdStr string
	err := tx.QueryRow(`SELECT created_at FROM plans WHERE id = ?`, id).Scan(&createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load created_at for plan %q: %w", id, err)
	}
	if createdStr == "" {
		zero := time.Time{}
		return &zero, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at for plan %q: %w", id, err)
	}
	return &ts, nil
}

// GetByID returns the plan with the given ID, or (nil, nil) when no such row exists.
func (pr *plansRepository) GetByID(id client.PlanID) (*client.Plan, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, created_at, updated_at FROM plans WHERE id = ?`
	var (
		p          client.Plan
		state      string
		createdStr string
		updatedStr string
	)
	err := pr.db.QueryRow(q, id).Scan(&p.ID, &p.Subject, &p.Description, &state, &p.AssignedAgent, &createdStr, &updatedStr)
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
	if p.CreatedAt, err = parseSQLTime(createdStr); err != nil {
		return nil, fmt.Errorf("parse created_at for plan %q: %w", id, err)
	}
	if p.UpdatedAt, err = parseSQLTime(updatedStr); err != nil {
		return nil, fmt.Errorf("parse updated_at for plan %q: %w", id, err)
	}
	return &p, nil
}

// GetAll returns every plan in the repository, ordered by UpdatedAt
// descending (most recently changed first). ID breaks ties.
func (pr *plansRepository) GetAll() ([]client.Plan, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, created_at, updated_at
		FROM plans ORDER BY updated_at DESC, id`
	rows, err := pr.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query plans: %w", err)
	}
	defer rows.Close()

	plans := make([]client.Plan, 0)
	for rows.Next() {
		var (
			p          client.Plan
			state      string
			createdStr string
			updatedStr string
		)
		if err := rows.Scan(&p.ID, &p.Subject, &p.Description, &state, &p.AssignedAgent, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan plan row: %w", err)
		}
		parsedState, err := client.ParsePlanState(state)
		if err != nil {
			return nil, fmt.Errorf("plan %q: %w", p.ID, err)
		}
		p.State = parsedState
		if p.CreatedAt, err = parseSQLTime(createdStr); err != nil {
			return nil, fmt.Errorf("parse created_at for plan %q: %w", p.ID, err)
		}
		if p.UpdatedAt, err = parseSQLTime(updatedStr); err != nil {
			return nil, fmt.Errorf("parse updated_at for plan %q: %w", p.ID, err)
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan rows: %w", err)
	}
	return plans, nil
}
