package task

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
	Usage: "List all tasks",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "parent",
			Usage: `Filter tasks by parent ID. Pass --parent "" to list only top-level tasks.`,
		},
		&cli.StringFlag{
			Name:  "plan",
			Usage: `DEPRECATED — use --parent. Filter tasks by parent ID.`,
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		var tasks []client.Task
		switch {
		case command.IsSet("parent"):
			tasks, err = c.GetTasksByParent(command.String("parent"))
		case command.IsSet("plan"):
			tasks, err = c.GetTasksByParent(command.String("plan"))
		default:
			tasks, err = c.ListTasks()
		}
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			fmt.Println(color.HiBlackString("No tasks yet. Create one with: tm task create --subject \"...\""))
			return nil
		}

		header := color.New(color.Bold).Sprint
		printRow(header("ID"), header("SUBJECT"), header("STATE"), header("AGENT"), header("PARENT"))
		for _, t := range tasks {
			agent := tui.Dash(t.AssignedAgent)
			if t.AssignedAgent != "" {
				agent = tui.Truncate(t.AssignedAgent, tui.ColAgentWidth-2)
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
				parent,
			)
		}
		return nil
	},
}

// printRow writes one aligned row to stdout using the fixed column widths.
// The last column is not padded since there's nothing after it.
func printRow(id, subject, state, agent, parent string) {
	fmt.Println(
		tui.PadRight(id, tui.ColIDWidth) +
			tui.PadRight(subject, tui.ColSubjectWidth) +
			tui.PadRight(state, tui.ColStateWidth) +
			tui.PadRight(agent, tui.ColAgentWidth) +
			parent,
	)
}
