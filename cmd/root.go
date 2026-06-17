package cmd

import (
	"context"
	"os"

	"github.com/sn3d/tm/cmd/inbox"
	"github.com/sn3d/tm/cmd/journal"
	"github.com/sn3d/tm/cmd/task"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// Root is the top-level tm command. Its Before hook loads the merged
// global + project config and stashes the *client.Config on the context.
// Subcommands that need a backend call app.NewClient with the stashed config,
// so commands like `tm --help` don't pay the cost of opening one.
var Root = &cli.Command{
	Name:  "tm",
	Usage: "Task Manager for (not only) Agents",
	Before: func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		cfg, err := client.DefaultConfig()
		if err != nil {
			return ctx, err
		}
		// Precedence: per-command --actor flag (applied later by the
		// subcommand) > taskmanager.yaml actor: > $USER > "cli".
		if cfg.Actor == "" {
			cfg.Actor = defaultCLIActor()
		}
		return context.WithValue(ctx, client.CfgKey, cfg), nil
	},
	Commands: []*cli.Command{
		initCmd,
		task.CreateCmd,
		task.ListCmd,
		task.GetCmd,
		task.EditCmd,
		task.CommentCmd,
		journal.Cmd,
		inbox.Cmd,
		mcpCmd,
	},
}

// defaultCLIActor uses $USER for journal attribution, falling back to "cli"
// when the env var is empty (e.g. in CI containers).
func defaultCLIActor() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "cli"
}
