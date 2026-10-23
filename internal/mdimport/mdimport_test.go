package mdimport

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

// writeTaskFile creates a filestorage-shaped task file under <dir>/tasks/.
func writeTaskFile(t *testing.T, dir, name, body string) {
	t.Helper()
	tasksDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse_FullFile(t *testing.T) {
	raw := `---
id: "42"
state: in_progress
assigned_agent: alice
depends_on:
    - "7"
    - "8"
parent_id: "1"
labels:
    - bug
    - area:auth
mode: planning
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-02-03T04:05:06Z
---

# Fix the login flow

Some **markdown** description.

With two paragraphs.

# Comments

<!-- comment id=C1 who=alice -->
First comment.
<!-- /comment -->

<!-- comment id=C2 who=bob -->
Second comment,
spanning lines.
<!-- /comment -->
`
	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.ID != "42" {
		t.Errorf("ID = %q, want %q", got.ID, "42")
	}
	if got.Subject != "Fix the login flow" {
		t.Errorf("Subject = %q", got.Subject)
	}
	if got.Description != "Some **markdown** description.\n\nWith two paragraphs." {
		t.Errorf("Description = %q", got.Description)
	}
	if got.State != client.TaskStateInProgress {
		t.Errorf("State = %q, want in_progress", got.State)
	}
	if got.AssignedAgent != "alice" {
		t.Errorf("AssignedAgent = %q", got.AssignedAgent)
	}
	if len(got.DependsOn) != 2 || got.DependsOn[0] != "7" || got.DependsOn[1] != "8" {
		t.Errorf("DependsOn = %v", got.DependsOn)
	}
	if got.ParentID != "1" {
		t.Errorf("ParentID = %q", got.ParentID)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "bug" {
		t.Errorf("Labels = %v", got.Labels)
	}
	if got.Mode != client.TaskModePlanning {
		t.Errorf("Mode = %q, want planning", got.Mode)
	}
	wantCreated := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !got.CreatedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, wantCreated)
	}
	if got.ArchivedAt != nil {
		t.Errorf("ArchivedAt = %v, want nil", got.ArchivedAt)
	}

	if len(got.Comments) != 2 {
		t.Fatalf("Comments = %d, want 2", len(got.Comments))
	}
	if got.Comments[0].ID != "C1" || got.Comments[0].Who != "alice" ||
		got.Comments[0].Body != "First comment." {
		t.Errorf("first comment = %+v", got.Comments[0])
	}
	if got.Comments[1].Body != "Second comment,\nspanning lines." {
		t.Errorf("second comment body = %q", got.Comments[1].Body)
	}
}

func TestParse_MinimalFile(t *testing.T) {
	// The oldest files carry only an id and a state.
	raw := `---
id: "1"
state: todo
assigned_agent: ""
---

# Just a subject
`
	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.ID != "1" || got.Subject != "Just a subject" {
		t.Errorf("got %+v", got)
	}
	if got.Description != "" {
		t.Errorf("Description = %q, want empty", got.Description)
	}
	// Files predating the timestamp fields read back as zero rather than
	// failing — they are real data that must still import.
	if !got.CreatedAt.IsZero() || !got.UpdatedAt.IsZero() {
		t.Errorf("legacy file should have zero timestamps, got %v / %v", got.CreatedAt, got.UpdatedAt)
	}
	if got.Mode != client.TaskModeStandard {
		t.Errorf("Mode = %q, want the default", got.Mode)
	}
	if len(got.Comments) != 0 {
		t.Errorf("Comments = %v, want none", got.Comments)
	}
}

func TestParse_ArchivedTask(t *testing.T) {
	raw := `---
id: "5"
state: todo
assigned_agent: ""
archived_at: 2026-03-04T05:06:07Z
---

# Parked
`
	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("ArchivedAt should be set")
	}
	want := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if !got.ArchivedAt.Equal(want) {
		t.Errorf("ArchivedAt = %v, want %v", got.ArchivedAt, want)
	}
}

func TestParse_NoSubjectKeepsDescription(t *testing.T) {
	raw := `---
id: "1"
state: todo
assigned_agent: ""
---

Just a body with no heading.
`
	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Subject != "" {
		t.Errorf("Subject = %q, want empty", got.Subject)
	}
	if got.Description != "Just a body with no heading." {
		t.Errorf("Description = %q", got.Description)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"no frontmatter", "# Just markdown\n"},
		{"unterminated frontmatter", "---\nid: \"1\"\n"},
		{"invalid yaml", "---\nid: [unclosed\n---\n\n# x\n"},
		{"invalid state", "---\nid: \"1\"\nstate: bogus\nassigned_agent: \"\"\n---\n\n# x\n"},
		{"invalid mode", "---\nid: \"1\"\nstate: todo\nmode: bogus\nassigned_agent: \"\"\n---\n\n# x\n"},
		{"invalid created_at", "---\nid: \"1\"\nstate: todo\ncreated_at: not-a-time\nassigned_agent: \"\"\n---\n\n# x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.raw)); err == nil {
				t.Errorf("Parse(%q) should fail", tt.name)
			}
		})
	}
}

