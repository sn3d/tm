package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/client"
)

// clientFor returns the Server's Client, rebound to the per-call actor and
// project when those args are present.
//
// `project` defaults to the cwd mapping the server started under, so an agent
// that never passes it keeps today's behaviour. Passing it reaches another
// project in the SAME instance on the same connection — there is no
// cross-instance hop.
func (s *Server) clientFor(args map[string]any) (*client.Client, error) {
	c := s.c
	if v, ok := args["actor"].(string); ok && v != "" {
		c = c.As(v)
	}
	if v, ok := args["project"].(string); ok && v != "" {
		bound, err := c.ForProject(v)
		if err != nil {
			return nil, err
		}
		c = bound
	}
	if c.Project() == "" {
		return nil, fmt.Errorf("no project selected: pass `project` with a ticker, " +
			"or start the server in a folder mapped by 'tm init'")
	}
	return c, nil
}

// refFromArgs reads the required "id" argument and resolves it against the
// call's project. Accepts every documented form — "42", "#42", "tm/42",
// "TM/#42" — so an agent holding a bare id from before this change keeps
// working inside its own project.
func refFromArgs(c *client.Client, args map[string]any) (client.TaskID, error) {
	raw, ok := args["id"].(string)
	if !ok || raw == "" {
		return client.TaskRef{}, fmt.Errorf("missing required argument: id")
	}
	ref, err := c.ParseRef(raw)
	if err != nil {
		return client.TaskRef{}, err
	}
	if ref.IsZero() {
		return client.TaskRef{}, fmt.Errorf("missing required argument: id")
	}
	// A qualified id that disagrees with the call's project is a caller bug,
	// not something to silently resolve one way or the other. Saying which
	// two values conflict is more useful than a bare "not found".
	if ref.Project != c.Project() {
		return client.TaskRef{}, fmt.Errorf(
			"task %s is in project %q but this call targets project %q; "+
				"drop the `project` argument to use the task's own project",
			ref, ref.Project, c.Project())
	}
	return ref, nil
}

// taskView is the JSON shape returned for tasks. State is encoded as its
// string form for agent-friendliness. The internal Mode field is not
// exposed — agents use `labels` for type/category. ArchivedAt is omitted
// when nil so active tasks don't gain a noisy field.
type taskView struct {
	// ID is the qualified TICKER/#id string form. Returning a bare "42"
	// where the agent then operates in a different project is exactly the
	// bug the qualified form prevents.
	ID            string   `json:"id"`
	Project       string   `json:"project"`
	Subject       string   `json:"subject"`
	Description   string   `json:"description,omitempty"`
	State         string   `json:"state"`
	AssignedAgent string   `json:"assigned_agent,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	ParentID      string   `json:"parent_id,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	ArchivedAt    *string  `json:"archived_at,omitempty"`
}

func viewTask(t client.Task) taskView {
	v := taskView{
		ID:            t.ID.String(),
		Project:       t.ID.Project,
		Subject:       t.Subject,
		Description:   t.Description,
		State:         t.State.String(),
		AssignedAgent: t.AssignedAgent,
		DependsOn:     client.FormatTaskRefs(t.DependsOn),
		ParentID:      t.ParentID.String(),
		Labels:        t.Labels,
	}
	if t.ArchivedAt != nil {
		s := t.ArchivedAt.Format(time.RFC3339Nano)
		v.ArchivedAt = &s
	}
	return v
}

// dependsOnFromArgs reads the optional "depends_on" MCP argument, which can
// arrive either as a JSON array of strings or as a comma-separated string.
// Returns (deps, true, nil) when the field was present, (nil, false, nil)
// when absent, and a non-nil error if the value is malformed.
func dependsOnFromArgs(c *client.Client, args map[string]any) ([]client.TaskID, bool, error) {
	raw, present := args["depends_on"]
	if !present {
		return nil, false, nil
	}
	var rawIDs []string
	switch v := raw.(type) {
	case nil:
		return nil, true, nil
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, true, fmt.Errorf("depends_on entries must be strings, got %T", item)
			}
			if s != "" {
				rawIDs = append(rawIDs, s)
			}
		}
	case string:
		rawIDs = parseCSVStrings(v)
	default:
		return nil, true, fmt.Errorf("depends_on must be an array or comma-separated string, got %T", raw)
	}
	if len(rawIDs) == 0 {
		return nil, true, nil
	}
	refs, err := c.ParseRefs(rawIDs)
	if err != nil {
		return nil, true, err
	}
	return refs, true, nil
}

