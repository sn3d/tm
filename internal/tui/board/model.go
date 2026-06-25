package board

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
)

// LoadFunc returns the slice of tasks the board should show. It is called
// at startup and after every state-changing action so filters (--mine,
// --parent, --archived) apply uniformly across reloads. Any error is
// shown to the user as a footer message; the UI keeps running.
type LoadFunc func() ([]client.Task, error)

// MoveFunc transitions a task to a new state. Called by the move keys.
// On success the runtime triggers a reload via LoadFunc; on error the
// message surfaces in the footer.
type MoveFunc func(id client.TaskID, to client.TaskState) error

// LoadDetailFunc fetches the freshest version of a task plus its comments
// and direct child tasks. The detail view re-invokes this after every
// mutation (state move, new comment, description edit) so the on-screen
// detail always matches storage.
type LoadDetailFunc func(id client.TaskID) (TaskDetail, error)

// CommentFunc posts a new comment on a task. The detail view triggers it
// after the user submits the comment textarea.
type CommentFunc func(id client.TaskID, body string) error

// ArchiveFunc soft-archives a task. The board cascades to descendants by
// default — matching the `tm archive` CLI — so an archive on the board
// hides the same set of tasks the CLI would hide. The returned count is
// the number of cascaded descendants (0 if archive was a no-op or only
// the named task was affected); the footer reports it so users see
// whether children went with the parent. Errors surface in the footer.
type ArchiveFunc func(id client.TaskID) (cascaded int, err error)

// EditDescriptionFunc opens the user's $EDITOR pre-populated with the
// current description, then saves the result. Returned tea.Cmd lets the
// runtime suspend the alt-screen, run the editor in the parent terminal,
// and resume — see cmd/board for the wiring. The id+current pair is
// captured so the caller can build the exec.Cmd; the returned Cmd
// publishes an editDescriptionDoneMsg on completion.
type EditDescriptionFunc func(id client.TaskID, current string) tea.Cmd

// TaskDetail is the read-side aggregate for the detail view. Tasks +
// comments + subtasks come from three different client calls; the LoadDetail
// closure stitches them so the view sees one consistent snapshot.
type TaskDetail struct {
	Task     client.Task
	Comments []client.Comment
	Subtasks []client.Task
}

// viewMode is the top-level UI mode dispatch. Each mode owns its
// keybindings and View shape; the Update switch routes by mode so a
// keystroke means different things in different contexts without
// per-case guards. The help modal is orthogonal — it's a bool on the
// Model, not a mode, because it overlays whatever's underneath rather
// than replacing it.
type viewMode int

const (
	viewBoard viewMode = iota
	viewDetail
	viewCommentInput
)

// Model is the bubbletea state for `tm board`. Constructed by NewModel and
// passed to tea.NewProgram. Pure-function transitions live in Update; the
// View renders the current model to a string.
type Model struct {
	resolver        *tui.Resolver
	load            LoadFunc
	move            MoveFunc
	loadDetail      LoadDetailFunc
	addComment      CommentFunc
	editDescription EditDescriptionFunc
	archive         ArchiveFunc

	// Layout, updated on WindowSizeMsg. Zero on first frame; View tolerates
	// that by falling back to a sensible default width.
	width, height int

	// Source-of-truth task slice; reloaded into here on every refresh.
	tasks []client.Task
	// grouped[colIdx] = []client.Task in left-to-right column order.
	// Re-derived from tasks on every reload so the selection logic and
	// the renderer agree on what's where.
	grouped [][]client.Task

	// colIdx is the focused column (0..3); rowIdx is the focused card
	// within that column. Both clamp to valid ranges in Update so View
	// can index without bounds checks.
	colIdx, rowIdx int

	// mode dispatches Update + View. See viewMode for the meaning.
	mode viewMode

	// detail is the loaded snapshot when in viewDetail / viewCommentInput.
	// detailScroll is the row offset into the detail content (j/k scroll).
	detail       TaskDetail
	detailScroll int

	// commentInput is the textarea used for new comments in viewCommentInput
	// mode. Initialised lazily on the first `c` keystroke.
	commentInput textarea.Model

	// showHelp is the single modal flag. When true, navigation keys are
	// inert; only the help-close keys do anything. See HANDLING_KEYS in
	// Update for the input-lockout pattern.
	showHelp bool

	// status is the footer line — error messages, last action confirmation.
	// Cleared on every successful Update where it would otherwise stale.
	status string
}

