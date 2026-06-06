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
			mcp.WithDescription(`Edit an existing task. Only the fields provided are changed; omitted fields keep their current value.

HANDOFF RULE: when you need someone else to act next, change `+"`state`"+` AND `+"`assigned_agent`"+` in the same call. The task leaves your inbox and lands in theirs. Changing only one of the two leaves the task stuck on your plate.

Typical handoffs:
- ask a question -> state=blocked, assigned_agent=<person who can answer>
- request review -> state=in_review, assigned_agent=<reviewer>
- send back for changes -> state=in_progress, assigned_agent=<original agent>
- approve / close -> state=done (assignee is irrelevant once done)`),
			mcp.WithString("id", mcp.Description("Task ID."), mcp.Required()),
			mcp.WithString("subject", mcp.Description("New subject. Omit to leave unchanged.")),
			mcp.WithString("description", mcp.Description("New description. Omit to leave unchanged.")),
			mcp.WithString("state",
				mcp.Description(`New state. Omit to leave unchanged. Allowed: `+joinTaskStates()+`.
- todo: not started yet.
- in_progress: actively being worked on by the assignee.
- blocked: cannot progress without input from someone else; pair with reassignment to whoever owes you the input.
- in_review: work is done from the assignee's side and awaits review; pair with reassignment to the reviewer.
- done: terminal success state. Drops the task out of every inbox.
- cancelled: terminal abandon state. Drops the task out of every inbox.`),
			),
			mcp.WithString("assigned_agent", mcp.Description("New assigned agent. Omit to leave unchanged. Set together with `state` when handing off (see HANDOFF RULE in the tool description).")),
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
			mcp.WithDescription(`Edit an existing plan. Only the fields provided are changed; omitted fields keep their current value.

HANDOFF RULE (same as task_edit): when you want someone else to act on this plan next, change `+"`state`"+` AND `+"`assigned_agent`"+` in the same call. Plans appear in the assignee's inbox while in draft / active / on_hold, and drop out on completed / cancelled.`),
			mcp.WithString("id", mcp.Description("Plan ID."), mcp.Required()),
			mcp.WithString("subject", mcp.Description("New subject. Omit to leave unchanged.")),
			mcp.WithString("description", mcp.Description("New description. Omit to leave unchanged.")),
			mcp.WithString("state",
				mcp.Description("New state. Omit to leave unchanged. Allowed: "+joinPlanStates()+". draft/active/on_hold keep the plan in the assignee's inbox; completed and cancelled remove it."),
			),
			mcp.WithString("assigned_agent", mcp.Description("New assigned agent. Omit to leave unchanged. Set together with `state` when handing off.")),
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
			mcp.WithDescription(`Return the actor's inbox. Use this as the heartbeat: call it each cycle to see what needs your attention.

Response sections:
- tasks / plans: items currently assigned to the actor in an open or active state (todo, in_progress, blocked, in_review for tasks; draft, active, on_hold for plans). These are your turn. Newest UpdatedAt first.
- resumable: subset of `+"`tasks`"+` whose UpdatedAt is after your last-seen cursor — someone moved the ball back into your court since your last heartbeat (typical: a reply on a blocked task, a reassignment to you, an unblock). Act on these first.
- recent_changes: journal events touching your assigned items (or reassignments TO you) since your last-seen cursor. Audit feed, oldest first. Events you authored yourself are excluded.
- last_seen_at: the cursor value as it was BEFORE this call. After a non-peek call the cursor is advanced to now, so the next inbox call will only show what arrived after this moment.

Handoff model: things appear here when someone changes a task's state AND reassigns it to you in the same edit (see task_edit). Things disappear when you do the same handoff in reverse, or when the task reaches a terminal state (done, cancelled).`),
			mcp.WithString("actor", mcp.Description("Inbox owner. Defaults to the server-wide actor.")),
			mcp.WithBoolean("peek", mcp.Description("If true, do not advance the last-seen cursor. Use when looking without committing to having processed the items — the next non-peek call will still surface them.")),
			mcp.WithNumber("limit", mcp.Description("Max recent_changes to return (0 = no limit). Default 50. Does not affect tasks/plans/resumable.")),
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
