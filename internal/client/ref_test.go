package client

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseTaskRef_AllDocumentedForms(t *testing.T) {
	tests := []struct {
		name           string
		in             string
		defaultProject string
		wantProject    string
		wantID         string
	}{
		{"canonical with hash", "tm/#42", "", "tm", "42"},
		{"uppercase ticker canonicalised", "TM/#42", "", "tm", "42"},
		{"mixed case ticker canonicalised", "Tm/#42", "", "tm", "42"},
		{"hash optional on input", "tm/42", "", "tm", "42"},
		{"bare id uses default project", "42", "tm", "tm", "42"},
		{"bare id with hash uses default project", "#42", "tm", "tm", "42"},
		{"explicit project beats default", "bill/#7", "tm", "bill", "7"},
		{"dash in ticker", "web-app/#3", "", "web-app", "3"},
		{"default project canonicalised", "42", "TM", "tm", "42"},
		{"max length ticker", "abcdefghijklmno/#1", "", "abcdefghijklmno", "1"},
		{"surrounding whitespace trimmed", "  tm/#42  ", "", "tm", "42"},
		{"multi digit id", "tm/#12345", "", "tm", "12345"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTaskRef(tt.in, tt.defaultProject)
			if err != nil {
				t.Fatalf("ParseTaskRef(%q, %q): %v", tt.in, tt.defaultProject, err)
			}
			if got.Project != tt.wantProject || got.ID != tt.wantID {
				t.Errorf("ParseTaskRef(%q, %q) = {%q %q}, want {%q %q}",
					tt.in, tt.defaultProject, got.Project, got.ID, tt.wantProject, tt.wantID)
			}
		})
	}
}

func TestParseTaskRef_EmptyStringIsZeroRef(t *testing.T) {
	// An empty reference is how callers express "no parent" / "top-level",
	// so it must parse cleanly rather than erroring.
	for _, in := range []string{"", "   "} {
		got, err := ParseTaskRef(in, "tm")
		if err != nil {
			t.Fatalf("ParseTaskRef(%q): unexpected error %v", in, err)
		}
		if !got.IsZero() {
			t.Errorf("ParseTaskRef(%q) = %+v, want zero ref", in, got)
		}
	}
}

func TestParseTaskRef_Invalid(t *testing.T) {
	tests := []struct {
		name           string
		in             string
		defaultProject string
		wantErrSubstr  string
	}{
		{"bare id without default project", "42", "", "no project"},
		{"hash id without default project", "#42", "", "no project"},
		{"underscore rejected in ticker", "my_proj/#1", "", "invalid ticker"},
		{"ticker starting with digit", "1tm/#1", "", "invalid ticker"},
		{"ticker too long", "abcdefghijklmnop/#1", "", "invalid ticker"},
		{"missing id", "tm/", "", "missing task ID"},
		{"missing id with hash", "tm/#", "", "missing task ID"},
		{"non numeric id", "tm/#4b", "", "must be numeric"},
		{"non numeric bare id", "abc", "tm", "must be numeric"},
		{"missing project before slash", "/#42", "", "missing project ticker"},
		{"empty ticker with default", "/42", "tm", "missing project ticker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTaskRef(tt.in, tt.defaultProject)
			if err == nil {
				t.Fatalf("ParseTaskRef(%q, %q): expected error, got nil", tt.in, tt.defaultProject)
			}
			if !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Errorf("ParseTaskRef(%q) error = %v, want substring %q", tt.in, err, tt.wantErrSubstr)
			}
		})
	}
}

func TestTaskRef_StringRoundTrip(t *testing.T) {
	// Parsing the output of String must reproduce the input ref. This is
	// what makes the canonical form safe to persist.
	refs := []TaskRef{
		{Project: "tm", ID: "42"},
		{Project: "bill", ID: "1"},
		{Project: "web-app", ID: "9999"},
	}
	for _, want := range refs {
		s := want.String()
		got, err := ParseTaskRef(s, "")
		if err != nil {
			t.Fatalf("ParseTaskRef(%q): %v", s, err)
		}
		if got != want {
			t.Errorf("round trip of %+v via %q gave %+v", want, s, got)
		}
	}
}