// NewModel constructs the initial model. The caller supplies the resolver
// (built from cfg.Styling) and the load/move closures (built around the
// configured filters and the *client.Client). loadDetail, addComment and
// editDescription power the detail view; any of the three may be nil for
// callers wanting a read-only board with no detail view (tests do this).
func NewModel(
	resolver *tui.Resolver,
	load LoadFunc,
	move MoveFunc,
	loadDetail LoadDetailFunc,
	addComment CommentFunc,
	editDescription EditDescriptionFunc,
	archive ArchiveFunc,
) Model {
	return Model{
		resolver:        resolver,
		load:            load,
		move:            move,
		loadDetail:      loadDetail,
		addComment:      addComment,
		editDescription: editDescription,
		archive:         archive,
	}
}

// keyBinding pairs a set of key strings with their help text. The Update
// switch and the help modal both read from this slice so there's only one
// place to add a new key.
type keyBinding struct {
	keys []string
	help string
}

// boardKeys is the registry for the board view. The leftmost key in each
// entry is the canonical name shown in help; the rest are aliases.
var boardKeys = []keyBinding{
	{keys: []string{"←", "h"}, help: "focus previous column"},
	{keys: []string{"→", "l"}, help: "focus next column"},
	{keys: []string{"↑", "k"}, help: "select card above"},
	{keys: []string{"↓", "j"}, help: "select card below"},
	{keys: []string{"enter"}, help: "open task details"},
	{keys: []string{"shift+L"}, help: "move card to next column"},
	{keys: []string{"shift+H"}, help: "move card to previous column"},
	{keys: []string{"a"}, help: "archive card (cascades to descendants)"},
	{keys: []string{"r"}, help: "reload"},
	{keys: []string{"?"}, help: "toggle this help"},
	{keys: []string{"q", "ctrl+c"}, help: "quit"},
}

// detailKeys is the registry for the detail view.
var detailKeys = []keyBinding{
	{keys: []string{"←", "esc"}, help: "back to board"},
	{keys: []string{"↑", "k"}, help: "scroll up"},
	{keys: []string{"↓", "j"}, help: "scroll down"},
	{keys: []string{"c"}, help: "add a comment"},
	{keys: []string{"e"}, help: "edit description in $EDITOR"},
	{keys: []string{"shift+L"}, help: "move task to next column"},
	{keys: []string{"shift+H"}, help: "move task to previous column"},
	{keys: []string{"a"}, help: "archive task (cascades to descendants)"},
	{keys: []string{"r"}, help: "reload"},
	{keys: []string{"?"}, help: "toggle this help"},
	{keys: []string{"q", "ctrl+c"}, help: "quit"},
}

// commentInputKeys is the registry for the comment-input modal.
var commentInputKeys = []keyBinding{
	{keys: []string{"ctrl+s", "ctrl+enter"}, help: "submit comment"},
	{keys: []string{"esc"}, help: "discard and return"},
}

// Messages — the only mechanism by which async results re-enter the model.

type tasksLoadedMsg struct {
	tasks []client.Task
}

type errMsg struct{ err error }

type cardMovedMsg struct{}

type detailLoadedMsg struct {
	detail TaskDetail
}

type commentSubmittedMsg struct{}

type editDescriptionDoneMsg struct{}

// taskArchivedMsg flows back from archiveCmd. The Update handler treats
// this like a successful state mutation: set status, reload the board,
// and (if we were in detail/comment mode) pop back to the board because
// the archived task is no longer in the default view.
type taskArchivedMsg struct{ cascaded int }

// ErrorMsg is the public error signal — closures in cmd/board surface
// errors by returning this so the board's Update can route them through
// the same status-line plumbing as internal errors.
type ErrorMsg struct{ Err error }

// DescriptionEditedMsg is the public success signal from the description
// editor closure. The board's Update treats it as editDescriptionDoneMsg.
type DescriptionEditedMsg struct{}

// Init is the first command tea runs. We kick off the initial task load
// so the UI starts populated.
func (m Model) Init() tea.Cmd {
	return loadCmd(m.load)
}

func loadCmd(load LoadFunc) tea.Cmd {
	return func() tea.Msg {
		tasks, err := load()
		if err != nil {
			return errMsg{err}
		}
		return tasksLoadedMsg{tasks}
	}
}

func moveCmd(move MoveFunc, id client.TaskID, to client.TaskState) tea.Cmd {
	return func() tea.Msg {
		if err := move(id, to); err != nil {
			return errMsg{err}
		}
		return cardMovedMsg{}
	}
}

func loadDetailCmd(load LoadDetailFunc, id client.TaskID) tea.Cmd {
	return func() tea.Msg {
		d, err := load(id)
		if err != nil {
			return errMsg{err}
		}
		return detailLoadedMsg{detail: d}
	}
}

