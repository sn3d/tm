package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/scope"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

var initCmd = &cli.Command{
	Name:  "init",
	Usage: "Initialize a TM project (sqlite in ~/.tm/ by default; use --cwd for filestorage in ./.tm/)",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "cwd",
			Usage: "Use filestorage and store markdown task files in ./.tm/ instead of a sqlite database in ~/.tm/projects/",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}

		if cmd.Bool("cwd") {
			return initCwd(cwd)
		}
		return initHome(cwd)
	},
}

// initCwd creates ./.tm/tasks in cwd and writes a project taskmanager.yaml
// containing only the filestorage backend selector. Styling is intentionally
// not written here so the global ~/.tm/taskmanager.yaml stays authoritative
// for defaults — a project-level styling block would silently override it
// and force every project to re-customise.
func initCwd(cwd string) error {
	dataDir := filepath.Join(cwd, scope.ProjectDataDir)
	already := dirExists(dataDir)

	if err := ensureFilestorageDirs(dataDir); err != nil {
		return err
	}
	cfgPath := filepath.Join(cwd, "taskmanager.yaml")
	wroteProject, err := writeProjectBackendConfig(cfgPath, "filestorage")
	if err != nil {
		return err
	}
	wroteGlobal, err := ensureGlobalStyling()
	if err != nil {
		return err
	}

	printInitResult(cwd, dataDir, already, wroteProject, wroteGlobal)
	return nil
}

// initHome creates ~/.tm/projects/<sanitized-cwd>/ and lets sqlite create
// tm.db there on first use. No project-level config is written — sqlite is
// the binary default, and styling lives in the global ~/.tm/taskmanager.yaml
// so it travels across all home-mode projects automatically.
func initHome(cwd string) error {
	dataDir, err := scope.HomeProjectDir("")
	if err != nil {
		return err
	}
	already := dirExists(dataDir)

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}
	wroteGlobal, err := ensureGlobalStyling()
	if err != nil {
		return err
	}

	printInitResult(cwd, dataDir, already, false, wroteGlobal)
	return nil
}

func ensureFilestorageDirs(dataDir string) error {
	if err := os.MkdirAll(filepath.Join(dataDir, "tasks"), 0o755); err != nil {
		return fmt.Errorf("create tasks dir: %w", err)
	}
	return nil
}

// writeProjectBackendConfig writes a minimal taskmanager.yaml at path that
// pins only the backend selector. No styling — that's a global concern.
// No-op when the file already exists. Returns true when a new file was
// written.
func writeProjectBackendConfig(path, backend string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	body, err := yaml.Marshal(client.Config{Backend: backend})
	if err != nil {
		return false, fmt.Errorf("marshal project config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create config dir for %s: %w", path, err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// ensureGlobalStyling writes the default styling block into
// ~/.tm/taskmanager.yaml when that file does not yet exist. The block comes
// from tui.DefaultStyling, the same source the binary uses at render time —
// users see in their file exactly what the runtime would do without it.
// No-op when the file already exists; we never overwrite a user's config.
// Returns true when a new file was written.
func ensureGlobalStyling() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("resolve home dir: %w", err)
	}
	path := filepath.Join(home, ".tm", "taskmanager.yaml")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	body, err := yaml.Marshal(client.Config{Styling: tui.DefaultStyling()})
	if err != nil {
		return false, fmt.Errorf("marshal global styling: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printInitResult(cwd, dataDir string, alreadyInitialized, wroteProject, wroteGlobal bool) {
	var suffix string
	switch {
	case wroteProject && wroteGlobal:
		suffix = " (wrote taskmanager.yaml + global ~/.tm/taskmanager.yaml)"
	case wroteProject:
		suffix = " (wrote taskmanager.yaml)"
	case wroteGlobal:
		suffix = " (wrote global ~/.tm/taskmanager.yaml)"
	}
	if alreadyInitialized {
		fmt.Printf("Already initialized: data at %s%s\n", dataDir, suffix)
		return
	}
	fmt.Printf("Initialized TM project: marker at %s, data at %s%s\n", cwd, dataDir, suffix)
}
