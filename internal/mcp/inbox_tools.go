package mcp

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

type inboxView struct {
	Actor         string      `json:"actor"`
	Project       string      `json:"project,omitempty"`
	Tasks         []taskView  `json:"tasks"`
	Resumable     []taskView  `json:"resumable"`
	RecentChanges []eventView `json:"recent_changes"`
	LastSeenAt    string      `json:"last_seen_at"`
}

func viewInbox(b *client.Inbox) inboxView {
	tasks := make([]taskView, len(b.Tasks))
	for i, t := range b.Tasks {
		tasks[i] = viewTask(t)
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
		Project:       b.Project,
		Tasks:         tasks,
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
	actor := s.c.Actor()
	if v, ok := args["actor"].(string); ok && v != "" {
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

	allProjects, _ := args["all_projects"].(bool)
	if allProjects {
		if v, ok := args["project"].(string); ok && v != "" {
			return mcp.NewToolResultError("pass either `project` or `all_projects`, not both"), nil
		}
		return s.handleInboxAll(actor, peek, limit)
	}

	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	c = c.As(actor)

	var box *client.Inbox
	if peek {
		box, err = c.PeekInbox(actor)
	} else {
		box, err = c.Inbox(actor)
	}
	if err != nil {
		return mcp.NewToolResultErrorFromErr("inbox", err), nil
	}

	trimChanges(box, limit)
	return mcp.NewToolResultJSON(viewInbox(box))
}

// handleInboxAll returns one inbox per project in the instance, each with its
// own cursor — reading one project's section never marks another's work as
// seen, which is why cursors are per (actor, project).
func (s *Server) handleInboxAll(actor string, peek bool, limit int) (*mcp.CallToolResult, error) {
	c := s.c.As(actor)
	var (
		boxes []*client.Inbox
		err   error
	)
	if peek {
		boxes, err = c.PeekInboxAll(actor)
	} else {
		boxes, err = c.InboxAll(actor)
	}
	if err != nil {
		return mcp.NewToolResultErrorFromErr("inbox", err), nil
	}
	views := make([]inboxView, len(boxes))
	for i, box := range boxes {
		trimChanges(box, limit)
		views[i] = viewInbox(box)
	}
	return mcp.NewToolResultJSON(map[string]any{
		"actor":    actor,
		"projects": views,
	})
}

// trimChanges keeps the most recent `limit` changes, preserving the
// oldest-first order of what remains.
func trimChanges(box *client.Inbox, limit int) {
	if limit > 0 && len(box.RecentChanges) > limit {
		box.RecentChanges = box.RecentChanges[len(box.RecentChanges)-limit:]
	}
}
