// Package scope resolves the on-disk location for a local TM backend.
//
// Two locations exist, each used by exactly one backend:
//
//   - filestorage → <project_root>/.tm/, where project_root is discovered by
//     walking up from cwd looking for a taskmanager.yaml or .tm/ marker.
//     CwdProjectDir errors if no marker is found.
//   - sqlite     → ~/.tm/projects/<sanitized-key>/, where the key is either
//     the discovered project_root (so subdirectories resolve to the same
//     project) or cwd as a fallback. HomeProjectDir creates this lazily.
package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sn3d/tm/internal/client"
)

const ProjectDataDir = ".tm"

// CwdProjectDir returns <project_root>/.tm. If projectRoot is empty it walks
// up from cwd looking for a taskmanager.yaml or .tm/ marker. Errors with a
// hint to run "tm init" when no marker is found — no project is created
// implicitly.
func CwdProjectDir(backendName, projectRoot string) (string, error) {
	if projectRoot != "" {
		return filepath.Join(projectRoot, ProjectDataDir), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	root, err := client.FindProjectRoot(cwd)
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", fmt.Errorf("%s: no TM project found in %s or any parent; run 'tm init' here or in a parent directory", backendName, cwd)
	}
	return filepath.Join(root, ProjectDataDir), nil
}

// HomeProjectDir returns ~/.tm/projects/<sanitized-key>. When projectRoot is
// set the sanitized project_root is used as the key so subdirectories of an
// initialized project route to the same data dir; otherwise cwd is used.
func HomeProjectDir(projectRoot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	key := projectRoot
	if key == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve cwd: %w", err)
		}
		key = cwd
	}
	return filepath.Join(home, ".tm", "projects", SanitizePath(key)), nil
}

// SanitizePath flattens an absolute path into a single directory name by
// replacing path separators with "-".
func SanitizePath(p string) string {
	return strings.ReplaceAll(p, string(filepath.Separator), "-")
}
