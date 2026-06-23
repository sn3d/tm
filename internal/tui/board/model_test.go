package board

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sn3d/tm/internal/client"
)

// buildModel constructs a Model pre-loaded with a fixed task graph so
// Update tests can navigate and move without round-tripping through
// async loadCmd/cardMovedMsg every time.
func buildModel(t *testing.T, tasks []client.Task) Model {
	t.Helper()
	var lastMove struct {
		id client.TaskID
		to client.TaskState
	}
	m := NewModel(newResolver(),
		func() ([]client.Task, error) { return tasks, nil },
		func(id client.TaskID, to client.TaskState) error {
			lastMove.id = id
			lastMove.to = to
			return nil
		},
		// loadDetail / addComment / editDescription default to harmless
		// stubs so detail-mode tests can still drive Update without the
		// closures being nil. Individual tests override as needed.
		func(id client.TaskID) (TaskDetail, error) {
			return TaskDetail{Task: client.Task{ID: id}}, nil
		},
		func(id client.TaskID, body string) error { return nil },
		func(id client.TaskID, current string) tea.Cmd { return nil },
	)
	// Skip Init by injecting the loaded state directly. Tea would have
	// done this for us via Init → tasksLoadedMsg but tests want a known
	// starting point without driving the runtime.
	nextModel, _ := m.Update(tasksLoadedMsg{tasks: tasks})
	m = nextModel.(Model)
	return m
}

func sampleTasks() []client.Task {
	return []client.Task{
		{ID: "1", Subject: "todo-a", State: client.TaskStateTodo},
		{ID: "2", Subject: "todo-b", State: client.TaskStateTodo},
		{ID: "3", Subject: "in-progress", State: client.TaskStateInProgress},
		{ID: "4", Subject: "review", State: client.TaskStateInReview},
		{ID: "5", Subject: "done", State: client.TaskStateDone},
	}
}

func TestUpdate_RightArrowFocusesNextColumn(t *testing.T) {
	m := buildModel(t, sampleTasks())
	if m.colIdx != 0 {
		t.Fatalf("expected starting colIdx=0, got %d", m.colIdx)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.colIdx != 1 {
		t.Errorf("expected colIdx=1 after right, got %d", m.colIdx)
	}
}

func TestUpdate_LeftAtFirstColumnIsClamped(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if m.colIdx != 0 {
		t.Errorf("expected colIdx=0 (clamped) after left at edge, got %d", m.colIdx)
	}
}

func TestUpdate_DownIncrementsRow(t *testing.T) {
	m := buildModel(t, sampleTasks())
	// Backlog has 2 cards (todo-a, todo-b).
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.rowIdx != 1 {
		t.Errorf("expected rowIdx=1 after down, got %d", m.rowIdx)
	}
}

func TestUpdate_DownPastEndClamps(t *testing.T) {
	m := buildModel(t, sampleTasks())
	for range 5 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	// Backlog has 2 cards → max rowIdx is 1.
	if m.rowIdx != 1 {
		t.Errorf("expected rowIdx=1 (clamped), got %d", m.rowIdx)
	}
}

func TestUpdate_RightArrowSkipsEmptyColumns(t *testing.T) {
	// Backlog has cards, In Progress + Review are empty, Done has one.
	// Pressing right from Backlog should land on Done, not the empty
	// columns between them.
	tasks := []client.Task{
		{ID: "a", State: client.TaskStateTodo},
		{ID: "z", State: client.TaskStateDone},
	}
	m := buildModel(t, tasks)
	if m.colIdx != 0 {
		t.Fatalf("setup: expected colIdx=0, got %d", m.colIdx)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.colIdx != 3 {
		t.Errorf("expected to skip empty In Progress + Review to Done (col 3), got col %d", m.colIdx)
	}
}

func TestUpdate_LeftArrowSkipsEmptyColumns(t *testing.T) {
	tasks := []client.Task{
		{ID: "a", State: client.TaskStateTodo},
		{ID: "z", State: client.TaskStateDone},
	}
	m := buildModel(t, tasks)
	// Move to Done (col 3) via the right-skip we just verified.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.colIdx != 3 {
		t.Fatalf("setup: expected colIdx=3, got %d", m.colIdx)
	}
	// Now left should skip back to Backlog (col 0).
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if m.colIdx != 0 {
		t.Errorf("expected to skip back to Backlog (col 0), got col %d", m.colIdx)
	}
}

func TestUpdate_ArrowAtEndOfNonEmptyColumnsIsNoop(t *testing.T) {
	// Only Backlog has tasks; right has nowhere to go.
	tasks := []client.Task{{ID: "a", State: client.TaskStateTodo}}
	m := buildModel(t, tasks)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.colIdx != 0 {
		t.Errorf("expected focus to stay on Backlog, got col %d", m.colIdx)
	}
}

func TestClampSelection_SeeksNonEmptyColumnAfterReload(t *testing.T) {
	// Start with tasks in Backlog and In Progress, focus In Progress,
	// then "reload" with In Progress emptied — clampSelection should hop
	// back to the still-populated Backlog.
	startTasks := []client.Task{
		{ID: "a", State: client.TaskStateTodo},
		{ID: "x", State: client.TaskStateInProgress},
	}
	m := buildModel(t, startTasks)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.colIdx != 1 {
		t.Fatalf("setup: expected colIdx=1 (In Progress), got %d", m.colIdx)
	}
	// Reload without the In Progress task — only Backlog has tasks now.
	emptied := []client.Task{{ID: "a", State: client.TaskStateTodo}}
	next, _ = m.Update(tasksLoadedMsg{tasks: emptied})
	m = next.(Model)
	if m.colIdx != 0 {
		t.Errorf("expected clampSelection to seek back to Backlog (col 0), got col %d", m.colIdx)
	}
}

func TestClampSelection_LoadIntoEmptyBacklogSeeksRight(t *testing.T) {
	// Initial load with Backlog empty should park focus on the first
	// non-empty column to the right rather than leaving the marker
	// invisible on col 0.
	tasks := []client.Task{
		{ID: "x", State: client.TaskStateInProgress},
	}
	m := buildModel(t, tasks)
	if m.colIdx != 1 {
		t.Errorf("expected initial focus on In Progress (col 1), got col %d", m.colIdx)
	}
}

func TestUpdate_ColumnSwitchResetsRowOverflow(t *testing.T) {
	m := buildModel(t, sampleTasks())
	// Move to row 1 in Backlog.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.rowIdx != 1 {
		t.Fatalf("setup: expected rowIdx=1 in Backlog, got %d", m.rowIdx)
	}
	// Right into In Progress (1 card).
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.rowIdx != 0 {
		t.Errorf("expected rowIdx clamped to 0 in In Progress (1 card), got %d", m.rowIdx)
	}
}

