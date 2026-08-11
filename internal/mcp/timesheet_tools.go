package mcp

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

func (s *Server) handleTimesheet(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	var since, until *time.Time
	if v, ok := args["since"].(string); ok && v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid since", err), nil
		}
		since = &ts
	}
	if v, ok := args["until"].(string); ok && v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid until", err), nil
		}
		until = &ts
	}
	lastWeek, _ := args["last_week"].(bool)
	from, to := client.ResolveTimesheetRange(time.Now(), lastWeek, since, until)

	actor, _ := args["actor"].(string)
	entries, err := s.c.Timesheet(from, to, actor)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("build timesheet", err), nil
	}
	return mcp.NewToolResultJSON(map[string]any{
		"from":    from.Format(time.RFC3339),
		"to":      to.Format(time.RFC3339),
		"entries": client.ViewTimesheetEntries(entries),
	})
}
