package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the merged view of the central registry (~/.tm/config.yaml) and
// the per-repo preference layers (taskmanager.yaml, taskmanager.local.yaml).
//
// Identity — which project a folder belongs to — comes only from the central
// registry's Mappings. The repo files are preference-only layers now
// (actor, styling); they are no longer project markers.
type Config struct {
	// Backend is retained as the seam where a future github / jira backend
	// plugs in. Only sqlite is implemented.
	Backend string            `yaml:"backend,omitempty"`
	Options map[string]string `yaml:"options,omitempty"`

	// Instances is the named DB registry. A project lives in exactly one
	// instance.
	Instances map[string]Instance `yaml:"instances,omitempty"`

	// DefaultInstance names the instance used when a mapping or flag does
	// not specify one.
	DefaultInstance string `yaml:"default_instance,omitempty"`

	// Mappings maps an absolute repo folder to "instance/TICKER" (or a bare
	// "TICKER", meaning DefaultInstance). Keys are absolute,
	// tilde-expanded and symlink-resolved on both write and lookup.
	Mappings map[string]string `yaml:"mappings,omitempty"`

	// Actor is the identity recorded on every journal event the Client emits.
	// Loaded from YAML; cmd entry points may override after DefaultConfig
	// returns. Falls back to ActorSystem when empty.
	Actor string `yaml:"actor,omitempty"`

	// Styling overrides the default TUI palette for task states and labels.
	// Stays global: loaded from ~/.tm/config.yaml and merged with the repo
	// taskmanager.yaml. Missing entries fall through to built-in defaults at
	// render time (see internal/tui).
	Styling Styling `yaml:"styling,omitempty"`

	// Resolved carries the outcome of scope resolution (which instance,
	// which project, which database). Not loaded from YAML; populated by the
	// root command's Before hook. A zero value means "no project resolved",
	// and every command that needs one errors with an actionable message.
	Resolved Resolved `yaml:"-"`
}

