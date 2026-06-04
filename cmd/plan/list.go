package plan

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

var listCmd = &cli.Command{
	Name:  "list",
	Usage: "List all plans",
	Flags: []cli.Flag{},
	Action: func(ctx context.Context, _ *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		plans, err := c.ListPlans()
		if err != nil {
			return err
		}
		if len(plans) == 0 {
			fmt.Println(color.HiBlackString("No plans yet. Create one with: tm plan create --subject \"...\""))
			return nil
		}

		header := color.New(color.Bold).Sprint
		printRow(header("ID"), header("SUBJECT"), header("STATE"), header("AGENT"))
		for _, p := range plans {
			agent := tui.Dash(p.AssignedAgent)
			if p.AssignedAgent != "" {
				agent = tui.Truncate(p.AssignedAgent, tui.ColAgentWidth-2)
			}
			printRow(
				p.ID,
				tui.Truncate(p.Subject, tui.ColSubjectWidth-2),
				tui.PlanStateBadge(p.State),
				agent,
			)
		}
		return nil
	},
}

// printRow writes one aligned row to stdout using the fixed column widths.
// The last column is not padded since there's nothing after it.
func printRow(id, subject, state, agent string) {
	fmt.Println(
		tui.PadRight(id, tui.ColIDWidth) +
			tui.PadRight(subject, tui.ColSubjectWidth) +
			tui.PadRight(state, tui.ColStateWidth) +
			agent,
	)
}
