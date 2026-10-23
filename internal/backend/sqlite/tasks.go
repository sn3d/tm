package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sn3d/tm/internal/client"
)

type tasksRepository struct {
	db *sql.DB
}

// taskColumns is the shared SELECT list for the list paths, which
// deliberately do NOT join task_descriptions — see GetAll's doc comment.
const taskColumns = `project_id, id, subject, state, assigned_agent, parent_id, labels, mode, created_at, updated_at, archived_at`

// Save inserts a new task or updates an existing one in the given project.
// When t.ID carries no number the next per-project sequential ID is assigned
// and the full ref written back into t.ID. CreatedAt is stamped on insert and
// preserved on update; UpdatedAt is refreshed on every save. Both timestamps
// are written back into t.
//
// The dependency list is fully replaced (delete-then-insert) and the
// description is written to task_descriptions, all inside one transaction, so
// a task and its body never diverge.
func (tr *tasksRepository) Save(project string, t *client.Task) error {
	canon, err := client.CanonicalTicker(project)
	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}

	tx, err := tr.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for task %s: %w", t.ID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if t.ID.ID == "" {
		next, err := nextProjectNumericID(tx, canon)
		if err != nil {
			return err
		}
		t.ID = client.TaskRef{Project: canon, ID: next}
	} else {
		// Normalise: the caller may have handed us a ref whose project half
		// is empty (built from a bare ID) — the project argument is
		// authoritative for where the row lands.
		t.ID = client.TaskRef{Project: canon, ID: t.ID.ID}
	}
	if t.State == "" {
		t.State = client.TaskStateDefault
	}
	if t.Mode == "" {
		t.Mode = client.TaskModeDefault
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

	labelsJSON, err := marshalLabels(t.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels for task %s: %w", t.ID, err)
	}

	// archived_at is NULL when not archived; non-NULL is RFC3339Nano. Use
	// sql.NullString so the driver writes a real SQL NULL rather than ''.
	var archivedAt sql.NullString
	if t.ArchivedAt != nil {
		archivedAt = sql.NullString{String: t.ArchivedAt.Format(time.RFC3339Nano), Valid: true}
	}

	const upsertTask = `
		INSERT INTO tasks (project_id, id, subject, state, assigned_agent, parent_id, labels, mode, created_at, updated_at, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, id) DO UPDATE SET
			subject        = excluded.subject,
			state          = excluded.state,
			assigned_agent = excluded.assigned_agent,
			parent_id      = excluded.parent_id,
			labels         = excluded.labels,
			mode           = excluded.mode,
			updated_at     = excluded.updated_at,
			archived_at    = excluded.archived_at`
	if _, err := tx.Exec(upsertTask, canon, t.ID.ID, t.Subject, string(t.State), t.AssignedAgent,
		t.ParentID.ID, labelsJSON, string(t.Mode),
		t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano), archivedAt); err != nil {
		return fmt.Errorf("upsert task %s: %w", t.ID, err)
	}

	if err := saveDescriptionTx(tx, t.ID, t.Description, t.UpdatedAt); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM task_deps WHERE project_id = ? AND task_id = ?`, canon, t.ID.ID); err != nil {
		return fmt.Errorf("clear deps for task %s: %w", t.ID, err)
	}
	if len(t.DependsOn) > 0 {
		const insertDep = `INSERT INTO task_deps (project_id, task_id, depends_on_id) VALUES (?, ?, ?)`
		for _, dep := range t.DependsOn {
			if _, err := tx.Exec(insertDep, canon, t.ID.ID, dep.ID); err != nil {
				return fmt.Errorf("insert dep %s for task %s: %w", dep, t.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task %s: %w", t.ID, err)
	}
	return nil
}

// loadTaskCreatedAt returns the existing created_at for a task within the
// current transaction. Returns (nil, nil) when the row doesn't exist,
// (&zero, nil) when the row exists with no stored timestamp (legacy rows
// from before this column existed).
func loadTaskCreatedAt(tx *sql.Tx, ref client.TaskID) (*time.Time, error) {
	var createdStr string
	err := tx.QueryRow(`SELECT created_at FROM tasks WHERE project_id = ? AND id = ?`,
		ref.Project, ref.ID).Scan(&createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load created_at for task %s: %w", ref, err)
	}
	if createdStr == "" {
		zero := time.Time{}
		return &zero, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at for task %s: %w", ref, err)
	}
	return &ts, nil
}

// GetByID returns the task the ref addresses, or (nil, nil) when no such row
// exists. The description IS joined in — this is the detail-view path.
func (tr *tasksRepository) GetByID(ref client.TaskID) (*client.Task, error) {
	if ref.Project == "" || ref.ID == "" {
		return nil, nil
	}
	const q = `SELECT t.project_id, t.id, t.subject, t.state, t.assigned_agent, t.parent_id,
			t.labels, t.mode, t.created_at, t.updated_at, t.archived_at,
			COALESCE(d.body, '')
		FROM tasks t
		LEFT JOIN task_descriptions d ON d.project_id = t.project_id AND d.task_id = t.id
		WHERE t.project_id = ? AND t.id = ?`
	var (
		t           client.Task
		projectID   string
		bareID      string
		parentBare  string
		state       string
		labelsStr   string
		modeStr     string
		createdStr  string
		updatedStr  string
		archivedRaw sql.NullString
		description string
	)
	err := tr.db.QueryRow(q, ref.Project, ref.ID).Scan(&projectID, &bareID, &t.Subject, &state,
		&t.AssignedAgent, &parentBare, &labelsStr, &modeStr, &createdStr, &updatedStr,
		&archivedRaw, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query task %s: %w", ref, err)
	}
	t.ID = client.TaskRef{Project: projectID, ID: bareID}
	t.Description = description
	if err := hydrateTaskScalars(&t, projectID, parentBare, state, labelsStr, modeStr, createdStr, updatedStr, archivedRaw); err != nil {
		return nil, err
	}
	deps, err := tr.loadDeps(t.ID)
	if err != nil {
		return nil, err
	}
	t.DependsOn = deps
	return &t, nil
}

// GetAll returns every task in the project, ordered by UpdatedAt descending
// (most recently changed first). ID breaks ties.
//
// Description is NOT populated — use GetByID for a task's body. Board and
// list views render subject, state and labels; pulling every description
// body to draw a board is waste.
func (tr *tasksRepository) GetAll(project string) ([]client.Task, error) {
	canon, err := client.CanonicalTicker(project)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	const q = `SELECT ` + taskColumns + `
		FROM tasks WHERE project_id = ? ORDER BY updated_at DESC, id`
	return tr.queryTasks(q, canon)
}

// GetByParent returns every task in the ref's project whose parent_id matches
// the ref's ID. Pass a ref with an empty ID (but a set Project) to list that
// project's top-level tasks.
//
// Description is NOT populated, for the same reason as GetAll.
func (tr *tasksRepository) GetByParent(parentRef client.TaskID) ([]client.Task, error) {
	if parentRef.Project == "" {
		return nil, fmt.Errorf("list tasks by parent: no project in reference %q", parentRef)
	}
	const q = `SELECT ` + taskColumns + `
		FROM tasks WHERE project_id = ? AND parent_id = ? ORDER BY updated_at DESC, id`
	return tr.queryTasks(q, parentRef.Project, parentRef.ID)
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
			t           client.Task
			projectID   string
			bareID      string
			parentBare  string
			state       string
			labelsStr   string
			modeStr     string
			createdStr  string
			updatedStr  string
			archivedRaw sql.NullString
		)
		if err := rows.Scan(&projectID, &bareID, &t.Subject, &state, &t.AssignedAgent,
			&parentBare, &labelsStr, &modeStr, &createdStr, &updatedStr, &archivedRaw); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.ID = client.TaskRef{Project: projectID, ID: bareID}
		if err := hydrateTaskScalars(&t, projectID, parentBare, state, labelsStr, modeStr, createdStr, updatedStr, archivedRaw); err != nil {
			return nil, err
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

// hydrateTaskScalars fills the parsed non-dependency scalars on t from their
// raw TEXT-column representations. Empty `labels` and `mode` (from rows
// inserted before earlier migrations) decode to nil and TaskModeDefault. An
// invalid archived_at sql.NullString decodes to a nil ArchivedAt.
//
// parentBare is the stored bare parent id; it is re-qualified with the row's
// own project because parent links are same-project by definition.
func hydrateTaskScalars(t *client.Task, projectID, parentBare, state, labelsStr, modeStr, createdStr, updatedStr string, archivedRaw sql.NullString) error {
	if parentBare != "" {
		t.ParentID = client.TaskRef{Project: projectID, ID: parentBare}
	} else {
		t.ParentID = client.TaskRef{}
	}
	parsedState, err := client.ParseTaskState(state)
	if err != nil {
		return fmt.Errorf("task %s: %w", t.ID, err)
	}
	t.State = parsedState
	labels, err := unmarshalLabels(labelsStr)
	if err != nil {
		return fmt.Errorf("parse labels for task %s: %w", t.ID, err)
	}
	t.Labels = labels
	mode, err := client.ParseTaskMode(modeStr)
	if err != nil {
		return fmt.Errorf("parse mode for task %s: %w", t.ID, err)
	}
	t.Mode = mode
	if t.CreatedAt, err = parseSQLTime(createdStr); err != nil {
		return fmt.Errorf("parse created_at for task %s: %w", t.ID, err)
	}
	if t.UpdatedAt, err = parseSQLTime(updatedStr); err != nil {
		return fmt.Errorf("parse updated_at for task %s: %w", t.ID, err)
	}
	if archivedRaw.Valid && archivedRaw.String != "" {
		parsed, err := parseSQLTime(archivedRaw.String)
		if err != nil {
			return fmt.Errorf("parse archived_at for task %s: %w", t.ID, err)
		}
		t.ArchivedAt = &parsed
	} else {
		t.ArchivedAt = nil
	}
	return nil
}

// marshalLabels encodes the label slice as a JSON array for the labels TEXT
// column. nil and empty slices both encode as `[]` for round-trip stability.
func marshalLabels(labels []string) (string, error) {
	if len(labels) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalLabels decodes a JSON array from the labels TEXT column. The empty
// string (legacy rows predating the column) and "[]" both return nil.
func unmarshalLabels(s string) ([]string, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// parseSQLTime parses an RFC3339Nano timestamp from a TEXT column. An empty
// string (from legacy rows predating the column) becomes zero time.
func parseSQLTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func (tr *tasksRepository) loadDeps(ref client.TaskID) ([]client.TaskID, error) {
	const q = `SELECT depends_on_id FROM task_deps WHERE project_id = ? AND task_id = ? ORDER BY depends_on_id`
	rows, err := tr.db.Query(q, ref.Project, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("query deps for task %s: %w", ref, err)
	}
	defer rows.Close()
	var out []client.TaskID
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, fmt.Errorf("scan dep row for task %s: %w", ref, err)
		}
		// Dependencies are same-project by definition, so the stored bare id
		// re-qualifies with the dependent's own project.
		out = append(out, client.TaskRef{Project: ref.Project, ID: dep})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dep rows for task %s: %w", ref, err)
	}
	return out, nil
}
