package tui

import (
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/client"
)

func init() {
	// Force ANSI on so tests can assert wrapping; tests run without a TTY.
	color.NoColor = false
}

func TestResolver_StateBadge_DefaultsWhenNoUserStyling(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.StateBadge(client.TaskStateTodo)
	if !strings.Contains(got, "📋") {
		t.Errorf("expected default todo icon in %q", got)
	}
	if !strings.Contains(got, "todo") {
		t.Errorf("expected state name in %q", got)
	}
	// Yellow ANSI escape is \x1b[33m.
	if !strings.Contains(got, "\x1b[33m") {
		t.Errorf("expected yellow color escape in %q", got)
	}
}

func TestResolver_StateBadge_UserIconOverride(t *testing.T) {
	user := client.Styling{
		States: map[string]client.Style{
			"todo": {Icon: "✏️"},
		},
	}
	r := NewResolver(user)
	got := r.StateBadge(client.TaskStateTodo)
	if !strings.Contains(got, "✏️") {
		t.Errorf("expected overridden icon ✏️ in %q", got)
	}
	// Color falls through to default (yellow).
	if !strings.Contains(got, "\x1b[33m") {
		t.Errorf("expected default yellow to survive icon-only override in %q", got)
	}
}

func TestResolver_StateBadge_UserColorOverride(t *testing.T) {
	user := client.Styling{
		States: map[string]client.Style{
			"todo": {Color: "red"},
		},
	}
	r := NewResolver(user)
	got := r.StateBadge(client.TaskStateTodo)
	// Default icon survives because user didn't set it.
	if !strings.Contains(got, "📋") {
		t.Errorf("expected default icon 📋 to survive color-only override in %q", got)
	}
	// Red ANSI escape is \x1b[31m.
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("expected red color in %q", got)
	}
}

func TestResolver_StateBadge_UnknownStateFallsBack(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.StateBadge(client.TaskState("nonsense"))
	if !strings.Contains(got, "❓") {
		t.Errorf("expected unknown-state ❓ in %q", got)
	}
	if !strings.Contains(got, "nonsense") {
		t.Errorf("expected raw state string in %q", got)
	}
}

func TestResolver_LabelBadge_KnownLabel(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.LabelBadge("plan")
	if !strings.Contains(got, "📝") {
		t.Errorf("expected default plan icon in %q", got)
	}
}

func TestResolver_LabelBadge_UnknownLabelUsesDefault(t *testing.T) {
	user := client.Styling{
		LabelsDefault: client.Style{Icon: "?", Color: "blue"},
	}
	r := NewResolver(user)
	got := r.LabelBadge("rare-label")
	if !strings.Contains(got, "?") {
		t.Errorf("expected default icon ? in %q", got)
	}
	if !strings.Contains(got, "\x1b[34m") {
		t.Errorf("expected blue color escape in %q", got)
	}
}

func TestResolver_LabelsBadge_JoinsWithComma(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.LabelsBadge([]string{"plan", "bug"})
	if !strings.Contains(got, ",") {
		t.Errorf("expected comma separator in %q", got)
	}
}

func TestResolver_LabelsBadge_EmptyReturnsDash(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.LabelsBadge(nil)
	if !strings.Contains(got, "—") {
		t.Errorf("expected em-dash for empty labels in %q", got)
	}
}

func TestResolver_StateText_NoIcon(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.StateText(client.TaskStateTodo)
	if strings.Contains(got, "📋") {
		t.Errorf("StateText must strip the icon, got %q", got)
	}
	if !strings.Contains(got, "todo") {
		t.Errorf("expected state name in %q", got)
	}
}

func TestResolver_LabelText_NoIcon(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.LabelText("plan")
	if strings.Contains(got, "📝") {
		t.Errorf("LabelText must strip the icon, got %q", got)
	}
	if !strings.Contains(got, "plan") {
		t.Errorf("expected label name in %q", got)
	}
}

func TestResolver_LabelsText_NoIconsAndJoined(t *testing.T) {
	r := NewResolver(client.Styling{})
	got := r.LabelsText([]string{"plan", "bug"})
	if strings.Contains(got, "📝") || strings.Contains(got, "🐛") {
		t.Errorf("LabelsText must strip icons, got %q", got)
	}
	if !strings.Contains(got, ",") {
		t.Errorf("expected comma separator in %q", got)
	}
}

func TestResolver_HexColor_DoesNotCrash(t *testing.T) {
	user := client.Styling{
		States: map[string]client.Style{
			"todo": {Color: "#ff5577"},
		},
	}
	r := NewResolver(user)
	got := r.StateBadge(client.TaskStateTodo)
	// lipgloss's renderer downsamples (or strips entirely) when no TTY is
	// attached; under `go test` we can't reliably assert the ANSI escape.
	// Verify the badge text survives at least — the hex path runs.
	if !strings.Contains(got, "📋") || !strings.Contains(got, "todo") {
		t.Errorf("expected icon + state name in %q", got)
	}
}

func TestResolver_UnknownColorNameRendersUnwrapped(t *testing.T) {
	user := client.Styling{
		States: map[string]client.Style{
			"todo": {Color: "vermilion"},
		},
	}
	r := NewResolver(user)
	got := r.StateBadge(client.TaskStateTodo)
	// Default icon still appears, and no ANSI wrap was added.
	if !strings.Contains(got, "📋") {
		t.Errorf("expected default icon to survive in %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("expected no ANSI escape for unknown color, got %q", got)
	}
}