func TestUpdate_ShiftLMoveTriggersMoveCmd(t *testing.T) {
	var lastMove struct {
		id client.TaskID
		to client.TaskState
	}
	m := NewModel(newResolver(),
		func() ([]client.Task, error) { return sampleTasks(), nil },
		func(id client.TaskID, to client.TaskState) error {
			lastMove.id = id
			lastMove.to = to
			return nil
		},
		func(id client.TaskID) (TaskDetail, error) {
			return TaskDetail{Task: client.Task{ID: id}}, nil
		},
		func(id client.TaskID, body string) error { return nil },
		func(id client.TaskID, current string) tea.Cmd { return nil },
	)
	next, _ := m.Update(tasksLoadedMsg{tasks: sampleTasks()})
	m = next.(Model)

	// Backlog row 0 = task #1 (todo-a). shift+L moves to In Progress.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected move cmd, got nil")
	}
	// Execute the returned cmd; it should invoke the move func and
	// resolve to a cardMovedMsg.
	msg := cmd()
	if _, ok := msg.(cardMovedMsg); !ok {
		t.Fatalf("expected cardMovedMsg, got %T (%v)", msg, msg)
	}
	if lastMove.id != "1" || lastMove.to != client.TaskStateInProgress {
		t.Errorf("expected move id=1 to=in_progress, got id=%q to=%q",
			lastMove.id, lastMove.to)
	}
}

func TestUpdate_ShiftLAtRightEdgeSetsStatusNotMove(t *testing.T) {
	m := buildModel(t, sampleTasks())
	// Navigate to Done column (the rightmost).
	for range 3 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(Model)
	}
	if m.colIdx != 3 {
		t.Fatalf("setup: expected colIdx=3, got %d", m.colIdx)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = next.(Model)
	if cmd != nil {
		t.Errorf("expected no cmd at right edge, got %T", cmd())
	}
	if m.status == "" {
		t.Errorf("expected status message at edge, got empty")
	}
}

func TestUpdate_HelpModalLocksOutNavigation(t *testing.T) {
	m := buildModel(t, sampleTasks())
	// Open help.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = next.(Model)
	if !m.showHelp {
		t.Fatal("expected showHelp=true after ?")
	}
	// Right arrow must be inert.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.colIdx != 0 {
		t.Errorf("expected colIdx unchanged while help open, got %d", m.colIdx)
	}
	// Close with ?.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = next.(Model)
	if m.showHelp {
		t.Errorf("expected showHelp=false after toggle close")
	}
}

func TestUpdate_QuitReturnsTeaQuit(t *testing.T) {
	m := buildModel(t, sampleTasks())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd, got nil")
	}
	// tea.Quit returns the tea.QuitMsg when invoked.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestUpdate_ErrMsgSetsStatus(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(errMsg{err: errors.New("nope")})
	m = next.(Model)
	if !strings.Contains(m.status, "nope") {
		t.Errorf("expected error in status, got %q", m.status)
	}
}

func TestView_RendersBoardWhenNotInHelp(t *testing.T) {
	m := buildModel(t, sampleTasks())
	m.width, m.height = 120, 30
	out := m.View()
	for _, want := range []string{"Backlog", "In Progress", "Review", "Done"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in view, got:\n%s", want, out)
		}
	}
}

