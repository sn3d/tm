package app

import (
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// ActorFlag is the shared --actor flag definition. Mutating CLI commands
// embed it in their Flags list and call ApplyActor before app.NewClient so
// the override propagates into journal events.
var ActorFlag = &cli.StringFlag{
	Name:  "actor",
	Usage: "Actor name recorded on every journal event this command emits (overrides taskmanager.yaml and $USER).",
}

// ApplyActor copies a non-empty --actor flag value into cfg.Actor. Call
// before app.NewClient(cfg) so the resulting Client embeds the override.
func ApplyActor(cfg *client.Config, command *cli.Command) {
	if v := command.String("actor"); v != "" {
		cfg.Actor = v
	}
}
