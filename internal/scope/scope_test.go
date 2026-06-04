package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCwdProjectDir_WithProjectRoot(t *testing.T) {
	root := t.TempDir()
	got, err := CwdProjectDir("test", root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, ProjectDataDir)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCwdProjectDir_NoMarkerErrors(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	dir := t.TempDir()
	t.Chdir(dir)

	_, err := CwdProjectDir("test", "")
	if err == nil {
		t.Fatal("expected error when no project marker found")
	}
	if !strings.Contains(err.Error(), "no TM project found") {
		t.Errorf("expected 'no TM project found' message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "tm init") {
		t.Errorf("expected hint to run tm init, got: %v", err)
	}
}

func TestCwdProjectDir_WalksUpToFindMarker(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	root := filepath.Join(fakeHome, "proj")
	if err := os.MkdirAll(filepath.Join(root, ProjectDataDir), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "src", "internal", "foo")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(deep)

	got, err := CwdProjectDir("test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, ProjectDataDir)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHomeProjectDir_FallsBackToCwd(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got, err := HomeProjectDir("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(tmpHome, ".tm", "projects", SanitizePath(cwd))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHomeProjectDir_UsesProjectRoot(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	root := "/some/project/path"

	got, err := HomeProjectDir(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(tmpHome, ".tm", "projects", SanitizePath(root))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizePath(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skipf("skipping on non-POSIX separator")
	}
	tests := []struct {
		in, want string
	}{
		{"/a/b/c", "-a-b-c"},
		{"/", "-"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := SanitizePath(tt.in)
			if got != tt.want {
				t.Errorf("SanitizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
