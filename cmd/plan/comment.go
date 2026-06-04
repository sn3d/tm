package plan

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

var commentCmd = &cli.Command{
	Name:  "comment",
	Usage: "Add a comment to a plan",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "id",
			Usage:    "Plan ID where to add comment",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "who",
			Usage:    "Who is adding comment to plan",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "comment",
			Usage:    "Comment to add to plan",
			Required: true,
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

		id := command.String("id")
		if err := c.AddPlanComment(id, command.String("who"), command.String("comment")); err != nil {
			return err
		}
		fmt.Printf("%s Added comment to plan %s\n", color.GreenString("✓"), color.New(color.Bold).Sprint(id))
		return nil
	},
}
