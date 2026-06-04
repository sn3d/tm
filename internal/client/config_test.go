package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeConfigs(t *testing.T) {
	tests := []struct {
		name        string
		global      *Config
		project     *Config
		wantBackend string
		wantOptions map[string]string
	}{
		{
			name:        "both nil",
			wantOptions: nil,
		},
		{
			name:        "only global",
			global:      &Config{Backend: "git", Options: map[string]string{"path": "/g"}},
			wantBackend: "git",
			wantOptions: map[string]string{"path": "/g"},
		},
		{
			name:        "only project",
			project:     &Config{Backend: "sqlite", Options: map[string]string{"dsn": "p.db"}},
			wantBackend: "sqlite",
			wantOptions: map[string]string{"dsn": "p.db"},
		},
		{
			name:        "project overrides backend",
			global:      &Config{Backend: "git"},
			project:     &Config{Backend: "sqlite"},
			wantBackend: "sqlite",
		},
		{
			name:        "empty project backend keeps global",
			global:      &Config{Backend: "git"},
			project:     &Config{Backend: ""},
			wantBackend: "git",
		},
		{
			name:        "per-key options merge with project winning",
			global:      &Config{Options: map[string]string{"a": "G", "b": "G", "c": "G"}},
			project:     &Config{Options: map[string]string{"b": "P", "d": "P"}},
			wantOptions: map[string]string{"a": "G", "b": "P", "c": "G", "d": "P"},
		},
		{
			name:        "three-way merge (global -> project -> local)",
			global:      &Config{Backend: "git", Options: map[string]string{"a": "G", "b": "G", "c": "G"}},
			project:     &Config{Backend: "sqlite", Options: map[string]string{"b": "P", "d": "P"}},
			wantBackend: "sqlite",
			wantOptions: map[string]string{"a": "G", "b": "P", "c": "G", "d": "P"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeConfigs(tt.global, tt.project)
			if got.Backend != tt.wantBackend {
				t.Errorf("Backend: got %q, want %q", got.Backend, tt.wantBackend)
			}
			if len(got.Options) != len(tt.wantOptions) {
				t.Fatalf("Options length: got %d (%v), want %d (%v)", len(got.Options), got.Options, len(tt.wantOptions), tt.wantOptions)
			}
			for k, v := range tt.wantOptions {
				if got.Options[k] != v {
					t.Errorf("Options[%q]: got %q, want %q", k, got.Options[k], v)
				}
			}
		})
	}
}

func TestLoadOptional_MissingFileReturnsNil(t *testing.T) {
	cfg, err := loadOptional(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config for missing file, got %+v", cfg)
	}
}

func TestLoadOptional_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("backend: git\noptions:\n  path: /x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadOptional(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || cfg.Backend != "git" || cfg.Options["path"] != "/x" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestMergeConfigs_LocalOverridesProject(t *testing.T) {
	global := &Config{Backend: "git", Options: map[string]string{"a": "G"}}
	project := &Config{Backend: "sqlite", Options: map[string]string{"a": "P", "b": "P"}}
	local := &Config{Options: map[string]string{"b": "L", "c": "L"}}

	got := mergeConfigs(mergeConfigs(global, project), local)

	if got.Backend != "sqlite" {
		t.Errorf("Backend: got %q, want %q (local has empty Backend, project wins)", got.Backend, "sqlite")
	}
	want := map[string]string{"a": "P", "b": "L", "c": "L"}
	for k, v := range want {
		if got.Options[k] != v {
			t.Errorf("Options[%q]: got %q, want %q", k, got.Options[k], v)
		}
	}
}

func TestLoadOptional_PropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("this is : : not valid\n  yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOptional(path); err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestFindProjectRoot(t *testing.T) {
	// Sandbox $HOME so the walk doesn't escape into the real home.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	root := filepath.Join(fakeHome, "proj")
	deep := filepath.Join(root, "src", "internal", "foo")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("no marker returns empty", func(t *testing.T) {
		got, err := FindProjectRoot(deep)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("expected empty (no marker), got %q", got)
		}
	})

	t.Run("taskmanager.yaml marker found via walk-up", func(t *testing.T) {
		cfg := filepath.Join(root, "taskmanager.yaml")
		if err := os.WriteFile(cfg, []byte("backend: filestorage\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(cfg) })

		got, err := FindProjectRoot(deep)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != root {
			t.Errorf("got %q, want %q", got, root)
		}
	})

	t.Run(".tm/ marker found via walk-up", func(t *testing.T) {
		dataDir := filepath.Join(root, ".tm")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

		got, err := FindProjectRoot(deep)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != root {
			t.Errorf("got %q, want %q", got, root)
		}
	})

	t.Run("stops at home directory", func(t *testing.T) {
		// Put a marker in $HOME — walk should NOT pick it up.
		cfg := filepath.Join(fakeHome, "taskmanager.yaml")
		if err := os.WriteFile(cfg, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(cfg) })

		got, err := FindProjectRoot(deep)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("expected empty (home is the boundary), got %q", got)
		}
	})
}
