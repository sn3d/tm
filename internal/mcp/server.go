package mcp

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sn3d/tm/internal/client"
)

func joinTaskStates() string {
	parts := make([]string, len(client.TaskStates))
	for i, s := range client.TaskStates {
		parts[i] = string(s)
	}
	return strings.Join(parts, " | ")
}

func joinPlanStates() string {
	parts := make([]string, len(client.PlanStates))
	for i, s := range client.PlanStates {
		parts[i] = string(s)
	}
	return strings.Join(parts, " | ")
}

const serverName = "tm"
const serverVersion = "0.1.0"

// Server wraps a *client.Client and exposes its operations as MCP tools.
type Server struct {
	mcp *server.MCPServer
	c   *client.Client
}

// NewServer builds an MCP server backed by the given client. All tools are
// registered eagerly. Call ServeStdio to run the stdio transport.
func NewServer(c *client.Client) *Server {
	s := &Server{
		mcp: server.NewMCPServer(serverName, serverVersion, server.WithToolCapabilities(true)),
		c:   c,
	}
	s.registerTools()
	return s
}

// ServeStdio runs the MCP server on stdin/stdout until the stream closes.
// Stdout is reserved for the JSON-RPC transport — handlers must never write
// to it directly.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcp)
}

func (s *Server) registerTools() {
	s.mcp.AddTool(
		mcp.NewTool("task_create",
			mcp.WithDescription("Create a new task. Returns the assigned task ID."),
			mcp.WithString("subject", mcp.Description("Short title of the task."), mcp.Required()),
			mcp.WithString("description", mcp.Description("Longer description of the task. Optional.")),
			mcp.WithString("assigned_agent", mcp.Description("Name of the agent the task is assigned to. Optional.")),
			mcp.WithArray("depends_on",
				mcp.Description("IDs of existing tasks this task depends on. Optional."),
				mcp.Items(map[string]any{"type": "string"}),
			),
			mcp.WithString("plan_id", mcp.Description("ID of the plan this task belongs to. Optional.")),
			mcp.WithString("actor", mcp.Description("Identity to record on the journal event. Overrides the server-wide default for this call only.")),
		),
		s.handleTaskCreate,
	)

	s.mcp.AddTool(
		mcp.NewTool("task_edit",
			mcp.WithDescription("Edit an existing task. Only the fields provided are changed; omitted fields keep their current value."),
			mcp.WithString("id", mcp.Description("Task ID."), mcp.Required()),
			mcp.WithString("subject", mcp.Description("New subject. Omit to leave unchanged.")),
			mcp.WithString("description", mcp.Description("New description. Omit to leave unchanged.")),
			mcp.WithString("state",
				mcp.Description("New state: "+joinTaskStates()+". Omit to leave unchanged."),
			),
			mcp.WithString("assigned_agent", mcp.Description("New assigned agent. Omit to leave unchanged.")),
			mcp.WithArray("depends_on",
				mcp.Description("Replacement dependency list (existing task IDs). Pass [] to clear; omit to leave unchanged."),
				mcp.Items(map[string]any{"type": "string"}),
			),
			mcp.WithString("plan_id", mcp.Description("New plan ID. Pass empty string to unassign from any plan. Omit to leave unchanged.")),
			mcp.WithString("actor", mcp.Description("Identity to record on the journal events. Overrides the server-wide default for this call only.")),
		),
		s.handleTaskEdit,
	)

	s.mcp.AddTool(
		mcp.NewTool("task_list",
			mcp.WithDescription("List all tasks. Optionally filter by plan."),
			mcp.WithString("plan_id", mcp.Description(`Filter tasks by plan ID. Pass an empty string to list only standalone tasks (no plan). Omit to list all tasks.`)),
		),
		s.handleTaskList,
	)

	s.mcp.AddTool(
		mcp.NewTool("task_get",
			mcp.WithDescription("Get a single task by ID."),
			mcp.WithString("id", mcp.Description("Task ID."), mcp.Required()),
		),
		s.handleTaskGet,
	)

	s.mcp.AddTool(
		mcp.NewTool("task_get_comments",
			mcp.WithDescription("Get all comments attached to a task."),
			mcp.WithString("id", mcp.Description("Task ID."), mcp.Required()),
		),
		s.handleTaskGetComments,
	)

	s.mcp.AddTool(
		mcp.NewTool("task_add_comment",
			mcp.WithDescription("Append a comment to a task."),
			mcp.WithString("id", mcp.Description("Task ID."), mcp.Required()),
			mcp.WithString("who", mcp.Description("Author of the comment."), mcp.Required()),
			mcp.WithString("comment", mcp.Description("Comment body. Markdown is supported."), mcp.Required()),
			mcp.WithString("actor", mcp.Description("Identity to record on the journal event. Overrides the server-wide default for this call only.")),
		),
		s.handleTaskAddComment,
	)

	s.mcp.AddTool(
		mcp.NewTool("plan_create",
			mcp.WithDescription("Create a new plan. Returns the assigned plan ID."),
			mcp.WithString("subject", mcp.Description("Short title of the plan."), mcp.Required()),
			mcp.WithString("description", mcp.Description("Longer description of the plan. Optional.")),
			mcp.WithString("assigned_agent", mcp.Description("Name of the agent the plan is assigned to. Optional.")),
			mcp.WithString("actor", mcp.Description("Identity to record on the journal event. Overrides the server-wide default for this call only.")),
		),
		s.handlePlanCreate,
	)

	s.mcp.AddTool(
		mcp.NewTool("plan_edit",
			mcp.WithDescription("Edit an existing plan. Only the fields provided are changed; omitted fields keep their current value."),
			mcp.WithString("id", mcp.Description("Plan ID."), mcp.Required()),
			mcp.WithString("subject", mcp.Description("New subject. Omit to leave unchanged.")),
			mcp.WithString("description", mcp.Description("New description. Omit to leave unchanged.")),
			mcp.WithString("state",
				mcp.Description("New state: "+joinPlanStates()+". Omit to leave unchanged."),
			),
			mcp.WithString("assigned_agent", mcp.Description("New assigned agent. Omit to leave unchanged.")),
			mcp.WithString("actor", mcp.Description("Identity to record on the journal events. Overrides the server-wide default for this call only.")),
		),
		s.handlePlanEdit,
	)

	s.mcp.AddTool(
		mcp.NewTool("plan_list",
			mcp.WithDescription("List all plans."),
		),
		s.handlePlanList,
	)

	s.mcp.AddTool(
		mcp.NewTool("plan_get",
			mcp.WithDescription("Get a single plan by ID."),
			mcp.WithString("id", mcp.Description("Plan ID."), mcp.Required()),
		),
		s.handlePlanGet,
	)

	s.mcp.AddTool(
		mcp.NewTool("plan_get_comments",
			mcp.WithDescription("Get all comments attached to a plan."),
			mcp.WithString("id", mcp.Description("Plan ID."), mcp.Required()),
		),
		s.handlePlanGetComments,
	)

	s.mcp.AddTool(
		mcp.NewTool("plan_add_comment",
			mcp.WithDescription("Append a comment to a plan."),
			mcp.WithString("id", mcp.Description("Plan ID."), mcp.Required()),
			mcp.WithString("who", mcp.Description("Author of the comment."), mcp.Required()),
			mcp.WithString("comment", mcp.Description("Comment body. Markdown is supported."), mcp.Required()),
			mcp.WithString("actor", mcp.Description("Identity to record on the journal event. Overrides the server-wide default for this call only.")),
		),
		s.handlePlanAddComment,
	)

	s.mcp.AddTool(
		mcp.NewTool("inbox",
			mcp.WithDescription("Return the actor's inbox: assigned open/active tasks and plans, plus journal events touching them since the actor's last-seen cursor. Advances the cursor unless peek=true."),
			mcp.WithString("actor", mcp.Description("Inbox owner. Defaults to the server-wide actor.")),
			mcp.WithBoolean("peek", mcp.Description("If true, do not advance the last-seen cursor. Default false.")),
			mcp.WithNumber("limit", mcp.Description("Max recent_changes to return (0 = no limit). Default 50.")),
		),
		s.handleInbox,
	)

	s.mcp.AddTool(
		mcp.NewTool("journal_list",
			mcp.WithDescription("List audit-log events, newest first. All filters are optional and AND-combined."),
			mcp.WithString("task_id", mcp.Description("Filter by task ID.")),
			mcp.WithString("plan_id", mcp.Description("Filter by plan ID.")),
			mcp.WithString("actor", mcp.Description("Filter by actor (agent / user name).")),
			mcp.WithArray("kinds",
				mcp.Description("Filter by event kinds (OR semantics)."),
				mcp.Items(map[string]any{"type": "string"}),
			),
			mcp.WithString("since", mcp.Description("Only events strictly after this RFC3339 timestamp.")),
			mcp.WithNumber("limit", mcp.Description("Max events to return (0 = no limit). Default 50.")),
		),
		s.handleJournalList,
	)
}
