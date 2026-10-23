package client

import (
	"fmt"
	"regexp"
	"strings"
)

// TickerMaxLen caps a project ticker. Short enough that `TICKER/#id` stays
// scannable in a terminal column, long enough for a readable name
// ("infrastructure" is 14).
const TickerMaxLen = 15

// tickerRE is the ticker charset: must start with a letter, then
// alphanumerics and dashes. `_` and `/` are deliberately absent — `/` is the
// TaskRef separator and `_` would invite confusion with slugs. Dash is fine
// since it never appears in the ref grammar.
var tickerRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,14}$`)

// CanonicalTicker lowercases and validates a ticker. Tickers are
// case-insensitive: `TM`, `tm` and `Tm` all address the same project, and the
// canonical (stored, compared) form is lowercase. The projects table carries
// a separate display_name column so canonicalisation never loses the user's
// presentation intent.
func CanonicalTicker(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("ticker is empty")
	}
	if !tickerRE.MatchString(trimmed) {
		return "", fmt.Errorf("invalid ticker %q: must start with a letter, "+
			"contain only letters, digits and dashes, and be at most %d characters",
			s, TickerMaxLen)
	}
	return strings.ToLower(trimmed), nil
}

// DeriveTicker turns an arbitrary string (typically a directory name) into a
// usable ticker, best-effort. Used by migration 00006 and `tm migrate` to
// guess a placeholder from a sanitized path like
// "-Users-zdenkovrabel-unravela-tm" → "tm", so upgrading stays zero-touch.
//
// The last path-ish segment is preferred because it is the repo name, which
// is nearly always the right ticker. Returns ("", false) when nothing usable
// survives, letting the caller fall back to a generic name.
func DeriveTicker(s string) (string, bool) {
	segments := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '/' || r == '_' || r == '.' || r == ' '
	})
	// Walk from the end: the trailing segment is the repo name. Skip
	// segments that cannot start a ticker (e.g. a trailing "2" in "repo-2").
	for i := len(segments) - 1; i >= 0; i-- {
		if cand, ok := sanitizeTickerCandidate(segments[i]); ok {
			return cand, true
		}
	}
	return "", false
}

// sanitizeTickerCandidate strips characters outside the ticker charset and
// truncates to TickerMaxLen. Returns ("", false) when the result would not be
// a valid ticker (empty, or not starting with a letter).
func sanitizeTickerCandidate(s string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	out := b.String()
	// Trim leading digits so the result starts with a letter.
	out = strings.TrimLeft(out, "0123456789")
	if out == "" {
		return "", false
	}
	if len(out) > TickerMaxLen {
		out = out[:TickerMaxLen]
	}
	return out, true
}
