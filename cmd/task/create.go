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

// CreateCmd is the top-level `tm create` command.
var CreateCmd = &cli.Command{
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
			Name:  "parent",
			Usage: "Parent task ID (optional). Omit for a top-level task.",
		},
		&cli.StringFlag{
			Name:  "labels",
			Usage: "Comma-separated labels (e.g. bug,chore,area:auth)",
		},
		&cli.StringFlag{
			Name:  "mode",
			Usage: "Render/filter hint: standard | planning (default standard)",
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
		parent := command.String("parent")
		labels := parseLabels(command.String("labels"))
		mode, err := client.ParseTaskMode(command.String("mode"))
		if err != nil {
			return err
		}

		if subject == "" {
			draft, err := editor.EditTaskDraft(editor.TaskDraft{
				Subject:     subject,
				Description: description,
				Assigned:    assigned,
				DependsOn:   dependsOn,
				Parent:      parent,
				Labels:      labels,
				Mode:        string(mode),
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
			parent = draft.Parent
			labels = draft.Labels
			mode, err = client.ParseTaskMode(draft.Mode)
			if err != nil {
				return err
			}
		}

		id, err := c.CreateTask(client.CreateTaskInput{
			Subject:       subject,
			Description:   description,
			AssignedAgent: assigned,
			DependsOn:     dependsOn,
			ParentID:      parent,
			Labels:        labels,
			Mode:          mode,
		})
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
	return parseCommaList(raw)
}

// parseLabels splits the --labels flag value into a slice, sharing the same
// trim/split logic as --depends-on. Kept as a thin wrapper so call sites
// remain self-documenting.
func parseLabels(raw string) []string {
	return parseCommaList(raw)
}

func parseCommaList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
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
