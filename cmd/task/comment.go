package task

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// CommentCmd is the top-level `tm comment <id> <text>` command. The author
// of the comment defaults to the resolved actor (cfg.Actor — same source
// used to attribute journal events); --who overrides that for one-off uses.
var CommentCmd = &cli.Command{
	Name:      "comment",
	Usage:     "Add a comment to a task",
	ArgsUsage: "<id> <text>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "who",
			Usage: "Author of the comment (defaults to current actor)",
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

		args := command.Args()
		if args.Len() < 2 {
			return fmt.Errorf("usage: tm comment <id> <text>")
		}
		id := args.Get(0)
		text := args.Get(1)

		who := command.String("who")
		if who == "" {
			who = cfg.Actor
		}

		if err := c.AddTaskComment(id, who, text); err != nil {
			return err
		}
		fmt.Printf("%s Added comment to task %s\n", color.GreenString("✓"), color.New(color.Bold).Sprint(id))
		return nil
	},
}
