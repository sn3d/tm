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
	ID        string `json:"id"`
	Timestamp string `json:"ts"`
	Actor     string `json:"actor"`
	Kind      string `json:"kind"`
	Project   string `json:"project,omitempty"`
	// TaskID is the qualified TICKER/#id string form, so an event read from a
	// cross-project journal is unambiguous.
	TaskID  string         `json:"task_id,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

func viewEvent(e client.Event) eventView {
	return eventView{
		ID:        e.ID,
		Timestamp: e.Timestamp.Format(time.RFC3339Nano),
		Actor:     e.Actor,
		Kind:      string(e.Kind),
		Project:   e.Project,
		TaskID:    e.TaskID.String(),
		Payload:   e.Payload,
	}
}

func (s *Server) handleJournalList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	// all_projects spans every project in the instance; otherwise the
	// journal is scoped to the call's project.
	allProjects, _ := args["all_projects"].(bool)
	c := s.c
	if !allProjects {
		bound, err := s.clientFor(args)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("select project", err), nil
		}
		c = bound
	} else if v, ok := args["project"].(string); ok && v != "" {
		return mcp.NewToolResultError("pass either `project` or `all_projects`, not both"), nil
	}

	filter := client.EventFilter{}
	if v, ok := args["task_id"].(string); ok && v != "" {
		ref, err := c.ParseRef(v)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid task_id", err), nil
		}
		filter.TaskID = ref
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

	var (
		events []client.Event
		err    error
	)
	if allProjects {
		events, err = c.JournalAll(filter)
	} else {
		events, err = c.ListEvents(filter)
	}
	if err != nil {
		return mcp.NewToolResultErrorFromErr("list events", err), nil
	}
	views := make([]eventView, len(events))
	for i, e := range events {
		views[i] = viewEvent(e)
	}
	return mcp.NewToolResultJSON(map[string]any{"events": views})
}
