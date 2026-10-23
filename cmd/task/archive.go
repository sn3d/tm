package task

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// ArchiveCmd is the top-level `tm archive <id>` command. Archive is a soft
// hide: the task keeps its state and stays a valid depends_on target, but it
// is excluded from default `tm list` and inbox views. Cascades to descendants
// by default; pass --no-cascade to archive only the named task.
var ArchiveCmd = &cli.Command{
	Name:      "archive",
	Usage:     "Archive a task (cascades to descendants by default)",
	ArgsUsage: "<id>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "no-cascade",
			Usage: "Archive only this task; leave descendants untouched.",
		},
		app.ActorFlag,
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		app.ApplyActor(cfg, command)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		arg := command.Args().First()
		if arg == "" {
			return fmt.Errorf("task ID is required, e.g. `tm archive 123` or `tm archive TM/#123`")
		}
		id, err := c.ParseRef(arg)
		if err != nil {
			return err
		}

		cascade := !command.Bool("no-cascade")
		count, err := c.ArchiveTask(id, cascade)
		if err != nil {
			return err
		}
		check := color.GreenString("✓")
		bold := color.New(color.Bold).Sprint
		shown := displayRef(c, id)
		if count > 0 {
			fmt.Printf("%s Archived task %s (+ %d descendants)\n", check, bold(shown), count)
		} else {
			fmt.Printf("%s Archived task %s\n", check, bold(shown))
		}
		return nil
	},
}
