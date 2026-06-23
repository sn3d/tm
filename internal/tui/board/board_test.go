package board

import (
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
)

func init() {
	// Force ANSI on so we can assert wrapping without a TTY.
	color.NoColor = false
}

func newResolver() *tui.Resolver {
	return tui.NewResolver(client.Styling{})
}

func TestGroupByColumn_CollapsesSevenStatesIntoFour(t *testing.T) {
	tasks := []client.Task{
		{ID: "d", State: client.TaskStateDraft},
		{ID: "t", State: client.TaskStateTodo},
		{ID: "p", State: client.TaskStateInProgress},
		{ID: "b", State: client.TaskStateBlocked},
		{ID: "r", State: client.TaskStateInReview},
		{ID: "x", State: client.TaskStateDone},
		{ID: "c", State: client.TaskStateCancelled},
	}
	got := groupByColumn(tasks)
	if len(got) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(got))
	}
	want := map[int][]string{
		0: {"d", "t"}, // Backlog: draft + todo
		1: {"p", "b"}, // In Progress: in_progress + blocked
		2: {"r"},      // Review: in_review
		3: {"x", "c"}, // Done: done + cancelled
	}
	for i, ids := range want {
		if len(got[i]) != len(ids) {
			t.Errorf("column %d: got %d tasks, want %d", i, len(got[i]), len(ids))
			continue
		}
		seen := map[string]bool{}
		for _, tsk := range got[i] {
			seen[tsk.ID] = true
		}
		for _, id := range ids {
			if !seen[id] {
				t.Errorf("column %d missing task %q", i, id)
			}
		}
	}
}

func TestGroupByColumn_SortsOldestFirst(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	tasks := []client.Task{
		{ID: "newer", State: client.TaskStateTodo, CreatedAt: base.Add(2 * time.Hour)},
		{ID: "older", State: client.TaskStateTodo, CreatedAt: base},
		{ID: "mid", State: client.TaskStateTodo, CreatedAt: base.Add(time.Hour)},
	}
	got := groupByColumn(tasks)
	backlog := got[0]
	if backlog[0].ID != "older" || backlog[1].ID != "mid" || backlog[2].ID != "newer" {
		t.Errorf("expected [older mid newer], got [%s %s %s]",
			backlog[0].ID, backlog[1].ID, backlog[2].ID)
	}
}

func TestGroupByColumn_DropsUnknownState(t *testing.T) {
	tasks := []client.Task{
		{ID: "ok", State: client.TaskStateTodo},
		{ID: "weird", State: client.TaskState("invented")},
	}
	got := groupByColumn(tasks)
	for i, bucket := range got {
		for _, tsk := range bucket {
			if tsk.ID == "weird" {
				t.Errorf("unknown state should be dropped, found in column %d", i)
			}
		}
	}
}