// Instance is one named database. Only driver "sqlite" is implemented; the
// field exists so a remote driver can be added without reshaping config.
type Instance struct {
	Driver string `yaml:"driver,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

// Resolved is the answer to "which project, in which database, am I acting
// on". It is produced by scope.Resolve and carried on Config.
type Resolved struct {
	Instance string // e.g. "local"
	Project  string // canonical ticker, e.g. "tm"
	DBPath   string // expanded absolute path to the sqlite file
	Root     string // the mapped folder that matched, "" for flag/env overrides
}

// IsZero reports whether no project was resolved.
func (r Resolved) IsZero() bool { return r.Project == "" }

// DriverSQLite is the only implemented instance driver.
const DriverSQLite = "sqlite"

// DefaultInstanceName is the instance `tm init` creates on first run.
const DefaultInstanceName = "local"

// CfgKey is the context.Value key callers (cmd, internal/mcp, ...) use to
// stash and retrieve a *Config off a context. The key's identity is the
// type *Config itself, so any importer reads it back with
// ctx.Value(client.CfgKey).(*client.Config).
var CfgKey = (*Config)(nil)

const (
	// CentralConfigSubpath is the central registry, relative to $HOME.
	CentralConfigSubpath = ".tm/config.yaml"
	// legacyGlobalConfigSubpath is the pre-projects global config. It is
	// still read (so an upgrading user keeps their actor and styling) but
	// never written.
	legacyGlobalConfigSubpath = ".tm/taskmanager.yaml"

	projectConfigFilename = "taskmanager.yaml"
	localConfigFilename   = "taskmanager.local.yaml"
)

// DefaultDBFilename is the sqlite file the default instance points at.
const DefaultDBFilename = "local.db"

// CentralConfigPath returns the absolute path of ~/.tm/config.yaml.
func CentralConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, CentralConfigSubpath), nil
}

// ExpandPath makes a config path absolute: it expands a leading "~",
// resolves symlinks where possible, and cleans the result.
//
// Symlink resolution matters on both write and lookup. Without it
// /Users/x/repo and a symlink pointing at it would be two different
// projects — and ~/.tm/taskmanager is already a symlink on the author's
// machine, so this is a live concern rather than a hypothetical.
//
// A path that does not exist yet (a database about to be created) cannot be
// symlink-resolved; the cleaned absolute form is returned instead.
func ExpandPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", p, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	// Not on disk yet (or unreadable): the cleaned absolute path is the best
	// available answer, and creating the file later makes it canonical.
	return filepath.Clean(abs), nil
}

// ExpandDirPath is ExpandPath for a path whose *parent* exists but whose
// final element may not (a database file inside a real directory). It
// symlink-resolves the directory and rejoins the base name, so a symlinked
// parent still collapses to one canonical path.
func ExpandDirPath(p string) (string, error) {
	expanded, err := ExpandPath(p)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(expanded); statErr == nil {
		return expanded, nil
	}
	dir, base := filepath.Split(expanded)
	resolvedDir, err := ExpandPath(filepath.Clean(dir))
	if err != nil {
		return expanded, nil //nolint:nilerr // best effort; fall back to the unresolved form
	}
	return filepath.Join(resolvedDir, base), nil
}

// ParseMapping splits a mapping value into its instance and canonical
// ticker. A bare "TICKER" means defaultInstance.
//
//	"local/TM" -> ("local", "tm")
//	"TM"       -> (defaultInstance, "tm")
func ParseMapping(value, defaultInstance string) (instance, ticker string, err error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", fmt.Errorf("mapping value is empty")
	}
	instance = defaultInstance
	tickerPart := trimmed
	if slash := strings.LastIndex(trimmed, "/"); slash >= 0 {
		instance, tickerPart = trimmed[:slash], trimmed[slash+1:]
		if instance == "" {
			return "", "", fmt.Errorf("mapping %q: missing instance before '/'", value)
		}
	}
	canon, err := CanonicalTicker(tickerPart)
	if err != nil {
		return "", "", fmt.Errorf("mapping %q: %w", value, err)
	}
	if instance == "" {
		return "", "", fmt.Errorf("mapping %q: no instance given and no default_instance configured", value)
	}
	return instance, canon, nil
}

// FormatMapping renders an instance + ticker pair for the mappings map,
// always fully qualified so the file stays unambiguous if default_instance
// later changes.
func FormatMapping(instance, ticker string) string {
	return instance + "/" + ticker
}

// InstancePath returns the expanded database path for a named instance.
func (c *Config) InstancePath(name string) (string, error) {
	inst, ok := c.Instances[name]
	if !ok {
		return "", fmt.Errorf("unknown instance %q; configured instances: %s",
			name, strings.Join(c.InstanceNames(), ", "))
	}
	if inst.Driver != "" && inst.Driver != DriverSQLite {
		return "", fmt.Errorf("instance %q uses driver %q, but only %q is implemented",
			name, inst.Driver, DriverSQLite)
	}
	if inst.Path == "" {
		return "", fmt.Errorf("instance %q has no path configured", name)
	}
	return ExpandDirPath(inst.Path)
}

// InstanceNames returns the configured instance names in sorted order, for
// error messages and `tm project` output.
func (c *Config) InstanceNames() []string {
	out := make([]string, 0, len(c.Instances))
	for name := range c.Instances {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

// sortStrings is a tiny insertion sort to avoid pulling "sort" into this
// file's imports for one call on a map of a handful of entries.
func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
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
//  1. ~/.tm/taskmanager.yaml — the pre-projects global file. Read for
//     backward compatibility (actor, styling) but never written.
//  2. ~/.tm/config.yaml — the central registry: instances,
//     default_instance, mappings, actor, styling.
//  3. ./taskmanager.yaml — per-repo preference (actor, styling). No longer
//     a project marker.
//  4. ./taskmanager.local.yaml — per-user / per-machine overrides, typically
//     left out of version control.
//
// All files are optional. Scope resolution (which project this directory
// belongs to) is a separate step — see scope.Resolve — because it depends on
// cwd and command flags.
func DefaultConfig() (*Config, error) {
	var legacy, central *Config
	if home, err := os.UserHomeDir(); err == nil {
		var loadErr error
		legacy, loadErr = loadOptional(filepath.Join(home, legacyGlobalConfigSubpath))
		if loadErr != nil {
			return nil, loadErr
		}
		central, loadErr = loadOptional(filepath.Join(home, CentralConfigSubpath))
		if loadErr != nil {
			return nil, loadErr
		}
	}

	project, err := loadOptional(projectConfigFilename)
	if err != nil {
		return nil, err
	}
	local, err := loadOptional(localConfigFilename)
	if err != nil {
		return nil, err
	}

	merged := mergeConfigs(mergeConfigs(mergeConfigs(legacy, central), project), local)
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

// mergeConfigs overlays `over` on top of `base`. Scalars win when non-empty;
// Instances and Mappings merge per key (so a repo file could in principle
// add a mapping, though in practice only the central registry carries them);
// styling merges per entry.
func mergeConfigs(base, over *Config) *Config {
	merged := &Config{}
	if base != nil {
		merged.Backend = base.Backend
		merged.Actor = base.Actor
		merged.DefaultInstance = base.DefaultInstance
		merged.Options = copyStringMap(base.Options)
		merged.Instances = copyInstanceMap(base.Instances)
		merged.Mappings = copyStringMap(base.Mappings)
		merged.Styling = base.Styling
	}
	if over != nil {
		if over.Backend != "" {
			merged.Backend = over.Backend
		}
		if over.Actor != "" {
			merged.Actor = over.Actor
		}
		if over.DefaultInstance != "" {
			merged.DefaultInstance = over.DefaultInstance
		}
		for k, v := range over.Options {
			if merged.Options == nil {
				merged.Options = map[string]string{}
			}
			merged.Options[k] = v
		}
		for k, v := range over.Instances {
			if merged.Instances == nil {
				merged.Instances = map[string]Instance{}
			}
			merged.Instances[k] = v
		}
		for k, v := range over.Mappings {
			if merged.Mappings == nil {
				merged.Mappings = map[string]string{}
			}
			merged.Mappings[k] = v
		}
		merged.Styling = mergeStyling(merged.Styling, over.Styling)
	}
	return merged
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyInstanceMap(in map[string]Instance) map[string]Instance {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Instance, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// SaveCentral writes the instances / default_instance / mappings / actor /
// styling registry to ~/.tm/config.yaml, creating the directory if needed.
// Only the central fields are written — Resolved and Options are runtime
// state, and Backend is only written when explicitly set.
func (c *Config) SaveCentral() error {
	path, err := CentralConfigPath()
	if err != nil {
		return err
	}
	out := Config{
		Backend:         c.Backend,
		Instances:       c.Instances,
		DefaultInstance: c.DefaultInstance,
		Mappings:        c.Mappings,
		Actor:           c.Actor,
		Styling:         c.Styling,
	}
	body, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal central config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// SetMapping records folder -> instance/TICKER, expanding and
// symlink-resolving the folder key first so a symlinked path and its target
// do not become two projects.
func (c *Config) SetMapping(folder, instance, ticker string) error {
	key, err := ExpandPath(folder)
	if err != nil {
		return err
	}
	canon, err := CanonicalTicker(ticker)
	if err != nil {
		return err
	}
	if c.Mappings == nil {
		c.Mappings = map[string]string{}
	}
	c.Mappings[key] = FormatMapping(instance, canon)
	return nil
}
