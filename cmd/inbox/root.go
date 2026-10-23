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
		&cli.BoolFlag{
			Name:  "all",
			Usage: "Show the inbox for every project in the instance.",
		},
		app.ActorFlag,
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		app.ApplyActor(cfg, command)
		peek := command.Bool("peek")
		limit := int(command.Int("limit"))
		resolver := tui.NewResolver(cfg.Styling)

		if command.Bool("all") {
			return runAll(cfg, peek, limit, resolver)
		}

		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}
		var box *client.Inbox
		if peek {
			box, err = c.PeekInbox(cfg.Actor)
		} else {
			box, err = c.Inbox(cfg.Actor)
		}
		if err != nil {
			return err
		}

		printTasks(box.Tasks, resolver, false)
		fmt.Println()
		printChanges(box.RecentChanges, box.LastSeenAt, limit, false)
		return nil
	},
}

// runAll prints one section per project with anything in it, spanning the
// resolved instance. Each project keeps its own cursor, so reading one
// project's section never marks another's work as seen.
func runAll(cfg *client.Config, peek bool, limit int, resolver *tui.Resolver) error {
	c, err := app.NewInstanceClient(cfg)
	if err != nil {
		return err
	}
	var boxes []*client.Inbox
	if peek {
		boxes, err = c.PeekInboxAll(cfg.Actor)
	} else {
		boxes, err = c.InboxAll(cfg.Actor)
	}
	if err != nil {
		return err
	}
	if len(boxes) == 0 {
		fmt.Println(color.HiBlackString("Nothing in any project."))
		return nil
	}
	for i, box := range boxes {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(color.New(color.Bold, color.FgCyan).Sprint(box.Project))
		printTasks(box.Tasks, resolver, true)
		fmt.Println()
		printChanges(box.RecentChanges, box.LastSeenAt, limit, true)
	}
	return nil
}

func printTasks(tasks []client.Task, resolver *tui.Resolver, qualified bool) {
	bold := color.New(color.Bold).Sprint
	fmt.Println(bold("Tasks"))
	if len(tasks) == 0 {
		fmt.Println(color.HiBlackString("  (none)"))
		return
	}
	taskHeader := bold
	printTaskRow(taskHeader("ID"), taskHeader("SUBJECT"), taskHeader("STATE"), taskHeader("PARENT"))
	for _, t := range tasks {
		parent := tui.Dash("")
		if !t.ParentID.IsZero() {
			parent = tui.Truncate(tui.RefBare(t.ParentID), tui.ColParentWidth-2)
		}
		printTaskRow(
			taskRefText(t.ID, qualified),
			tui.Truncate(t.Subject, tui.ColSubjectWidth-2),
			resolver.StateBadge(t.State),
			parent,
		)
	}
}

func printChanges(events []client.Event, lastSeen time.Time, limit int, qualified bool) {
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
			padRight(targetLabel(e, qualified), 16),
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

// targetLabel produces the task column. A cross-project view qualifies the
// ref so the reader can tell which repo an entry came from.
func targetLabel(e client.Event, qualified bool) string {
	if e.TaskID.IsZero() {
		return "-"
	}
	if qualified {
		return e.TaskID.String()
	}
	return "task " + e.TaskID.ID
}

// taskRefText renders a task's own ref for the ID column.
func taskRefText(ref client.TaskID, qualified bool) string {
	if qualified {
		return ref.String()
	}
	return ref.ID
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
