package filestorage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

const eventsFilename = "events.ndjson"

type eventsRepository struct {
	dir string
}

// eventLine is the on-disk JSON representation. Field names use snake_case
// so the file is friendlier to hand inspection. The legacy plan_id field is
// kept on read so older event logs deserialize cleanly; new events never
// write it.
type eventLine struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"ts"`
	Actor     string         `json:"actor"`
	Kind      string         `json:"kind"`
	TaskID    string         `json:"task_id,omitempty"`
	PlanID    string         `json:"plan_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func (er *eventsRepository) Append(e *client.Event) error {
	if e.ID == "" {
		e.ID = ulid.Make().String()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	line := eventLine{
		ID:        e.ID,
		Timestamp: e.Timestamp.Format(time.RFC3339Nano),
		Actor:     e.Actor,
		Kind:      string(e.Kind),
		TaskID:    e.TaskID,
		Payload:   e.Payload,
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	path := filepath.Join(er.dir, eventsFilename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (er *eventsRepository) List(filter client.EventFilter) ([]client.Event, error) {
	path := filepath.Join(er.dir, eventsFilename)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []client.Event{}, nil
		}
		return nil, fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()

	all := make([]client.Event, 0)
	reader := bufio.NewReader(f)
	for {
		raw, err := reader.ReadBytes('\n')
		if len(raw) > 0 {
			ev, parseErr := parseEventLine(raw)
			if parseErr != nil {
				return nil, parseErr
			}
			all = append(all, ev)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read events file: %w", err)
		}
	}

	// newest-first; the file is append-only so reverse-iterate.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Timestamp.Equal(all[j].Timestamp) {
			return all[i].ID > all[j].ID
		}
		return all[i].Timestamp.After(all[j].Timestamp)
	})

	out := make([]client.Event, 0, len(all))
	for _, e := range all {
		if !matches(e, filter) {
			continue
		}
		out = append(out, e)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func parseEventLine(raw []byte) (client.Event, error) {
	// strip trailing newline; tolerate blank lines.
	for len(raw) > 0 && (raw[len(raw)-1] == '\n' || raw[len(raw)-1] == '\r') {
		raw = raw[:len(raw)-1]
	}
	if len(raw) == 0 {
		return client.Event{}, nil
	}
	var line eventLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return client.Event{}, fmt.Errorf("parse event line: %w", err)
	}
	ts, err := time.Parse(time.RFC3339Nano, line.Timestamp)
	if err != nil {
		return client.Event{}, fmt.Errorf("parse event ts %q: %w", line.Timestamp, err)
	}
	return client.Event{
		ID:        line.ID,
		Timestamp: ts,
		Actor:     line.Actor,
		Kind:      client.EventKind(line.Kind),
		TaskID:    line.TaskID,
		Payload:   line.Payload,
	}, nil
}

func matches(e client.Event, f client.EventFilter) bool {
	if e.ID == "" {
		return false // skip blank-line zero value
	}
	if f.TaskID != "" && e.TaskID != f.TaskID {
		return false
	}
	if f.Actor != "" && e.Actor != f.Actor {
		return false
	}
	if len(f.Kinds) > 0 {
		match := false
		for _, k := range f.Kinds {
			if e.Kind == k {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	if !f.Since.IsZero() && !e.Timestamp.After(f.Since) {
		return false
	}
	return true
}
