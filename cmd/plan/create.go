package plan

import (
	"context"
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/editor"
	"github.com/urfave/cli/v3"
)

var createCmd = &cli.Command{
	Name:  "create",
	Usage: "Create a new plan",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "subject",
			Aliases: []string{"s"},
			Usage:   "Subject of plan (if omitted, opens $EDITOR with a template)",
		},
		&cli.StringFlag{
			Name:        "description",
			Aliases:     []string{"d"},
			Usage:       "Description of plan (empty string if not set)",
			DefaultText: "",
		},
		&cli.StringFlag{
			Name:        "assigned",
			Aliases:     []string{"a"},
			Usage:       "Assigned agent of plan",
			DefaultText: "none",
		},
		app.ActorFlag,
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		app.ApplyActor(cfg, command)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		subject := command.String("subject")
		description := command.String("description")
		assigned := command.String("assigned")

		if subject == "" {
			draft, err := editor.EditPlanDraft(editor.PlanDraft{
				Subject:     subject,
				Description: description,
				Assigned:    assigned,
			})
			if errors.Is(err, editor.ErrNotTerminal) {
				return fmt.Errorf("--subject is required when not running in a terminal")
			}
			if err != nil {
				return err
			}
			if draft.Subject == "" {
				return fmt.Errorf("aborting: subject is empty")
			}
			subject = draft.Subject
			description = draft.Description
			assigned = draft.Assigned
		}

		id, err := c.CreatePlan(subject, description, assigned)
		if err != nil {
			return err
		}
		fmt.Printf("%s Created plan %s\n", color.GreenString("✓"), color.New(color.Bold).Sprint(id))
		return nil
	},
}
