package task

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
	Usage: "Edit an existing task",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "id",
			Usage:    "Task ID",
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
			Usage: "State to set: draft | todo | in_progress | blocked | in_review | done | cancelled",
		},
		&cli.StringFlag{
			Name:  "assigned",
			Usage: "Assigned agent",
		},
		&cli.StringFlag{
			Name:  "depends-on",
			Usage: "Comma-separated list of task IDs this task depends on (replaces existing list; pass \"\" to clear)",
		},
		&cli.StringFlag{
			Name:  "plan",
			Usage: `Plan ID this task belongs to. Pass --plan "" to clear. DEPRECATED — use --parent.`,
		},
		&cli.StringFlag{
			Name:  "parent",
			Usage: `Parent task ID. Pass --parent "" to make top-level.`,
		},
		&cli.StringFlag{
			Name:  "labels",
			Usage: `Replacement comma-separated labels. Pass --labels "" to clear.`,
		},
		&cli.StringFlag{
			Name:  "mode",
			Usage: "Render/filter hint: standard | planning",
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
		t, err := c.GetTask(id)
		if err != nil {
			return err
		}

		subject := t.Subject
		description := t.Description
		state := t.State
		assigned := t.AssignedAgent
		dependsOn := t.DependsOn
		planID := t.PlanID
		parentID := t.ParentID
		labels := t.Labels
		mode := t.Mode

		interactive := !command.IsSet("subject") &&
			!command.IsSet("description") &&
			!command.IsSet("state") &&
			!command.IsSet("assigned") &&
			!command.IsSet("depends-on") &&
			!command.IsSet("plan") &&
			!command.IsSet("parent") &&
			!command.IsSet("labels") &&
			!command.IsSet("mode")

		if interactive {
			// this section is executed as 'interactive' mode
			draft, err := editor.EditTaskDraft(editor.TaskDraft{
				Subject:     subject,
				Description: description,
				Assigned:    assigned,
				State:       state.String(),
				DependsOn:   dependsOn,
				Plan:        planID,
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
			dependsOn = draft.DependsOn
			planID = draft.Plan
			if draft.State != "" {
				state, err = client.ParseTaskState(draft.State)
				if err != nil {
					return err
				}
			}
		} else {
			// this section is executed as regular command
			if command.IsSet("subject") {
				subject = command.String("subject")
			}
			if command.IsSet("description") {
				description = command.String("description")
			}
			if command.IsSet("state") {
				state, err = client.ParseTaskState(command.String("state"))
				if err != nil {
					return err
				}
			}
			if command.IsSet("assigned") {
				assigned = command.String("assigned")
			}
			if command.IsSet("depends-on") {
				dependsOn = parseDependsOn(command.String("depends-on"))
			}
			if command.IsSet("plan") {
				planID = command.String("plan")
			}
			if command.IsSet("parent") {
				parentID = command.String("parent")
			}
			if command.IsSet("labels") {
				labels = parseLabels(command.String("labels"))
			}
			if command.IsSet("mode") {
				mode, err = client.ParseTaskMode(command.String("mode"))
				if err != nil {
					return err
				}
			}
		}

		if err := c.EditTask(id, client.EditTaskInput{
			Subject:       subject,
			Description:   description,
			State:         state,
			AssignedAgent: assigned,
			DependsOn:     dependsOn,
			PlanID:        planID,
			ParentID:      parentID,
			Labels:        labels,
			Mode:          mode,
		}); err != nil {
			return err
		}
		fmt.Printf("%s Edited task %s\n", color.GreenString("✓"), color.New(color.Bold).Sprint(id))
		return nil
	},
}