func TestColumnWidth(t *testing.T) {
	tests := []struct {
		name  string
		total int
		want  int
	}{
		{"no TTY falls back to default", 0, 28},
		// 200 / 4 = 50 content cells per column, minus 4 cells of border
		// + padding overhead = 46 content cells. Four columns at 46+4=50
		// cells each = exactly 200, fitting the terminal.
		{"wide terminal accounts for border + padding", 200, 46},
		// 100 / 4 = 25 - 4 = 21 < min(24). Clamped to 24 — at 100 cols
		// the board overflows by 12 cells but that's better than
		// unreadable cards.
		{"narrow terminal clamps to minimum", 100, 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := columnWidth(tt.total); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRender_ColumnTitlesAndCounts(t *testing.T) {
	tasks := []client.Task{
		{ID: "1", Subject: "a", State: client.TaskStateTodo},
		{ID: "2", Subject: "b", State: client.TaskStateInProgress},
		{ID: "3", Subject: "c", State: client.TaskStateInReview},
		{ID: "4", Subject: "d", State: client.TaskStateDone},
		{ID: "5", Subject: "e", State: client.TaskStateCancelled},
	}
	out := render(tasks, newResolver(), 200, "")
	for _, want := range []string{
		"Backlog", "(1)",
		"In Progress", "(1)",
		"Review", "(1)",
		"Done", "(2)", // done + cancelled
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in render output", want)
		}
	}
	for _, want := range []string{"#1", "#2", "#3", "#4", "#5"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected card %q in render", want)
		}
	}
}

func TestRender_BlockedShowsStateBadgeInProgressColumn(t *testing.T) {
	tasks := []client.Task{
		{ID: "B", Subject: "blocked one", State: client.TaskStateBlocked},
	}
	out := render(tasks, newResolver(), 200, "")
	if !strings.Contains(out, "blocked") {
		t.Errorf("expected blocked state to render, output:\n%s", out)
	}
	// Cards must not emit emoji — that breaks column alignment in some
	// terminals (see Resolver.StateText / board.go for context).
	if strings.Contains(out, "⛔") {
		t.Errorf("expected no ⛔ emoji on cards, got:\n%s", out)
	}
}

func TestRender_TodoCardSuppressesStateBadge(t *testing.T) {
	tasks := []client.Task{
		{ID: "T", Subject: "neutral subject", State: client.TaskStateTodo},
	}
	out := render(tasks, newResolver(), 200, "")
	// A todo card in the Backlog column should NOT emit a "todo" badge —
	// the column header already implies the state. The text "Backlog" is
	// the column title and is allowed; the bare word "todo" on a card is
	// not. Check for the state's coloured text form specifically.
	resolver := newResolver()
	if strings.Contains(out, resolver.StateText(client.TaskStateTodo)) {
		t.Errorf("expected todo state suppressed on card, got:\n%s", out)
	}
}

func TestRender_UnassignedShowsDash(t *testing.T) {
	tasks := []client.Task{
		{ID: "U", Subject: "no agent", State: client.TaskStateTodo},
	}
	out := render(tasks, newResolver(), 200, "")
	if !strings.Contains(out, "—") {
		t.Errorf("expected em-dash for unassigned, got:\n%s", out)
	}
}

func TestRender_SelectionMarkerOnFocusedCardOnly(t *testing.T) {
	tasks := []client.Task{
		{ID: "1", Subject: "first", State: client.TaskStateTodo},
		{ID: "2", Subject: "second", State: client.TaskStateTodo},
	}
	out := render(tasks, newResolver(), 200, "1")
	// The marker must appear before card #1's ID and NOT before card #2's.
	first := strings.Index(out, "#1")
	second := strings.Index(out, "#2")
	if first < 0 || second < 0 {
		t.Fatalf("expected both cards in output:\n%s", out)
	}
	// Look at the 5 cells preceding each #N to see if the marker is there.
	preFirst := out[max(0, first-5):first]
	preSecond := out[max(0, second-5):second]
	if !strings.Contains(preFirst, selectionMarker) {
		t.Errorf("expected selection marker before #1, got pre=%q", preFirst)
	}
	if strings.Contains(preSecond, selectionMarker) {
		t.Errorf("expected NO selection marker before #2, got pre=%q", preSecond)
	}
}

func TestRender_ChildCardShowsParentID(t *testing.T) {
	tasks := []client.Task{
		{ID: "child-7", Subject: "do the thing", State: client.TaskStateTodo,
			AssignedAgent: "bob", ParentID: "plan-1"},
	}
	out := render(tasks, newResolver(), 200, "")
	if !strings.Contains(out, "#plan-1") {
		t.Errorf("expected child to show parent id #plan-1, got:\n%s", out)
	}
	// Parent agent is intentionally NOT shown — only the parent ID.
	if !strings.Contains(out, "@bob") {
		t.Errorf("expected own assignee @bob to appear, got:\n%s", out)
	}
}

func TestRender_TopLevelCardOmitsParentReference(t *testing.T) {
	tasks := []client.Task{
		{ID: "root", Subject: "no parent", State: client.TaskStateTodo, AssignedAgent: "alice"},
	}
	out := render(tasks, newResolver(), 200, "")
	// "#root" appears as the card's own ID; check that the assignee line
	// doesn't gain a trailing "#" parent reference.
	if strings.Contains(out, "@alice  #") {
		t.Errorf("expected no parent reference on top-level card, got:\n%s", out)
	}
}

func TestRender_LabelsAppliedWithPalette(t *testing.T) {
	tasks := []client.Task{
		{ID: "L", Subject: "labelled", State: client.TaskStateTodo, Labels: []string{"plan"}},
	}
	out := render(tasks, newResolver(), 200, "")
	if !strings.Contains(out, "plan") {
		t.Errorf("expected 'plan' label to render, got:\n%s", out)
	}
	// Icons stripped on cards — see Resolver.LabelsText for context.
	if strings.Contains(out, "📝") {
		t.Errorf("expected no 📝 emoji on cards, got:\n%s", out)
	}
}
