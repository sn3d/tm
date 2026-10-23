package task

import (
	"github.com/sn3d/tm/internal/client"
)

// displayRef renders a task reference the way every command prints it:
// qualified, using the project's preferred casing — `TM/#42`.
//
// Output is always qualified even in a single-project command. Returning a
// bare `42` is exactly the ambiguity the qualified form exists to remove: an
// agent that reads `42` and then operates in a different project writes to
// the wrong task.
//
// The display name is looked up once per call. A lookup failure falls back to
// the canonical lowercase form rather than failing the command — the ref is
// still correct and addressable, just not in the user's casing.
func displayRef(c *client.Client, ref client.TaskID) string {
	if ref.IsZero() {
		return ""
	}
	return ref.Display(projectDisplayName(c, ref.Project))
}

// projectDisplayName resolves a ticker to its display casing, returning ""
// when the project cannot be loaded (which makes Display fall back to the
// canonical form).
func projectDisplayName(c *client.Client, ticker string) string {
	if c == nil || ticker == "" {
		return ""
	}
	p, err := c.Projects().GetByTicker(ticker)
	if err != nil || p == nil {
		return ""
	}
	return p.DisplayName
}
