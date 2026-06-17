package task

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// UnarchiveCmd is the top-level `tm unarchive <id>` command. Restores a task
// to the active view. Intentionally does not cascade — reviving a tree
// requires explicit per-task action to avoid silently un-hiding rows you
// didn't mean to.
var UnarchiveCmd = &cli.Command{
	Name:      "unarchive",
	Usage:     "Unarchive a task (does not cascade)",
	ArgsUsage: "<id>",
	Flags: []cli.Flag{
		app.ActorFlag,
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		app.ApplyActor(cfg, command)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}
		id := command.Args().First()
		if id == "" {
			return fmt.Errorf("task ID is required, e.g. `tm unarchive 123`")
		}
		if err := c.UnarchiveTask(id); err != nil {
			return err
		}
		bold := color.New(color.Bold).Sprint
		fmt.Printf("%s Unarchived task %s\n", color.GreenString("✓"), bold(id))
		return nil
	},
}
