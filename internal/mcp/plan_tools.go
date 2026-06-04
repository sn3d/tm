package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

// planView is the JSON shape returned for plans. State is encoded as its
// string form for agent-friendliness.
type planView struct {
	ID            string `json:"id"`
	Subject       string `json:"subject"`
	Description   string `json:"description,omitempty"`
	State         string `json:"state"`
	AssignedAgent string `json:"assigned_agent,omitempty"`
}

func viewPlan(p client.Plan) planView {
	return planView{
		ID:            p.ID,
		Subject:       p.Subject,
		Description:   p.Description,
		State:         p.State.String(),
		AssignedAgent: p.AssignedAgent,
	}
}

func (s *Server) handlePlanCreate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	subject, ok := args["subject"].(string)
	if !ok || subject == "" {
		return mcp.NewToolResultError("missing required argument: subject"), nil
	}
	description, _ := args["description"].(string)
	assignedAgent, _ := args["assigned_agent"].(string)

	id, err := s.clientFor(args).CreatePlan(subject, description, assignedAgent)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("create plan", err), nil
	}
	return mcp.NewToolResultJSON(map[string]string{"id": id})
}

func (s *Server) handlePlanEdit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}

	current, err := s.c.GetPlan(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get plan %q", id), err), nil
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
		parsed, err := client.ParsePlanState(v)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid state", err), nil
		}
		state = parsed
	}
	assignedAgent := current.AssignedAgent
	if v, ok := args["assigned_agent"].(string); ok {
		assignedAgent = v
	}

	if err := s.clientFor(args).EditPlan(id, subject, description, state, assignedAgent); err != nil {
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("update plan %q", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}

func (s *Server) handlePlanList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	plans, err := s.c.ListPlans()
	if err != nil {
		return mcp.NewToolResultErrorFromErr("list plans", err), nil
	}
	views := make([]planView, len(plans))
	for i, p := range plans {
		views[i] = viewPlan(p)
	}
	return mcp.NewToolResultJSON(map[string]any{"plans": views})
}

func (s *Server) handlePlanGet(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}

	plan, err := s.c.GetPlan(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get plan %q", id), err), nil
	}
	return mcp.NewToolResultJSON(viewPlan(*plan))
}

func (s *Server) handlePlanGetComments(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("missing required argument: id"), nil
	}

	comments, err := s.c.GetPlanComments(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get comments for plan %q", id), err), nil
	}
	return mcp.NewToolResultJSON(map[string]any{"comments": comments})
}

func (s *Server) handlePlanAddComment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	if err := s.clientFor(args).AddPlanComment(id, who, comment); err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("add comment to plan %q", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}
