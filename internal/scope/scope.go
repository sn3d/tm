// Package scope answers one question: which project, in which database
// instance, is this command acting on?
//
// Before projects were entities, identity was derived from the filesystem —
// FindProjectRoot walked up looking for a taskmanager.yaml, a .tm/ directory,
// or an existing ~/.tm/projects/<sanitized-dir>/. That last marker was the
// awkward one: a project's existence was inferred from a hashed directory
// name. With the central registry in ~/.tm/config.yaml, identity is looked
// up, not inferred.
package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sn3d/tm/internal/client"
)

// Override carries the higher-precedence selectors: command flags first,
// then environment variables. Empty fields mean "not set".
type Override struct {
	Project  string
	Instance string
}

// Environment variable names checked when no flag is set.
const (
	EnvProject  = "TM_PROJECT"
	EnvInstance = "TM_INSTANCE"
)

// OverrideFromEnv reads TM_PROJECT / TM_INSTANCE, filling only the fields the
// caller left empty — so a flag always beats the environment.
func OverrideFromEnv(o Override) Override {
	if o.Project == "" {
		o.Project = os.Getenv(EnvProject)
	}
	if o.Instance == "" {
		o.Instance = os.Getenv(EnvInstance)
	}
	return o
}

// Resolve determines the instance, project and database path for a command.
//
// Precedence, highest first:
//
//  1. override.Project / override.Instance (flags, then env — the caller
//     folds env in via OverrideFromEnv)
//  2. longest-prefix mapping match walking up from cwd
//  3. error
//
// Longest-prefix rather than first-match makes nested mappings well-defined:
// with /repo -> A and /repo/sub -> B, a cwd of /repo/sub/x resolves to B.
//
// No command silently writes to a guessed project: an unmapped directory
// with no override is an error carrying an actionable hint.
func Resolve(cwd string, cfg *client.Config, override Override) (client.Resolved, error) {
	if cfg == nil {
		return client.Resolved{}, fmt.Errorf("resolve scope: no configuration loaded")
	}

	instance := override.Instance
	project := override.Project
	root := ""

	// A mapping is consulted whenever the project was not given explicitly.
	// It also supplies the instance when only --project was passed, so
	// `tm -p TM` inside a mapped repo keeps using that repo's instance.
	if project == "" {
		mappedInstance, mappedProject, mappedRoot, found, err := lookupMapping(cwd, cfg)
		if err != nil {
			return client.Resolved{}, err
		}
		if !found {
			return client.Resolved{}, &UnmappedError{Dir: cwd}
		}
		project, root = mappedProject, mappedRoot
		if instance == "" {
			instance = mappedInstance
		}
	}

	canonProject, err := client.CanonicalTicker(project)
	if err != nil {
		return client.Resolved{}, fmt.Errorf("resolve scope: %w", err)
	}

	if instance == "" {
		instance = cfg.DefaultInstance
	}
	if instance == "" {
		return client.Resolved{}, fmt.Errorf("no instance selected for project %s: "+
			"set default_instance in ~/.tm/config.yaml or pass --instance", canonProject)
	}

	dbPath, err := cfg.InstancePath(instance)
	if err != nil {
		return client.Resolved{}, err
	}

	return client.Resolved{
		Instance: instance,
		Project:  canonProject,
		DBPath:   dbPath,
		Root:     root,
	}, nil
}

// ResolveInstanceOnly resolves just the instance and its database path,
// without requiring a project. Commands that operate on the registry rather
// than on tasks (`tm project list`, `tm init --list`, `tm migrate`) need a
// connection but not a bound project.
func ResolveInstanceOnly(cwd string, cfg *client.Config, override Override) (client.Resolved, error) {
	if cfg == nil {
		return client.Resolved{}, fmt.Errorf("resolve scope: no configuration loaded")
	}
	instance := override.Instance
	project := override.Project
	root := ""

	if instance == "" {
		// A mapping for this directory is the best hint about which instance
		// the user means; fall back to the configured default. A lookup
		// failure is not fatal here — these commands only need a connection.
		mappedInstance, mappedProject, mappedRoot, found, lookupErr := lookupMapping(cwd, cfg)
		if lookupErr == nil && found {
			instance = mappedInstance
			root = mappedRoot
			if project == "" {
				project = mappedProject
			}
		}
	}
	if instance == "" {
		instance = cfg.DefaultInstance
	}
	if instance == "" {
		return client.Resolved{}, fmt.Errorf("no instance configured; run 'tm init' to create one")
	}
	dbPath, err := cfg.InstancePath(instance)
	if err != nil {
		return client.Resolved{}, err
	}
	canonProject := ""
	if project != "" {
		if canonProject, err = client.CanonicalTicker(project); err != nil {
			return client.Resolved{}, fmt.Errorf("resolve scope: %w", err)
		}
	}
	return client.Resolved{Instance: instance, Project: canonProject, DBPath: dbPath, Root: root}, nil
}

