package client

import (
	"strings"
	"testing"
)

func TestCanonicalTicker_Valid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already lowercase", "tm", "tm"},
		{"uppercase lowered", "TM", "tm"},
		{"mixed case lowered", "WebApp", "webapp"},
		{"single letter", "a", "a"},
		{"digits after letter", "tm2", "tm2"},
		{"dash allowed", "web-app", "web-app"},
		{"multiple dashes", "a-b-c", "a-b-c"},
		{"trailing dash", "tm-", "tm-"},
		{"exactly 15 chars", "abcdefghijklmno", "abcdefghijklmno"},
		{"15 chars mixed case", "ABCDEFGHIJKLMNO", "abcdefghijklmno"},
		{"whitespace trimmed", "  TM  ", "tm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalTicker(tt.in)
			if err != nil {
				t.Fatalf("CanonicalTicker(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("CanonicalTicker(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCanonicalTicker_CaseInsensitiveCollapse(t *testing.T) {
	// TM, tm and Tm must all address the same project — the whole point of
	// storing the canonical lowercase form.
	variants := []string{"TM", "tm", "Tm", "tM"}
	first, err := CanonicalTicker(variants[0])
	if err != nil {
		t.Fatalf("CanonicalTicker(%q): %v", variants[0], err)
	}
	for _, v := range variants[1:] {
		got, err := CanonicalTicker(v)
		if err != nil {
			t.Fatalf("CanonicalTicker(%q): %v", v, err)
		}
		if got != first {
			t.Errorf("CanonicalTicker(%q) = %q, want %q (same project as %q)", v, got, first, variants[0])
		}
	}
}

func TestCanonicalTicker_Invalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		// '_' is rejected so tickers never get confused with slugs.
		{"underscore", "my_proj"},
		{"leading underscore", "_tm"},
		// '/' is the TaskRef separator; allowing it would make refs ambiguous.
		{"slash", "tm/x"},
		{"leading slash", "/tm"},
		{"starts with digit", "1tm"},
		{"all digits", "123"},
		{"starts with dash", "-tm"},
		{"16 chars too long", "abcdefghijklmnop"},
		{"space inside", "web app"},
		{"dot", "web.app"},
		{"colon", "web:app"},
		{"at sign", "web@app"},
		{"non ascii", "tmé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalTicker(tt.in)
			if err == nil {
				t.Fatalf("CanonicalTicker(%q) = %q, want error", tt.in, got)
			}
		})
	}
}

func TestCanonicalTicker_ErrorMentionsLimit(t *testing.T) {
	_, err := CanonicalTicker("abcdefghijklmnop")
	if err == nil {
		t.Fatal("expected error for over-long ticker")
	}
	if !strings.Contains(err.Error(), "15") {
		t.Errorf("error should mention the 15-character limit, got: %v", err)
	}
}

func TestTickerMaxLenMatchesPattern(t *testing.T) {
	// The regexp hard-codes {0,14} after the leading letter. If TickerMaxLen
	// is ever changed without updating the pattern, this catches the drift.
	atLimit := strings.Repeat("a", TickerMaxLen)
	if _, err := CanonicalTicker(atLimit); err != nil {
		t.Errorf("ticker of exactly TickerMaxLen (%d) should be valid: %v", TickerMaxLen, err)
	}
	overLimit := strings.Repeat("a", TickerMaxLen+1)
	if _, err := CanonicalTicker(overLimit); err == nil {
		t.Errorf("ticker of TickerMaxLen+1 (%d) should be invalid", TickerMaxLen+1)
	}
}

func TestDeriveTicker(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		// The sanitized-directory form that ~/.tm/projects/ uses: the
		// trailing segment is the repo name and nearly always the right guess.
		{"sanitized home project dir", "-Users-zdenkovrabel-unravela-tm", "tm", true},
		{"sanitized billing dir", "-Users-zdenkovrabel-billing", "billing", true},
		{"plain path", "/Users/x/repo", "repo", true},
		{"plain name", "myproject", "myproject", true},
		{"underscores split", "my_cool_project", "project", true},
		{"mixed case lowered", "WebApp", "webapp", true},
		{"strips invalid chars", "my.proj!", "proj", true},
		{"truncates to 15", "-tmp-averyverylongprojectname", "averyverylongpr", true},
		{"skips trailing numeric segment", "repo-2", "repo", true},
		{"strips leading digits", "2fast", "fast", true},
		{"empty", "", "", false},
		{"only separators", "---", "", false},
		{"only digits", "123", "", false},
		{"only punctuation", "!!!", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DeriveTicker(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("DeriveTicker(%q) ok = %v, want %v (got %q)", tt.in, ok, tt.wantOK, got)
			}
			if got != tt.want {
				t.Errorf("DeriveTicker(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDeriveTicker_OutputIsAlwaysCanonical(t *testing.T) {
	// Whatever DeriveTicker returns must survive CanonicalTicker, since the
	// migration inserts it straight into the projects table whose CHECK
	// constraint enforces lowercase.
	inputs := []string{
		"-Users-zdenkovrabel-unravela-tm",
		"WebApp",
		"my.proj!",
		"-tmp-averyverylongprojectname",
		"2fast",
		"repo-2",
	}
	for _, in := range inputs {
		got, ok := DeriveTicker(in)
		if !ok {
			continue
		}
		canon, err := CanonicalTicker(got)
		if err != nil {
			t.Errorf("DeriveTicker(%q) = %q which is not a valid ticker: %v", in, got, err)
			continue
		}
		if canon != got {
			t.Errorf("DeriveTicker(%q) = %q, not canonical (want %q)", in, got, canon)
		}
	}
}
