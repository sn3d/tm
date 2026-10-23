// Package board renders a kanban-style view of tasks grouped into four
// Jira-style columns: Backlog, In Progress, Review, Done. The collapse
// from the 7 internal states keeps the layout legible on a typical
// terminal — 7 columns would force each card down to ~12 chars.
//
// Render is pure: it takes a slice of tasks plus a Resolver and produces
// the rendered string. The cmd layer owns terminal-width detection and
// filtering, so this package stays unit-testable without a TTY.
package board

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
)

// Column groups task states into one visual lane.
type Column struct {
	Title  string
	Header client.TaskState // shown as "main" state — cards in this state suppress the state badge
	States []client.TaskState
}

// columns is the canonical Jira-style mapping. Order is left-to-right.
// The "main" state is the one whose badge is suppressed on cards in that
// column (e.g. a `todo` card in Backlog doesn't repeat 📋 todo — but a
// `blocked` card in In Progress shows ⛔ blocked so the off-column state
// stays visible).
var columns = []Column{
	{Title: "Backlog", Header: client.TaskStateTodo,
		States: []client.TaskState{client.TaskStateDraft, client.TaskStateTodo}},
	{Title: "In Progress", Header: client.TaskStateInProgress,
		States: []client.TaskState{client.TaskStateInProgress, client.TaskStateBlocked}},
	{Title: "Review", Header: client.TaskStateInReview,
		States: []client.TaskState{client.TaskStateInReview}},
	{Title: "Done", Header: client.TaskStateDone,
		States: []client.TaskState{client.TaskStateDone, client.TaskStateCancelled}},
}

// cardSlotLines is the vertical space one card occupies in a column,
// including the blank separator that precedes every card after the first.
// A card body is at most 3 lines (id / agent / labels); the +1 accounts for
// the separator. Windowing math treats every card as this fixed height so
// the visible-count is stable regardless of which cards carry a labels row.
const cardSlotLines = 4

// columnHeaderLines is the number of lines columnBody emits before the first
// card: the "Title (n)" header plus a blank separator.
const columnHeaderLines = 2

// visibleCardCapacity is how many cards fit in one column for the given
// terminal height. The budget must leave room for everything columnBody and
// View draw around the cards, or a mid-scrolled column pushes its bottom
// border (and the footer) off-screen — the exact overflow this windowing
// fixes. Reserved, top to bottom:
//
//   - borderRows (2): the column's ╭ top and ╰ bottom frame rows.
//   - columnHeaderLines (2): the "Title (n)" header + its blank separator.
//   - hintRows (3): worst case a scrolled column shows a top "↑ N more"
//     hint (1 line) AND a bottom "↓ N more" hint (blank + line = 2). We
//     always reserve both so scrolling never overflows, at the cost of one
//     unused row in the columns that happen to show only one hint.
//   - footerReserve (2): View appends the footer — one hint line that can
//     wrap to two on narrow terminals, plus a status line when set. Two is
//     the common ceiling; a wrapped hint on a very narrow terminal can still
//     nudge past it, but that degrades to one clipped card, not a broken grid.
//
// The remaining rows hold cards. Each card is at most 3 lines and every card
// after the first adds a 1-line separator, so C cards occupy 4C-1 rows — the
// slot model counts 4C and the spare row absorbs rounding. Floored at 1 so a
// tiny terminal still shows the selected card rather than nothing.
func visibleCardCapacity(height int) int {
	const (
		borderRows    = 2 // ╭ top + ╰ bottom
		hintRows      = 3 // top hint (1) + bottom hint (blank + line = 2)
		footerReserve = 2 // wrapped hint line + optional status line
	)
	avail := height - borderRows - columnHeaderLines - hintRows - footerReserve
	// +1 recovers the missing separator before the first card (4C-1 rows for
	// C cards), so a column exactly filling `avail` isn't under-counted.
	capacity := (avail + 1) / cardSlotLines
	if capacity < 1 {
		return 1
	}
	return capacity
}