func TestIDFromFilename(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1--foo-bar.md", "1"},
		{"42.md", "42"},
		// Two dashes separate the id from the slug so an id containing a
		// single dash round-trips cleanly.
		{"TASK-123--foo.md", "TASK-123"},
		{"TASK-123.md", "TASK-123"},
		{"7--a--b.md", "7"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := IDFromFilename(tt.in); got != tt.want {
				t.Errorf("IDFromFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestReadDir_SortsNumerically(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"10", "2", "1"} {
		writeTaskFile(t, dir, id+"--task.md",
			"---\nid: \""+id+"\"\nstate: todo\nassigned_agent: \"\"\n---\n\n# Task "+id+"\n")
	}

	got, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3", len(got))
	}
	// Numeric, not lexical: "10" must sort after "2".
	want := []string{"1", "2", "10"}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("task %d = %q, want %q (numeric ordering)", i, got[i].ID, want[i])
		}
	}
}

func TestReadDir_IgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "1--a.md", "---\nid: \"1\"\nstate: todo\nassigned_agent: \"\"\n---\n\n# A\n")
	writeTaskFile(t, dir, "notes.txt", "not a task")
	if err := os.MkdirAll(filepath.Join(dir, "tasks", "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d tasks, want 1 (only .md files)", len(got))
	}
}

func TestReadDir_FallsBackToFilenameID(t *testing.T) {
	dir := t.TempDir()
	// A file whose frontmatter lost its id still carries it in the name.
	writeTaskFile(t, dir, "9--recovered.md",
		"---\nid: \"\"\nstate: todo\nassigned_agent: \"\"\n---\n\n# Recovered\n")

	got, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tasks, want 1", len(got))
	}
	if got[0].ID != "9" {
		t.Errorf("ID = %q, want %q recovered from the filename", got[0].ID, "9")
	}
}

// TestReadDir_ParseErrorIsReported matters because this is a one-shot
// migration of data the user cannot otherwise recover: silently dropping an
// unparseable task would be the worst outcome.
func TestReadDir_ParseErrorIsReported(t *testing.T) {
	dir := t.TempDir()
	writeTaskFile(t, dir, "1--ok.md", "---\nid: \"1\"\nstate: todo\nassigned_agent: \"\"\n---\n\n# OK\n")
	writeTaskFile(t, dir, "2--broken.md", "this file has no frontmatter at all")

	if _, err := ReadDir(dir); err == nil {
		t.Error("ReadDir should report an unparseable file rather than skipping it")
	}
}

func TestReadDir_MissingDirErrors(t *testing.T) {
	if _, err := ReadDir(filepath.Join(t.TempDir(), "nothing")); err == nil {
		t.Error("ReadDir should error on a missing directory")
	}
}

func TestIsFilestorageDir(t *testing.T) {
	t.Run("directory with markdown tasks", func(t *testing.T) {
		dir := t.TempDir()
		writeTaskFile(t, dir, "1--a.md", "---\nid: \"1\"\nstate: todo\nassigned_agent: \"\"\n---\n\n# A\n")
		if !IsFilestorageDir(dir) {
			t.Error("expected a directory with tasks/*.md to be recognised")
		}
	})

	t.Run("empty tasks dir", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if IsFilestorageDir(dir) {
			t.Error("a tasks/ dir with no .md files is not a filestorage dir")
		}
	})

	t.Run("no tasks dir", func(t *testing.T) {
		if IsFilestorageDir(t.TempDir()) {
			t.Error("a directory with no tasks/ is not a filestorage dir")
		}
	})

	t.Run("sqlite project dir", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tm.db"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if IsFilestorageDir(dir) {
			t.Error("a sqlite project dir must not be mistaken for filestorage")
		}
	})
}

// TestParse_RoundTripsRealisticFile uses the exact layout the deleted
// filestorage renderer produced, so the preserved parser stays compatible
// with files already on disk.
func TestParse_RoundTripsRealisticFile(t *testing.T) {
	raw := "---\n" +
		"id: \"3\"\n" +
		"state: blocked\n" +
		"assigned_agent: go-developer\n" +
		"depends_on:\n    - \"2\"\n" +
		"labels:\n    - plan\n" +
		"created_at: 2026-05-06T07:08:09.123456789Z\n" +
		"updated_at: 2026-05-07T07:08:09.123456789Z\n" +
		"---\n\n" +
		"# Ship the thing\n\n" +
		"Body line one.\n\n" +
		"# Comments\n\n" +
		"<!-- comment id=01HV who=zdenko -->\nLooks good.\n<!-- /comment -->\n"

	got, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.ID != "3" || got.Subject != "Ship the thing" {
		t.Errorf("got %+v", got)
	}
	if got.State != client.TaskStateBlocked {
		t.Errorf("State = %q", got.State)
	}
	if got.Description != "Body line one." {
		t.Errorf("Description = %q", got.Description)
	}
	if len(got.Comments) != 1 || got.Comments[0].Who != "zdenko" {
		t.Errorf("Comments = %+v", got.Comments)
	}
	// Nanosecond precision survives.
	if got.CreatedAt.Nanosecond() != 123456789 {
		t.Errorf("CreatedAt lost precision: %v", got.CreatedAt)
	}
}