// UnmappedError is returned when a directory has no mapped project and no
// override was given. It is a distinct type so callers (and tests) can
// detect the condition without string matching, and so `tm init` can treat
// it as "not initialized yet" rather than a failure.
type UnmappedError struct {
	Dir string
}

func (e *UnmappedError) Error() string {
	return fmt.Sprintf("no TM project mapped for %s\n\n"+
		"Run 'tm init' here or in a parent directory, or pass --project TICKER.", e.Dir)
}

// lookupMapping finds the mapping whose folder is the longest prefix of cwd.
//
// Both the mapping keys and cwd are expanded and symlink-resolved before
// comparison, so a symlinked checkout and its target resolve to the same
// project. Comparison stays case-sensitive on the path: macOS is
// case-insensitive at the FS layer, but normalising case would break
// genuinely case-sensitive volumes, and symlink resolution already collapses
// the common duplicates.
func lookupMapping(cwd string, cfg *client.Config) (instance, project, root string, found bool, err error) {
	if len(cfg.Mappings) == 0 {
		return "", "", "", false, nil
	}
	resolvedCwd, err := client.ExpandPath(cwd)
	if err != nil {
		return "", "", "", false, err
	}

	bestLen := -1
	var bestValue, bestRoot string
	for folder, value := range cfg.Mappings {
		key, err := client.ExpandPath(folder)
		if err != nil {
			// A mapping pointing at an unreadable path should not break every
			// other lookup; skip it.
			continue
		}
		if !isPrefixDir(key, resolvedCwd) {
			continue
		}
		if len(key) > bestLen {
			bestLen, bestValue, bestRoot = len(key), value, key
		}
	}
	if bestLen < 0 {
		return "", "", "", false, nil
	}

	instance, project, err = client.ParseMapping(bestValue, cfg.DefaultInstance)
	if err != nil {
		return "", "", "", false, fmt.Errorf("mapping for %s: %w", bestRoot, err)
	}
	return instance, project, bestRoot, true, nil
}

// isPrefixDir reports whether dir is an ancestor of (or equal to) target,
// comparing whole path segments so /repo does not match /repository.
func isPrefixDir(dir, target string) bool {
	if dir == target {
		return true
	}
	withSep := dir
	if !strings.HasSuffix(withSep, string(filepath.Separator)) {
		withSep += string(filepath.Separator)
	}
	return strings.HasPrefix(target, withSep)
}

// LegacyProjectsDir returns ~/.tm/projects — the pre-registry storage root
// that `tm migrate` scans. Kept here (rather than in the migrate command)
// because the sanitised-name encoding is this package's historical format.
func LegacyProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".tm", "projects"), nil
}

// SanitizePath flattens an absolute path into a single directory name by
// replacing path separators with "-". This is the encoding the legacy
// per-directory databases were stored under.
func SanitizePath(p string) string {
	return strings.ReplaceAll(p, string(filepath.Separator), "-")
}

// UnsanitizePath reverses SanitizePath, turning a legacy directory name back
// into an absolute path so `tm migrate` can write a folder mapping for the
// project it imports.
//
// The encoding is lossy: a path segment that itself contained a "-" is
// indistinguishable from a separator. The result is therefore a best-effort
// guess, verified against the filesystem by the caller — migrate only writes
// a mapping when the reconstructed directory actually exists.
func UnsanitizePath(name string) string {
	if name == "" {
		return ""
	}
	return strings.ReplaceAll(name, "-", string(filepath.Separator))
}
