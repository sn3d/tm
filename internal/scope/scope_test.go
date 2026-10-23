package scope

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sn3d/tm/internal/client"
)

// newConfig builds a config with one instance and the given mappings, the
// shape scope.Resolve reads.
func newConfig(t *testing.T, mappings map[string]string) *client.Config {
	t.Helper()
	dir := t.TempDir()
	return &client.Config{
		Instances: map[string]client.Instance{
			"local": {Driver: client.DriverSQLite, Path: filepath.Join(dir, "local.db")},
			"work":  {Driver: client.DriverSQLite, Path: filepath.Join(dir, "work.db")},
		},
		DefaultInstance: "local",
		Mappings:        mappings,
	}
}

func TestResolve_MappedFolder(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "local/TM"})

	got, err := Resolve(repo, cfg, Override{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Project != "tm" {
		t.Errorf("Project = %q, want %q", got.Project, "tm")
	}
	if got.Instance != "local" {
		t.Errorf("Instance = %q, want %q", got.Instance, "local")
	}
	if got.DBPath == "" {
		t.Error("DBPath should be populated from the instance")
	}
}

func TestResolve_WalksUpFromSubdirectory(t *testing.T) {
	repo := t.TempDir()
	deep := filepath.Join(repo, "src", "internal", "foo")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := newConfig(t, map[string]string{repo: "local/TM"})

	got, err := Resolve(deep, cfg, Override{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Project != "tm" {
		t.Errorf("Project = %q, want %q", got.Project, "tm")
	}
	// Root reports which mapped folder matched, resolved to its real path.
	wantRoot, _ := client.ExpandPath(repo)
	if got.Root != wantRoot {
		t.Errorf("Root = %q, want %q", got.Root, wantRoot)
	}
}

// TestResolve_LongestPrefixWins is the behaviour the design states
// explicitly: with /repo -> A and /repo/sub -> B, a cwd under /repo/sub
// resolves to B. First-match-while-walking would be equivalent, but stating
// it as longest-prefix makes nested mappings well-defined.
func TestResolve_LongestPrefixWins(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "sub")
	deeper := filepath.Join(inner, "pkg")
	if err := os.MkdirAll(deeper, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := newConfig(t, map[string]string{
		outer: "local/OUTER",
		inner: "local/INNER",
	})

	tests := []struct {
		cwd  string
		want string
	}{
		{outer, "outer"},
		{inner, "inner"},
		{deeper, "inner"},
	}
	for _, tt := range tests {
		got, err := Resolve(tt.cwd, cfg, Override{})
		if err != nil {
			t.Fatalf("Resolve(%s): %v", tt.cwd, err)
		}
		if got.Project != tt.want {
			t.Errorf("Resolve(%s).Project = %q, want %q", tt.cwd, got.Project, tt.want)
		}
	}
}

// TestResolve_SiblingPrefixNotMatched guards the segment-boundary check: a
// mapping for /repo must not match /repository.
func TestResolve_SiblingPrefixNotMatched(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	repository := filepath.Join(base, "repository")
	for _, d := range []string{repo, repository} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := newConfig(t, map[string]string{repo: "local/TM"})

	if _, err := Resolve(repository, cfg, Override{}); err == nil {
		t.Error("a mapping for /repo must not match /repository")
	}
}

func TestResolve_NoMappingErrors(t *testing.T) {
	cfg := newConfig(t, nil)
	dir := t.TempDir()

	_, err := Resolve(dir, cfg, Override{})
	if err == nil {
		t.Fatal("expected an error when no project is mapped")
	}
	// A distinct type so `tm init` can treat it as "not initialized yet"
	// rather than a failure.
	var unmapped *UnmappedError
	if !errors.As(err, &unmapped) {
		t.Fatalf("expected *UnmappedError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "tm init") {
		t.Errorf("error should hint at 'tm init', got: %v", err)
	}
	if !strings.Contains(err.Error(), "--project") {
		t.Errorf("error should hint at --project, got: %v", err)
	}
}

func TestResolve_FlagOverridesMapping(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "local/TM"})

	got, err := Resolve(repo, cfg, Override{Project: "BILL"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Project != "bill" {
		t.Errorf("Project = %q, want %q (flag must beat the mapping)", got.Project, "bill")
	}
	// With only --project given, the instance falls back to the default
	// rather than being left empty.
	if got.Instance != "local" {
		t.Errorf("Instance = %q, want %q", got.Instance, "local")
	}
}

func TestResolve_InstanceFlagOverridesMapping(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "local/TM"})

	got, err := Resolve(repo, cfg, Override{Instance: "work"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Instance != "work" {
		t.Errorf("Instance = %q, want %q", got.Instance, "work")
	}
	// The project still comes from the mapping — only the instance moved.
	if got.Project != "tm" {
		t.Errorf("Project = %q, want %q", got.Project, "tm")
	}
}

// TestResolve_PrecedenceFlagsBeatEnv checks the documented order:
// flags > env > mapping > error.
func TestResolve_PrecedenceFlagsBeatEnv(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "local/MAPPED"})

	t.Setenv(EnvProject, "fromenv")
	t.Setenv(EnvInstance, "work")

	// Env only: beats the mapping.
	envOnly, err := Resolve(repo, cfg, OverrideFromEnv(Override{}))
	if err != nil {
		t.Fatalf("Resolve (env): %v", err)
	}
	if envOnly.Project != "fromenv" {
		t.Errorf("env Project = %q, want %q", envOnly.Project, "fromenv")
	}
	if envOnly.Instance != "work" {
		t.Errorf("env Instance = %q, want %q", envOnly.Instance, "work")
	}

	// Flag plus env: the flag wins.
	flagWins, err := Resolve(repo, cfg, OverrideFromEnv(Override{Project: "fromflag"}))
	if err != nil {
		t.Fatalf("Resolve (flag): %v", err)
	}
	if flagWins.Project != "fromflag" {
		t.Errorf("flag Project = %q, want %q (flag must beat env)", flagWins.Project, "fromflag")
	}
}

func TestResolve_EnvOnlyFillsUnsetFields(t *testing.T) {
	t.Setenv(EnvProject, "envproj")
	t.Setenv(EnvInstance, "work")

	got := OverrideFromEnv(Override{Project: "flagproj"})
	if got.Project != "flagproj" {
		t.Errorf("Project = %q, want the flag value to survive", got.Project)
	}
	if got.Instance != "work" {
		t.Errorf("Instance = %q, want the env value to fill the gap", got.Instance)
	}
}

// TestResolve_SymlinkResolvesToSameProject is the live concern the design
// calls out: without EvalSymlinks a symlinked checkout and its target would
// be two different projects.
func TestResolve_SymlinkResolvesToSameProject(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Map via the REAL path, then resolve via the symlink.
	cfg := newConfig(t, map[string]string{real: "local/TM"})
	viaLink, err := Resolve(link, cfg, Override{})
	if err != nil {
		t.Fatalf("Resolve via symlink: %v", err)
	}
	if viaLink.Project != "tm" {
		t.Errorf("via symlink Project = %q, want %q", viaLink.Project, "tm")
	}

	// And the reverse: map via the symlink, resolve via the real path.
	cfg2 := newConfig(t, map[string]string{link: "local/TM"})
	viaReal, err := Resolve(real, cfg2, Override{})
	if err != nil {
		t.Fatalf("Resolve via real path: %v", err)
	}
	if viaReal.Project != "tm" {
		t.Errorf("via real path Project = %q, want %q", viaReal.Project, "tm")
	}
}

func TestResolve_BareTickerMappingUsesDefaultInstance(t *testing.T) {
	repo := t.TempDir()
	// A bare TICKER (no "instance/") means default_instance.
	cfg := newConfig(t, map[string]string{repo: "TM"})

	got, err := Resolve(repo, cfg, Override{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Instance != "local" {
		t.Errorf("Instance = %q, want the default %q", got.Instance, "local")
	}
	if got.Project != "tm" {
		t.Errorf("Project = %q, want %q", got.Project, "tm")
	}
}

func TestResolve_UnknownInstanceErrors(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "nope/TM"})

	_, err := Resolve(repo, cfg, Override{})
	if err == nil {
		t.Fatal("expected an error for an unconfigured instance")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the unknown instance, got: %v", err)
	}
}

func TestResolve_InvalidTickerInMappingErrors(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "local/bad_ticker"})

	if _, err := Resolve(repo, cfg, Override{}); err == nil {
		t.Error("expected an error for a mapping with an invalid ticker")
	}
}

