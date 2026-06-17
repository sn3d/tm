package client

// Styling carries the user-overridable icon + color choices used when
// rendering tasks in the TUI. It is loaded from YAML alongside Backend
// and Actor on Config. Empty maps mean "use built-in defaults"; partial
// maps override only the keys present (other keys still fall back).
type Styling struct {
	// States keyed by TaskState string ("todo", "in_progress", ...). Unknown
	// keys are ignored at load time; missing keys fall back to built-in
	// defaults at render time.
	States map[string]Style `yaml:"states,omitempty"`

	// Labels keyed by exact label string ("plan", "bug", ...). Labels not
	// listed here render with LabelsDefault.
	Labels map[string]Style `yaml:"labels,omitempty"`

	// LabelsDefault is the fallback for any label not present in Labels.
	// Empty Style fields fall back further to built-in defaults.
	LabelsDefault Style `yaml:"labels_default,omitempty"`
}

// Style is one badge's visual recipe. An empty field means "fall back" —
// either to the next config layer, or finally to the built-in default for
// that key. Color accepts named values ("red", "bright_red", ...) or hex
// ("#ff5577"). The renderer resolves the string into a writer at use time.
type Style struct {
	Icon  string `yaml:"icon,omitempty"`
	Color string `yaml:"color,omitempty"`
}

// mergeStyling overlays project on top of global. Maps replace per-key
// (project value wins for any state/label name it sets); within a Style,
// non-empty fields from project override global. Both inputs may be empty
// zero values.
func mergeStyling(global, project Styling) Styling {
	out := Styling{
		States:        copyStyleMap(global.States),
		Labels:        copyStyleMap(global.Labels),
		LabelsDefault: global.LabelsDefault,
	}
	for k, v := range project.States {
		if out.States == nil {
			out.States = map[string]Style{}
		}
		out.States[k] = mergeStyle(out.States[k], v)
	}
	for k, v := range project.Labels {
		if out.Labels == nil {
			out.Labels = map[string]Style{}
		}
		out.Labels[k] = mergeStyle(out.Labels[k], v)
	}
	out.LabelsDefault = mergeStyle(out.LabelsDefault, project.LabelsDefault)
	return out
}

// mergeStyle merges two Styles, with `over` winning per non-empty field.
func mergeStyle(base, over Style) Style {
	if over.Icon != "" {
		base.Icon = over.Icon
	}
	if over.Color != "" {
		base.Color = over.Color
	}
	return base
}

func copyStyleMap(in map[string]Style) map[string]Style {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Style, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
