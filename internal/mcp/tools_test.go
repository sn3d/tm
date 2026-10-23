package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sn3d/tm/internal/backend/sqlite"
	"github.com/sn3d/tm/internal/client"
)

// newTestServer builds a Server over a real sqlite instance holding the given
// projects, bound to the first one. Using the real backend rather than a stub
// keeps these tests honest about what an agent actually receives.
func newTestServer(t *testing.T, tickers ...string) (*Server, client.Backend) {
	t.Helper()
	if len(tickers) == 0 {
		tickers = []string{"tm"}
	}
	dbPath := filepath.Join(t.TempDir(), "mcp.db")
	b, err := sqlite.NewBackend(dbPath)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	for _, tk := range tickers {
		if err := b.Projects().Save(&client.Project{Ticker: tk, DisplayName: strings.ToUpper(tk)}); err != nil {
			t.Fatalf("save project %q: %v", tk, err)
		}
	}
	c := client.New(b, client.WithActor("agent"), client.WithProject(tickers[0]))
	return NewServer(c), b
}

// call invokes a handler with the given arguments and returns the decoded
// JSON payload plus the raw text, mirroring how an agent consumes a response.
func call(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned a transport error: %v", err)
	}
	text := resultText(t, res)
	var decoded map[string]any
	if json.Unmarshal([]byte(text), &decoded) != nil {
		decoded = nil
	}
	return decoded, text, res.IsError
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestTaskCreate_ReturnsQualifiedID is the wire-format guarantee: responses
// carry the TICKER/#id form so an agent cannot address the wrong project with
// a bare number.
func TestTaskCreate_ReturnsQualifiedID(t *testing.T) {
	s, _ := newTestServer(t, "tm")

	got, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "do the thing"})
	if isErr {
		t.Fatalf("task_create failed: %s", raw)
	}
	id, ok := got["id"].(string)
	if !ok {
		t.Fatalf("id is not a string in %s", raw)
	}
	if id != "tm/#1" {
		t.Errorf("id = %q, want %q", id, "tm/#1")
	}
	if got["project"] != "tm" {
		t.Errorf("project = %v, want tm", got["project"])
	}
}

// TestTaskGet_IDIsAStringNotAnObject is risk #1 from the design doc. Task IDs
// were bare strings on the wire before projects existed; if TaskRef
// marshalled as an object every agent caller would break silently.
func TestTaskGet_IDIsAStringNotAnObject(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "s"}); isErr {
		t.Fatalf("setup create failed: %s", raw)
	}

	got, raw, isErr := call(t, s.handleTaskGet, map[string]any{"id": "1"})
	if isErr {
		t.Fatalf("task_get failed: %s", raw)
	}

	// The decoded value must be a JSON string, not a nested object.
	if _, isObject := got["id"].(map[string]any); isObject {
		t.Fatal("task id serialised as an object; every agent caller would break")
	}
	id, ok := got["id"].(string)
	if !ok {
		t.Fatalf("id is not a string: %T in %s", got["id"], raw)
	}
	if id != "tm/#1" {
		t.Errorf("id = %q, want %q", id, "tm/#1")
	}
	// Likewise for the nested reference fields.
	if _, isObject := got["parent_id"].(map[string]any); isObject {
		t.Error("parent_id serialised as an object")
	}
}

func TestTaskGet_DependsOnIsStringArray(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "dep"}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "dependent", "depends_on": []any{"1"},
	}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleTaskGet, map[string]any{"id": "2"})
	if isErr {
		t.Fatalf("task_get failed: %s", raw)
	}
	deps, ok := got["depends_on"].([]any)
	if !ok {
		t.Fatalf("depends_on is not an array: %T in %s", got["depends_on"], raw)
	}
	if len(deps) != 1 {
		t.Fatalf("depends_on = %v, want one entry", deps)
	}
	if s, ok := deps[0].(string); !ok || s != "tm/#1" {
		t.Errorf("depends_on[0] = %v (%T), want the string %q", deps[0], deps[0], "tm/#1")
	}
}

// TestIDArgument_AcceptsEveryDocumentedForm keeps an agent holding a bare id
// from before this change working inside its own project.
func TestIDArgument_AcceptsEveryDocumentedForm(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "target"}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	for _, form := range []string{"1", "#1", "tm/1", "tm/#1", "TM/#1"} {
		got, raw, isErr := call(t, s.handleTaskGet, map[string]any{"id": form})
		if isErr {
			t.Errorf("task_get with id=%q failed: %s", form, raw)
			continue
		}
		if got["subject"] != "target" {
			t.Errorf("id=%q resolved to %v, want the target task", form, got["subject"])
		}
	}
}

