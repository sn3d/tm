package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

// clientFor returns the Server's Client, optionally overridden with a
// per-call actor when the "actor" arg is present and non-empty. Used by
// mutating handlers so each MCP call can attribute itself to a distinct
// identity without a server-side reconfiguration.
func (s *Server) clientFor(args map[string]any) *client.Client {
	if v, ok := args["actor"].(string); ok && v != "" {
		return s.c.As(v)
	}
	return s.c
}

// taskView is the JSON shape returned for tasks. State is encoded as its
// string form for agent-friendliness.
type taskView struct {
	ID            string   `json:"id"`
	Subject       string   `json:"subject"`
	Description   string   `json:"description,omitempty"`
	State         string   `json:"state"`
	AssignedAgent string   `json:"assigned_agent,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	PlanID        string   `json:"plan_id,omitempty"`
}

func viewTask(t client.Task) taskView {
	return taskView{
		ID:            t.ID,
		Subject:       t.Subject,
		Description:   t.Description,
		State:         t.State.String(),
		AssignedAgent: t.AssignedAgent,
		DependsOn:     t.DependsOn,
		PlanID:        t.PlanID,
	}
}

// dependsOnFromArgs reads the optional "depends_on" MCP argument, which can
// arrive either as a JSON array of strings or as a comma-separated string.
// Returns (deps, true, nil) when the field was present, (nil, false, nil)
// when absent, and a non-nil error if the value is malformed.
func dependsOnFromArgs(args map[string]any) ([]client.TaskID, bool, error) {
	raw, present := args["depends_on"]
	if !present {
		return nil, false, nil
	}
	switch v := raw.(type) {
	case nil:
		return nil, true, nil
	case []any:
		out := make([]client.TaskID, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, true, fmt.Errorf("depends_on entries must be strings, got %T", item)
			}
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, true, nil
		}
		return out, true, nil
	case string:
		return parseCSVIDs(v), true, nil
	default:
		return nil, true, fmt.Errorf("depends_on must be an array or comma-separated string, got %T", raw)
	}
}

func parseCSVIDs(raw string) []client.TaskID {
	if raw == "" {
		return nil
	}
	var out []client.TaskID
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == ',' {
			seg := raw[start:i]
			j, k := 0, len(seg)
			for j < k && (seg[j] == ' ' || seg[j] == '\t') {
				j++
			}
			for k > j && (seg[k-1] == ' ' || seg[k-1] == '\t') {
				k--
			}
			if j < k {
				out = append(out, seg[j:k])
			}
			start = i + 1
		}
	}
	return out
}

func (s *Server) handleTaskCreate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	subject, ok := args["subject"].(string)
	if !ok || subject == "" {
		return mcp.NewToolResultError("missing required argument: subject"), nil
	}
	description, _ := args["description"].(string)
	assignedAgent, _ := args["assigned_agent"].(string)
	dependsOn, _, err := dependsOnFromArgs(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("invalid depends_on", err), nil
	}
	planID, _ := args["plan_id"].(string)

	id, err := s.clientFor(args).CreateTask(subject, description, assignedAgent, dependsOn, planID)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("create task", err), nil
	}
	return mcp.NewToolResultJSON(map[string]string{"id": id})
}

func (s *Server) handleTaskEdit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}

	current, err := s.c.GetTask(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get task %q", id), err), nil
	}

	subject := current.Subject
	if v, ok := args["subject"].(string); ok {
		subject = v
	}
	description := current.Description
	if v, ok := args["description"].(string); ok {
		description = v
	}
	state := current.State
	if v, ok := args["state"].(string); ok && v != "" {
		parsed, err := client.ParseTaskState(v)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid state", err), nil
		}
		state = parsed
	}
	assignedAgent := current.AssignedAgent
	if v, ok := args["assigned_agent"].(string); ok {
		assignedAgent = v
	}
	dependsOn := current.DependsOn
	if newDeps, present, err := dependsOnFromArgs(args); err != nil {
		return mcp.NewToolResultErrorFromErr("invalid depends_on", err), nil
	} else if present {
		dependsOn = newDeps
	}
	planID := current.PlanID
	if v, ok := args["plan_id"].(string); ok {
		planID = v
	}

	if err := s.clientFor(args).EditTask(id, subject, description, state, assignedAgent, dependsOn, planID); err != nil {
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("update task %q", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}

func (s *Server) handleTaskList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	var (
		tasks []client.Task
		err   error
	)
	if planID, ok := args["plan_id"].(string); ok {
		tasks, err = s.c.GetTasksByPlan(planID)
	} else {
		tasks, err = s.c.ListTasks()
	}
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr("list tasks", err), nil
	}
	views := make([]taskView, len(tasks))
	for i, t := range tasks {
		views[i] = viewTask(t)
	}
	return mcp.NewToolResultJSON(map[string]any{"tasks": views})
}

func (s *Server) handleTaskGet(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}

	task, err := s.c.GetTask(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get task %q", id), err), nil
	}
	return mcp.NewToolResultJSON(viewTask(*task))
}

func (s *Server) handleTaskGetComments(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}

	comments, err := s.c.GetTaskComments(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get comments for task %q", id), err), nil
	}
	return mcp.NewToolResultJSON(map[string]any{"comments": comments})
}

func (s *Server) handleTaskAddComment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}
	who, ok := args["who"].(string)
	if !ok || who == "" {
		return mcp.NewToolResultError("missing required argument: who"), nil
	}
	comment, ok := args["comment"].(string)
	if !ok || comment == "" {
		return mcp.NewToolResultError("missing required argument: comment"), nil
	}

	if err := s.clientFor(args).AddTaskComment(id, who, comment); err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("add comment to task %q", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}
