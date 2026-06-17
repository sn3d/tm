package task

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

// ListCmd is the top-level `tm list [label]` command.
var ListCmd = &cli.Command{
	Name:      "list",
	Usage:     "List tasks, optionally filtered by label",
	ArgsUsage: "[label]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "parent",
			Usage: `Filter tasks by parent ID. Pass --parent "" to list only top-level tasks.`,
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		label := command.Args().First()

		var tasks []client.Task
		switch {
		case label != "":
			tasks, err = c.GetTasksByLabel(label)
		case command.IsSet("parent"):
			tasks, err = c.GetTasksByParent(command.String("parent"))
		default:
			tasks, err = c.ListTasks()
		}
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			fmt.Println(color.HiBlackString(`No tasks yet. Create one with: tm create --subject "..."`))
			return nil
		}

		sort.SliceStable(tasks, func(i, j int) bool {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		})

		header := color.New(color.Bold).Sprint
		printRow(header("ID"), header("SUBJECT"), header("STATE"), header("AGENT"), header("LABELS"), header("PARENT"))
		for _, t := range tasks {
			agent := tui.Dash(t.AssignedAgent)
			if t.AssignedAgent != "" {
				agent = tui.Truncate(t.AssignedAgent, tui.ColAgentWidth-2)
			}
			labels := tui.Dash("")
			if len(t.Labels) > 0 {
				labels = tui.Truncate(strings.Join(t.Labels, ","), tui.ColLabelsWidth-2)
			}
			parent := tui.Dash(t.ParentID)
			if t.ParentID != "" {
				parent = tui.Truncate(t.ParentID, tui.ColParentWidth-2)
			}
			printRow(
				t.ID,
				tui.Truncate(t.Subject, tui.ColSubjectWidth-2),
				tui.TaskStateBadge(t.State),
				agent,
				labels,
				parent,
			)
		}
		return nil
	},
}

// printRow writes one aligned row to stdout using the fixed column widths.
// The last column is not padded since there's nothing after it.
func printRow(id, subject, state, agent, labels, parent string) {
	fmt.Println(
		tui.PadRight(id, tui.ColIDWidth) +
			tui.PadRight(subject, tui.ColSubjectWidth) +
			tui.PadRight(state, tui.ColStateWidth) +
			tui.PadRight(agent, tui.ColAgentWidth) +
			tui.PadRight(labels, tui.ColLabelsWidth) +
			parent,
	)
}
