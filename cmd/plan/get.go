package plan

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

var getCmd = &cli.Command{
	Name:  "get",
	Usage: "Get a single plan by ID",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "id",
			Usage:    "Plan ID",
			Required: true,
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		id := command.String("id")
		p, err := c.GetPlan(id)
		if err != nil {
			return err
		}
		tasks, err := c.GetTasksByPlan(id)
		if err != nil {
			return err
		}
		comments, err := c.GetPlanComments(id)
		if err != nil {
			return err
		}

		bold := color.New(color.Bold).Sprint
		dim := color.HiBlackString

		fmt.Printf("%s: %s\n", bold(p.ID), p.Subject)
		fmt.Printf("   %s %s\n", dim("status:"), tui.PlanStateBadge(p.State))
		fmt.Printf("   %s %s\n", dim("agent:"), tui.Dash(p.AssignedAgent))

		fmt.Println()
		fmt.Println(bold("Description:"))
		if p.Description != "" {
			fmt.Print(tui.Markdown(p.Description))
		} else {
			fmt.Println(dim("(none)"))
		}

		fmt.Println()
		fmt.Println(bold("Tasks:"))
		if len(tasks) == 0 {
			fmt.Println(dim("(none)"))
		} else {
			header := color.New(color.Bold).Sprint
			printTaskRow(header("ID"), header("SUBJECT"), header("STATE"))
			for _, t := range tasks {
				printTaskRow(
					t.ID,
					tui.Truncate(t.Subject, tui.ColSubjectWidth-2),
					tui.TaskStateBadge(t.State),
				)
			}
		}

		fmt.Println()
		fmt.Println(bold("Comments:"))
		if len(comments) == 0 {
			fmt.Println(dim("(none)"))
			return nil
		}
		for i, com := range comments {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("%s %s\n", color.MagentaString("•"), bold(com.Who))
			fmt.Println(indent(com.Comment, "  "))
		}
		return nil
	},
}

// printTaskRow writes one aligned 3-column task row using the shared widths.
// The last column is not padded since there's nothing after it.
func printTaskRow(id, subject, state string) {
	fmt.Println(
		tui.PadRight(id, tui.ColIDWidth) +
			tui.PadRight(subject, tui.ColSubjectWidth) +
			state,
	)
}

// indent prefixes every non-empty line in s with prefix.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
