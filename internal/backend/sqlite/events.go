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
	const q = `INSERT INTO events (id, ts, actor, kind, task_id, plan_id, payload) VALUES (?, ?, ?, ?, ?, ?, ?)`
	if _, err := er.db.Exec(q, e.ID, e.Timestamp.Format(time.RFC3339Nano), e.Actor, string(e.Kind), e.TaskID, e.PlanID, string(raw)); err != nil {
		return fmt.Errorf("insert event %q: %w", e.ID, err)
	}
	return nil
}

func (er *eventsRepository) List(filter client.EventFilter) ([]client.Event, error) {
	var (
		conds []string
		args  []any
	)
	if filter.TaskID != "" {
		conds = append(conds, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.PlanID != "" {
		conds = append(conds, "plan_id = ?")
		args = append(args, filter.PlanID)
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
	if !filter.Since.IsZero() {
		conds = append(conds, "ts > ?")
		args = append(args, filter.Since.Format(time.RFC3339Nano))
	}

	q := "SELECT id, ts, actor, kind, task_id, plan_id, payload FROM events"
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
			e       client.Event
			tsStr   string
			kindStr string
			raw     string
		)
		if err := rows.Scan(&e.ID, &tsStr, &e.Actor, &kindStr, &e.TaskID, &e.PlanID, &raw); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
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