// render produces the kanban view as a single string. totalWidth is the
// terminal column count; the function divides it into 4 lanes minus
// borders/padding. totalHeight bounds each column to a scrolling window of
// cards — columns taller than the terminal show only capacity cards starting
// at scrollOff[colIdx], with "↑ N more" / "↓ N more" hints marking the
// hidden remainder. Tasks are routed into columns by State; any task whose
// state doesn't match any column (shouldn't happen with the canonical 7
// states) is dropped silently rather than rendered into a fallback column —
// surfacing that case is a job for tm list, not the kanban view.
//
// Pure: no terminal I/O, no global state. Called by Model.View.
func render(tasks []client.Task, resolver *tui.Resolver, totalWidth, totalHeight int, selectedID client.TaskID, scrollOff []int) string {
	colW := columnWidth(totalWidth)
	capacity := visibleCardCapacity(totalHeight)
	grouped := groupByColumn(tasks)
	bodies := make([]string, len(columns))
	maxLines := 0
	for i, col := range columns {
		off := 0
		if i < len(scrollOff) {
			off = scrollOff[i]
		}
		bodies[i] = columnBody(col, grouped[i], resolver, colW, selectedID, off, capacity)
		if n := strings.Count(bodies[i], "\n") + 1; n > maxLines {
			maxLines = n
		}
	}
	// Pad to a target line count that depends only on capacity, not on the
	// current contents. A full window's body is columnHeaderLines + top hint
	// (1) + cards (4·capacity − 1) + bottom hint (2). Because columnBody now
	// always reserves the hint rows, this is the tallest any column can be, so
	// padding every column to it makes the whole board a constant height —
	// the footer below it no longer shifts as the user scrolls hints in and
	// out. maxLines is a floor guard in case a body somehow exceeds the
	// computed target (it shouldn't, but padding never truncates).
	target := max(columnHeaderLines+1+(cardSlotLines*capacity-1)+2, maxLines)
	// Pad each column body to the same line count BEFORE wrapping it in
	// the bordered style. JoinHorizontal does not equalise column heights,
	// and styling a column to a target Height re-wraps any pre-existing
	// border characters, so we hand it pre-padded line-matched bodies and
	// let lipgloss only draw a single border around each.
	rendered := make([]string, len(columns))
	for i, body := range bodies {
		padded := padLines(body, target)
		rendered[i] = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1).
			Width(colW).
			Render(padded)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// padLines appends empty lines until the body reaches the requested line
// count. Used to equalise column heights so the closing `╰` row lines up
// across all columns.
func padLines(body string, target int) string {
	have := strings.Count(body, "\n") + 1
	if have >= target {
		return body
	}
	return body + strings.Repeat("\n", target-have)
}

// columnWidth divides totalWidth across the 4 columns. Lipgloss's Style.Width
// sets the CONTENT width — borders (2 cells per column: ╭...╮) and horizontal
// padding (2 cells: " ... ") are added on top. So each rendered column
// occupies colW + columnFrameOverhead cells, and we have to subtract the
// frame overhead from each column's share or the rightmost column clips off
// the screen.
func columnWidth(totalWidth int) int {
	const (
		// columnFrameOverhead is the per-column non-content cell count
		// lipgloss adds around our Width-set value. 2 cells of border
		// (one each side) + 2 cells of horizontal padding (Padding(0, 1)).
		columnFrameOverhead = 4
		minColumnWidth      = 24
		defaultWidth        = 28
	)
	if totalWidth <= 0 {
		return defaultWidth
	}
	per := totalWidth/len(columns) - columnFrameOverhead
	if per < minColumnWidth {
		return minColumnWidth
	}
	return per
}

// groupByColumn buckets tasks by column. Each bucket is sorted oldest-
// CreatedAt first so the column reads chronologically — top-of-column is
// the longest-waiting card, which matches "what's been sitting here?"
// reading intent.
func groupByColumn(tasks []client.Task) [][]client.Task {
	state2col := make(map[client.TaskState]int, 8)
	for i, col := range columns {
		for _, s := range col.States {
			state2col[s] = i
		}
	}
	buckets := make([][]client.Task, len(columns))
	for _, t := range tasks {
		idx, ok := state2col[t.State]
		if !ok {
			continue
		}
		buckets[idx] = append(buckets[idx], t)
	}
	for i := range buckets {
		sort.SliceStable(buckets[i], func(a, b int) bool {
			return buckets[i][a].CreatedAt.Before(buckets[i][b].CreatedAt)
		})
	}
	return buckets
}

var (
	idStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	// selectionStyle paints the focus arrow and the ID of the currently
	// focused card. Bright yellow (ANSI 11) matches the warm amber the
	// Homebrew "Update available!" line uses — visible against both light
	// and dark terminal themes without competing with the per-label
	// foreground colours already on the card.
	selectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
)

// selectionMarker is what we prepend to the focused card's first line —
// width-stable, single cell, no background painting needed. Non-focused
// cards reserve the same cell with a space so subsequent lines (agent,
// labels) line up vertically with the ID line.
const (
	selectionMarker = "→"
	markerGutter    = "  " // 2 cells: marker + 1 space
)

// columnBody produces just the inside of one column — header, blank
// separator, then a window of at most `capacity` cards starting at `off`.
// selectedID, when non-empty, prefixes the matching card's first line with a
// bold marker. Non-selected cards reserve the same two-cell gutter with
// whitespace so every card aligns vertically. When cards are hidden above or
// below the window, an "↑ N more" / "↓ N more" hint replaces the top/bottom
// of the visible slice so the user knows the column continues. The caller
// wraps this in the bordered style after equalising line counts across
// columns.
func columnBody(col Column, tasks []client.Task, resolver *tui.Resolver, width int, selectedID client.TaskID, off, capacity int) string {
	header := titleStyle.Render(col.Title) + " " + dimStyle.Render(fmt.Sprintf("(%d)", len(tasks)))
	lines := []string{header, ""}

	// Clamp the window to the available cards. off is trusted from the
	// Model's scroll tracking, but a shrunk terminal or a reload can leave
	// it past the end. Bound start at len-capacity (the same maxOff syncScroll
	// enforces) rather than len, so a stale offset degrades to the last full
	// window of cards instead of an empty column showing only an "↑ N more"
	// hint.
	maxOff := max(len(tasks)-capacity, 0)
	start := min(max(off, 0), maxOff)
	end := min(start+capacity, len(tasks))

	// The top and bottom "↑/↓ N more" hints each occupy a fixed row (the
	// bottom hint also carries a leading blank). We ALWAYS reserve those rows
	// — emitting a blank placeholder when there's nothing hidden — so a
	// column's height doesn't change as the user scrolls a hint into or out
	// of view. Without this the board's total height would fluctuate and the
	// footer below it would jump.
	if start > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ↑ %d more", start)))
	} else {
		lines = append(lines, "")
	}
	for i := start; i < end; i++ {
		if i > start {
			lines = append(lines, "")
		}
		t := tasks[i]
		selected := !selectedID.IsZero() && t.ID == selectedID
		// Card content area shrinks by the gutter width so the marker
		// doesn't push the right border out.
		card := renderCard(t, col.Header, resolver, width-4-len(markerGutter), selected)
		card = applyMarker(card, selected)
		lines = append(lines, card)
	}
	if end < len(tasks) {
		lines = append(lines, "", dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(tasks)-end)))
	} else {
		lines = append(lines, "", "")
	}
	return strings.Join(lines, "\n")
}

