package project

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// archiveCmd hides a project from pickers and lists. Archive over delete:
// destroying a project would destroy its tasks, comments and journal. An
// explicit --purge can come later if genuinely wanted.
var archiveCmd = &cli.Command{
	Name:      "archive",
	Usage:     "Archive a project (hides it from pickers and lists; its data is kept)",
	ArgsUsage: "<TICKER>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "undo",
			Usage: "Unarchive the project instead.",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		raw := command.Args().First()
		if raw == "" {
			return fmt.Errorf("ticker is required, e.g. `tm project archive OLD`")
		}
		c, err := app.NewInstanceClient(cfg)
		if err != nil {
			return err
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
		if command.Bool("undo") {
			if p.ArchivedAt == nil {
				fmt.Printf("Project %s is not archived.\n", p.Label())
				return nil
			}
			p.ArchivedAt = nil
		} else {
			if p.ArchivedAt != nil {
				fmt.Printf("Project %s is already archived.\n", p.Label())
				return nil
			}
			now := time.Now().UTC()
			p.ArchivedAt = &now
		}
		if err := c.Projects().Save(p); err != nil {
			return err
		}
		verb := "Archived"
		if command.Bool("undo") {
			verb = "Unarchived"
		}
		fmt.Printf("%s %s project %s\n", color.GreenString("✓"), verb,
			color.New(color.Bold).Sprint(p.Label()))
		return nil
	},
}