func commentCmd(post CommentFunc, id client.TaskID, body string) tea.Cmd {
	return func() tea.Msg {
		if err := post(id, body); err != nil {
			return errMsg{err}
		}
		return commentSubmittedMsg{}
	}
}

func archiveCmd(archive ArchiveFunc, id client.TaskID) tea.Cmd {
	return func() tea.Msg {
		cascaded, err := archive(id)
		if err != nil {
			return errMsg{err}
		}
		return taskArchivedMsg{cascaded: cascaded}
	}
}

// Update is the heart of the model. Every message routes through here; the
// returned Model is the next state, and the returned Cmd (if any) schedules
// async follow-up work. Mode-agnostic messages (size, errors, reload
// results) are handled at the top. KeyMsg dispatches to a per-mode handler
// so a keystroke can mean different things in board vs. detail vs. comment
// input without per-case guards.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.commentInput.Focused() {
			m.commentInput.SetWidth(commentTextareaWidth(m.width))
		}
		return m, nil

	case tasksLoadedMsg:
		m.tasks = msg.tasks
		m.grouped = groupByColumn(msg.tasks)
		m.status = ""
		m.clampSelection()
		return m, nil

	case cardMovedMsg:
		m.status = "moved"
		// In detail mode we also need the detail snapshot to reflect the
		// new state — re-fetch alongside the board reload.
		if m.mode == viewDetail || m.mode == viewCommentInput {
			return m, tea.Batch(loadCmd(m.load), loadDetailCmd(m.loadDetail, m.detail.Task.ID))
		}
		return m, loadCmd(m.load)

	case detailLoadedMsg:
		m.detail = msg.detail
		m.status = ""
		return m, nil

	case commentSubmittedMsg:
		m.mode = viewDetail
		m.commentInput.Reset()
		m.commentInput.Blur()
		m.status = "comment added"
		return m, loadDetailCmd(m.loadDetail, m.detail.Task.ID)

	case editDescriptionDoneMsg, DescriptionEditedMsg:
		m.status = "description updated"
		return m, loadDetailCmd(m.loadDetail, m.detail.Task.ID)

	case taskArchivedMsg:
		// Archived tasks are hidden from the default board view, so pop
		// back to the board regardless of where the action was triggered
		// — leaving the detail view open on a task the next reload will
		// drop would be confusing. Only touch the textarea if it was
		// actually initialised (entering comment-input mode is what
		// constructs it) — calling Reset on a zero-value textarea
		// panics inside its viewport.
		if msg.cascaded > 0 {
			m.status = fmt.Sprintf("archived (+ %d descendants)", msg.cascaded)
		} else {
			m.status = "archived"
		}
		if m.mode == viewCommentInput {
			m.commentInput.Reset()
			m.commentInput.Blur()
		}
		m.mode = viewBoard
		m.detail = TaskDetail{}
		m.detailScroll = 0
		return m, loadCmd(m.load)

	case errMsg:
		m.status = msg.err.Error()
		return m, nil

	case ErrorMsg:
		m.status = msg.Err.Error()
		return m, nil

	case tea.KeyMsg:
		// Help overlays every mode. The single lockout branch handles
		// both opening (any key except the closers is inert while open)
		// and closing.
		if m.showHelp {
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
			}
			return m, nil
		}
		switch m.mode {
		case viewBoard:
			return m.updateBoard(msg)
		case viewDetail:
			return m.updateDetail(msg)
		case viewCommentInput:
			return m.updateCommentInput(msg)
		}
	}
	return m, nil
}

// updateBoard handles keys when the kanban board is the active view.
func (m Model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "r":
		return m, loadCmd(m.load)
	case "left", "h":
		m.focusColumn(-1)
		return m, nil
	case "right", "l":
		m.focusColumn(+1)
		return m, nil
	case "up", "k":
		m.focusRow(m.rowIdx - 1)
		return m, nil
	case "down", "j":
		m.focusRow(m.rowIdx + 1)
		return m, nil
	case "L":
		return m.moveSelected(+1)
	case "H":
		return m.moveSelected(-1)
	case "a":
		sel, ok := m.selected()
		if !ok || m.archive == nil {
			return m, nil
		}
		return m, archiveCmd(m.archive, sel.ID)
	case "enter":
		sel, ok := m.selected()
		if !ok || m.loadDetail == nil {
			return m, nil
		}
		m.mode = viewDetail
		m.detail = TaskDetail{Task: sel}
		m.detailScroll = 0
		return m, loadDetailCmd(m.loadDetail, sel.ID)
	}
	return m, nil
}