func TestIDArgument_MissingIsAnError(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	for _, args := range []map[string]any{{}, {"id": ""}} {
		_, raw, isErr := call(t, s.handleTaskGet, args)
		if !isErr {
			t.Errorf("task_get with args %v should error, got %s", args, raw)
		}
	}
}

// TestProjectArgument_OptionalDefaultsToCurrent is the behaviour the user
// called out: omitting `project` acts on the current project.
func TestProjectArgument_OptionalDefaultsToCurrent(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")

	// No project argument at all.
	got, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "implicit"})
	if isErr {
		t.Fatalf("task_create without project failed: %s", raw)
	}
	if got["project"] != "tm" {
		t.Errorf("project = %v, want the current project tm", got["project"])
	}

	// An explicitly empty project is treated the same as absent.
	got, raw, isErr = call(t, s.handleTaskCreate, map[string]any{"subject": "empty", "project": ""})
	if isErr {
		t.Fatalf("task_create with project=\"\" failed: %s", raw)
	}
	if got["project"] != "tm" {
		t.Errorf("project = %v, want the current project tm", got["project"])
	}
}

func TestProjectArgument_RebindsToAnotherProject(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")

	got, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "in billing", "project": "BILL",
	})
	if isErr {
		t.Fatalf("task_create with project=BILL failed: %s", raw)
	}
	if got["project"] != "bill" {
		t.Errorf("project = %v, want the canonical bill", got["project"])
	}
	// Numbering restarts per project, so this is bill/#1 even though tm has
	// its own #1 sequence.
	if got["id"] != "bill/#1" {
		t.Errorf("id = %v, want %q", got["id"], "bill/#1")
	}
}

func TestProjectArgument_UnknownProjectErrors(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	_, raw, isErr := call(t, s.handleTaskList, map[string]any{"project": "bad_ticker"})
	if !isErr {
		t.Errorf("an invalid ticker should error, got %s", raw)
	}
}

// TestProjectArgument_ConflictingQualifiedIDErrors covers the explicit
// failure added so a caller passing both a project and a qualified id from
// another project gets a clear message rather than "not found".
func TestProjectArgument_ConflictingQualifiedIDErrors(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "in tm"}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	_, raw, isErr := call(t, s.handleTaskGet, map[string]any{"id": "tm/#1", "project": "bill"})
	if !isErr {
		t.Fatalf("a qualified id disagreeing with `project` should error, got %s", raw)
	}
	if !strings.Contains(raw, "bill") || !strings.Contains(raw, "tm") {
		t.Errorf("error should name both conflicting projects, got: %s", raw)
	}
}

func TestTaskList_ScopedToProject(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "tm one"}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "bill one", "project": "bill"}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleTaskList, nil)
	if isErr {
		t.Fatalf("task_list failed: %s", raw)
	}
	if got["project"] != "tm" {
		t.Errorf("project = %v, want tm", got["project"])
	}
	tasks, ok := got["tasks"].([]any)
	if !ok {
		t.Fatalf("tasks is not an array in %s", raw)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 (the other project must not leak)", len(tasks))
	}
	first := tasks[0].(map[string]any)
	if first["subject"] != "tm one" {
		t.Errorf("subject = %v, want %q", first["subject"], "tm one")
	}
	if first["id"] != "tm/#1" {
		t.Errorf("id = %v, want the qualified form", first["id"])
	}
}

func TestTaskEdit_PartialUpdate(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "original", "description": "body",
	}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	if _, raw, isErr := call(t, s.handleTaskEdit, map[string]any{
		"id": "1", "state": "in_progress",
	}); isErr {
		t.Fatalf("task_edit failed: %s", raw)
	}

	got, raw, isErr := call(t, s.handleTaskGet, map[string]any{"id": "1"})
	if isErr {
		t.Fatalf("task_get failed: %s", raw)
	}
	if got["state"] != "in_progress" {
		t.Errorf("state = %v, want in_progress", got["state"])
	}
	// Omitted fields keep their current value.
	if got["subject"] != "original" {
		t.Errorf("subject = %v, want it unchanged", got["subject"])
	}
	if got["description"] != "body" {
		t.Errorf("description = %v, want it unchanged", got["description"])
	}
}