// labelsFromArgs reads the optional "labels" MCP argument. Accepts either a
// JSON array of strings or a comma-separated string. Returns (labels, true,
// nil) when the field was present, (nil, false, nil) when absent. An empty
// array (or empty string) is treated as "present and clearing labels".
func labelsFromArgs(args map[string]any) ([]string, bool, error) {
	raw, present := args["labels"]
	if !present {
		return nil, false, nil
	}
	switch v := raw.(type) {
	case nil:
		return nil, true, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, true, fmt.Errorf("labels entries must be strings, got %T", item)
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
		out := parseCSVStrings(v)
		if len(out) == 0 {
			return nil, true, nil
		}
		return out, true, nil
	default:
		return nil, true, fmt.Errorf("labels must be an array or comma-separated string, got %T", raw)
	}
}

// parseCSVStrings splits a comma-separated string into trimmed non-empty
// entries. Used by labelsFromArgs and shares the trimming logic with
// parseCSVIDs but kept separate so the return types stay distinct.
func parseCSVStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
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
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	description, _ := args["description"].(string)
	assignedAgent, _ := args["assigned_agent"].(string)
	dependsOn, _, err := dependsOnFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("invalid depends_on", err), nil
	}
	parentRaw, _ := args["parent_id"].(string)
	parentID, err := c.ParseRef(parentRaw)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("invalid parent_id", err), nil
	}
	labels, _, err := labelsFromArgs(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("invalid labels", err), nil
	}

	id, err := c.CreateTask(client.CreateTaskInput{
		Subject:       subject,
		Description:   description,
		AssignedAgent: assignedAgent,
		DependsOn:     dependsOn,
		ParentID:      parentID,
		Labels:        labels,
	})
	if err != nil {
		return mcp.NewToolResultErrorFromErr("create task", err), nil
	}
	// The qualified form goes back on the wire so a follow-up call cannot
	// address the wrong project with a bare number.
	return mcp.NewToolResultJSON(map[string]string{
		"id":      id.String(),
		"project": id.Project,
	})
}

func (s *Server) handleTaskEdit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	id, err := refFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	current, err := c.GetTask(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get task %s", id), err), nil
	}

	in := client.EditTaskInput{
		Subject:       current.Subject,
		Description:   current.Description,
		State:         current.State,
		AssignedAgent: current.AssignedAgent,
		DependsOn:     current.DependsOn,
		ParentID:      current.ParentID,
		Labels:        current.Labels,
		Mode:          current.Mode,
	}
	if v, ok := args["subject"].(string); ok {
		in.Subject = v
	}
	if v, ok := args["description"].(string); ok {
		in.Description = v
	}
	if v, ok := args["state"].(string); ok && v != "" {
		parsed, perr := client.ParseTaskState(v)
		if perr != nil {
			return mcp.NewToolResultErrorFromErr("invalid state", perr), nil
		}
		in.State = parsed
	}
	if v, ok := args["assigned_agent"].(string); ok {
		in.AssignedAgent = v
	}
	if newDeps, present, derr := dependsOnFromArgs(c, args); derr != nil {
		return mcp.NewToolResultErrorFromErr("invalid depends_on", derr), nil
	} else if present {
		in.DependsOn = newDeps
	}
	if v, ok := args["parent_id"].(string); ok {
		parent, perr := c.ParseRef(v)
		if perr != nil {
			return mcp.NewToolResultErrorFromErr("invalid parent_id", perr), nil
		}
		in.ParentID = parent
	}
	if newLabels, present, lerr := labelsFromArgs(args); lerr != nil {
		return mcp.NewToolResultErrorFromErr("invalid labels", lerr), nil
	} else if present {
		in.Labels = newLabels
	}

	if err := c.EditTask(id, in); err != nil {
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("update task %s", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}

func (s *Server) handleTaskList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	var tasks []client.Task
	parentRaw, hasParent := args["parent_id"].(string)
	label, _ := args["label"].(string)
	archivedStr, _ := args["archived"].(string)
	archived, err := client.ParseArchivedFilter(archivedStr)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("invalid archived", err), nil
	}

	// parent_id picks the base set; label narrows it. label alone hits
	// the dedicated client lookup; combined with parent_id we list under
	// the parent and then filter in-process.
	switch {
	case hasParent:
		parent, perr := c.ParseRef(parentRaw)
		if perr != nil {
			return mcp.NewToolResultErrorFromErr("invalid parent_id", perr), nil
		}
		tasks, err = c.GetTasksByParent(parent, archived)
	case label != "":
		tasks, err = c.GetTasksByLabel(label, archived)
		label = "" // already applied by the dedicated lookup
	default:
		tasks, err = c.ListTasks(archived)
	}
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr("list tasks", err), nil
	}
	if label != "" {
		tasks = filterByLabel(tasks, label)
	}

	views := make([]taskView, len(tasks))
	for i, t := range tasks {
		views[i] = viewTask(t)
	}
	return mcp.NewToolResultJSON(map[string]any{
		"project": c.Project(),
		"tasks":   views,
	})
}

