package journal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

var listCmd = &cli.Command{
	Name:  "list",
	Usage: "List journal events, oldest at top",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "task", Usage: "Filter by task ID"},
		&cli.StringFlag{Name: "actor", Usage: "Filter by actor (agent / user name)"},
		&cli.StringSliceFlag{Name: "kind", Usage: "Filter by event kind (repeatable; OR semantics)"},
		&cli.StringFlag{Name: "since", Usage: "Only events after this RFC3339 timestamp"},
		&cli.IntFlag{Name: "limit", Usage: "Max events to return (0 = no limit)", Value: 50},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		filter := client.EventFilter{
			TaskID: command.String("task"),
			Actor:  command.String("actor"),
			Limit:  int(command.Int("limit")),
		}
		for _, k := range command.StringSlice("kind") {
			filter.Kinds = append(filter.Kinds, client.EventKind(k))
		}
		if s := command.String("since"); s != "" {
			ts, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			filter.Since = ts
		}

		events, err := c.ListEvents(filter)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			fmt.Println(color.HiBlackString("No events yet."))
			return nil
		}

		// Repository returns newest-first so --limit selects the N most recent;
		// flip to oldest-at-top here so the printed output reads like a log.
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}

		dim := color.HiBlackString
		for _, e := range events {
			ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
			target := targetLabel(e)
			fmt.Printf("%s  %s  %s  %s  %s\n",
				dim(ts),
				color.CyanString(padRight(e.Actor, 10)),
				padRight(target, 16),
				color.YellowString(padRight(verbFromKind(e.Kind), 22)),
				summary(e),
			)
		}
		return nil
	},
}

// targetLabel produces the "task TASK-5" column.
func targetLabel(e client.Event) string {
	if e.TaskID != "" {
		return "task " + e.TaskID
	}
	return "-"
}

// verbFromKind strips the "task." prefix so the column reads
// "state_changed" rather than "task.state_changed" — the target column
// already shows which entity it is.
func verbFromKind(k client.EventKind) string {
	s := string(k)
	if i := strings.Index(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// summary renders the kind-specific payload as a single line. Best-effort:
// unknown shapes fall through to a JSON-ish dump.
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
		// also handle []string after a round-trip through the in-memory backend
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
