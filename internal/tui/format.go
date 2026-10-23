package tui

import (
	"sync"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/client"
)

// defaultResolver is the no-config Resolver — every call without a
// user-supplied styling shares it. Initialised lazily so DefaultStyling's
// map allocations don't happen at import time.
var (
	defaultResolverOnce sync.Once
	defaultResolver     *Resolver
)

// TaskStateBadge keeps the historical no-config API. Equivalent to building
// a fresh Resolver with no user styling and calling StateBadge. Used by
// callers that don't have access to a *client.Config (a handful of tests,
// some package-level helpers).
func TaskStateBadge(s client.TaskState) string {
	defaultResolverOnce.Do(func() { defaultResolver = NewResolver(client.Styling{}) })
	return defaultResolver.StateBadge(s)
}

// Dash returns a dim em-dash when s is empty, otherwise s. Used for table cells
// where an empty value would otherwise read as visual noise.
func Dash(s string) string {
	if s == "" {
		return color.HiBlackString("—")
	}
	return s
}

// RefText renders a task reference for display. Inside a single-project view
// (the board, the detail pane) the project is implicit and the bare "#42"
// form keeps the column grid tight; cross-project views pass qualified=true
// to get "TM/#42" so the reader can tell which repo a row came from.
//
// The zero ref renders as an em-dash, matching Dash.
func RefText(ref client.TaskID, displayName string, qualified bool) string {
	if ref.IsZero() {
		return color.HiBlackString("—")
	}
	if qualified {
		return ref.Display(displayName)
	}
	return "#" + ref.ID
}

// RefBare renders just the "#42" form, for the many single-project call
// sites that previously interpolated a plain string ID.
func RefBare(ref client.TaskID) string {
	if ref.IsZero() {
		return ""
	}
	return "#" + ref.ID
}
