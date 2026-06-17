package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

// GetCmd is the top-level `tm get <id>` command.
var GetCmd = &cli.Command{
	Name:      "get",
	Usage:     "Get a single task by ID",
	ArgsUsage: "<id>",
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		id := command.Args().First()
		if id == "" {
			return fmt.Errorf("task ID is required, e.g. `tm get 123`")
		}
		t, err := c.GetTask(id)
		if err != nil {
			return err
		}
		comments, err := c.GetTaskComments(id)
		if err != nil {
			return err
		}

		bold := color.New(color.Bold).Sprint
		dim := color.HiBlackString

		fmt.Printf("%s: %s\n", bold(t.ID), t.Subject)
		fmt.Printf("   %s %s\n", dim("status:"), tui.TaskStateBadge(t.State))
		if t.ArchivedAt != nil {
			fmt.Printf("   %s %s\n", dim("archived:"),
				color.YellowString("since %s", t.ArchivedAt.Format(time.RFC3339Nano)))
		}
		fmt.Printf("   %s %s\n", dim("agent:"), tui.Dash(t.AssignedAgent))
		fmt.Printf("   %s %s\n", dim("parent:"), tui.Dash(t.ParentID))
		if len(t.DependsOn) > 0 {
			fmt.Printf("   %s %s\n", dim("depends on:"), strings.Join(t.DependsOn, ", "))
		}

		fmt.Println()
		fmt.Println(bold("Description:"))
		if t.Description != "" {
			fmt.Print(tui.Markdown(t.Description))
		} else {
			fmt.Println(dim("(none)"))
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
