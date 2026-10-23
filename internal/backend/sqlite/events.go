package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

type eventsRepository struct {
	db *sql.DB
}

func (er *eventsRepository) Append(e *client.Event) error {
	if e.ID == "" {
		e.ID = ulid.Make().String()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	// project_id is stored separately from the task reference so the journal
	// survives project deletion (it is deliberately not a foreign key), and
	// so cross-project queries can filter without parsing task_id.
	project := e.Project
	if project == "" && !e.TaskID.IsZero() {
		project = e.TaskID.Project
	}
	const q = `INSERT INTO events (id, ts, actor, kind, project_id, task_id, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	if _, err := er.db.Exec(q, e.ID, e.Timestamp.Format(time.RFC3339Nano), e.Actor,
		string(e.Kind), project, e.TaskID.ID, string(raw)); err != nil {
		return fmt.Errorf("insert event %q: %w", e.ID, err)
	}
	e.Project = project
	return nil
}

func (er *eventsRepository) List(filter client.EventFilter) ([]client.Event, error) {
	var (
		conds []string
		args  []any
	)
	// An explicit project narrows to one journal; an empty Project means
	// "every project in this instance", which is what JournalAll wants.
	if filter.Project != "" {
		conds = append(conds, "project_id = ?")
		args = append(args, filter.Project)
	}
	if !filter.TaskID.IsZero() {
		conds = append(conds, "task_id = ?")
		args = append(args, filter.TaskID.ID)
		// A task reference also pins the project, even when the caller did
		// not set Project — without this, bare ids would collide across
		// projects that both number a task 42.
		if filter.Project == "" && filter.TaskID.Project != "" {
			conds = append(conds, "project_id = ?")
			args = append(args, filter.TaskID.Project)
		}
	}
	if filter.Actor != "" {
		conds = append(conds, "actor = ?")
		args = append(args, filter.Actor)
	}
	if len(filter.Kinds) > 0 {
		placeholders := strings.Repeat("?,", len(filter.Kinds))
		placeholders = placeholders[:len(placeholders)-1]
		conds = append(conds, "kind IN ("+placeholders+")")
		for _, k := range filter.Kinds {
			args = append(args, string(k))
		}
	}
	// Bounds are compared lexicographically against the stored ts strings,
	// which are always UTC ("Z"). Normalize to UTC so a caller passing a
	// local-zone bound (e.g. from CurrentWeek) compares correctly.
	if !filter.Since.IsZero() {
		conds = append(conds, "ts > ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if !filter.Until.IsZero() {
		conds = append(conds, "ts < ?")
		args = append(args, filter.Until.UTC().Format(time.RFC3339Nano))
	}

	q := "SELECT id, ts, actor, kind, project_id, task_id, payload FROM events"
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY ts DESC, id DESC"
	if filter.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := er.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	out := make([]client.Event, 0)
	for rows.Next() {
		var (
			e         client.Event
			tsStr     string
			kindStr   string
			projectID string
			bareTask  string
			raw       string
		)
		if err := rows.Scan(&e.ID, &tsStr, &e.Actor, &kindStr, &projectID, &bareTask, &raw); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		e.Project = projectID
		if bareTask != "" {
			e.TaskID = client.TaskRef{Project: projectID, ID: bareTask}
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return nil, fmt.Errorf("parse event ts %q: %w", tsStr, err)
		}
		e.Timestamp = ts
		e.Kind = client.EventKind(kindStr)
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, fmt.Errorf("unmarshal event payload for %q: %w", e.ID, err)
		}
		e.Payload = payload
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}
	return out, nil
}
