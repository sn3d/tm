package filestorage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sn3d/tm/internal/client"
	"gopkg.in/yaml.v3"
)

// planFile is the in-memory representation of one .md plan file. It mirrors
// taskFile but is kept structurally separate so plan and task storage
// formats can evolve independently.
type planFile struct {
	frontmatter planFrontmatter
	subject     string
	description string
	comments    []client.Comment
}

type planFrontmatter struct {
	ID            string `yaml:"id"`
	State         string `yaml:"state"`
	AssignedAgent string `yaml:"assigned_agent"`
}

// readPlanFile loads and parses a plan file by ID. Returns (nil, nil) if no
// such file exists.
func readPlanFile(dir string, id client.PlanID) (*planFile, error) {
	path, err := findPlanPath(dir, id)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %q: %w", id, err)
	}
	pf, err := parsePlanFile(raw)
	if err != nil {
		return nil, fmt.Errorf("parse plan %q: %w", id, err)
	}
	return pf, nil
}

// writePlanFile serializes the plan file and writes it atomically. If a file
// for the same ID exists under a different slug (because the subject
// changed), the old file is removed after the new one is in place.
func writePlanFile(dir string, pf *planFile) error {
	body := renderPlanFile(pf)
	newPath := planPath(dir, pf.frontmatter.ID, pf.subject)
	tmp := newPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write plan %q: %w", pf.frontmatter.ID, err)
	}

	oldPath, err := findPlanPath(dir, pf.frontmatter.ID)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, newPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename plan %q: %w", pf.frontmatter.ID, err)
	}
	if oldPath != "" && oldPath != newPath {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("remove stale plan file %q: %w", oldPath, err)
		}
	}
	return nil
}

// planPath builds the path for a plan file given its ID and (current) subject.
// Empty subject yields {id}.md; otherwise {id}--{slug}.md.
func planPath(dir string, id client.PlanID, subject string) string {
	name := id
	if slug := slugify(subject); slug != "" {
		name = id + idSlugSep + slug
	}
	return filepath.Join(dir, "plans", name+".md")
}

// findPlanPath locates the on-disk file for a plan ID. It tries {id}--*.md
// first, then the bare {id}.md form. Returns "" with no error if no file
// matches.
func findPlanPath(dir string, id client.PlanID) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "plans", id+idSlugSep+"*.md"))
	if err != nil {
		return "", fmt.Errorf("glob plan %q: %w", id, err)
	}
	if len(matches) > 0 {
		return matches[0], nil
	}
	bare := filepath.Join(dir, "plans", id+".md")
	if _, err := os.Stat(bare); err == nil {
		return bare, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat plan %q: %w", id, err)
	}
	return "", nil
}

func parsePlanFile(raw []byte) (*planFile, error) {
	m := frontmatterRE.FindSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("missing frontmatter")
	}
	var fm planFrontmatter
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter: %w", err)
	}

	body := string(m[2])
	subject, description, commentsSection := splitBody(body)
	comments, err := parseComments(commentsSection)
	if err != nil {
		return nil, err
	}
	return &planFile{
		frontmatter: fm,
		subject:     subject,
		description: description,
		comments:    comments,
	}, nil
}

func renderPlanFile(pf *planFile) []byte {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	fm, _ := yaml.Marshal(pf.frontmatter)
	buf.Write(fm)
	buf.WriteString("---\n\n")
	if pf.subject != "" {
		buf.WriteString("# ")
		buf.WriteString(pf.subject)
		buf.WriteString("\n\n")
	}
	if pf.description != "" {
		buf.WriteString(pf.description)
		buf.WriteString("\n\n")
	}
	if len(pf.comments) > 0 {
		buf.WriteString(commentsHeader)
		buf.WriteString("\n\n")
		for i, c := range pf.comments {
			fmt.Fprintf(&buf, "<!-- comment id=%s who=%s -->\n%s\n<!-- /comment -->\n", c.ID, c.Who, c.Comment)
			if i < len(pf.comments)-1 {
				buf.WriteString("\n")
			}
		}
	}
	return buf.Bytes()
}
