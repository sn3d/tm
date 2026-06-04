package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/editor"
	"github.com/urfave/cli/v3"
)

var createCmd = &cli.Command{
	Name:  "create",
	Usage: "Create a new task",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subject",
			Usage: "Subject of task (if omitted, opens $EDITOR with a template)",
		},
		&cli.StringFlag{
			Name:        "description",
			Usage:       "Description of task (empty string if not set)",
			DefaultText: "",
		},
		&cli.StringFlag{
			Name:        "assigned",
			Usage:       "Assigned agent of task",
			DefaultText: "none",
		},
		&cli.StringFlag{
			Name:  "depends-on",
			Usage: "Comma-separated list of task IDs this task depends on",
		},
		&cli.StringFlag{
			Name:  "plan",
			Usage: "Plan ID this task belongs to (optional)",
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
		dependsOn := parseDependsOn(command.String("depends-on"))
		plan := command.String("plan")

		if subject == "" {
			draft, err := editor.EditTaskDraft(editor.TaskDraft{
				Subject:     subject,
				Description: description,
				Assigned:    assigned,
				DependsOn:   dependsOn,
				Plan:        plan,
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
			dependsOn = draft.DependsOn
			plan = draft.Plan
		}

		id, err := c.CreateTask(subject, description, assigned, dependsOn, plan)
		if err != nil {
			return err
		}
		fmt.Printf("%s Created task %s\n", color.GreenString("✓"), color.New(color.Bold).Sprint(id))
		return nil
	},
}

// parseDependsOn splits the --depends-on flag value into a slice of task IDs,
// trimming whitespace and dropping empty entries. Returns nil for an empty
// string so the resulting task has no dependency list at all.
func parseDependsOn(raw string) []client.TaskID {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]client.TaskID, 0, len(parts))
	for _, p := range parts {
		if id := strings.TrimSpace(p); id != "" {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