func TestTaskRef_String(t *testing.T) {
	tests := []struct {
		name string
		ref  TaskRef
		want string
	}{
		{"canonical", TaskRef{Project: "tm", ID: "42"}, "tm/#42"},
		{"zero ref is empty string", TaskRef{}, ""},
		{"id without project", TaskRef{ID: "42"}, "#42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskRef_Display(t *testing.T) {
	ref := TaskRef{Project: "tm", ID: "42"}
	if got := ref.Display("TM"); got != "TM/#42" {
		t.Errorf("Display(%q) = %q, want %q", "TM", got, "TM/#42")
	}
	// An empty display name falls back to canonical rather than printing
	// "/#42" and losing the project.
	if got := ref.Display(""); got != "tm/#42" {
		t.Errorf("Display(\"\") = %q, want %q", got, "tm/#42")
	}
	if got := (TaskRef{}).Display("TM"); got != "" {
		t.Errorf("zero ref Display() = %q, want empty", got)
	}
}

func TestTaskRef_IsZero(t *testing.T) {
	tests := []struct {
		name string
		ref  TaskRef
		want bool
	}{
		{"zero value", TaskRef{}, true},
		{"full ref", TaskRef{Project: "tm", ID: "1"}, false},
		{"project only is not a task reference", TaskRef{Project: "tm"}, false},
		{"id only", TaskRef{ID: "1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTaskRef_MarshalJSON_ProducesString is risk #1 from the design doc.
// Before projects existed, TaskID was a string alias, so MCP tool responses
// and journal event payloads serialised task IDs as bare strings. If TaskRef
// marshalled as an object instead, every agent caller would break silently.
func TestTaskRef_MarshalJSON_ProducesString(t *testing.T) {
	ref := TaskRef{Project: "tm", ID: "42"}

	raw, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Literal expectation, not derived from ref.String(), so a bug in
	// String() cannot make this test agree with itself.
	if string(raw) != `"tm/#42"` {
		t.Fatalf("json.Marshal(TaskRef) = %s, want %s", raw, `"tm/#42"`)
	}
	// Belt and braces: assert it decodes as a JSON string, not an object.
	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		t.Fatalf("marshalled TaskRef is not a JSON string: %v", err)
	}
	if asString != "tm/#42" {
		t.Errorf("decoded string = %q, want %q", asString, "tm/#42")
	}
	if strings.HasPrefix(string(raw), "{") {
		t.Error("TaskRef marshalled as a JSON object; MCP agent callers would break")
	}
}

func TestTaskRef_MarshalJSON_ZeroRefIsEmptyString(t *testing.T) {
	// An empty parent_id must stay an empty string on the wire, matching
	// the pre-projects behaviour of the string alias.
	raw, err := json.Marshal(TaskRef{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(raw) != `""` {
		t.Errorf("json.Marshal(zero TaskRef) = %s, want %s", raw, `""`)
	}
}

func TestTaskRef_MarshalJSON_NestedInPayload(t *testing.T) {
	// Event payloads embed refs inside maps and slices; verify the string
	// form survives both.
	payload := map[string]any{
		"task":       TaskRef{Project: "tm", ID: "42"},
		"depends_on": []TaskRef{{Project: "tm", ID: "1"}, {Project: "tm", ID: "2"}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"depends_on":["tm/#1","tm/#2"],"task":"tm/#42"}`
	if string(raw) != want {
		t.Errorf("json.Marshal(payload) = %s, want %s", raw, want)
	}
}

func TestTaskRef_UnmarshalJSON(t *testing.T) {
	var ref TaskRef
	if err := json.Unmarshal([]byte(`"TM/#42"`), &ref); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if ref.Project != "tm" || ref.ID != "42" {
		t.Errorf("unmarshalled = %+v, want {tm 42}", ref)
	}
}

func TestTaskRef_UnmarshalJSON_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		// A machine-written payload that lost its project must fail loudly
		// rather than be attributed to some arbitrary project.
		{"bare id has no project", `"42"`},
		{"object form rejected", `{"project":"tm","id":"42"}`},
		{"number rejected", `42`},
		{"invalid ticker", `"my_proj/#1"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ref TaskRef
			if err := json.Unmarshal([]byte(tt.in), &ref); err == nil {
				t.Errorf("json.Unmarshal(%s): expected error, got ref %+v", tt.in, ref)
			}
		})
	}
}

func TestTaskRef_JSONRoundTrip(t *testing.T) {
	want := TaskRef{Project: "web-app", ID: "123"}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got TaskRef
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

func TestTaskRef_YAMLRoundTrip(t *testing.T) {
	type holder struct {
		Ref TaskRef `yaml:"ref"`
	}
	in := holder{Ref: TaskRef{Project: "tm", ID: "42"}}

	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if !strings.Contains(string(raw), "tm/#42") {
		t.Errorf("yaml.Marshal produced %q, want it to contain %q", raw, "tm/#42")
	}

	var got holder
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if got.Ref != in.Ref {
		t.Errorf("round trip gave %+v, want %+v", got.Ref, in.Ref)
	}
}

func TestNewTaskRef(t *testing.T) {
	ref, err := NewTaskRef("TM", "42")
	if err != nil {
		t.Fatalf("NewTaskRef: %v", err)
	}
	if ref.Project != "tm" || ref.ID != "42" {
		t.Errorf("NewTaskRef(TM, 42) = %+v, want {tm 42}", ref)
	}
	if _, err := NewTaskRef("bad_ticker", "1"); err == nil {
		t.Error("NewTaskRef with invalid ticker: expected error")
	}
}

func TestParseTaskRefs(t *testing.T) {
	got, err := ParseTaskRefs([]string{"1", "TM/#2", "bill/#3"}, "tm")
	if err != nil {
		t.Fatalf("ParseTaskRefs: %v", err)
	}
	want := []TaskRef{{Project: "tm", ID: "1"}, {Project: "tm", ID: "2"}, {Project: "bill", ID: "3"}}
	if len(got) != len(want) {
		t.Fatalf("ParseTaskRefs returned %d refs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ref %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if got, err := ParseTaskRefs(nil, "tm"); err != nil || got != nil {
		t.Errorf("ParseTaskRefs(nil) = %v, %v; want nil, nil", got, err)
	}
	if _, err := ParseTaskRefs([]string{"nope"}, "tm"); err == nil {
		t.Error("ParseTaskRefs with invalid entry: expected error")
	}
}

func TestFormatTaskRefs(t *testing.T) {
	got := FormatTaskRefs([]TaskRef{{Project: "tm", ID: "1"}, {Project: "bill", ID: "2"}})
	want := []string{"tm/#1", "bill/#2"}
	if len(got) != len(want) {
		t.Fatalf("FormatTaskRefs returned %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
	if FormatTaskRefs(nil) != nil {
		t.Error("FormatTaskRefs(nil) should be nil")
	}
}
