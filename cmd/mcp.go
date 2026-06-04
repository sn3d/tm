package cmd

import (
	"context"

	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/mcp"
	"github.com/urfave/cli/v3"
)

var mcpCmd = &cli.Command{
	Name:  "mcp",
	Usage: "Start the local MCP stdio server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "actor",
			Usage: "Actor name recorded on every event the server emits.",
			Value: "agent",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		cfg.Actor = command.String("actor")
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}
		return mcp.NewServer(c).ServeStdio()
	},
}
