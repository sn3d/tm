package project

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

var newCmd = &cli.Command{
	Name:      "new",
	Usage:     "Create a new project in the current instance",
	ArgsUsage: "<TICKER>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "name",
			Usage: "Human-readable project name (e.g. \"Task Manager for Agents\").",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		raw := command.Args().First()
		if raw == "" {
			return fmt.Errorf("ticker is required, e.g. `tm project new TM --name \"Task Manager\"`")
		}
		c, err := app.NewInstanceClient(cfg)
		if err != nil {
			return err
		}
		p, err := Create(c, raw, command.String("name"))
		if err != nil {
			return err
		}
		fmt.Printf("%s Created project %s\n", color.GreenString("✓"),
			color.New(color.Bold).Sprint(p.Label()))
		return nil
	},
}

// Create validates the ticker, refuses a duplicate, and inserts the row.
// Shared with `tm init --new` so both paths apply identical validation.
//
// DisplayName preserves the caller's casing while Ticker is canonicalised,
// which is what lets `TM` print as `TM` while `tm` and `TM` address the same
// project.
func Create(c *client.Client, rawTicker, name string) (*client.Project, error) {
	canon, err := client.CanonicalTicker(rawTicker)
	if err != nil {
		return nil, err
	}
	existing, err := c.Projects().GetByTicker(canon)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("project %s already exists (ticker %q); pick another ticker or use it directly",
			existing.Label(), canon)
	}
	p := &client.Project{
		Ticker:      canon,
		DisplayName: rawTicker,
		Name:        name,
	}
	if err := c.Projects().Save(p); err != nil {
		return nil, fmt.Errorf("create project %q: %w", canon, err)
	}
	return p, nil
}
