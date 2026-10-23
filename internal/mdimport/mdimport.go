// Package mdimport reads the markdown task files the deleted `filestorage`
// backend used to write: one .md file per task under <dir>/tasks/, with YAML
// frontmatter and inline HTML-comment comment blocks.
//
// The backend itself is gone — with all markdown in SQLite its entire reason
// for existing had been removed, and keeping it would have meant implementing
// projects, instances and the markdown tables twice. But deleting it is a
// one-way door: anyone with checked-in markdown task files would have been
// orphaned. So its parser survives here, read-only, as the import path for
// `tm migrate`.
//
// This package deliberately does not write. Restoring the write path would
// resurrect the backend, which is not the intent.
package mdimport

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sn3d/tm/internal/client"
	"gopkg.in/yaml.v3"
)

// Task is one parsed markdown task file, in the storage-agnostic shape the
// migrate command needs. IDs stay bare here: the project a file belongs to is
// decided by the importer, not by the file.
type Task struct {
	ID            string
	Subject       string
	Description   string
	State         client.TaskState
	AssignedAgent string
	DependsOn     []string
	ParentID      string
	Labels        []string
	Mode          client.TaskMode
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ArchivedAt    *time.Time
	Comments      []Comment
}

// Comment is one inline comment block from a task file.
type Comment struct {
	ID   string
	Who  string
	Body string
}

// frontmatter is the YAML header of a task file.
type frontmatter struct {
	ID            string   `yaml:"id"`
	State         string   `yaml:"state"`
	AssignedAgent string   `yaml:"assigned_agent"`
	DependsOn     []string `yaml:"depends_on,omitempty"`
	ParentID      string   `yaml:"parent_id,omitempty"`
	Labels        []string `yaml:"labels,omitempty"`
	Mode          string   `yaml:"mode,omitempty"`
	CreatedAt     string   `yaml:"created_at,omitempty"`
	UpdatedAt     string   `yaml:"updated_at,omitempty"`
	ArchivedAt    string   `yaml:"archived_at,omitempty"`
}

const commentsHeader = "# Comments"

// idSlugSep separates the task ID from the URL-style slug in the filename.
// Two dashes were used so IDs that themselves contain a single dash (e.g.
// JIRA-style "TASK-123") round-trip cleanly.
const idSlugSep = "--"

var (
	frontmatterRE  = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n?(.*)\z`)
	subjectRE      = regexp.MustCompile(`(?m)^# (.+)$`)
	commentBlockRE = regexp.MustCompile(
		`(?s)<!-- comment id=(\S+) who=(\S+) -->\n?(.*?)\n?<!-- /comment -->`,
	)
)

// IsFilestorageDir reports whether dir looks like a filestorage data
// directory, i.e. it has a tasks/ subdirectory containing at least one .md
// file. Used by `tm migrate` to tell these apart from sqlite project dirs.
func IsFilestorageDir(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, "tasks"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
	}
	return false
}

// ReadDir parses every .md file under <dir>/tasks/, sorted by numeric then
// lexical ID so the import order is deterministic.
//
// A file that fails to parse is reported as an error rather than skipped:
// this is a one-shot migration of data the user cannot otherwise recover, so
// silently dropping a task would be the worst outcome.
func ReadDir(dir string) ([]Task, error) {
	tasksDir := filepath.Join(dir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", tasksDir, err)
	}
	out := make([]Task, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(tasksDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		task, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		// Frontmatter is authoritative, but a file whose header lost its id
		// still carries it in the filename.
		if task.ID == "" {
			task.ID = IDFromFilename(e.Name())
		}
		out = append(out, *task)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ni, erri := parseInt(out[i].ID)
		nj, errj := parseInt(out[j].ID)
		if erri == nil && errj == nil {
			return ni < nj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Parse decodes one task file's bytes.
func Parse(raw []byte) (*Task, error) {
	m := frontmatterRE.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("missing frontmatter")
	}
	var fm frontmatter
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter: %w", err)
	}

	subject, description, commentsSection := splitBody(string(m[2]))

	state, err := client.ParseTaskState(fm.State)
	if err != nil {
		return nil, fmt.Errorf("parse state for task %q: %w", fm.ID, err)
	}
	mode, err := client.ParseTaskMode(fm.Mode)
	if err != nil {
		return nil, fmt.Errorf("parse mode for task %q: %w", fm.ID, err)
	}
	createdAt, err := parseTime(fm.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at for task %q: %w", fm.ID, err)
	}
	updatedAt, err := parseTime(fm.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at for task %q: %w", fm.ID, err)
	}
	var archivedAt *time.Time
	if fm.ArchivedAt != "" {
		parsed, perr := parseTime(fm.ArchivedAt)
		if perr != nil {
			return nil, fmt.Errorf("parse archived_at for task %q: %w", fm.ID, perr)
		}
		archivedAt = &parsed
	}

	return &Task{
		ID:            fm.ID,
		Subject:       subject,
		Description:   description,
		State:         state,
		AssignedAgent: fm.AssignedAgent,
		DependsOn:     fm.DependsOn,
		ParentID:      fm.ParentID,
		Labels:        fm.Labels,
		Mode:          mode,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ArchivedAt:    archivedAt,
		Comments:      parseComments(commentsSection),
	}, nil
}

// splitBody finds the first H1 (= subject) and the # Comments section,
// returning subject, description, and the raw comments section.
func splitBody(body string) (subject, description, commentsSection string) {
	if idx := strings.Index(body, "\n"+commentsHeader); idx >= 0 {
		commentsSection = strings.TrimLeft(body[idx+len("\n"+commentsHeader):], "\n")
		body = body[:idx]
	}
	if m := subjectRE.FindStringSubmatchIndex(body); m != nil {
		subject = strings.TrimSpace(body[m[2]:m[3]])
		description = strings.TrimSpace(body[m[1]:])
	} else {
		description = strings.TrimSpace(body)
	}
	return subject, description, commentsSection
}

func parseComments(section string) []Comment {
	if strings.TrimSpace(section) == "" {
		return nil
	}
	matches := commentBlockRE.FindAllStringSubmatch(section, -1)
	out := make([]Comment, 0, len(matches))
	for _, m := range matches {
		out = append(out, Comment{
			ID:   m[1],
			Who:  m[2],
			Body: strings.TrimRight(m[3], "\n"),
		})
	}
	return out
}

// IDFromFilename strips the .md suffix and any --slug tail, returning just
// the leading ID portion. "1--foo-bar.md" → "1", "TASK-123--foo.md" →
// "TASK-123", "42.md" → "42".
func IDFromFilename(name string) string {
	stem := strings.TrimSuffix(name, ".md")
	if i := strings.Index(stem, idSlugSep); i >= 0 {
		return stem[:i]
	}
	return stem
}

// parseTime parses an RFC3339Nano timestamp from frontmatter. Returns a zero
// time when s is empty so legacy files (predating timestamps) read as zero.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

// parseInt is a minimal numeric parse used only for sort ordering.
func parseInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not numeric")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
