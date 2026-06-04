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

var editCmd = &cli.Command{
	Name:  "edit",
	Usage: "Edit an existing plan",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "id",
			Usage:    "Plan ID",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "subject",
			Usage:   "Subject to edit",
			Aliases: []string{"s"},
		},
		&cli.StringFlag{
			Name:    "description",
			Usage:   "Description to edit",
			Aliases: []string{"d"},
		},
		&cli.StringFlag{
			Name:  "state",
			Usage: "State to set: draft | active | on_hold | completed | cancelled",
		},
		&cli.StringFlag{
			Name:    "assigned",
			Usage:   "Assigned agent",
			Aliases: []string{"a"},
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

		id := command.String("id")
		p, err := c.GetPlan(id)
		if err != nil {
			return err
		}

		subject := p.Subject
		description := p.Description
		state := p.State
		assigned := p.AssignedAgent

		interactive := !command.IsSet("subject") &&
			!command.IsSet("description") &&
			!command.IsSet("state") &&
			!command.IsSet("assigned")

		if interactive {
			draft, err := editor.EditPlanDraft(editor.PlanDraft{
				Subject:     subject,
				Description: description,
				Assigned:    assigned,
				State:       state.String(),
			})
			if errors.Is(err, editor.ErrNotTerminal) {
				return fmt.Errorf("specify at least one field flag when not running in a terminal")
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
			if draft.State != "" {
				state, err = client.ParsePlanState(draft.State)
				if err != nil {
					return err
				}
			}
		} else {
			if command.IsSet("subject") {
				subject = command.String("subject")
			}
			if command.IsSet("description") {
				description = command.String("description")
			}
			if command.IsSet("state") {
				state, err = client.ParsePlanState(command.String("state"))
				if err != nil {
					return err
				}
			}
			if command.IsSet("assigned") {
				assigned = command.String("assigned")
			}
		}

		if err := c.EditPlan(id, subject, description, state, assigned); err != nil {
			return err
		}
		fmt.Printf("%s Edited plan %s\n", color.GreenString("✓"), color.New(color.Bold).Sprint(id))
		return nil
	},
}
