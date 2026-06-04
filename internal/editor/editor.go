// Package editor opens the user's $EDITOR with a YAML-frontmatter task
// template and parses the saved file back into task fields. Used by
// `tm task create` when --subject is omitted, mirroring `git commit`'s
// editor-driven message UX.
package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// descriptionPlaceholder is rendered into the template when the user hasn't
// prefilled a description. If the saved body matches it byte-for-byte (after
// trimming), we treat the description as empty. A user who legitimately types
// this exact string gets an empty description — accepted footgun.
const (
	descriptionPlaceholder     = "Describe the task here..."
	planDescriptionPlaceholder = "Describe the plan here..."
)

// ErrNotTerminal is returned by EditTaskDraft when stdin is not a TTY, so
// scripted invocations get a clear error instead of a hung `vi`.
var ErrNotTerminal = errors.New("cannot open editor: stdin is not a terminal")

// TaskDraft is the result of an editor session: the fields the user filled
// into the template, ready to pass to Client.Create or Client.Edit. State
// is a free-form string here so this package stays independent of client.
type TaskDraft struct {
	Subject     string
	Description string
	Assigned    string
	State       string
	DependsOn   []string
	Plan        string
}

type frontmatter struct {
	Subject   string   `yaml:"subject"`
	Assigned  string   `yaml:"assigned"`
	Plan      string   `yaml:"plan,omitempty"`
	State     string   `yaml:"state,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

// PlanDraft is the result of an editor session for a plan: the fields the user
// filled into the template, ready to pass to Client.CreatePlan or
// Client.EditPlan. Mirrors TaskDraft but without dependencies or plan ref.
type PlanDraft struct {
	Subject     string
	Description string
	Assigned    string
	State       string
}

type planFrontmatter struct {
	Subject  string `yaml:"subject"`
	Assigned string `yaml:"assigned"`
	State    string `yaml:"state,omitempty"`
}

// EditTaskDraft writes a markdown template prefilled with `prefill`, launches
// the user's editor against it, and returns the parsed result. The editor is
// resolved from $VISUAL, then $EDITOR, falling back to `vi`. Returns
// ErrNotTerminal if stdin isn't a TTY.
func EditTaskDraft(prefill TaskDraft) (TaskDraft, error) {
	if !stdinIsTerminal() {
		return TaskDraft{}, ErrNotTerminal
	}

	f, err := os.CreateTemp("", "tm-task-*.md")
	if err != nil {
		return TaskDraft{}, fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)

	if _, err := f.WriteString(renderTemplate(prefill)); err != nil {
		f.Close()
		return TaskDraft{}, fmt.Errorf("write template: %w", err)
	}
	if err := f.Close(); err != nil {
		return TaskDraft{}, fmt.Errorf("close template: %w", err)
	}

	if err := runEditor(path); err != nil {
		return TaskDraft{}, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return TaskDraft{}, fmt.Errorf("read saved template: %w", err)
	}
	return parseTemplate(content)
}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func renderTemplate(d TaskDraft) string {
	description := d.Description
	if description == "" {
		description = descriptionPlaceholder
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "subject: %s\n", d.Subject)
	fmt.Fprintf(&b, "assigned: %s\n", d.Assigned)
	fmt.Fprintf(&b, "plan: %q\n", d.Plan)
	if d.State != "" {
		fmt.Fprintf(&b, "state: %s\n", d.State)
	}
	if len(d.DependsOn) > 0 {
		b.WriteString("depends_on:\n")
		for _, dep := range d.DependsOn {
			fmt.Fprintf(&b, "  - %s\n", dep)
		}
	} else {
		b.WriteString("depends_on: []\n")
	}
	b.WriteString("---\n")
	b.WriteString(description)
	b.WriteString("\n")
	return b.String()
}

// runEditor resolves the user's editor and runs it against path. $EDITOR
// commonly contains arguments (e.g. `code --wait`), so we pass it through
// `sh -c` and escape only `path` — the editor string itself is the user's own
// env and is intentionally left to the shell's own word-splitting.
func runEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %s", editor, shellQuote(path)))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q exited with error: %w", editor, err)
	}
	return nil
}

// shellQuote wraps s in POSIX single quotes, escaping any embedded single
// quotes via the standard `'\”` trick. Safe against $, backticks, \, etc.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func parseTemplate(content []byte) (TaskDraft, error) {
	const fence = "---"
	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(bytes.TrimLeft(normalized, " \t\n"), []byte(fence))
	if !ok {
		return TaskDraft{}, fmt.Errorf("missing opening frontmatter fence %q", fence)
	}
	rest = bytes.TrimLeft(rest, " \t\n")
	fmYAML, afterFence, ok := bytes.Cut(rest, []byte("\n"+fence))
	if !ok {
		return TaskDraft{}, fmt.Errorf("missing closing frontmatter fence %q", fence)
	}
	body := afterFence
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = nil
	}

	var fm frontmatter
	if err := yaml.Unmarshal(fmYAML, &fm); err != nil {
		return TaskDraft{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	description := strings.TrimSpace(string(body))
	if description == descriptionPlaceholder {
		description = ""
	}
	deps := make([]string, 0, len(fm.DependsOn))
	for _, d := range fm.DependsOn {
		if trimmed := strings.TrimSpace(d); trimmed != "" {
			deps = append(deps, trimmed)
		}
	}
	if len(deps) == 0 {
		deps = nil
	}
	return TaskDraft{
		Subject:     strings.TrimSpace(fm.Subject),
		Description: description,
		Assigned:    strings.TrimSpace(fm.Assigned),
		State:       strings.TrimSpace(fm.State),
		DependsOn:   deps,
		Plan:        strings.TrimSpace(fm.Plan),
	}, nil
}

// EditPlanDraft writes a markdown template prefilled with `prefill`, launches
// the user's editor against it, and returns the parsed plan result. Mirrors
// EditTaskDraft. Returns ErrNotTerminal if stdin isn't a TTY.
func EditPlanDraft(prefill PlanDraft) (PlanDraft, error) {
	if !stdinIsTerminal() {
		return PlanDraft{}, ErrNotTerminal
	}

	f, err := os.CreateTemp("", "tm-plan-*.md")
	if err != nil {
		return PlanDraft{}, fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)

	if _, err := f.WriteString(renderPlanTemplate(prefill)); err != nil {
		f.Close()
		return PlanDraft{}, fmt.Errorf("write template: %w", err)
	}
	if err := f.Close(); err != nil {
		return PlanDraft{}, fmt.Errorf("close template: %w", err)
	}

	if err := runEditor(path); err != nil {
		return PlanDraft{}, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return PlanDraft{}, fmt.Errorf("read saved template: %w", err)
	}
	return parsePlanTemplate(content)
}

func renderPlanTemplate(d PlanDraft) string {
	description := d.Description
	if description == "" {
		description = planDescriptionPlaceholder
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "subject: %s\n", d.Subject)
	fmt.Fprintf(&b, "assigned: %s\n", d.Assigned)
	if d.State != "" {
		fmt.Fprintf(&b, "state: %s\n", d.State)
	}
	b.WriteString("---\n")
	b.WriteString(description)
	b.WriteString("\n")
	return b.String()
}

func parsePlanTemplate(content []byte) (PlanDraft, error) {
	const fence = "---"
	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(bytes.TrimLeft(normalized, " \t\n"), []byte(fence))
	if !ok {
		return PlanDraft{}, fmt.Errorf("missing opening frontmatter fence %q", fence)
	}
	rest = bytes.TrimLeft(rest, " \t\n")
	fmYAML, afterFence, ok := bytes.Cut(rest, []byte("\n"+fence))
	if !ok {
		return PlanDraft{}, fmt.Errorf("missing closing frontmatter fence %q", fence)
	}
	body := afterFence
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = nil
	}

	var fm planFrontmatter
	if err := yaml.Unmarshal(fmYAML, &fm); err != nil {
		return PlanDraft{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	description := strings.TrimSpace(string(body))
	if description == planDescriptionPlaceholder {
		description = ""
	}
	subject := strings.TrimSpace(fm.Subject)
	if subject == "" {
		return PlanDraft{}, fmt.Errorf("subject is required")
	}
	return PlanDraft{
		Subject:     subject,
		Description: description,
		Assigned:    strings.TrimSpace(fm.Assigned),
		State:       strings.TrimSpace(fm.State),
	}, nil
}
