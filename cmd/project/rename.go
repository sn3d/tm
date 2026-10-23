package project

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// renameCmd changes a project's human-readable name and/or display casing.
// It deliberately does NOT change the ticker: the ticker is the primary key
// that every task, comment and journal row references, so renaming it would
// be a data migration rather than a rename. It exists mainly to fix the
// placeholder names migration 00006 derives from directory names.
var renameCmd = &cli.Command{
	Name:      "rename",
	Usage:     "Change a project's name or display casing (the ticker itself is immutable)",
	ArgsUsage: "<TICKER>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "name",
			Usage: "New human-readable project name.",
		},
		&cli.StringFlag{
			Name:  "display",
			Usage: "New display casing for the ticker (e.g. WebApp). Must canonicalise to the same ticker.",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		raw := command.Args().First()
		if raw == "" {
			return fmt.Errorf("ticker is required, e.g. `tm project rename TM --name \"Task Manager\"`")
		}
		if !command.IsSet("name") && !command.IsSet("display") {
			return fmt.Errorf("pass --name and/or --display to change something")
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
		if command.IsSet("name") {
			p.Name = command.String("name")
		}
		if command.IsSet("display") {
			display := command.String("display")
			// A display name that canonicalises to a different ticker would
			// silently point the UI at another project's identity.
			displayCanon, cerr := client.CanonicalTicker(display)
			if cerr != nil {
				return fmt.Errorf("--display: %w", cerr)
			}
			if displayCanon != canon {
				return fmt.Errorf("--display %q canonicalises to %q, not %q; the ticker itself cannot be changed",
					display, displayCanon, canon)
			}
			p.DisplayName = display
		}
		if err := c.Projects().Save(p); err != nil {
			return err
		}
		fmt.Printf("%s Updated project %s\n", color.GreenString("✓"),
			color.New(color.Bold).Sprint(p.Label()))
		return nil
	},
}
