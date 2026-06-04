package tui

import (
	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/client"
)

// TaskStateBadge returns an emoji + colored label for a task state, suitable
// for embedding in a tabwriter row or a one-line header. fatih/color
// auto-detects TTY and respects NO_COLOR, so output stays clean when piped.
func TaskStateBadge(s client.TaskState) string {
	switch s {
	case client.TaskStateTodo:
		return color.YellowString("📋 todo")
	case client.TaskStateInProgress:
		return color.CyanString("🔄 in_progress")
	case client.TaskStateBlocked:
		return color.RedString("⛔ blocked")
	case client.TaskStateInReview:
		return color.MagentaString("👀 in_review")
	case client.TaskStateDone:
		return color.GreenString("✅ done")
	case client.TaskStateCancelled:
		return color.HiBlackString("🚫 cancelled")
	default:
		return color.HiBlackString("❓ unknown")
	}
}

// PlanStateBadge mirrors TaskStateBadge for plan states.
func PlanStateBadge(s client.PlanState) string {
	switch s {
	case client.PlanStateDraft:
		return color.YellowString("📝 draft")
	case client.PlanStateActive:
		return color.CyanString("🔄 active")
	case client.PlanStateOnHold:
		return color.HiYellowString("⏸ on_hold")
	case client.PlanStateCompleted:
		return color.GreenString("✅ completed")
	case client.PlanStateCancelled:
		return color.HiBlackString("🚫 cancelled")
	default:
		return color.HiBlackString("❓ unknown")
	}
}

// Dash returns a dim em-dash when s is empty, otherwise s. Used for table cells
// where an empty value would otherwise read as visual noise.
func Dash(s string) string {
	if s == "" {
		return color.HiBlackString("—")
	}
	return s
}
