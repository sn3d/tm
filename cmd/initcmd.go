package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sn3d/tm/internal/scope"
	"github.com/urfave/cli/v3"
)

const cwdProjectConfig = `backend: filestorage
`

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

// initCwd creates ./.tm/tasks in cwd and writes a taskmanager.yaml marker
// selecting the filestorage backend.
func initCwd(cwd string) error {
	dataDir := filepath.Join(cwd, scope.ProjectDataDir)
	already := dirExists(dataDir)

	if err := ensureFilestorageDirs(dataDir); err != nil {
		return err
	}
	wroteConfig, err := writeProjectConfig(cwd, cwdProjectConfig)
	if err != nil {
		return err
	}

	printInitResult(cwd, dataDir, already, wroteConfig)
	return nil
}

// initHome creates ~/.tm/projects/<sanitized-cwd>/ and lets sqlite create
// tm.db inside it on first use. No marker is written in cwd; subsequent
// commands discover the project by walking up and checking whether the
// matching home directory exists.
func initHome(cwd string) error {
	dataDir, err := scope.HomeProjectDir("")
	if err != nil {
		return err
	}
	already := dirExists(dataDir)

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	printInitResult(cwd, dataDir, already, false)
	return nil
}

func ensureFilestorageDirs(dataDir string) error {
	if err := os.MkdirAll(filepath.Join(dataDir, "tasks"), 0o755); err != nil {
		return fmt.Errorf("create tasks dir: %w", err)
	}
	return nil
}

// writeProjectConfig writes taskmanager.yaml in cwd if absent. Returns true
// when a new file was written.
func writeProjectConfig(cwd, contents string) (bool, error) {
	path := filepath.Join(cwd, "taskmanager.yaml")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printInitResult(cwd, dataDir string, alreadyInitialized, wroteConfig bool) {
	suffix := ""
	if wroteConfig {
		suffix = " (wrote taskmanager.yaml)"
	}
	if alreadyInitialized {
		fmt.Printf("Already initialized: data at %s%s\n", dataDir, suffix)
		return
	}
	fmt.Printf("Initialized TM project: marker at %s, data at %s\n", cwd, dataDir)
}
