package timesheet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/urfave/cli/v3"
)

// dateOnly is the accepted bare-date form for --since / --until.
const dateOnly = "2006-01-02"

var Cmd = &cli.Command{
	Name:  "timesheet",
	Usage: "Weekly activity log from the journal (task create/start/finish + comments)",
	Description: "Reports create/start/finish milestones and comments in a date window, " +
		"resolved to task subjects. Defaults to the current Mon-Sun week. " +
		"Use --format json for machine consumption.",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "since", Usage: "Start of the window (RFC3339 or YYYY-MM-DD). Overrides the default week."},
		&cli.StringFlag{Name: "until", Usage: "End of the window, exclusive (RFC3339 or YYYY-MM-DD; a bare date covers the whole day). Overrides the default week."},
		&cli.BoolFlag{Name: "last-week", Usage: "Use the previous Mon-Sun week instead of the current one."},
		&cli.StringFlag{Name: "actor", Usage: "Filter to one actor. Omit for all actors."},
		&cli.StringFlag{Name: "format", Usage: "Output format: text|json.", Value: "text"},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		from, to, err := resolveRange(command, time.Now())
		if err != nil {
			return err
		}

		entries, err := c.Timesheet(from, to, command.String("actor"))
		if err != nil {
			return err
		}

		switch command.String("format") {
		case "json":
			return renderJSON(from, to, entries)
		case "text", "":
			renderText(entries)
			return nil
		default:
			return fmt.Errorf("--format: unknown value %q (want text|json)", command.String("format"))
		}
	},
}

// resolveRange determines the [from, to) window. --since/--until override the
// default week (either or both); otherwise --last-week or the current week is
// used. A bare-date --until is bumped to the start of the following day so the
// exclusive upper bound covers the whole named day.
func resolveRange(command *cli.Command, now time.Time) (from, to time.Time, err error) {
	var since, until *time.Time
	if s := command.String("since"); s != "" {
		ts, _, perr := parseBound(s)
		if perr != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--since: %w", perr)
		}
		since = &ts
	}
	if s := command.String("until"); s != "" {
		ts, bare, perr := parseBound(s)
		if perr != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--until: %w", perr)
		}
		if bare {
			ts = ts.AddDate(0, 0, 1) // cover the whole named day
		}
		until = &ts
	}
	from, to = client.ResolveTimesheetRange(now, command.Bool("last-week"), since, until)
	return from, to, nil
}

// parseBound parses a --since/--until value as RFC3339, falling back to a bare
// YYYY-MM-DD date (in local time). bare reports whether the date-only form
// matched, so callers can widen an exclusive end bound to the full day.
func parseBound(s string) (t time.Time, bare bool, err error) {
	if ts, e := time.Parse(time.RFC3339, s); e == nil {
		return ts, false, nil
	}
	if ts, e := time.ParseInLocation(dateOnly, s, time.Local); e == nil {
		return ts, true, nil
	}
	return time.Time{}, false, fmt.Errorf("%q is not RFC3339 or YYYY-MM-DD", s)
}

func renderText(entries []client.TimesheetEntry) {
	if len(entries) == 0 {
		fmt.Println(color.HiBlackString("No activity in range."))
		return
	}
	dim := color.HiBlackString
	for _, e := range entries {
		ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
		fmt.Printf("%s  %s  %s  %s  %s\n",
			dim(ts),
			color.YellowString(padRight(string(e.Activity), 10)),
			color.CyanString(padRight("task "+e.TaskID, 14)),
			padRight(truncate(e.TaskSubject, 40), 40),
			detail(e),
		)
	}
}

// detail renders the right-hand context column: the terminal state for a
// finished entry, or the (truncated) comment body for a commented one.
func detail(e client.TimesheetEntry) string {
	if e.Detail == "" {
		return ""
	}
	if e.Activity == client.ActivityCommented {
		return color.HiBlackString("“" + truncate(e.Detail, 60) + "”")
	}
	return color.HiBlackString(e.Detail)
}

func renderJSON(from, to time.Time, entries []client.TimesheetEntry) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"from":    from.Format(time.RFC3339),
		"to":      to.Format(time.RFC3339),
		"entries": client.ViewTimesheetEntries(entries),
	})
}

// padRight pads to n display columns, counting runes rather than bytes so
// multibyte subjects (e.g. Slovak, CJK) still align.
func padRight(s string, n int) string {
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// truncate shortens s to at most max runes, appending an ellipsis when it cuts.
// It slices on rune boundaries so multibyte text is never corrupted.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
