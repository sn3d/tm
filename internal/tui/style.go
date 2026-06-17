// Package tui's style.go owns the user-overridable rendering palette for
// task states and labels. The Resolver layers the loaded config.Styling on
// top of built-in defaults so callers always get a usable Style — empty
// fields in the config fall through to the defaults; missing labels fall
// through to LabelsDefault.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/client"
)

// DefaultStyling returns the built-in palette. Used as the runtime fallback
// AND as the seed for `tm init`'s generated taskmanager.yaml, so users see
// the same defaults in their file as the binary uses when the file is
// missing.
func DefaultStyling() client.Styling {
	return client.Styling{
		States: map[string]client.Style{
			string(client.TaskStateDraft):      {Icon: "📝", Color: "yellow"},
			string(client.TaskStateTodo):       {Icon: "📋", Color: "yellow"},
			string(client.TaskStateInProgress): {Icon: "🔄", Color: "cyan"},
			string(client.TaskStateBlocked):    {Icon: "⛔", Color: "red"},
			string(client.TaskStateInReview):   {Icon: "👀", Color: "magenta"},
			string(client.TaskStateDone):       {Icon: "✅", Color: "green"},
			string(client.TaskStateCancelled):  {Icon: "🚫", Color: "gray"},
		},
		Labels: map[string]client.Style{
			"plan": {Icon: "📝", Color: "magenta"},
			"bug":  {Color: "red"},
			"task": {Color: "white"},
		},
		LabelsDefault: client.Style{Color: "white"},
	}
}

// Resolver renders state and label badges with user-overridable styling.
// Build one per *client.Client (or per command) via NewResolver. Safe to
// reuse across rows; no mutation after construction.
type Resolver struct {
	states        map[string]client.Style
	labels        map[string]client.Style
	labelsDefault client.Style
	// unknownState is the fallback used for an unrecognized state string.
	unknownState client.Style
}

// NewResolver layers user on top of DefaultStyling using the same rules as
// mergeStyling in the config layer: per-key replacement for maps, per-field
// fallback within a Style. A zero-value user is fine and yields the
// built-in palette unchanged.
func NewResolver(user client.Styling) *Resolver {
	def := DefaultStyling()
	merged := overlay(def, user)
	return &Resolver{
		states:        merged.States,
		labels:        merged.Labels,
		labelsDefault: nonEmpty(merged.LabelsDefault, def.LabelsDefault),
		unknownState:  client.Style{Icon: "❓", Color: "gray"},
	}
}

// overlay is the renderer-side equivalent of mergeStyling. It's duplicated
// rather than reusing client.mergeStyling because we want all "fall through
// to default" logic in one package; the client merge only combines layers
// of user config without knowing what the built-in defaults are.
func overlay(def, user client.Styling) client.Styling {
	out := client.Styling{
		States:        copyStyleMap(def.States),
		Labels:        copyStyleMap(def.Labels),
		LabelsDefault: def.LabelsDefault,
	}
	for k, v := range user.States {
		out.States[k] = mergeOver(out.States[k], v)
	}
	for k, v := range user.Labels {
		if out.Labels == nil {
			out.Labels = map[string]client.Style{}
		}
		out.Labels[k] = mergeOver(out.Labels[k], v)
	}
	out.LabelsDefault = mergeOver(out.LabelsDefault, user.LabelsDefault)
	return out
}

func copyStyleMap(in map[string]client.Style) map[string]client.Style {
	if len(in) == 0 {
		return map[string]client.Style{}
	}
	out := make(map[string]client.Style, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeOver(base, over client.Style) client.Style {
	if over.Icon != "" {
		base.Icon = over.Icon
	}
	if over.Color != "" {
		base.Color = over.Color
	}
	return base
}

// nonEmpty returns a if any field is set, else b. Used to make sure the
// LabelsDefault always has something — even if the user clears it in YAML.
func nonEmpty(a, b client.Style) client.Style {
	if a.Icon != "" || a.Color != "" {
		return a
	}
	return b
}

// StateBadge renders "<icon> <state>" colored per the resolved style. For
// unrecognized states it falls back to a "❓ <state>" badge in gray —
// the renderer never returns the empty string.
func (r *Resolver) StateBadge(s client.TaskState) string {
	st, ok := r.states[string(s)]
	if !ok {
		st = r.unknownState
	}
	return applyColor(st.Color, st.Icon+" "+string(s))
}

// LabelBadge renders "<icon> <name>" for a single label, using the
// per-label style if present, otherwise LabelsDefault.
func (r *Resolver) LabelBadge(name string) string {
	st, ok := r.labels[name]
	if !ok {
		st = r.labelsDefault
	}
	text := name
	if st.Icon != "" {
		text = st.Icon + " " + name
	}
	return applyColor(st.Color, text)
}

// LabelsBadge renders multiple labels joined by ", ". Used by the LABELS
// column in `tm list`. Empty input returns the dim em-dash via Dash.
func (r *Resolver) LabelsBadge(names []string) string {
	if len(names) == 0 {
		return Dash("")
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = r.LabelBadge(n)
	}
	return strings.Join(parts, ", ")
}

// applyColor turns a color name (or hex) into a wrapped string. Names go
// through fatih/color, hex through lipgloss so truecolor terminals get the
// exact shade and lesser terminals get a graceful fallback. An empty color
// returns the text unchanged.
func applyColor(name, text string) string {
	if text == "" {
		return ""
	}
	if name == "" {
		return text
	}
	if strings.HasPrefix(name, "#") {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(name)).Render(text)
	}
	switch strings.ToLower(name) {
	case "red":
		return color.RedString("%s", text)
	case "green":
		return color.GreenString("%s", text)
	case "yellow":
		return color.YellowString("%s", text)
	case "blue":
		return color.BlueString("%s", text)
	case "cyan":
		return color.CyanString("%s", text)
	case "magenta":
		return color.MagentaString("%s", text)
	case "white":
		return color.WhiteString("%s", text)
	case "gray", "grey":
		return color.HiBlackString("%s", text)
	case "bright_red":
		return color.HiRedString("%s", text)
	case "bright_green":
		return color.HiGreenString("%s", text)
	case "bright_yellow":
		return color.HiYellowString("%s", text)
	case "bright_blue":
		return color.HiBlueString("%s", text)
	case "bright_cyan":
		return color.HiCyanString("%s", text)
	case "bright_magenta":
		return color.HiMagentaString("%s", text)
	case "bright_white":
		return color.HiWhiteString("%s", text)
	}
	// Unknown color name → no color rather than an error; the value still
	// renders so a typo doesn't blank out the badge.
	return text
}