// updateDetail handles keys when the detail view is active. Move keys still
// work here so users can shift state without bouncing back to the board.
func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "esc", "left", "h":
		m.mode = viewBoard
		m.detail = TaskDetail{}
		m.detailScroll = 0
		return m, nil
	case "r":
		return m, loadDetailCmd(m.loadDetail, m.detail.Task.ID)
	case "up", "k":
		if m.detailScroll > 0 {
			m.detailScroll--
		}
		return m, nil
	case "down", "j":
		m.detailScroll++
		return m, nil
	case "c":
		if m.addComment == nil {
			return m, nil
		}
		m.mode = viewCommentInput
		m.commentInput = newCommentTextarea(commentTextareaWidth(m.width))
		return m, textarea.Blink
	case "e":
		if m.editDescription == nil {
			return m, nil
		}
		return m, m.editDescription(m.detail.Task.ID, m.detail.Task.Description)
	case "L":
		return m.moveDetail(+1)
	case "H":
		return m.moveDetail(-1)
	case "a":
		if m.archive == nil || m.detail.Task.ID == "" {
			return m, nil
		}
		return m, archiveCmd(m.archive, m.detail.Task.ID)
	}
	return m, nil
}

// updateCommentInput handles keys while the comment textarea is focused.
// Most keys feed straight into the textarea; we capture submit (ctrl+s,
// ctrl+enter) and cancel (esc).
func (m Model) updateCommentInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = viewDetail
		m.commentInput.Reset()
		m.commentInput.Blur()
		return m, nil
	case "ctrl+s", "ctrl+enter":
		body := strings.TrimSpace(m.commentInput.Value())
		if body == "" {
			m.status = "empty comment — type something or esc to cancel"
			return m, nil
		}
		return m, commentCmd(m.addComment, m.detail.Task.ID, body)
	}
	var cmd tea.Cmd
	m.commentInput, cmd = m.commentInput.Update(msg)
	return m, cmd
}

// moveDetail is the detail-view equivalent of moveSelected — same target
// math, but the move applies to the currently-detailed task rather than
// the board's selection cursor.
func (m Model) moveDetail(dir int) (tea.Model, tea.Cmd) {
	t := m.detail.Task
	if t.ID == "" {
		return m, nil
	}
	// Find which column the current state belongs to.
	from := -1
	for i, col := range columns {
		for _, s := range col.States {
			if s == t.State {
				from = i
				break
			}
		}
	}
	if from < 0 {
		return m, nil
	}
	target := from + dir
	if target < 0 || target >= len(columns) {
		m.status = "already at edge"
		return m, nil
	}
	return m, moveCmd(m.move, t.ID, columns[target].Header)
}

// focusColumn moves focus by `step` columns (typically ±1), skipping any
// empty columns along the way so the focus marker never disappears into a
// lane with no cards. If every column in the requested direction is empty
// the cursor stays where it is — emptiness shouldn't push the user past
// the edge.
//
// rowIdx is clamped to the new column's card count so an inherited row
// from a taller previous column doesn't index out of bounds.
func (m *Model) focusColumn(step int) {
	if step == 0 {
		return
	}
	dir := 1
	if step < 0 {
		dir = -1
	}
	idx := m.colIdx + dir
	for idx >= 0 && idx < len(columns) {
		if len(m.grouped[idx]) > 0 {
			m.colIdx = idx
			if m.rowIdx >= len(m.grouped[m.colIdx]) {
				m.rowIdx = max(0, len(m.grouped[m.colIdx])-1)
			}
			return
		}
		idx += dir
	}
	// No non-empty column in this direction — leave focus untouched so
	// the user can tell the input was a no-op.
}

// focusRow moves within the current column, clamping to the column's card
// count. Selection wraps neither up nor down — at the top, `up` is a no-op.
func (m *Model) focusRow(idx int) {
	cards := m.grouped[m.colIdx]
	if len(cards) == 0 {
		m.rowIdx = 0
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cards) {
		idx = len(cards) - 1
	}
	m.rowIdx = idx
}

