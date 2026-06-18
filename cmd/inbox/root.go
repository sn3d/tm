package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

var Cmd = &cli.Command{
	Name:  "inbox",
	Usage: "Show your assigned open work and changes since you last looked",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "peek",
			Usage: "Do not advance the last-seen cursor; show the same recent changes on the next call.",
		},
		&cli.IntFlag{
			Name:  "limit",
			Usage: "Max recent changes to return (0 = no limit).",
			Value: 50,
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

		var box *client.Inbox
		if command.Bool("peek") {
			box, err = c.PeekInbox(cfg.Actor)
		} else {
			box, err = c.Inbox(cfg.Actor)
		}
		if err != nil {
			return err
		}

		limit := int(command.Int("limit"))
		resolver := tui.NewResolver(cfg.Styling)
		printTasks(box.Tasks, resolver)
		fmt.Println()
		printChanges(box.RecentChanges, box.LastSeenAt, limit)
		return nil
	},
}

func printTasks(tasks []client.Task, resolver *tui.Resolver) {
	bold := color.New(color.Bold).Sprint
	fmt.Println(bold("Tasks"))
	if len(tasks) == 0 {
		fmt.Println(color.HiBlackString("  (none)"))
		return
	}
	taskHeader := bold
	printTaskRow(taskHeader("ID"), taskHeader("SUBJECT"), taskHeader("STATE"), taskHeader("PARENT"))
	for _, t := range tasks {
		parent := tui.Dash(t.ParentID)
		if t.ParentID != "" {
			parent = tui.Truncate(t.ParentID, tui.ColParentWidth-2)
		}
		printTaskRow(
			t.ID,
			tui.Truncate(t.Subject, tui.ColSubjectWidth-2),
			resolver.StateBadge(t.State),
			parent,
		)
	}
}

func printChanges(events []client.Event, lastSeen time.Time, limit int) {
	bold := color.New(color.Bold).Sprint
	header := "Since " + formatSince(lastSeen)
	fmt.Println(bold(header))
	if len(events) == 0 {
		fmt.Println(color.HiBlackString("  (none)"))
		return
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	dim := color.HiBlackString
	for _, e := range events {
		ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
		fmt.Printf("%s  %s  %s  %s  %s\n",
			dim(ts),
			color.CyanString(padRight(e.Actor, 10)),
			padRight(targetLabel(e), 16),
			color.YellowString(padRight(verbFromKind(e.Kind), 22)),
			summary(e),
		)
	}
}

func formatSince(t time.Time) string {
	if t.IsZero() {
		return "(first time)"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func printTaskRow(id, subject, state, parent string) {
	fmt.Println(
		tui.PadRight(id, tui.ColIDWidth) +
			tui.PadRight(subject, tui.ColSubjectWidth) +
			tui.PadRight(state, tui.ColStateWidth) +
			parent,
	)
}

func targetLabel(e client.Event) string {
	if e.TaskID != "" {
		return "task " + e.TaskID
	}
	return "-"
}

func verbFromKind(k client.EventKind) string {
	s := string(k)
	if i := strings.Index(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

func summary(e client.Event) string {
	switch e.Kind {
	case client.EventTaskStateChanged, client.EventTaskAssigned,
		client.EventTaskParentChanged, client.EventTaskModeChanged:
		return fmt.Sprintf("%s → %s", strOrDash(e.Payload["from"]), strOrDash(e.Payload["to"]))
	case client.EventTaskDependsOnChanged, client.EventTaskLabelsChanged:
		return fmt.Sprintf("%s → %s", listOrDash(e.Payload["from"]), listOrDash(e.Payload["to"]))
	case client.EventTaskCreated:
		return strOrDash(e.Payload["subject"])
	case client.EventTaskCommented:
		return fmt.Sprintf("by %s (%s)", strOrDash(e.Payload["who"]), strOrDash(e.Payload["comment_id"]))
	case client.EventTaskEdited:
		return editedFields(e.Payload)
	default:
		return ""
	}
}

func editedFields(p map[string]any) string {
	to, _ := p["to"].(map[string]any)
	if len(to) == 0 {
		return ""
	}
	keys := make([]string, 0, len(to))
	for k := range to {
		keys = append(keys, k)
	}
	return "fields: " + strings.Join(keys, ", ")
}

func strOrDash(v any) string {
	s, _ := v.(string)
	if s == "" {
		return "—"
	}
	return s
}

func listOrDash(v any) string {
	arr, ok := v.([]any)
	if !ok {
		if ids, ok := v.([]string); ok {
			if len(ids) == 0 {
				return "—"
			}
			return "[" + strings.Join(ids, ",") + "]"
		}
		return "—"
	}
	if len(arr) == 0 {
		return "—"
	}
	parts := make([]string, len(arr))
	for i, x := range arr {
		s, _ := x.(string)
		parts[i] = s
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
