package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

// eventView is the JSON shape returned for events. Timestamp is encoded as
// RFC3339Nano so agents see explicit precision.
type eventView struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"ts"`
	Actor     string         `json:"actor"`
	Kind      string         `json:"kind"`
	TaskID    string         `json:"task_id,omitempty"`
	PlanID    string         `json:"plan_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func viewEvent(e client.Event) eventView {
	return eventView{
		ID:        e.ID,
		Timestamp: e.Timestamp.Format(time.RFC3339Nano),
		Actor:     e.Actor,
		Kind:      string(e.Kind),
		TaskID:    e.TaskID,
		PlanID:    e.PlanID,
		Payload:   e.Payload,
	}
}

func (s *Server) handleJournalList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	filter := client.EventFilter{}
	if v, ok := args["task_id"].(string); ok {
		filter.TaskID = v
	}
	if v, ok := args["plan_id"].(string); ok {
		filter.PlanID = v
	}
	if v, ok := args["actor"].(string); ok {
		filter.Actor = v
	}
	if raw, ok := args["kinds"]; ok && raw != nil {
		switch v := raw.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					filter.Kinds = append(filter.Kinds, client.EventKind(s))
				}
			}
		case []string:
			for _, s := range v {
				if s != "" {
					filter.Kinds = append(filter.Kinds, client.EventKind(s))
				}
			}
		default:
			return mcp.NewToolResultError(fmt.Sprintf("kinds must be array of strings, got %T", raw)), nil
		}
	}
	if v, ok := args["since"].(string); ok && v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid since", err), nil
		}
		filter.Since = ts
	}
	// limit arrives as float64 from JSON; default to 50 when absent so a
	// naive call doesn't dump the entire journal.
	filter.Limit = 50
	if v, ok := args["limit"].(float64); ok {
		filter.Limit = int(v)
	}

	events, err := s.c.ListEvents(filter)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("list events", err), nil
	}
	views := make([]eventView, len(events))
	for i, e := range events {
		views[i] = viewEvent(e)
	}
	return mcp.NewToolResultJSON(map[string]any{"events": views})
}