// clampSelection keeps the focused position valid after reloads might have
// changed the underlying card counts. If the current column went empty
// (e.g. its last card was just moved out), seek rightward then leftward
// for a column with at least one card so the arrow marker stays visible.
// When every column is empty there's nothing to focus and we leave the
// indices at 0/0 — View suppresses the marker on an empty board.
func (m *Model) clampSelection() {
	if m.colIdx < 0 || m.colIdx >= len(m.grouped) {
		m.colIdx = 0
	}
	if len(m.grouped[m.colIdx]) == 0 {
		for i := m.colIdx + 1; i < len(m.grouped); i++ {
			if len(m.grouped[i]) > 0 {
				m.colIdx = i
				m.rowIdx = 0
				return
			}
		}
		for i := m.colIdx - 1; i >= 0; i-- {
			if len(m.grouped[i]) > 0 {
				m.colIdx = i
				m.rowIdx = 0
				return
			}
		}
		m.rowIdx = 0
		return
	}
	if m.rowIdx >= len(m.grouped[m.colIdx]) {
		m.rowIdx = max(0, len(m.grouped[m.colIdx])-1)
	}
}

// moveSelected transitions the focused card to a state in the neighbour
// column. dir=+1 moves right; -1 moves left. The target state is the
// neighbour's header state — moving to In Progress sets `in_progress`,
// not `blocked`. Reassignment is intentionally out of scope (see the
// design doc).
func (m Model) moveSelected(dir int) (tea.Model, tea.Cmd) {
	sel, ok := m.selected()
	if !ok {
		return m, nil
	}
	target := m.colIdx + dir
	if target < 0 || target >= len(columns) {
		m.status = "already at edge"
		return m, nil
	}
	to := columns[target].Header
	return m, moveCmd(m.move, sel.ID, to)
}

func (m Model) selected() (client.Task, bool) {
	if m.colIdx >= len(m.grouped) {
		return client.Task{}, false
	}
	cards := m.grouped[m.colIdx]
	if m.rowIdx >= len(cards) {
		return client.Task{}, false
	}
	return cards[m.rowIdx], true
}

// View renders the current model. Called by tea after every Update; we
// always produce a complete frame. The mode field decides which inner
// renderer runs; help and the comment textarea overlay whatever's
// underneath via lipgloss.Place.
func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 112 // 4 × 28 fallback for the very first frame
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	var frame string
	switch m.mode {
	case viewBoard, viewCommentInput:
		// viewCommentInput overlays the modal on top of the detail view,
		// which is also where it returns to on submit/cancel. While the
		// modal is open the underlying screen is still the detail.
		if m.mode == viewCommentInput {
			frame = renderDetail(m.detail, m.resolver, width, height, m.detailScroll) + "\n" + m.renderFooter()
		} else {
			var selectedID client.TaskID
			if sel, ok := m.selected(); ok {
				selectedID = sel.ID
			}
			frame = render(m.tasks, m.resolver, width, selectedID) + "\n" + m.renderFooter()
		}
	case viewDetail:
		frame = renderDetail(m.detail, m.resolver, width, height, m.detailScroll) + "\n" + m.renderFooter()
	}

	if m.showHelp {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			renderHelpModal(m.helpKeys()), lipgloss.WithWhitespaceChars(" "))
	}
	if m.mode == viewCommentInput {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			renderCommentModal(m.commentInput.View()), lipgloss.WithWhitespaceChars(" "))
	}
	return frame
}

// helpKeys returns the registry to show in the help modal, picked by mode
// so the keys list always matches the keys that actually work right now.
func (m Model) helpKeys() []keyBinding {
	switch m.mode {
	case viewDetail:
		return detailKeys
	case viewCommentInput:
		return commentInputKeys
	default:
		return boardKeys
	}
}

func (m Model) renderFooter() string {
	var hint string
	switch m.mode {
	case viewBoard:
		hint = "←→/hl columns · ↑↓/jk rows · enter detail · shift+L/H move · a archive · ? help · q quit"
	case viewDetail:
		hint = "←/esc back · ↑↓ scroll · c comment · e edit · shift+L/H move · a archive · ? help · q quit"
	case viewCommentInput:
		hint = "ctrl+s submit · esc cancel"
	}
	keys := dimStyle.Render(hint)
	if m.status == "" {
		return keys
	}
	return keys + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.status)
}

// renderHelpModal builds the help modal from a key-binding registry. The
// single source-of-truth pattern keeps keymap + help text aligned by
// construction.
func renderHelpModal(keys []keyBinding) string {
	var rows []string
	for _, kb := range keys {
		rows = append(rows, fmt.Sprintf("%-18s  %s", strings.Join(kb.keys, " / "), kb.help))
	}
	body := strings.Join(rows, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 2).
		Render(titleStyle.Render("tm board — keys") + "\n\n" + body)
}

// ErrNoSelection is surfaced by handlers when an action requires a
// selected card but none exists (e.g. an empty board).
var ErrNoSelection = errors.New("no card selected")