func TestTaskArchiveUnarchive(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "parent"}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "child", "parent_id": "1",
	}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleTaskArchive, map[string]any{"id": "1"})
	if isErr {
		t.Fatalf("task_archive failed: %s", raw)
	}
	if got["id"] != "tm/#1" {
		t.Errorf("id = %v, want the qualified form", got["id"])
	}
	// Cascade defaults to true, so the child goes with it.
	if n, _ := got["cascade_count"].(float64); n != 1 {
		t.Errorf("cascade_count = %v, want 1", got["cascade_count"])
	}

	if _, raw, isErr := call(t, s.handleTaskUnarchive, map[string]any{"id": "1"}); isErr {
		t.Fatalf("task_unarchive failed: %s", raw)
	}
	after, raw, isErr := call(t, s.handleTaskGet, map[string]any{"id": "1"})
	if isErr {
		t.Fatalf("task_get failed: %s", raw)
	}
	if _, present := after["archived_at"]; present {
		t.Error("archived_at should be absent after unarchiving")
	}
}

func TestTaskComments_RoundTrip(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "s"}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	if _, raw, isErr := call(t, s.handleTaskAddComment, map[string]any{
		"id": "1", "who": "alice", "comment": "a note",
	}); isErr {
		t.Fatalf("task_add_comment failed: %s", raw)
	}

	got, raw, isErr := call(t, s.handleTaskGetComments, map[string]any{"id": "1"})
	if isErr {
		t.Fatalf("task_get_comments failed: %s", raw)
	}
	if got["id"] != "tm/#1" {
		t.Errorf("id = %v, want the qualified form", got["id"])
	}
	comments, ok := got["comments"].([]any)
	if !ok || len(comments) != 1 {
		t.Fatalf("comments = %v, want one entry", got["comments"])
	}
	first := comments[0].(map[string]any)
	if first["who"] != "alice" || first["comment"] != "a note" {
		t.Errorf("comment = %+v", first)
	}
	// created_at is exposed now that it is a real column.
	if first["created_at"] == nil || first["created_at"] == "" {
		t.Error("created_at should be populated")
	}
}

func TestTaskAddComment_RequiresWhoAndComment(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "s"}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	for _, args := range []map[string]any{
		{"id": "1", "comment": "x"},
		{"id": "1", "who": "alice"},
	} {
		if _, raw, isErr := call(t, s.handleTaskAddComment, args); !isErr {
			t.Errorf("task_add_comment with %v should error, got %s", args, raw)
		}
	}
}

func TestProjectList(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "one"}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleProjectList, nil)
	if isErr {
		t.Fatalf("project_list failed: %s", raw)
	}
	if got["default_project"] != "tm" {
		t.Errorf("default_project = %v, want tm", got["default_project"])
	}
	projects, ok := got["projects"].([]any)
	if !ok {
		t.Fatalf("projects is not an array in %s", raw)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
	byTicker := map[string]map[string]any{}
	for _, p := range projects {
		pm := p.(map[string]any)
		byTicker[pm["ticker"].(string)] = pm
	}
	tm, ok := byTicker["tm"]
	if !ok {
		t.Fatalf("missing tm in %v", byTicker)
	}
	// The ticker is the canonical form an agent passes back as `project`.
	if tm["display_name"] != "TM" {
		t.Errorf("display_name = %v, want TM", tm["display_name"])
	}
	if n, _ := tm["task_count"].(float64); n != 1 {
		t.Errorf("task_count = %v, want 1", tm["task_count"])
	}
}

func TestInbox_ProjectScoped(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "mine", "assigned_agent": "agent",
	}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleInbox, map[string]any{"peek": true})
	if isErr {
		t.Fatalf("inbox failed: %s", raw)
	}
	if got["project"] != "tm" {
		t.Errorf("project = %v, want tm", got["project"])
	}
	tasks, ok := got["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks = %v, want one entry", got["tasks"])
	}
	if tasks[0].(map[string]any)["id"] != "tm/#1" {
		t.Errorf("task id = %v, want the qualified form", tasks[0].(map[string]any)["id"])
	}
}

func TestInbox_AllProjects(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "tm work", "assigned_agent": "agent",
	}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{
		"subject": "bill work", "assigned_agent": "agent", "project": "bill",
	}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleInbox, map[string]any{"all_projects": true, "peek": true})
	if isErr {
		t.Fatalf("inbox all_projects failed: %s", raw)
	}
	projects, ok := got["projects"].([]any)
	if !ok {
		t.Fatalf("projects is not an array in %s", raw)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d inboxes, want 2", len(projects))
	}
}

