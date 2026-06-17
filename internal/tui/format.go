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