func TestResolve_NilConfigErrors(t *testing.T) {
	if _, err := Resolve("/tmp", nil, Override{}); err == nil {
		t.Error("expected an error when no config is loaded")
	}
}

func TestResolveInstanceOnly_NoProjectNeeded(t *testing.T) {
	cfg := newConfig(t, nil)
	dir := t.TempDir()

	got, err := ResolveInstanceOnly(dir, cfg, Override{})
	if err != nil {
		t.Fatalf("ResolveInstanceOnly: %v", err)
	}
	if got.Instance != "local" {
		t.Errorf("Instance = %q, want %q", got.Instance, "local")
	}
	if got.DBPath == "" {
		t.Error("DBPath should be populated")
	}
	// No mapping, so no project — and that is not an error here.
	if got.Project != "" {
		t.Errorf("Project = %q, want empty", got.Project)
	}
}

func TestResolveInstanceOnly_UsesMappingWhenPresent(t *testing.T) {
	repo := t.TempDir()
	cfg := newConfig(t, map[string]string{repo: "work/BILL"})

	got, err := ResolveInstanceOnly(repo, cfg, Override{})
	if err != nil {
		t.Fatalf("ResolveInstanceOnly: %v", err)
	}
	if got.Instance != "work" {
		t.Errorf("Instance = %q, want %q (the mapping's instance)", got.Instance, "work")
	}
	if got.Project != "bill" {
		t.Errorf("Project = %q, want %q", got.Project, "bill")
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
			if got := SanitizePath(tt.in); got != tt.want {
				t.Errorf("SanitizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnsanitizePath(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skipf("skipping on non-POSIX separator")
	}
	// The encoding is lossy — a segment containing a dash is
	// indistinguishable from a separator — so callers verify the result
	// against the filesystem before trusting it.
	tests := []struct {
		in, want string
	}{
		{"-a-b-c", "/a/b/c"},
		{"-Users-zdenkovrabel-unravela-tm", "/Users/zdenkovrabel/unravela/tm"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := UnsanitizePath(tt.in); got != tt.want {
				t.Errorf("UnsanitizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeUnsanitizeRoundTripsSimplePaths(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skipf("skipping on non-POSIX separator")
	}
	// Paths whose segments contain no dash round-trip exactly.
	for _, p := range []string{"/a/b/c", "/Users/me/repo", "/tmp"} {
		if got := UnsanitizePath(SanitizePath(p)); got != p {
			t.Errorf("round trip of %q gave %q", p, got)
		}
	}
	// And one that does NOT round-trip, documenting the known lossiness.
	lossy := "/Users/me/my-repo"
	if got := UnsanitizePath(SanitizePath(lossy)); got == lossy {
		t.Errorf("expected %q to lose its dash in the round trip, but it survived", lossy)
	}
}
