package filestorage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBackendFromOptions_UsesProjectRoot(t *testing.T) {
	// With project_root set explicitly (the path DefaultConfig populates
	// after walking up), the backend writes under <root>/.tm/.
	dir := t.TempDir()
	t.Chdir(dir)

	b, err := NewBackendFromOptions(map[string]string{"project_root": dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
	if _, err := os.Stat(filepath.Join(dir, ".tm", "tasks")); err != nil {
		t.Errorf("expected tasks dir under %q/.tm, stat: %v", dir, err)
	}
}

func TestNewBackendFromOptions_NoMarkerErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	_, err := NewBackendFromOptions(nil)
	if err == nil {
		t.Fatal("expected error when no project marker found")
	}
	if !strings.Contains(err.Error(), "no TM project found") {
		t.Errorf("expected error to mention missing project, got: %v", err)
	}
}

func TestNewBackendFromOptions_MarkerFoundViaWalkup(t *testing.T) {
	// .tm/ in parent should be picked up by FindProjectRoot when called via
	// DefaultConfig; we simulate that by passing project_root explicitly here
	// (mirroring what app.NewClient does).
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".tm"), 0o755); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	child := filepath.Join(parent, "subdir")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}
	t.Chdir(child)

	b, err := NewBackendFromOptions(map[string]string{"project_root": parent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
	if _, err := os.Stat(filepath.Join(parent, ".tm", "tasks")); err != nil {
		t.Errorf("expected tasks dir under parent project root, stat: %v", err)
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/Users/zdenkovrabel/unravela/tm", "-Users-zdenkovrabel-unravela-tm"},
		{"/a/b/c", "-a-b-c"},
		{"/", "-"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if filepath.Separator != '/' {
				t.Skipf("skipping on non-POSIX separator %q", string(filepath.Separator))
			}
			got := sanitizePath(tt.in)
			if got != tt.want {
				t.Errorf("sanitizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.Contains(got, string(filepath.Separator)) {
				t.Errorf("sanitized path still contains separator: %q", got)
			}
		})
	}
}
