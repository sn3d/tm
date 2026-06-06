package mcp

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

type inboxView struct {
	Actor         string      `json:"actor"`
	Tasks         []taskView  `json:"tasks"`
	Plans         []planView  `json:"plans"`
	Resumable     []taskView  `json:"resumable"`
	RecentChanges []eventView `json:"recent_changes"`
	LastSeenAt    string      `json:"last_seen_at"`
}

func viewInbox(b *client.Inbox) inboxView {
	tasks := make([]taskView, len(b.Tasks))
	for i, t := range b.Tasks {
		tasks[i] = viewTask(t)
	}
	plans := make([]planView, len(b.Plans))
	for i, p := range b.Plans {
		plans[i] = viewPlan(p)
	}
	resumable := make([]taskView, len(b.Resumable))
	for i, t := range b.Resumable {
		resumable[i] = viewTask(t)
	}
	changes := make([]eventView, len(b.RecentChanges))
	for i, e := range b.RecentChanges {
		changes[i] = viewEvent(e)
	}
	lastSeen := ""
	if !b.LastSeenAt.IsZero() {
		lastSeen = b.LastSeenAt.Format(time.RFC3339Nano)
	}
	return inboxView{
		Actor:         b.Actor,
		Tasks:         tasks,
		Plans:         plans,
		Resumable:     resumable,
		RecentChanges: changes,
		LastSeenAt:    lastSeen,
	}
}

func (s *Server) handleInbox(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	// Default to the server-wide actor when none is supplied. Reaching into
	// As("") would resolve to "system", which is almost never the caller's
	// real inbox; using the configured actor matches the CLI's behavior.
	c := s.c
	actor := s.c.Actor()
	if v, ok := args["actor"].(string); ok && v != "" {
		c = s.c.As(v)
		actor = v
	}

	peek := false
	if v, ok := args["peek"].(bool); ok {
		peek = v
	}

	limit := 50
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	var (
		box *client.Inbox
		err error
	)
	if peek {
		box, err = c.PeekInbox(actor)
	} else {
		box, err = c.Inbox(actor)
	}
	if err != nil {
		return mcp.NewToolResultErrorFromErr("inbox", err), nil
	}

	if limit > 0 && len(box.RecentChanges) > limit {
		box.RecentChanges = box.RecentChanges[len(box.RecentChanges)-limit:]
	}

	return mcp.NewToolResultJSON(viewInbox(box))
}