// applyMarker prefixes the first line of card with the selection marker
// (or its gutter equivalent). Subsequent lines get whitespace of the same
// width so the rest of the card body indents consistently. Multi-line
// cards never have the marker repeat — it lives only on the ID line.
func applyMarker(card string, selected bool) string {
	prefix := strings.Repeat(" ", lipgloss.Width(selectionMarker)) + " "
	if selected {
		prefix = selectionStyle.Render(selectionMarker) + " "
	}
	lines := strings.Split(card, "\n")
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
		} else {
			lines[i] = strings.Repeat(" ", lipgloss.Width(selectionMarker)+1) + line
		}
	}
	return strings.Join(lines, "\n")
}

// renderCard formats one task. Layout:
//
//	#ID  subject
//	    @agent  #parent   (parent appended when set; either may be missing)
//	    labels  [state-badge if off-column]
//
// The labels row is omitted when there are neither labels nor an
// off-column state to surface. When selected is true the ID adopts the
// selection style (yellow/amber) so the focused card stands out from the
// list at a glance, matching the arrow marker the caller prepends.
func renderCard(t client.Task, columnHeader client.TaskState, resolver *tui.Resolver, contentWidth int, selected bool) string {
	idText := tui.RefBare(t.ID)
	subjectRoom := contentWidth - lipgloss.Width(idText) - 2
	if subjectRoom < 1 {
		subjectRoom = 1
	}
	idCellStyle := idStyle
	if selected {
		idCellStyle = selectionStyle
	}
	idLine := idCellStyle.Render(idText) + "  " + truncate(t.Subject, subjectRoom)

	// The second row combines assignee and parent ID. Both are dim grey
	// because they're contextual metadata for the card's own ID/subject.
	// Either may be absent: an unassigned top-level task shows just "—",
	// an unassigned child shows "— #<parent>", an assigned top-level
	// shows "@alice".
	var agentLine string
	if t.AssignedAgent != "" {
		agentLine = dimStyle.Render("    @") +
			lipgloss.NewStyle().MaxWidth(contentWidth-5).Render(dimStyle.Render(t.AssignedAgent))
	} else {
		agentLine = dimStyle.Render("    —")
	}
	if !t.ParentID.IsZero() {
		agentLine += dimStyle.Render("  ") + dimStyle.Render(tui.RefBare(t.ParentID))
	}

	parts := []string{idLine, agentLine}
	var bottom []string
	// Cards use the icon-less label/state forms — multi-cell emoji
	// (📝, ⛔, ✅, ...) render with inconsistent widths in some terminals
	// (iTerm with certain Unicode-version settings, particularly), which
	// breaks the column grid. The column header already conveys the
	// state, and label colour alone identifies the label. tm list and tm
	// get keep the icons since they don't share this column constraint.
	if len(t.Labels) > 0 {
		bottom = append(bottom, resolver.LabelsText(t.Labels))
	}
	if t.State != "" && t.State != columnHeader {
		bottom = append(bottom, resolver.StateText(t.State))
	}
	if len(bottom) > 0 {
		joined := strings.Join(bottom, "  ")
		clipped := lipgloss.NewStyle().MaxWidth(contentWidth - 4).Render(joined)
		parts = append(parts, "    "+clipped)
	}
	return strings.Join(parts, "\n")
}

// truncate clips to max runes with an ellipsis, accounting for emojis that
// occupy two display cells (lipgloss.Width handles that).
func truncate(s string, max int) string {
	if max <= 1 {
		return s
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	if max <= 3 {
		return string(r[:min(max, len(r))])
	}
	for cut := len(r); cut > 0; cut-- {
		candidate := string(r[:cut]) + "…"
		if lipgloss.Width(candidate) <= max {
			return candidate
		}
	}
	return "…"
}
