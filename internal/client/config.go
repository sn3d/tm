package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Backend string            `yaml:"backend,omitempty"`
	Options map[string]string `yaml:"options,omitempty"`

	// ProjectRoot is the absolute path of the discovered TM project root, or
	// "" when no project marker (taskmanager.yaml or .tm/) was found walking
	// up from cwd. Not loaded from YAML; populated by DefaultConfig.
	ProjectRoot string `yaml:"-"`

	// Actor is the identity recorded on every journal event the Client emits.
	// Loaded from YAML; cmd entry points may override after DefaultConfig
	// returns. Falls back to ActorSystem when empty.
	Actor string `yaml:"actor,omitempty"`

	// Styling overrides the default TUI palette for task states and labels.
	// Loaded from YAML; merged across global ~/.tm/taskmanager.yaml and the
	// project taskmanager.yaml. Missing entries fall through to built-in
	// defaults at render time (see internal/tui).
	Styling Styling `yaml:"styling,omitempty"`
}

// CfgKey is the context.Value key callers (cmd, internal/mcp, ...) use to
// stash and retrieve a *Config off a context. The key's identity is the
// type *Config itself, so any importer reads it back with
// ctx.Value(client.CfgKey).(*client.Config).
var CfgKey = (*Config)(nil)

const (
	globalConfigSubpath   = ".tm/taskmanager.yaml"
	projectConfigFilename = "taskmanager.yaml"
	localConfigFilename   = "taskmanager.local.yaml"
	projectDataDir        = ".tm"
)

// FindProjectRoot walks up from start looking for a TM project. A project
// is identified by any of these markers at an ancestor directory:
//
//   - taskmanager.yaml file in the directory
//   - .tm/ directory inside it (cwd-scoped project)
//   - ~/.tm/projects/<sanitized-dir>/ existing on disk (home-scoped project,
//     so subdirectories of a home-init'd project still resolve correctly
//     without any marker file living next to the user's code)
//
// Walk stops at the user's home directory and at the filesystem root.
// Returns the absolute path of the first ancestor with a marker, or "" if
// no project is found.
func FindProjectRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve start path: %w", err)
	}
	home, _ := os.UserHomeDir()

	for {
		if home != "" && dir == home {
			return "", nil
		}
		if hasProjectMarker(dir, home) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func hasProjectMarker(dir, home string) bool {
	if _, err := os.Stat(filepath.Join(dir, projectConfigFilename)); err == nil {
		return true
	}
	if info, err := os.Stat(filepath.Join(dir, projectDataDir)); err == nil && info.IsDir() {
		return true
	}
	if home != "" {
		homeProj := filepath.Join(home, ".tm", "projects", sanitizePathForHome(dir))
		if info, err := os.Stat(homeProj); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// sanitizePathForHome mirrors scope.SanitizePath. Duplicated here because
// the scope package imports this one and the cycle can't be broken cheaply.
func sanitizePathForHome(p string) string {
	return strings.ReplaceAll(p, string(filepath.Separator), "-")
}

// ConfigFromYAML loads a Config from a YAML file at the given path.
func ConfigFromYAML(filename string) (*Config, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", filename, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", filename, err)
	}
	return &cfg, nil
}

// DefaultConfig loads config layers in this order, with each layer
// overriding the previous:
//
//  1. ~/.tm/taskmanager.yaml — global defaults shared across all projects.
//     `tm init` writes the default styling block here (if missing) so the
//     palette lives in one place and travels across projects.
//  2. <project_root>/taskmanager.yaml — checked-in project config, written
//     by `tm init --cwd` to pin the backend selector. Project files are
//     intentionally NOT seeded with a styling block so they don't silently
//     shadow the global defaults.
//  3. <project_root>/taskmanager.local.yaml — per-user / per-machine
//     overrides typically left out of version control.
//
// All files are optional. The discovered root (or "" if none) is returned
// via Config.ProjectRoot so backends and commands can use it without
// re-walking.
func DefaultConfig() (*Config, error) {
	var global *Config
	if home, err := os.UserHomeDir(); err == nil {
		var loadErr error
		global, loadErr = loadOptional(filepath.Join(home, globalConfigSubpath))
		if loadErr != nil {
			return nil, loadErr
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve cwd: %w", err)
	}
	root, err := FindProjectRoot(cwd)
	if err != nil {
		return nil, err
	}

	projectPath := projectConfigFilename
	localPath := localConfigFilename
	if root != "" {
		projectPath = filepath.Join(root, projectConfigFilename)
		localPath = filepath.Join(root, localConfigFilename)
	}

	project, err := loadOptional(projectPath)
	if err != nil {
		return nil, err
	}
	local, err := loadOptional(localPath)
	if err != nil {
		return nil, err
	}

	merged := mergeConfigs(mergeConfigs(global, project), local)
	merged.ProjectRoot = root
	return merged, nil
}

func loadOptional(path string) (*Config, error) {
	cfg, err := ConfigFromYAML(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

func mergeConfigs(global, project *Config) *Config {
	merged := &Config{}
	if global != nil {
		merged.Backend = global.Backend
		merged.Actor = global.Actor
		for k, v := range global.Options {
			if merged.Options == nil {
				merged.Options = map[string]string{}
			}
			merged.Options[k] = v
		}
		merged.Styling = global.Styling
	}
	if project != nil {
		if project.Backend != "" {
			merged.Backend = project.Backend
		}
		if project.Actor != "" {
			merged.Actor = project.Actor
		}
		for k, v := range project.Options {
			if merged.Options == nil {
				merged.Options = map[string]string{}
			}
			merged.Options[k] = v
		}
		merged.Styling = mergeStyling(merged.Styling, project.Styling)
	}
	return merged
}
