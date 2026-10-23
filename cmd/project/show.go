package project

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

var showCmd = &cli.Command{
	Name:      "show",
	Usage:     "Show one project's details (defaults to the current folder's project)",
	ArgsUsage: "[TICKER]",
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewInstanceClient(cfg)
		if err != nil {
			return err
		}

		raw := command.Args().First()
		if raw == "" {
			raw = cfg.Resolved.Project
		}
		if raw == "" {
			return fmt.Errorf("no project mapped for this folder; pass a ticker, e.g. `tm project show TM`")
		}
		canon, err := client.CanonicalTicker(raw)
		if err != nil {
			return err
		}
		p, err := c.Projects().GetByTicker(canon)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("no project with ticker %q in instance %s", canon, cfg.Resolved.Instance)
		}
		counts, err := c.Projects().TaskCounts()
		if err != nil {
			return err
		}

		bold := color.New(color.Bold).Sprint
		dim := color.HiBlackString
		fmt.Printf("%s %s\n", bold(p.Label()), dim("("+p.Ticker+")"))
		fmt.Printf("   %s %s\n", dim("name:"), tui.Dash(p.Name))
		fmt.Printf("   %s %s\n", dim("instance:"), cfg.Resolved.Instance)
		fmt.Printf("   %s %d\n", dim("active tasks:"), counts[p.Ticker])
		if !p.CreatedAt.IsZero() {
			fmt.Printf("   %s %s\n", dim("created:"), p.CreatedAt.Local().Format(time.RFC3339))
		}
		if p.ArchivedAt != nil {
			fmt.Printf("   %s %s\n", dim("archived:"),
				color.YellowString(p.ArchivedAt.Local().Format(time.RFC3339)))
		}
		// Folders mapping to this project, so `show` answers "where does this
		// project live on disk".
		var folders []string
		for folder, value := range cfg.Mappings {
			if _, ticker, perr := client.ParseMapping(value, cfg.DefaultInstance); perr == nil && ticker == canon {
				folders = append(folders, folder)
			}
		}
		if len(folders) > 0 {
			fmt.Printf("   %s\n", dim("folders:"))
			for _, f := range folders {
				fmt.Printf("     %s\n", f)
			}
		}
		return nil
	},
}
