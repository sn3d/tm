package project

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
	Usage: "List projects in the current instance",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "archived",
			Usage: "Include archived projects.",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewInstanceClient(cfg)
		if err != nil {
			return err
		}
		projects, err := c.Projects().GetAll(command.Bool("archived"))
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Println(color.HiBlackString("No projects yet. Create one with: tm project new TICKER --name \"...\""))
			return nil
		}
		counts, err := c.Projects().TaskCounts()
		if err != nil {
			return err
		}
		return Render(projects, counts, cfg.Resolved.Project)
	},
}

// Render prints the project table, marking the currently-resolved project so
// a user running the command inside a mapped repo can see where they are.
// Shared with `tm init --list` so the two surfaces cannot drift.
func Render(projects []client.Project, counts map[string]int, current string) error {
	bold := color.New(color.Bold).Sprint
	fmt.Println(tui.PadRight(bold("TICKER"), 18) +
		tui.PadRight(bold("NAME"), 34) +
		tui.PadRight(bold("TASKS"), 8) + bold("STATUS"))
	for _, p := range projects {
		status := ""
		if p.ArchivedAt != nil {
			status = color.YellowString("archived")
		}
		marker := "  "
		if current != "" && p.Ticker == current {
			marker = color.GreenString("→ ")
		}
		row := marker +
			tui.PadRight(p.Label(), 16) +
			tui.PadRight(tui.Truncate(tui.Dash(p.Name), 32), 34) +
			tui.PadRight(fmt.Sprintf("%d", counts[p.Ticker]), 8) +
			status
		fmt.Println(row)
	}
	return nil
}
