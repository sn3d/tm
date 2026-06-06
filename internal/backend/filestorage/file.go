package filestorage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sn3d/tm/internal/client"
	"gopkg.in/yaml.v3"
)

// taskFile is the in-memory representation of one .md task file. It exists
// because tasks and comments share storage — both repositories read, mutate,
// and write the same file.
type taskFile struct {
	frontmatter frontmatter
	subject     string
	description string
	comments    []client.Comment
}

type frontmatter struct {
	ID            string   `yaml:"id"`
	State         string   `yaml:"state"`
	AssignedAgent string   `yaml:"assigned_agent"`
	DependsOn     []string `yaml:"depends_on,omitempty"`
	PlanID        string   `yaml:"plan_id,omitempty"`
	CreatedAt     string   `yaml:"created_at,omitempty"`
	UpdatedAt     string   `yaml:"updated_at,omitempty"`
}

const (
	commentsHeader = "# Comments"
	slugMaxLen     = 50
)

var (
	frontmatterRE = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n?(.*)\z`)
	subjectRE     = regexp.MustCompile(`(?m)^# (.+)$`)
	commentBlockRE = regexp.MustCompile(
		`(?s)<!-- comment id=(\S+) who=(\S+) -->\n?(.*?)\n?<!-- /comment -->`,
	)
	nonAlnumRE = regexp.MustCompile(`[^a-z0-9]+`)
)

// readTaskFile loads and parses a task file by ID. Returns (nil, nil) if no
// such file exists. The filename is {id}-{slug}.md or {id}.md when the
// subject is empty; we glob on {id}-*.md plus a check for the bare form.
func readTaskFile(dir string, id client.TaskID) (*taskFile, error) {
	path, err := findTaskPath(dir, id)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read task %q: %w", id, err)
	}
	tf, err := parseTaskFile(raw)
	if err != nil {
		return nil, fmt.Errorf("parse task %q: %w", id, err)
	}
	return tf, nil
}

// writeTaskFile serializes the task file and writes it atomically. If a file
// for the same ID exists under a different slug (because the subject
// changed), the old file is removed after the new one is in place.
func writeTaskFile(dir string, tf *taskFile) error {
	body := renderTaskFile(tf)
	newPath := taskPath(dir, tf.frontmatter.ID, tf.subject)
	tmp := newPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write task %q: %w", tf.frontmatter.ID, err)
	}

	oldPath, err := findTaskPath(dir, tf.frontmatter.ID)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, newPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename task %q: %w", tf.frontmatter.ID, err)
	}
	if oldPath != "" && oldPath != newPath {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("remove stale task file %q: %w", oldPath, err)
		}
	}
	return nil
}

// idSlugSep separates the task ID from the URL-style slug in the filename.
// Two dashes are used so IDs that themselves contain a single dash (e.g.
// JIRA-style "TASK-123") round-trip cleanly.
const idSlugSep = "--"

// taskPath builds the path for a task file given its ID and (current) subject.
// Empty subject yields {id}.md; otherwise {id}--{slug}.md.
func taskPath(dir string, id client.TaskID, subject string) string {
	name := id
	if slug := slugify(subject); slug != "" {
		name = id + idSlugSep + slug
	}
	return filepath.Join(dir, "tasks", name+".md")
}

// findTaskPath locates the on-disk file for a task ID. It tries {id}--*.md
// first, then the bare {id}.md form. Returns "" with no error if no file
// matches. If multiple {id}--*.md files exist (shouldn't happen unless the
// directory was manually edited), it returns the first match.
func findTaskPath(dir string, id client.TaskID) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "tasks", id+idSlugSep+"*.md"))
	if err != nil {
		return "", fmt.Errorf("glob task %q: %w", id, err)
	}
	if len(matches) > 0 {
		return matches[0], nil
	}
	bare := filepath.Join(dir, "tasks", id+".md")
	if _, err := os.Stat(bare); err == nil {
		return bare, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat task %q: %w", id, err)
	}
	return "", nil
}

// slugify lowercases s, replaces runs of non-alphanumeric characters with a
// single '-', trims leading/trailing '-', and caps the result at slugMaxLen.
// Returns "" when nothing usable remains.
func slugify(s string) string {
	slug := nonAlnumRE.ReplaceAllString(strings.ToLower(s), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > slugMaxLen {
		slug = strings.TrimRight(slug[:slugMaxLen], "-")
	}
	return slug
}

func parseTaskFile(raw []byte) (*taskFile, error) {
	m := frontmatterRE.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("missing frontmatter")
	}
	var fm frontmatter
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter: %w", err)
	}

	body := string(m[2])
	subject, description, commentsSection := splitBody(body)
	comments, err := parseComments(commentsSection)
	if err != nil {
		return nil, err
	}
	return &taskFile{
		frontmatter: fm,
		subject:     subject,
		description: description,
		comments:    comments,
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

func parseComments(section string) ([]client.Comment, error) {
	if strings.TrimSpace(section) == "" {
		return nil, nil
	}
	matches := commentBlockRE.FindAllStringSubmatch(section, -1)
	out := make([]client.Comment, 0, len(matches))
	for _, m := range matches {
		out = append(out, client.Comment{
			ID:      m[1],
			Who:     m[2],
			Comment: strings.TrimRight(m[3], "\n"),
		})
	}
	return out, nil
}

func renderTaskFile(tf *taskFile) []byte {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	fm, _ := yaml.Marshal(tf.frontmatter)
	buf.Write(fm)
	buf.WriteString("---\n\n")
	if tf.subject != "" {
		buf.WriteString("# ")
		buf.WriteString(tf.subject)
		buf.WriteString("\n\n")
	}
	if tf.description != "" {
		buf.WriteString(tf.description)
		buf.WriteString("\n\n")
	}
	if len(tf.comments) > 0 {
		buf.WriteString(commentsHeader)
		buf.WriteString("\n\n")
		for i, c := range tf.comments {
			fmt.Fprintf(&buf, "<!-- comment id=%s who=%s -->\n%s\n<!-- /comment -->\n", c.ID, c.Who, c.Comment)
			if i < len(tf.comments)-1 {
				buf.WriteString("\n")
			}
		}
	}
	return buf.Bytes()
}
