package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sn3d/tm/internal/client"
)

type tasksRepository struct {
	db *sql.DB
}

// Save inserts a new task or updates an existing one. When t.ID is empty the
// next sequential ID from the shared task/plan counter is assigned and
// written back into t.ID. When t.ID is set the existing row is replaced.
// CreatedAt is stamped on insert and preserved on update; UpdatedAt is
// refreshed on every save. Both timestamps are written back into t. The
// dependency list is fully replaced (delete-then-insert) inside the same
// transaction.
func (tr *tasksRepository) Save(t *client.Task) error {
	tx, err := tr.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for task %q: %w", t.ID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if t.ID == "" {
		next, err := nextSharedNumericID(tx)
		if err != nil {
			return err
		}
		t.ID = next
	}
	if t.State == "" {
		t.State = client.TaskStateDefault
	}

	// CreatedAt resolution, in priority order:
	//   1. existing stored value (preserve across updates)
	//   2. caller-supplied non-zero value (import path, or legacy row
	//      whose stored column is '' getting a real timestamp)
	//   3. now (fresh insert with no caller hint)
	now := time.Now().UTC()
	existing, err := loadTaskCreatedAt(tx, t.ID)
	if err != nil {
		return err
	}
	if existing != nil && !existing.IsZero() {
		t.CreatedAt = *existing
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	const upsertTask = `
		INSERT INTO tasks (id, subject, description, state, assigned_agent, plan_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			subject        = excluded.subject,
			description    = excluded.description,
			state          = excluded.state,
			assigned_agent = excluded.assigned_agent,
			plan_id        = excluded.plan_id,
			updated_at     = excluded.updated_at`
	if _, err := tx.Exec(upsertTask, t.ID, t.Subject, t.Description, string(t.State), t.AssignedAgent, t.PlanID,
		t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("upsert task %q: %w", t.ID, err)
	}

	if _, err := tx.Exec(`DELETE FROM task_deps WHERE task_id = ?`, t.ID); err != nil {
		return fmt.Errorf("clear deps for task %q: %w", t.ID, err)
	}
	if len(t.DependsOn) > 0 {
		const insertDep = `INSERT INTO task_deps (task_id, depends_on_id) VALUES (?, ?)`
		for _, dep := range t.DependsOn {
			if _, err := tx.Exec(insertDep, t.ID, dep); err != nil {
				return fmt.Errorf("insert dep %q for task %q: %w", dep, t.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task %q: %w", t.ID, err)
	}
	return nil
}

// loadTaskCreatedAt returns the existing created_at for a task ID within
// the current transaction. Returns (nil, nil) when the row doesn't exist,
// (&zero, nil) when the row exists with no stored timestamp (legacy rows
// from before this column existed).
func loadTaskCreatedAt(tx *sql.Tx, id client.TaskID) (*time.Time, error) {
	var createdStr string
	err := tx.QueryRow(`SELECT created_at FROM tasks WHERE id = ?`, id).Scan(&createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load created_at for task %q: %w", id, err)
	}
	if createdStr == "" {
		zero := time.Time{}
		return &zero, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at for task %q: %w", id, err)
	}
	return &ts, nil
}

// GetByID returns the task with the given ID, or (nil, nil) when no such row exists.
func (tr *tasksRepository) GetByID(id client.TaskID) (*client.Task, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, plan_id, created_at, updated_at FROM tasks WHERE id = ?`
	var (
		t          client.Task
		state      string
		createdStr string
		updatedStr string
	)
	err := tr.db.QueryRow(q, id).Scan(&t.ID, &t.Subject, &t.Description, &state, &t.AssignedAgent, &t.PlanID, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query task %q: %w", id, err)
	}
	parsedState, err := client.ParseTaskState(state)
	if err != nil {
		return nil, fmt.Errorf("task %q: %w", id, err)
	}
	t.State = parsedState
	if t.CreatedAt, err = parseSQLTime(createdStr); err != nil {
		return nil, fmt.Errorf("parse created_at for task %q: %w", id, err)
	}
	if t.UpdatedAt, err = parseSQLTime(updatedStr); err != nil {
		return nil, fmt.Errorf("parse updated_at for task %q: %w", id, err)
	}
	deps, err := tr.loadDeps(t.ID)
	if err != nil {
		return nil, err
	}
	t.DependsOn = deps
	return &t, nil
}

// GetAll returns every task in the repository, ordered by UpdatedAt
// descending (most recently changed first). ID breaks ties.
func (tr *tasksRepository) GetAll() ([]client.Task, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, plan_id, created_at, updated_at
		FROM tasks ORDER BY updated_at DESC, id`
	return tr.queryTasks(q)
}

// GetByPlan returns every task whose PlanID matches the given plan, ordered
// by UpdatedAt descending. Returns an empty slice when no tasks reference
// the plan.
func (tr *tasksRepository) GetByPlan(planID client.PlanID) ([]client.Task, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, plan_id, created_at, updated_at
		FROM tasks WHERE plan_id = ? ORDER BY updated_at DESC, id`
	return tr.queryTasks(q, planID)
}

func (tr *tasksRepository) queryTasks(q string, args ...any) ([]client.Task, error) {
	rows, err := tr.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]client.Task, 0)
	for rows.Next() {
		var (
			t          client.Task
			state      string
			createdStr string
			updatedStr string
		)
		if err := rows.Scan(&t.ID, &t.Subject, &t.Description, &state, &t.AssignedAgent, &t.PlanID, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		parsedState, err := client.ParseTaskState(state)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", t.ID, err)
		}
		t.State = parsedState
		if t.CreatedAt, err = parseSQLTime(createdStr); err != nil {
			return nil, fmt.Errorf("parse created_at for task %q: %w", t.ID, err)
		}
		if t.UpdatedAt, err = parseSQLTime(updatedStr); err != nil {
			return nil, fmt.Errorf("parse updated_at for task %q: %w", t.ID, err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task rows: %w", err)
	}
	for i := range tasks {
		deps, err := tr.loadDeps(tasks[i].ID)
		if err != nil {
			return nil, err
		}
		tasks[i].DependsOn = deps
	}
	return tasks, nil
}

// parseSQLTime parses an RFC3339Nano timestamp from a TEXT column. An empty
// string (from legacy rows predating the column) becomes zero time.
func parseSQLTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func (tr *tasksRepository) loadDeps(id client.TaskID) ([]client.TaskID, error) {
	const q = `SELECT depends_on_id FROM task_deps WHERE task_id = ? ORDER BY depends_on_id`
	rows, err := tr.db.Query(q, id)
	if err != nil {
		return nil, fmt.Errorf("query deps for task %q: %w", id, err)
	}
	defer rows.Close()
	var out []client.TaskID
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, fmt.Errorf("scan dep row for task %q: %w", id, err)
		}
		out = append(out, dep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dep rows for task %q: %w", id, err)
	}
	return out, nil
}
