package editor

import (
	"testing"
)

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSubject string
		wantDesc    string
		wantAssign  string
		wantState   string
		wantDeps    []string
		wantErr     bool
	}{
		{
			name:        "happy path",
			input:       "---\nsubject: ship it\nassigned: alice\n---\nbody text\n",
			wantSubject: "ship it",
			wantDesc:    "body text",
			wantAssign:  "alice",
		},
		{
			name:        "with state frontmatter",
			input:       "---\nsubject: s\nassigned: a\nstate: in_progress\n---\nbody\n",
			wantSubject: "s",
			wantDesc:    "body",
			wantAssign:  "a",
			wantState:   "in_progress",
		},
		{
			name:        "with depends_on list",
			input:       "---\nsubject: s\nassigned: a\ndepends_on:\n  - ABC-1\n  - ABC-2\n---\nbody\n",
			wantSubject: "s",
			wantDesc:    "body",
			wantAssign:  "a",
			wantDeps:    []string{"ABC-1", "ABC-2"},
		},
		{
			name:        "with depends_on inline list",
			input:       "---\nsubject: s\nassigned: a\ndepends_on: [X, Y, Z]\n---\nbody\n",
			wantSubject: "s",
			wantDesc:    "body",
			wantAssign:  "a",
			wantDeps:    []string{"X", "Y", "Z"},
		},
		{
			name:        "with empty depends_on list",
			input:       "---\nsubject: s\nassigned: a\ndepends_on: []\n---\nbody\n",
			wantSubject: "s",
			wantDesc:    "body",
			wantAssign:  "a",
		},
		{
			name:        "crlf line endings",
			input:       "---\r\nsubject: win\r\nassigned: bob\r\n---\r\nbody\r\n",
			wantSubject: "win",
			wantDesc:    "body",
			wantAssign:  "bob",
		},
		{
			name:        "placeholder body treated as empty",
			input:       "---\nsubject: s\nassigned: \n---\n" + descriptionPlaceholder + "\n",
			wantSubject: "s",
			wantDesc:    "",
		},
		{
			name:        "markdown headings pass through",
			input:       "---\nsubject: s\nassigned: \n---\n# heading\n\nparagraph\n",
			wantSubject: "s",
			wantDesc:    "# heading\n\nparagraph",
		},
		{
			name:        "empty body",
			input:       "---\nsubject: s\nassigned: \n---\n",
			wantSubject: "s",
			wantDesc:    "",
		},
		{
			name:        "no trailing newline",
			input:       "---\nsubject: s\nassigned: \n---\nbody",
			wantSubject: "s",
			wantDesc:    "body",
		},
		{
			name:        "leading whitespace before opening fence",
			input:       "\n\n---\nsubject: s\nassigned: \n---\nbody\n",
			wantSubject: "s",
			wantDesc:    "body",
		},
		{
			name:    "missing opening fence",
			input:   "subject: s\n---\nbody\n",
			wantErr: true,
		},
		{
			name:    "missing closing fence",
			input:   "---\nsubject: s\nbody without fence\n",
			wantErr: true,
		},
		{
			name:    "malformed yaml frontmatter",
			input:   "---\nsubject: : :\n  bad indent\n---\nbody\n",
			wantErr: true,
		},
		{
			name:        "subject trimmed",
			input:       "---\nsubject: '  spaced  '\nassigned: \n---\nbody\n",
			wantSubject: "spaced",
			wantDesc:    "body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTemplate([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; result=%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Subject != tt.wantSubject {
				t.Errorf("Subject: got %q, want %q", got.Subject, tt.wantSubject)
			}
			if got.Description != tt.wantDesc {
				t.Errorf("Description: got %q, want %q", got.Description, tt.wantDesc)
			}
			if got.Assigned != tt.wantAssign {
				t.Errorf("Assigned: got %q, want %q", got.Assigned, tt.wantAssign)
			}
			if got.State != tt.wantState {
				t.Errorf("State: got %q, want %q", got.State, tt.wantState)
			}
			if len(got.DependsOn) != len(tt.wantDeps) {
				t.Errorf("DependsOn length: got %v, want %v", got.DependsOn, tt.wantDeps)
			} else {
				for i := range got.DependsOn {
					if got.DependsOn[i] != tt.wantDeps[i] {
						t.Errorf("DependsOn[%d]: got %q, want %q", i, got.DependsOn[i], tt.wantDeps[i])
					}
				}
			}
		})
	}
}

func TestRenderTemplateRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		draft TaskDraft
	}{
		{name: "all fields", draft: TaskDraft{Subject: "ship", Description: "do the thing", Assigned: "alice"}},
		{name: "no description uses placeholder", draft: TaskDraft{Subject: "ship", Assigned: "alice"}},
		{name: "empty assigned", draft: TaskDraft{Subject: "ship", Description: "body"}},
		{name: "with state", draft: TaskDraft{Subject: "ship", Description: "body", Assigned: "alice", State: "in_progress"}},
		{name: "with deps", draft: TaskDraft{Subject: "ship", Description: "body", Assigned: "alice", DependsOn: []string{"ABC-1", "ABC-2"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderTemplate(tt.draft)
			got, err := parseTemplate([]byte(rendered))
			if err != nil {
				t.Fatalf("parse after render failed: %v\nrendered:\n%s", err, rendered)
			}
			wantDesc := tt.draft.Description // placeholder is collapsed back to empty
			if got.Subject != tt.draft.Subject {
				t.Errorf("Subject: got %q, want %q", got.Subject, tt.draft.Subject)
			}
			if got.Description != wantDesc {
				t.Errorf("Description: got %q, want %q", got.Description, wantDesc)
			}
			if got.Assigned != tt.draft.Assigned {
				t.Errorf("Assigned: got %q, want %q", got.Assigned, tt.draft.Assigned)
			}
			if got.State != tt.draft.State {
				t.Errorf("State: got %q, want %q", got.State, tt.draft.State)
			}
			if len(got.DependsOn) != len(tt.draft.DependsOn) {
				t.Errorf("DependsOn length: got %v, want %v", got.DependsOn, tt.draft.DependsOn)
			} else {
				for i := range got.DependsOn {
					if got.DependsOn[i] != tt.draft.DependsOn[i] {
						t.Errorf("DependsOn[%d]: got %q, want %q", i, got.DependsOn[i], tt.draft.DependsOn[i])
					}
				}
			}
		})
	}
}

func TestTaskDraft_PlanField_RoundTrips(t *testing.T) {
	tests := []struct {
		name  string
		draft TaskDraft
	}{
		{name: "plan set", draft: TaskDraft{Subject: "ship", Assigned: "alice", Plan: "PLAN-1"}},
		{name: "plan empty", draft: TaskDraft{Subject: "ship", Assigned: "alice"}},
		{name: "plan with state and deps", draft: TaskDraft{Subject: "ship", Assigned: "alice", Plan: "PLAN-9", State: "in_progress", DependsOn: []string{"A", "B"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderTemplate(tt.draft)
			got, err := parseTemplate([]byte(rendered))
			if err != nil {
				t.Fatalf("parse after render failed: %v\nrendered:\n%s", err, rendered)
			}
			if got.Plan != tt.draft.Plan {
				t.Errorf("Plan: got %q, want %q (rendered:\n%s)", got.Plan, tt.draft.Plan, rendered)
			}
		})
	}
}

func TestParsePlanTemplate(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSubject string
		wantDesc    string
		wantAssign  string
		wantState   string
		wantErr     bool
	}{
		{
			name:        "happy path",
			input:       "---\nsubject: kickoff\nassigned: alice\n---\nbody text\n",
			wantSubject: "kickoff",
			wantDesc:    "body text",
			wantAssign:  "alice",
		},
		{
			name:        "with state",
			input:       "---\nsubject: s\nassigned: a\nstate: active\n---\nbody\n",
			wantSubject: "s",
			wantDesc:    "body",
			wantAssign:  "a",
			wantState:   "active",
		},
		{
			name:        "placeholder body treated as empty",
			input:       "---\nsubject: s\nassigned: \n---\n" + planDescriptionPlaceholder + "\n",
			wantSubject: "s",
			wantDesc:    "",
		},
		{
			name:    "empty subject rejected",
			input:   "---\nsubject:\nassigned: a\n---\nbody\n",
			wantErr: true,
		},
		{
			name:    "missing opening fence",
			input:   "subject: s\n---\nbody\n",
			wantErr: true,
		},
		{
			name:    "missing closing fence",
			input:   "---\nsubject: s\nbody without fence\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePlanTemplate([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; result=%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Subject != tt.wantSubject {
				t.Errorf("Subject: got %q, want %q", got.Subject, tt.wantSubject)
			}
			if got.Description != tt.wantDesc {
				t.Errorf("Description: got %q, want %q", got.Description, tt.wantDesc)
			}
			if got.Assigned != tt.wantAssign {
				t.Errorf("Assigned: got %q, want %q", got.Assigned, tt.wantAssign)
			}
			if got.State != tt.wantState {
				t.Errorf("State: got %q, want %q", got.State, tt.wantState)
			}
		})
	}
}

func TestRenderPlanTemplateRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		draft PlanDraft
	}{
		{name: "all fields", draft: PlanDraft{Subject: "kickoff", Description: "let's go", Assigned: "alice"}},
		{name: "no description uses placeholder", draft: PlanDraft{Subject: "kickoff", Assigned: "alice"}},
		{name: "empty assigned", draft: PlanDraft{Subject: "kickoff", Description: "body"}},
		{name: "with state", draft: PlanDraft{Subject: "kickoff", Description: "body", Assigned: "alice", State: "active"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderPlanTemplate(tt.draft)
			got, err := parsePlanTemplate([]byte(rendered))
			if err != nil {
				t.Fatalf("parse after render failed: %v\nrendered:\n%s", err, rendered)
			}
			if got.Subject != tt.draft.Subject {
				t.Errorf("Subject: got %q, want %q", got.Subject, tt.draft.Subject)
			}
			if got.Description != tt.draft.Description {
				t.Errorf("Description: got %q, want %q", got.Description, tt.draft.Description)
			}
			if got.Assigned != tt.draft.Assigned {
				t.Errorf("Assigned: got %q, want %q", got.Assigned, tt.draft.Assigned)
			}
			if got.State != tt.draft.State {
				t.Errorf("State: got %q, want %q", got.State, tt.draft.State)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/tmp/plain.md", `'/tmp/plain.md'`},
		{"/tmp/with space.md", `'/tmp/with space.md'`},
		{"/tmp/it's.md", `'/tmp/it'\''s.md'`},
		{"/tmp/$weird`.md", "'/tmp/$weird`.md'"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := shellQuote(tt.in); got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