func TestInbox_ProjectAndAllProjectsConflict(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	_, raw, isErr := call(t, s.handleInbox, map[string]any{
		"all_projects": true, "project": "bill",
	})
	if !isErr {
		t.Errorf("passing both project and all_projects should error, got %s", raw)
	}
}

func TestJournalList_QualifiedTaskIDs(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "s"}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleJournalList, nil)
	if isErr {
		t.Fatalf("journal_list failed: %s", raw)
	}
	events, ok := got["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("events = %v, want at least one", got["events"])
	}
	first := events[0].(map[string]any)
	if first["task_id"] != "tm/#1" {
		t.Errorf("task_id = %v, want the qualified form", first["task_id"])
	}
	if first["project"] != "tm" {
		t.Errorf("project = %v, want tm", first["project"])
	}
	// Still a string, never an object.
	if _, isObject := first["task_id"].(map[string]any); isObject {
		t.Error("journal task_id serialised as an object")
	}
}

func TestJournalList_AllProjects(t *testing.T) {
	s, _ := newTestServer(t, "tm", "bill")
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "tm"}); isErr {
		t.Fatalf("setup: %s", raw)
	}
	if _, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "bill", "project": "bill"}); isErr {
		t.Fatalf("setup: %s", raw)
	}

	got, raw, isErr := call(t, s.handleJournalList, map[string]any{"all_projects": true})
	if isErr {
		t.Fatalf("journal_list all_projects failed: %s", raw)
	}
	events := got["events"].([]any)
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.(map[string]any)["project"].(string)] = true
	}
	if !seen["tm"] || !seen["bill"] {
		t.Errorf("expected events from both projects, got %v", seen)
	}
}

// TestUnboundServer_ErrorsWithActionableMessage covers the MCP server started
// in an unmapped folder: it must tell the agent what to do rather than
// writing to a guessed project.
func TestUnboundServer_ErrorsWithActionableMessage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mcp.db")
	b, err := sqlite.NewBackend(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// No project bound and none in the registry.
	s := NewServer(client.New(b, client.WithActor("agent")))

	_, raw, isErr := call(t, s.handleTaskCreate, map[string]any{"subject": "s"})
	if !isErr {
		t.Fatalf("expected an error with no project selected, got %s", raw)
	}
	if !strings.Contains(raw, "project") {
		t.Errorf("error should mention the project argument, got: %s", raw)
	}
}

func TestDependsOnFromArgs(t *testing.T) {
	s, _ := newTestServer(t, "tm")
	c, err := s.clientFor(nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("absent", func(t *testing.T) {
		deps, present, err := dependsOnFromArgs(c, map[string]any{})
		if err != nil || present || deps != nil {
			t.Errorf("got %v, %v, %v; want nil, false, nil", deps, present, err)
		}
	})

	t.Run("array of bare ids resolves against the project", func(t *testing.T) {
		deps, present, err := dependsOnFromArgs(c, map[string]any{"depends_on": []any{"1", "2"}})
		if err != nil || !present {
			t.Fatalf("got %v, %v, %v", deps, present, err)
		}
		if len(deps) != 2 || deps[0].String() != "tm/#1" || deps[1].String() != "tm/#2" {
			t.Errorf("deps = %v, want tm/#1 and tm/#2", client.FormatTaskRefs(deps))
		}
	})

	t.Run("comma separated string", func(t *testing.T) {
		deps, present, err := dependsOnFromArgs(c, map[string]any{"depends_on": "1, 2"})
		if err != nil || !present {
			t.Fatalf("got %v, %v, %v", deps, present, err)
		}
		if len(deps) != 2 {
			t.Errorf("deps = %v, want two entries", client.FormatTaskRefs(deps))
		}
	})

	t.Run("empty array clears", func(t *testing.T) {
		deps, present, err := dependsOnFromArgs(c, map[string]any{"depends_on": []any{}})
		if err != nil || !present || deps != nil {
			t.Errorf("got %v, %v, %v; want nil, true, nil", deps, present, err)
		}
	})

	t.Run("non-string entry errors", func(t *testing.T) {
		if _, _, err := dependsOnFromArgs(c, map[string]any{"depends_on": []any{42}}); err == nil {
			t.Error("expected an error for a non-string entry")
		}
	})

	t.Run("invalid reference errors", func(t *testing.T) {
		if _, _, err := dependsOnFromArgs(c, map[string]any{"depends_on": []any{"not-a-ref"}}); err == nil {
			t.Error("expected an error for an unparseable reference")
		}
	})
}
