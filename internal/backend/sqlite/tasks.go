package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/sn3d/tm/internal/client"
)

const tasksSchema = `
CREATE TABLE IF NOT EXISTS tasks (
	id             TEXT    PRIMARY KEY,
	subject        TEXT    NOT NULL DEFAULT '',
	description    TEXT    NOT NULL DEFAULT '',
	state          TEXT    NOT NULL DEFAULT 'todo',
	assigned_agent TEXT    NOT NULL DEFAULT '',
	plan_id        TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS task_deps (
	task_id       TEXT NOT NULL,
	depends_on_id TEXT NOT NULL,
	PRIMARY KEY (task_id, depends_on_id),
	FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_deps_task_id ON task_deps(task_id);
CREATE INDEX IF NOT EXISTS idx_tasks_plan_id ON tasks(plan_id);
`

type tasksRepository struct {
	db *sql.DB
}

// Save inserts a new task or updates an existing one. When t.ID is empty the
// next sequential ID from the shared task/plan counter is assigned and
// written back into t.ID. When t.ID is set the existing row is replaced.
// The dependency list is fully replaced (delete-then-insert) inside the
// same transaction.
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

	const upsertTask = `
		INSERT INTO tasks (id, subject, description, state, assigned_agent, plan_id)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			subject        = excluded.subject,
			description    = excluded.description,
			state          = excluded.state,
			assigned_agent = excluded.assigned_agent,
			plan_id        = excluded.plan_id`
	if _, err := tx.Exec(upsertTask, t.ID, t.Subject, t.Description, string(t.State), t.AssignedAgent, t.PlanID); err != nil {
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

// GetByID returns the task with the given ID, or (nil, nil) when no such row exists.
func (tr *tasksRepository) GetByID(id client.TaskID) (*client.Task, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, plan_id FROM tasks WHERE id = ?`
	var (
		t     client.Task
		state string
	)
	err := tr.db.QueryRow(q, id).Scan(&t.ID, &t.Subject, &t.Description, &state, &t.AssignedAgent, &t.PlanID)
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
	deps, err := tr.loadDeps(t.ID)
	if err != nil {
		return nil, err
	}
	t.DependsOn = deps
	return &t, nil
}

// GetAll returns every task in the repository, ordered by ID (ULID
// lexicographic order, which is also creation order).
func (tr *tasksRepository) GetAll() ([]client.Task, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, plan_id FROM tasks ORDER BY id`
	rows, err := tr.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]client.Task, 0)
	for rows.Next() {
		var (
			t     client.Task
			state string
		)
		if err := rows.Scan(&t.ID, &t.Subject, &t.Description, &state, &t.AssignedAgent, &t.PlanID); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		parsedState, err := client.ParseTaskState(state)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", t.ID, err)
		}
		t.State = parsedState
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

// GetByPlan returns every task whose PlanID matches the given plan, ordered
// by ID. Returns an empty slice when no tasks reference the plan.
func (tr *tasksRepository) GetByPlan(planID client.PlanID) ([]client.Task, error) {
	const q = `SELECT id, subject, description, state, assigned_agent, plan_id FROM tasks WHERE plan_id = ? ORDER BY id`
	rows, err := tr.db.Query(q, planID)
	if err != nil {
		return nil, fmt.Errorf("query tasks for plan %q: %w", planID, err)
	}
	defer rows.Close()

	tasks := make([]client.Task, 0)
	for rows.Next() {
		var (
			t     client.Task
			state string
		)
		if err := rows.Scan(&t.ID, &t.Subject, &t.Description, &state, &t.AssignedAgent, &t.PlanID); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		parsedState, err := client.ParseTaskState(state)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", t.ID, err)
		}
		t.State = parsedState
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