func TestView_RendersHelpWhenModalOpen(t *testing.T) {
	m := buildModel(t, sampleTasks())
	m.width, m.height = 120, 30
	m.showHelp = true
	out := m.View()
	if !strings.Contains(out, "toggle this help") {
		t.Errorf("expected help text in view, got:\n%s", out)
	}
}

func TestUpdate_EnterOpensDetail(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.mode != viewDetail {
		t.Errorf("expected viewDetail mode, got %d", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected loadDetailCmd, got nil")
	}
	// Run the cmd; should resolve to detailLoadedMsg with task id "1".
	msg := cmd()
	dl, ok := msg.(detailLoadedMsg)
	if !ok {
		t.Fatalf("expected detailLoadedMsg, got %T", msg)
	}
	if dl.detail.Task.ID != "1" {
		t.Errorf("expected task id 1, got %q", dl.detail.Task.ID)
	}
}

func TestUpdate_EscFromDetailReturnsToBoard(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.mode != viewDetail {
		t.Fatalf("setup: expected viewDetail, got %d", m.mode)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.mode != viewBoard {
		t.Errorf("expected viewBoard after esc, got %d", m.mode)
	}
}

func TestUpdate_DetailJKScrollsBody(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.detailScroll != 0 {
		t.Fatalf("setup: expected scroll 0, got %d", m.detailScroll)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.detailScroll != 1 {
		t.Errorf("expected scroll 1 after down, got %d", m.detailScroll)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if m.detailScroll != 0 {
		t.Errorf("expected scroll 0 after up, got %d", m.detailScroll)
	}
	// Up at top clamps, doesn't underflow.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if m.detailScroll != 0 {
		t.Errorf("expected scroll clamped at 0, got %d", m.detailScroll)
	}
}

func TestUpdate_DetailCOpensCommentInput(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m.width, m.height = 100, 30
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	if m.mode != viewCommentInput {
		t.Errorf("expected viewCommentInput, got %d", m.mode)
	}
	if !m.commentInput.Focused() {
		t.Errorf("expected comment textarea focused")
	}
}

func TestUpdate_CommentInputEscapesBackToDetail(t *testing.T) {
	m := buildModel(t, sampleTasks())
	m.width, m.height = 100, 30
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.mode != viewDetail {
		t.Errorf("expected viewDetail after esc from comment, got %d", m.mode)
	}
}

func TestUpdate_CommentInputCtrlSSubmits(t *testing.T) {
	var posted struct {
		id   client.TaskID
		body string
	}
	m := NewModel(newResolver(),
		func() ([]client.Task, error) { return sampleTasks(), nil },
		func(id client.TaskID, to client.TaskState) error { return nil },
		func(id client.TaskID) (TaskDetail, error) {
			return TaskDetail{Task: client.Task{ID: id}}, nil
		},
		func(id client.TaskID, body string) error {
			posted.id = id
			posted.body = body
			return nil
		},
		func(id client.TaskID, current string) tea.Cmd { return nil },
	)
	next, _ := m.Update(tasksLoadedMsg{tasks: sampleTasks()})
	m = next.(Model)
	m.width, m.height = 100, 30
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open detail for task #1
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	// Inject text into the textarea, then submit.
	m.commentInput.SetValue("looks good")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected commentCmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(commentSubmittedMsg); !ok {
		t.Fatalf("expected commentSubmittedMsg, got %T", msg)
	}
	if posted.id != "1" || posted.body != "looks good" {
		t.Errorf("expected post(id=1, body=looks good), got post(id=%q, body=%q)",
			posted.id, posted.body)
	}
}

func TestUpdate_CommentInputEmptyDoesNotSubmit(t *testing.T) {
	m := buildModel(t, sampleTasks())
	m.width, m.height = 100, 30
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	// Whitespace-only must not submit.
	m.commentInput.SetValue("   ")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if cmd != nil {
		t.Errorf("expected no submit cmd for empty comment, got %T", cmd())
	}
	if m.mode != viewCommentInput {
		t.Errorf("expected to remain in viewCommentInput, got %d", m.mode)
	}
	if m.status == "" {
		t.Errorf("expected status hint for empty submit")
	}
}

func TestUpdate_DescriptionEditedMsgPostsReload(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, cmd := m.Update(DescriptionEditedMsg{})
	m = next.(Model)
	if m.status == "" {
		t.Errorf("expected status set after description edit")
	}
	if cmd == nil {
		t.Fatal("expected reload cmd")
	}
	msg := cmd()
	if _, ok := msg.(detailLoadedMsg); !ok {
		t.Errorf("expected detailLoadedMsg from reload, got %T", msg)
	}
}

func TestUpdate_ErrorMsgPublic(t *testing.T) {
	m := buildModel(t, sampleTasks())
	next, _ := m.Update(ErrorMsg{Err: errors.New("editor exploded")})
	m = next.(Model)
	if !strings.Contains(m.status, "editor exploded") {
		t.Errorf("expected error in status, got %q", m.status)
	}
}
