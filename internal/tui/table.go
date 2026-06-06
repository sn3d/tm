package tui

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ColIDWidth      = 28
	ColSubjectWidth = 32
	ColStateWidth   = 16
	ColAgentWidth   = 18
	ColParentWidth  = 14
)

var (
	ansiRE  = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	emojiRE = regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]`)
)

// PadRight returns s padded with spaces to occupy width visible cells. ANSI
// color codes are stripped before measuring; emojis in our state badges are
// counted as two cells (handled by VisualWidth).
func PadRight(s string, width int) string {
	used := VisualWidth(s)
	if used >= width {
		return s
	}
	return s + strings.Repeat(" ", width-used)
}

// VisualWidth returns the terminal column count of s. ANSI escape sequences
// contribute zero. Emojis (the ones we emit in state badges) contribute two;
// every other rune contributes one. Good enough for our state column — not a
// general-purpose Unicode width calculator.
func VisualWidth(s string) int {
	stripped := ansiRE.ReplaceAllString(s, "")
	emojiCount := len(emojiRE.FindAllString(stripped, -1))
	withoutEmoji := emojiRE.ReplaceAllString(stripped, "")
	return utf8.RuneCountInString(withoutEmoji) + emojiCount*2
}

// Truncate clips s to max-1 runes plus an ellipsis when longer than max.
func Truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
