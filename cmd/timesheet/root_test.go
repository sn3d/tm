package timesheet

import (
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"short ascii", "hello", 40, "hello"},
		{"exact ascii", "hello", 5, "hello"},
		{"long ascii", "hello world", 5, "hell…"},
		{"multibyte fits by runes", "rozpočtu", 8, "rozpočtu"},
		{"multibyte truncated on rune boundary", "Aktualizácia rozpočtu", 10, "Aktualizá…"},
		{"cjk truncated", "日本語のタスク", 4, "日本語…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q,%d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncate produced invalid UTF-8: %q", got)
			}
			if utf8.RuneCountInString(got) > tt.max {
				t.Errorf("truncate exceeded max runes: %q (%d > %d)", got, utf8.RuneCountInString(got), tt.max)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		n       int
		wantLen int // expected rune width
	}{
		{"ascii pads", "abc", 5, 5},
		{"ascii exact", "abcde", 5, 5},
		{"ascii over", "abcdef", 5, 6},
		{"multibyte pads to rune width", "rozpočtu", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padRight(tt.in, tt.n)
			if w := utf8.RuneCountInString(got); w != tt.wantLen {
				t.Errorf("padRight(%q,%d) rune width = %d, want %d", tt.in, tt.n, w, tt.wantLen)
			}
		})
	}
}
