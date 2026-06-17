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
		&cli.BoolFlag{
			Name:  "archived",
			Usage: "Show only archived tasks (default hides them).",
		},
		&cli.BoolFlag{
			Name:  "all",
			Usage: "Show every task including archived ones.",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		if command.Bool("archived") && command.Bool("all") {
			return fmt.Errorf("--archived and --all are mutually exclusive")
		}
		archived := client.ArchivedActive
		switch {
		case command.Bool("archived"):
			archived = client.ArchivedOnly
		case command.Bool("all"):
			archived = client.ArchivedAll
		}

		label := command.Args().First()

		var tasks []client.Task
		switch {
		case label != "":
			tasks, err = c.GetTasksByLabel(label, archived)
		case command.IsSet("parent"):
			tasks, err = c.GetTasksByParent(command.String("parent"), archived)
		default:
			tasks, err = c.ListTasks(archived)
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
		fmt.Println(formatRow(header("ID"), header("SUBJECT"), header("STATE"), header("AGENT"), header("LABELS"), header("PARENT")))
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
			row := formatRow(
				t.ID,
				tui.Truncate(t.Subject, tui.ColSubjectWidth-2),
				tui.TaskStateBadge(t.State),
				agent,
				labels,
				parent,
			)
			// Archived rows render dimmed end-to-end so the user can tell at a
			// glance which rows are hidden by default. The state emoji's own
			// colour escape gets overridden by the wrap — that's intentional;
			// the dim treatment is the signal.
			if t.ArchivedAt != nil {
				row = color.HiBlackString(row)
			}
			fmt.Println(row)
		}
		return nil
	},
}

// formatRow assembles one aligned row using the fixed column widths. The last
// column is not padded since there's nothing after it. Returned as a string
// (not printed) so the caller can wrap archived rows in a dim colour helper.
func formatRow(id, subject, state, agent, labels, parent string) string {
	return tui.PadRight(id, tui.ColIDWidth) +
		tui.PadRight(subject, tui.ColSubjectWidth) +
		tui.PadRight(state, tui.ColStateWidth) +
		tui.PadRight(agent, tui.ColAgentWidth) +
		tui.PadRight(labels, tui.ColLabelsWidth) +
		parent
}
