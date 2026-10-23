package client

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TaskRef identifies a task globally: a project ticker plus the task's
// per-project numeric ID. Tasks are addressed as `TICKER/#id` (e.g. `TM/#42`).
//
// This is a distinct type rather than the `type TaskID = string` alias it
// replaced, specifically so the compiler finds every call site that needs a
// project: a site missing one becomes a build error rather than a silent bug
// writing into the wrong project.
//
// The zero value is the "no reference" sentinel (see IsZero), which is what
// an empty ParentID or an event not tied to a task carries.
type TaskRef struct {
	Project string // canonical lowercase ticker
	ID      string // bare numeric, e.g. "42"
}

// TaskID is retained as an alias for TaskRef so the many signatures that
// already read `TaskID` keep their intent without a mechanical rename.
type TaskID = TaskRef

// NewTaskRef builds a ref from a project ticker and a bare ID, canonicalising
// the ticker. Returns an error when the ticker is invalid.
func NewTaskRef(project, id string) (TaskRef, error) {
	canon, err := CanonicalTicker(project)
	if err != nil {
		return TaskRef{}, err
	}
	return TaskRef{Project: canon, ID: id}, nil
}

// MustTaskRef is NewTaskRef for literals known to be valid (tests, constants).
// Panics on an invalid ticker.
func MustTaskRef(project, id string) TaskRef {
	ref, err := NewTaskRef(project, id)
	if err != nil {
		panic(err)
	}
	return ref
}

// IsZero reports whether the ref points at nothing. Both halves must be empty:
// a ref carrying only a project is not a task reference, and callers treat
// the zero value as "no parent" / "no task".
func (r TaskRef) IsZero() bool {
	return r.Project == "" && r.ID == ""
}

// String renders the canonical wire form, `tm/#42`. This is the form used in
// JSON payloads, event rows and anywhere a ref is persisted or logged. The
// zero ref renders as "" so empty parents and task-less events serialise to
// an empty string exactly as they did when TaskID was a bare string.
func (r TaskRef) String() string {
	if r.IsZero() {
		return ""
	}
	if r.Project == "" {
		// Defensive: an ID with no project is not addressable, but rendering
		// it bare beats rendering "/#42" and losing the number entirely.
		return "#" + r.ID
	}
	return r.Project + "/#" + r.ID
}

// Display renders the ref using the project's preferred casing, e.g.
// `TM/#42`. Pass the project's DisplayName; an empty name falls back to the
// canonical lowercase form so callers without a loaded Project still print
// something addressable.
func (r TaskRef) Display(displayName string) string {
	if r.IsZero() {
		return ""
	}
	if displayName == "" {
		return r.String()
	}
	return displayName + "/#" + r.ID
}

// ParseTaskRef parses any documented reference form into a TaskRef.
//
//	tm/#42  -> {tm, 42}
//	TM/#42  -> {tm, 42}   (ticker canonicalised)
//	tm/42   -> {tm, 42}   ('#' is optional on input)
//	42      -> {<defaultProject>, 42}
//	#42     -> {<defaultProject>, 42}
//
// A bare ID resolves against defaultProject — the cwd's mapped project — so
// existing muscle memory (`tm task get 42`) keeps working inside a mapped
// repo. A bare ID with no defaultProject is an error rather than a guess:
// no command should silently address an unknown project.
//
// The empty string parses to the zero ref with no error, so callers can pass
// an optional field (e.g. a `--parent ""` flag meaning "top-level") straight
// through.
func ParseTaskRef(s string, defaultProject string) (TaskRef, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return TaskRef{}, nil
	}

	project, idPart := defaultProject, trimmed
	if slash := strings.LastIndex(trimmed, "/"); slash >= 0 {
		project, idPart = trimmed[:slash], trimmed[slash+1:]
		if project == "" {
			return TaskRef{}, fmt.Errorf("invalid task reference %q: missing project ticker before '/'", s)
		}
	}

	idPart = strings.TrimPrefix(idPart, "#")
	if err := validateBareID(idPart, s); err != nil {
		return TaskRef{}, err
	}

	if project == "" {
		return TaskRef{}, fmt.Errorf("task reference %q has no project and no project is mapped for the current directory; "+
			"pass TICKER/#%s or --project TICKER", s, idPart)
	}
	canon, err := CanonicalTicker(project)
	if err != nil {
		return TaskRef{}, fmt.Errorf("invalid task reference %q: %w", s, err)
	}
	return TaskRef{Project: canon, ID: idPart}, nil
}

// validateBareID checks the ID half of a reference. IDs are the per-project
// numeric counter's output, so digits only; rejecting anything else here
// keeps a typo like "tm/#4b" from becoming a lookup that quietly misses.
func validateBareID(id, original string) error {
	if id == "" {
		return fmt.Errorf("invalid task reference %q: missing task ID", original)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid task reference %q: task ID %q must be numeric", original, id)
		}
	}
	return nil
}

// MarshalJSON encodes a TaskRef as its canonical STRING form.
//
// This is load-bearing: before projects existed, TaskID was a `string` alias,
// so every MCP tool response and every journal event payload serialised task
// IDs as bare strings. Marshalling as a struct here would silently reshape
// those payloads into objects and break every agent caller. The round-trip is
// covered by tests.
func (r TaskRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON accepts the string form produced by MarshalJSON. Parsing is
// strict about the project half — a payload is machine-written, so a bare
// `42` arriving here has lost its project and must not be guessed at.
func (r *TaskRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("task reference must be a string: %w", err)
	}
	ref, err := ParseTaskRef(s, "")
	if err != nil {
		return err
	}
	*r = ref
	return nil
}

// MarshalYAML encodes a TaskRef as its canonical string form, matching the
// JSON representation so config and payloads agree.
func (r TaskRef) MarshalYAML() (any, error) {
	return r.String(), nil
}

// UnmarshalYAML accepts the string form. Like UnmarshalJSON it requires an
// explicit project.
func (r *TaskRef) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("task reference must be a string: %w", err)
	}
	ref, err := ParseTaskRef(s, "")
	if err != nil {
		return err
	}
	*r = ref
	return nil
}

// ParseTaskRefs parses a list of references against one default project,
// short-circuiting on the first invalid entry. Used by the depends-on flag
// and the MCP `depends_on` argument.
func ParseTaskRefs(in []string, defaultProject string) ([]TaskRef, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]TaskRef, 0, len(in))
	for _, s := range in {
		ref, err := ParseTaskRef(s, defaultProject)
		if err != nil {
			return nil, err
		}
		if ref.IsZero() {
			continue
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// FormatTaskRefs renders refs as canonical strings, for event payloads and
// flag echoes.
func FormatTaskRefs(refs []TaskRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}
