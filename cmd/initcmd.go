package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/sn3d/tm/cmd/project"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/scope"
	"github.com/sn3d/tm/internal/tui"
	"github.com/sn3d/tm/internal/tui/picker"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// initCmd no longer creates storage. It selects or creates a project in an
// instance and writes the folder mapping into ~/.tm/config.yaml.
var initCmd = &cli.Command{
	Name:  "init",
	Usage: "Map this folder to a TM project (interactive picker on a terminal; flag-driven otherwise)",
	Description: `Selects or creates a project and records this folder -> instance/TICKER
in ~/.tm/config.yaml.

On a terminal, bare 'tm init' shows a project picker. Non-interactively
every path has a flag equivalent:

  tm init --project TM                    map to an existing project
  tm init --instance work --project BILL  map in an explicit instance
  tm init --new TM --name "Task Manager"  create and map
  tm init --list                          print projects; write nothing

Run in an already-mapped folder it prints the current mapping and exits 0,
so it is safe to run speculatively. Pass --force to remap.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "new",
			Usage: "Create a project with this ticker and map the folder to it.",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "Human-readable name for a project created with --new.",
		},
		&cli.BoolFlag{
			Name:  "list",
			Usage: "List the instance's projects and exit without writing anything.",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Remap this folder even if it is already mapped to a project.",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}
		return runInit(cfg, command, cwd)
	},
}

func runInit(cfg *client.Config, command *cli.Command, cwd string) error {
	// First run: seed the central registry with a local instance and the
	// default styling block, preserving the old ensureGlobalStyling
	// behaviour. The database and schema are created on first open.
	seeded, err := ensureCentralConfig(cfg)
	if err != nil {
		return err
	}
	if seeded {
		path, _ := client.CentralConfigPath()
		fmt.Printf("%s Wrote %s (instance %q at %s)\n", color.GreenString("✓"),
			path, client.DefaultInstanceName, cfg.Instances[client.DefaultInstanceName].Path)
	}

	// Resolve the instance (not the project — that is what init decides).
	override := ScopeOverride(command)
	resolved, err := scope.ResolveInstanceOnly(cwd, cfg, override)
	if err != nil {
		return err
	}
	cfg.Resolved = resolved

	c, err := app.NewInstanceClient(cfg)
	if err != nil {
		return err
	}

	if command.Bool("list") {
		return listProjects(c, cfg)
	}

	newTicker := command.String("new")
	explicitProject := override.Project

	// An already-mapped folder prints its mapping and exits 0 — safe to run
	// speculatively, and useful as a "where am I?" check. Exit 0 rather than
	// an error matters for scripts that just want to ensure a mapping exists.
	if !command.Bool("force") && newTicker == "" && explicitProject == "" {
		if existing, ok := currentMapping(cwd, cfg); ok {
			printExistingMapping(c, cwd, existing)
			return nil
		}
	}

	switch {
	case newTicker != "":
		p, err := project.Create(c, newTicker, command.String("name"))
		if err != nil {
			return err
		}
		return writeMapping(cfg, cwd, resolved.Instance, p, true)

	case explicitProject != "":
		canon, err := client.CanonicalTicker(explicitProject)
		if err != nil {
			return err
		}
		p, err := c.Projects().GetByTicker(canon)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("no project with ticker %q in instance %s\n\n"+
				"Create it with 'tm init --new %s', or list what exists with 'tm init --list'.",
				canon, resolved.Instance, explicitProject)
		}
		return writeMapping(cfg, cwd, resolved.Instance, p, false)

	default:
		return runInteractive(c, cfg, cwd, resolved)
	}
}

// runInteractive shows the BubbleTea picker. TM is agent-facing, so when
// stdout is not a terminal this errors rather than hanging on a prompt no
// agent can answer.
func runInteractive(c *client.Client, cfg *client.Config, cwd string, resolved client.Resolved) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("tm init needs a terminal to show the project picker.\n" +
			"Pass --project TICKER to map an existing project, or --new TICKER to create one.")
	}

	projects, err := c.Projects().GetAll(false)
	if err != nil {
		return err
	}
	counts, err := c.Projects().TaskCounts()
	if err != nil {
		return err
	}

	result, err := picker.Run(picker.NewModel(
		resolved.Instance, resolved.DBPath, cwd, projects, counts))
	if err != nil {
		return err
	}
	if result.Cancelled {
		fmt.Println(color.HiBlackString("Cancelled; nothing was written."))
		return nil
	}

	var p *client.Project
	if result.Created {
		if p, err = project.Create(c, result.DisplayName, result.Name); err != nil {
			return err
		}
	} else {
		if p, err = c.Projects().GetByTicker(result.Ticker); err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("project %q disappeared between listing and selection", result.Ticker)
		}
	}
	return writeMapping(cfg, cwd, resolved.Instance, p, result.Created)
}

// ensureCentralConfig writes ~/.tm/config.yaml on first run: a `local`
// instance at ~/.tm/local.db, default_instance: local, and the default
// styling block (so users see in their file exactly what the runtime would
// do without it). Returns true when it created the instance registry.
//
// It never overwrites a user's existing instances.
func ensureCentralConfig(cfg *client.Config) (bool, error) {
	if len(cfg.Instances) > 0 {
		// Instances exist but no default was named — pick one deterministically
		// so resolution has something to fall back on.
		if cfg.DefaultInstance == "" {
			cfg.DefaultInstance = cfg.InstanceNames()[0]
			if err := cfg.SaveCentral(); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("resolve home dir: %w", err)
	}
	cfg.Instances = map[string]client.Instance{
		client.DefaultInstanceName: {
			Driver: client.DriverSQLite,
			Path:   filepath.Join(home, ".tm", client.DefaultDBFilename),
		},
	}
	cfg.DefaultInstance = client.DefaultInstanceName
	if len(cfg.Styling.States) == 0 && len(cfg.Styling.Labels) == 0 {
		cfg.Styling = tui.DefaultStyling()
	}
	if err := cfg.SaveCentral(); err != nil {
		return false, err
	}
	return true, nil
}

// currentMapping returns the mapping covering cwd, if any.
func currentMapping(cwd string, cfg *client.Config) (client.Resolved, bool) {
	resolved, err := scope.Resolve(cwd, cfg, scope.Override{})
	if err != nil {
		return client.Resolved{}, false
	}
	return resolved, true
}

func printExistingMapping(c *client.Client, cwd string, resolved client.Resolved) {
	label := resolved.Project
	name := ""
	if p, err := c.Projects().GetByTicker(resolved.Project); err == nil && p != nil {
		label = p.Label()
		name = p.Name
	}
	fmt.Println(cwd)
	if name != "" {
		fmt.Printf("  -> %s/%s (%s)\n", resolved.Instance, label, name)
	} else {
		fmt.Printf("  -> %s/%s\n", resolved.Instance, label)
	}
	fmt.Println()
	fmt.Println(color.HiBlackString("Already initialized. Pass --force to remap this folder to another project."))
}

// writeMapping records folder -> instance/TICKER and saves the registry.
func writeMapping(cfg *client.Config, cwd, instance string, p *client.Project, created bool) error {
	if err := cfg.SetMapping(cwd, instance, p.Ticker); err != nil {
		return err
	}
	if err := cfg.SaveCentral(); err != nil {
		return err
	}
	check := color.GreenString("✓")
	bold := color.New(color.Bold).Sprint
	if created {
		fmt.Printf("%s Created project %s\n", check, bold(p.Label()))
	}
	fmt.Printf("%s Mapped %s -> %s/%s\n", check, cwd, instance, bold(p.Label()))
	return nil
}

func listProjects(c *client.Client, cfg *client.Config) error {
	projects, err := c.Projects().GetAll(false)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println(color.HiBlackString(
			"No projects in instance " + cfg.Resolved.Instance + " yet. Create one with: tm init --new TICKER"))
		return nil
	}
	counts, err := c.Projects().TaskCounts()
	if err != nil {
		return err
	}
	return project.Render(projects, counts, cfg.Resolved.Project)
}