func filterByLabel(tasks []client.Task, label string) []client.Task {
	out := make([]client.Task, 0, len(tasks))
	for _, t := range tasks {
		for _, l := range t.Labels {
			if l == label {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func (s *Server) handleTaskArchive(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	id, err := refFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// cascade defaults to true when the field is absent or explicitly omitted.
	cascade := true
	if v, present := args["cascade"]; present {
		if b, ok := v.(bool); ok {
			cascade = b
		}
	}
	count, err := c.ArchiveTask(id, cascade)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("archive task %s", id), err), nil
	}
	return mcp.NewToolResultJSON(map[string]any{
		"id":            id.String(),
		"project":       id.Project,
		"cascade_count": count,
	})
}

func (s *Server) handleTaskUnarchive(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	id, err := refFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := c.UnarchiveTask(id); err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("unarchive task %s", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}

func (s *Server) handleTaskGet(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	id, err := refFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	task, err := c.GetTask(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get task %s", id), err), nil
	}
	return mcp.NewToolResultJSON(viewTask(*task))
}

func (s *Server) handleTaskGetComments(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	id, err := refFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	comments, err := c.GetTaskComments(id)
	if err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("get comments for task %s", id), err), nil
	}
	return mcp.NewToolResultJSON(map[string]any{
		"id":       id.String(),
		"comments": viewComments(comments),
	})
}

// commentView is the JSON shape for comments. CreatedAt is exposed now that
// it is a real column rather than something inferred from the ULID.
type commentView struct {
	ID        string `json:"id"`
	Who       string `json:"who"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at,omitempty"`
}

func viewComments(in []client.Comment) []commentView {
	out := make([]commentView, len(in))
	for i, c := range in {
		out[i] = commentView{ID: c.ID, Who: c.Who, Comment: c.Comment}
		if !c.CreatedAt.IsZero() {
			out[i].CreatedAt = c.CreatedAt.Format(time.RFC3339Nano)
		}
	}
	return out
}

func (s *Server) handleTaskAddComment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	c, err := s.clientFor(args)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("select project", err), nil
	}
	id, err := refFromArgs(c, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	who, ok := args["who"].(string)
	if !ok || who == "" {
		return mcp.NewToolResultError("missing required argument: who"), nil
	}
	comment, ok := args["comment"].(string)
	if !ok || comment == "" {
		return mcp.NewToolResultError("missing required argument: comment"), nil
	}

	if err := c.AddTaskComment(id, who, comment); err != nil {
		var nfe *client.NotFoundError
		if errors.As(err, &nfe) {
			return mcp.NewToolResultError(nfe.Error()), nil
		}
		return mcp.NewToolResultErrorFromErr(fmt.Sprintf("add comment to task %s", id), err), nil
	}
	return mcp.NewToolResultText("ok"), nil
}

// handleProjectList is the new cross-project discovery tool: an agent that
// does not know which tickers exist can enumerate them before passing
// `project` to any other tool.
func (s *Server) handleProjectList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	includeArchived := false
	if v, ok := args["include_archived"].(bool); ok {
		includeArchived = v
	}
	projects, err := s.c.Projects().GetAll(includeArchived)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("list projects", err), nil
	}
	counts, err := s.c.Projects().TaskCounts()
	if err != nil {
		return mcp.NewToolResultErrorFromErr("count tasks", err), nil
	}
	views := make([]projectView, len(projects))
	for i, p := range projects {
		views[i] = projectView{
			Ticker:      p.Ticker,
			DisplayName: p.DisplayName,
			Name:        p.Name,
			TaskCount:   counts[p.Ticker],
		}
		if p.ArchivedAt != nil {
			ts := p.ArchivedAt.Format(time.RFC3339Nano)
			views[i].ArchivedAt = &ts
		}
	}
	return mcp.NewToolResultJSON(map[string]any{
		"default_project": s.c.Project(),
		"projects":        views,
	})
}

// projectView is the JSON shape for a project. Ticker is the canonical form
// an agent passes back as the `project` argument.
type projectView struct {
	Ticker      string  `json:"ticker"`
	DisplayName string  `json:"display_name"`
	Name        string  `json:"name,omitempty"`
	TaskCount   int     `json:"task_count"`
	ArchivedAt  *string `json:"archived_at,omitempty"`
}
