package cmd

import (
	"context"
	"os"

	"github.com/sn3d/tm/cmd/board"
	"github.com/sn3d/tm/cmd/inbox"
	"github.com/sn3d/tm/cmd/journal"
	"github.com/sn3d/tm/cmd/project"
	"github.com/sn3d/tm/cmd/task"
	"github.com/sn3d/tm/cmd/timesheet"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/scope"
	"github.com/urfave/cli/v3"
)

// Root is the top-level tm command. Its Before hook loads the config layers
// and resolves which project/instance this invocation acts on, stashing the
// *client.Config on the context. Subcommands that need a backend call
// app.NewClient with the stashed config, so `tm --help` doesn't pay the cost
// of opening one.
//
// Resolution failure is NOT fatal here: `tm init` and `tm project` must run
// in an unmapped directory. The error is recorded on the config and surfaced
// by app.NewClient when a command actually needs a project.
var Root = &cli.Command{
	Name:  "tm",
	Usage: "Task Manager for (not only) Agents",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "project",
			Aliases: []string{"p"},
			Usage:   "Project ticker to act on (overrides the folder mapping and $TM_PROJECT).",
		},
		&cli.StringFlag{
			Name:  "instance",
			Usage: "DB instance to use (overrides the folder mapping and $TM_INSTANCE).",
		},
	},
	Before: func(ctx context.Context, command *cli.Command) (context.Context, error) {
		cfg, err := client.DefaultConfig()
		if err != nil {
			return ctx, err
		}
		// Precedence: per-command --actor flag (applied later by the
		// subcommand) > config actor: > $USER > "cli".
		if cfg.Actor == "" {
			cfg.Actor = defaultCLIActor()
		}
		applyScope(cfg, command)
		return context.WithValue(ctx, client.CfgKey, cfg), nil
	},
	Commands: []*cli.Command{
		initCmd,
		migrateCmd,
		project.Cmd,
		task.CreateCmd,
		task.ListCmd,
		task.GetCmd,
		task.EditCmd,
		task.CommentCmd,
		task.ArchiveCmd,
		task.UnarchiveCmd,
		board.Cmd,
		journal.Cmd,
		inbox.Cmd,
		timesheet.Cmd,
		mcpCmd,
	},
}

// applyScope resolves the project/instance for this invocation. Precedence is
// flags, then environment, then the longest-prefix folder mapping. A failure
// leaves cfg.Resolved zero — commands needing a project then fail with an
// actionable message, while `tm init` proceeds to create the mapping.
func applyScope(cfg *client.Config, command *cli.Command) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	override := scope.OverrideFromEnv(scope.Override{
		Project:  command.String("project"),
		Instance: command.String("instance"),
	})
	if resolved, err := scope.Resolve(cwd, cfg, override); err == nil {
		cfg.Resolved = resolved
	}
}

// ScopeOverride rebuilds the override a subcommand should use, folding in the
// root's flags. Subcommands that offer their own --project (none today — the
// flag is global) would extend this.
func ScopeOverride(command *cli.Command) scope.Override {
	return scope.OverrideFromEnv(scope.Override{
		Project:  command.String("project"),
		Instance: command.String("instance"),
	})
}

// defaultCLIActor uses $USER for journal attribution, falling back to "cli"
// when the env var is empty (e.g. in CI containers).
func defaultCLIActor() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "cli"
}
