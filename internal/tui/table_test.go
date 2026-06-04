package tui

import (
	"testing"

	"github.com/fatih/color"
)

func TestPadRight(t *testing.T) {
	// force color codes regardless of TTY so ANSI cases run deterministically.
	color.NoColor = false

	red := color.New(color.FgRed).SprintFunc()

	tests := []struct {
		name    string
		in      string
		width   int
		wantLen int
	}{
		{
			name:    "ascii pads to width",
			in:      "abc",
			width:   10,
			wantLen: 10,
		},
		{
			name:    "ascii at width is untouched length",
			in:      "abcdefghij",
			width:   10,
			wantLen: 10,
		},
		{
			name:    "ansi color codes do not count toward width",
			in:      red("abc"),
			width:   10,
			wantLen: 10,
		},
		{
			name:    "emoji counts as two cells",
			in:      "🔄 in_progress",
			width:   16,
			wantLen: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PadRight(tt.in, tt.width)
			if VisualWidth(got) != tt.wantLen {
				t.Fatalf("PadRight(%q, %d) visual width = %d, want %d (got string %q)",
					tt.in, tt.width, VisualWidth(got), tt.wantLen, got)
			}
		})
	}
}

func TestVisualWidth(t *testing.T) {
	color.NoColor = false
	red := color.New(color.FgRed).SprintFunc()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{"plain ascii", "hello", 5},
		{"empty", "", 0},
		{"ansi stripped", red("hello"), 5},
		{"emoji counted as 2", "✅", 2},
		{"emoji and text", "✅ completed", 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisualWidth(tt.in); got != tt.want {
				t.Fatalf("VisualWidth(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{
			name: "under max returns unchanged",
			in:   "short",
			max:  10,
			want: "short",
		},
		{
			name: "at max returns unchanged",
			in:   "exactly10!",
			max:  10,
			want: "exactly10!",
		},
		{
			name: "over max truncates with ellipsis",
			in:   "this is too long to fit",
			max:  10,
			want: "this is...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.in, tt.max); got != tt.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}
