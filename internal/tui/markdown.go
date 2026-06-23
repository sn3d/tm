package tui

import (
	"os"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"
)

// Markdown renders markdown text for terminal display: headings, bold, code,
// lists, etc. When stdout is not a TTY (piped, redirected, CI), or rendering
// fails, the input is returned unchanged so downstream consumers see clean
// markdown source rather than ANSI escapes.
func Markdown(s string) string {
	if s == "" {
		return s
	}

	width := TerminalWidth()
	style := styles.AutoStyle
	if width == 0 {
		style = styles.NoTTYStyle
	}

	wrap := 80
	if width > 4 {
		wrap = width - 4
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		return s
	}
	out, err := r.Render(s)
	if err != nil {
		return s
	}
	return out
}

// TerminalWidth returns the stdout terminal width in columns, or 0 when
// stdout is not a TTY.
func TerminalWidth() int {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 0
	}
	w, _, err := term.GetSize(fd)
	if err != nil || w <= 0 {
		return 0
	}
	return w
}
