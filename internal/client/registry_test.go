package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfigFromYAML_CentralRegistry parses the shape the design document
// specifies for ~/.tm/config.yaml.
func TestConfigFromYAML_CentralRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
instances:
  local:
    driver: sqlite
    path: ~/.tm/local.db
  work:
    driver: sqlite
    path: ~/Dropbox/tm/work.db

default_instance: local

mappings:
  /Users/zdenkovrabel/unravela/tm: local/TM
  /Users/zdenkovrabel/unravela/billing: work/BILL
  /Users/zdenkovrabel/scratch/spike: local/TM

actor: zdenko

styling:
  states:
    todo:
      icon: "o"
      color: blue
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ConfigFromYAML(path)
	if err != nil {
		t.Fatalf("ConfigFromYAML: %v", err)
	}

	if len(cfg.Instances) != 2 {
		t.Fatalf("Instances = %d, want 2", len(cfg.Instances))
	}
	local, ok := cfg.Instances["local"]
	if !ok {
		t.Fatal("missing 'local' instance")
	}
	if local.Driver != DriverSQLite {
		t.Errorf("local.Driver = %q, want %q", local.Driver, DriverSQLite)
	}
	if local.Path != "~/.tm/local.db" {
		t.Errorf("local.Path = %q, want the unexpanded form as written", local.Path)
	}
	if cfg.DefaultInstance != "local" {
		t.Errorf("DefaultInstance = %q, want %q", cfg.DefaultInstance, "local")
	}
	if len(cfg.Mappings) != 3 {
		t.Errorf("Mappings = %d, want 3", len(cfg.Mappings))
	}
	// Many folders can map to one project.
	if cfg.Mappings["/Users/zdenkovrabel/scratch/spike"] != "local/TM" {
		t.Errorf("a second folder should be able to map to the same project, got %q",
			cfg.Mappings["/Users/zdenkovrabel/scratch/spike"])
	}
	if cfg.Actor != "zdenko" {
		t.Errorf("Actor = %q, want %q", cfg.Actor, "zdenko")
	}
	if cfg.Styling.States["todo"].Color != "blue" {
		t.Errorf("styling did not load: %+v", cfg.Styling)
	}
}

func TestParseMapping(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		defaultInstance string
		wantInstance    string
		wantTicker      string
	}{
		{"qualified", "local/TM", "local", "local", "tm"},
		{"qualified other instance", "work/BILL", "local", "work", "bill"},
		{"bare ticker uses default", "TM", "local", "local", "tm"},
		{"lowercase ticker", "local/tm", "local", "local", "tm"},
		{"ticker canonicalised", "local/WebApp", "local", "local", "webapp"},
		{"dash in ticker", "local/web-app", "local", "local", "web-app"},
		{"whitespace trimmed", "  local/TM  ", "local", "local", "tm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance, ticker, err := ParseMapping(tt.value, tt.defaultInstance)
			if err != nil {
				t.Fatalf("ParseMapping(%q): %v", tt.value, err)
			}
			if instance != tt.wantInstance {
				t.Errorf("instance = %q, want %q", instance, tt.wantInstance)
			}
			if ticker != tt.wantTicker {
				t.Errorf("ticker = %q, want %q", ticker, tt.wantTicker)
			}
		})
	}
}

func TestParseMapping_Invalid(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		defaultInstance string
	}{
		{"empty", "", "local"},
		{"missing instance before slash", "/TM", "local"},
		{"invalid ticker", "local/bad_ticker", "local"},
		{"bare ticker with no default instance", "TM", ""},
		{"ticker too long", "local/abcdefghijklmnop", "local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ParseMapping(tt.value, tt.defaultInstance); err == nil {
				t.Errorf("ParseMapping(%q, %q) should fail", tt.value, tt.defaultInstance)
			}
		})
	}
}

func TestFormatMapping(t *testing.T) {
	if got := FormatMapping("local", "tm"); got != "local/tm" {
		t.Errorf("FormatMapping = %q, want %q", got, "local/tm")
	}
}

func TestFormatParseMappingRoundTrip(t *testing.T) {
	// Mappings are always written fully qualified so the file stays
	// unambiguous if default_instance later changes.
	for _, tc := range []struct{ instance, ticker string }{
		{"local", "tm"},
		{"work", "bill"},
		{"local", "web-app"},
	} {
		value := FormatMapping(tc.instance, tc.ticker)
		gotInstance, gotTicker, err := ParseMapping(value, "other")
		if err != nil {
			t.Fatalf("ParseMapping(%q): %v", value, err)
		}
		if gotInstance != tc.instance || gotTicker != tc.ticker {
			t.Errorf("round trip of %v gave %q/%q", tc, gotInstance, gotTicker)
		}
	}
}

func TestExpandPath_TildeExpansion(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := ExpandPath("~/.tm/local.db")
	if err != nil {
		t.Fatalf("ExpandPath: %v", err)
	}
	// The file does not exist, so symlink resolution cannot apply; the
	// cleaned absolute path is the answer.
	want := filepath.Join(fakeHome, ".tm", "local.db")
	if got != want {
		t.Errorf("ExpandPath = %q, want %q", got, want)
	}
}

func TestExpandPath_BareTilde(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := ExpandPath("~")
	if err != nil {
		t.Fatalf("ExpandPath: %v", err)
	}
	// fakeHome is a real directory, so it symlink-resolves (on macOS
	// /var/folders is itself behind a symlink, which is exactly the case
	// this resolution exists to collapse).
	wantResolved, _ := filepath.EvalSymlinks(fakeHome)
	if got != wantResolved && got != fakeHome {
		t.Errorf("ExpandPath(~) = %q, want %q", got, wantResolved)
	}
}

func TestExpandPath_Empty(t *testing.T) {
	got, err := ExpandPath("")
	if err != nil {
		t.Fatalf("ExpandPath(\"\"): %v", err)
	}
	if got != "" {
		t.Errorf("ExpandPath(\"\") = %q, want empty", got)
	}
}

// TestExpandPath_ResolvesSymlinks is the live concern from the design: without
// this, a path and a symlink pointing at it would be two different projects.
func TestExpandPath_ResolvesSymlinks(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	viaReal, err := ExpandPath(real)
	if err != nil {
		t.Fatalf("ExpandPath(real): %v", err)
	}
	viaLink, err := ExpandPath(link)
	if err != nil {
		t.Fatalf("ExpandPath(link): %v", err)
	}
	if viaReal != viaLink {
		t.Errorf("a symlink and its target must expand to one path: %q != %q", viaLink, viaReal)
	}
}

// TestExpandDirPath_NonexistentFileInRealDir covers the database-about-to-be
// -created case: the parent exists and symlink-resolves, the file does not
// exist yet.
func TestExpandDirPath_NonexistentFileInRealDir(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "data")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "datalink")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	viaReal, err := ExpandDirPath(filepath.Join(realDir, "new.db"))
	if err != nil {
		t.Fatalf("ExpandDirPath: %v", err)
	}
	viaLink, err := ExpandDirPath(filepath.Join(link, "new.db"))
	if err != nil {
		t.Fatalf("ExpandDirPath: %v", err)
	}
	if viaReal != viaLink {
		t.Errorf("a symlinked parent must collapse to one path: %q != %q", viaLink, viaReal)
	}
	if !strings.HasSuffix(viaReal, "new.db") {
		t.Errorf("ExpandDirPath dropped the base name: %q", viaReal)
	}
}

func TestConfig_InstancePath(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	cfg := &Config{
		Instances: map[string]Instance{
			"local": {Driver: DriverSQLite, Path: "~/.tm/local.db"},
		},
		DefaultInstance: "local",
	}

	got, err := cfg.InstancePath("local")
	if err != nil {
		t.Fatalf("InstancePath: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".tm", "local.db")) {
		t.Errorf("InstancePath = %q, want it to end in .tm/local.db", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("InstancePath = %q, want an absolute path", got)
	}
}

func TestConfig_InstancePath_Errors(t *testing.T) {
	cfg := &Config{
		Instances: map[string]Instance{
			"local":    {Driver: DriverSQLite, Path: "/tmp/local.db"},
			"nopath":   {Driver: DriverSQLite},
			"remote":   {Driver: "postgres", Path: "/tmp/x.db"},
			"otherone": {Driver: DriverSQLite, Path: "/tmp/other.db"},
		},
	}

	t.Run("unknown instance names the configured ones", func(t *testing.T) {
		_, err := cfg.InstancePath("missing")
		if err == nil {
			t.Fatal("expected an error for an unknown instance")
		}
		if !strings.Contains(err.Error(), "local") {
			t.Errorf("error should list the configured instances, got: %v", err)
		}
	})

	t.Run("no path configured", func(t *testing.T) {
		if _, err := cfg.InstancePath("nopath"); err == nil {
			t.Error("expected an error when the instance has no path")
		}
	})

	t.Run("unimplemented driver", func(t *testing.T) {
		_, err := cfg.InstancePath("remote")
		if err == nil {
			t.Fatal("expected an error for a non-sqlite driver")
		}
		// Only sqlite is implemented; the field exists so a remote driver can
		// be added later without reshaping config.
		if !strings.Contains(err.Error(), "postgres") {
			t.Errorf("error should name the unsupported driver, got: %v", err)
		}
	})
}

func TestConfig_InstanceNamesSorted(t *testing.T) {
	cfg := &Config{Instances: map[string]Instance{
		"work": {}, "local": {}, "archive": {},
	}}
	got := cfg.InstanceNames()
	want := []string{"archive", "local", "work"}
	if len(got) != len(want) {
		t.Fatalf("InstanceNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("InstanceNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestConfig_SetMapping_ExpandsAndCanonicalises(t *testing.T) {
	repo := t.TempDir()
	cfg := &Config{}

	if err := cfg.SetMapping(repo, "local", "TM"); err != nil {
		t.Fatalf("SetMapping: %v", err)
	}
	// The key is stored expanded and symlink-resolved so a symlinked path
	// and its target do not become two projects.
	wantKey, _ := ExpandPath(repo)
	value, ok := cfg.Mappings[wantKey]
	if !ok {
		t.Fatalf("mapping not stored under the expanded key %q; got %v", wantKey, cfg.Mappings)
	}
	// The ticker is canonicalised on write.
	if value != "local/tm" {
		t.Errorf("mapping value = %q, want %q", value, "local/tm")
	}
}

func TestConfig_SetMapping_RejectsInvalidTicker(t *testing.T) {
	cfg := &Config{}
	if err := cfg.SetMapping(t.TempDir(), "local", "bad_ticker"); err == nil {
		t.Error("SetMapping should reject an invalid ticker")
	}
}

func TestConfig_SetMapping_RemapOverwrites(t *testing.T) {
	repo := t.TempDir()
	cfg := &Config{}
	if err := cfg.SetMapping(repo, "local", "TM"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetMapping(repo, "work", "BILL"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Mappings) != 1 {
		t.Errorf("remapping should replace, not add: %v", cfg.Mappings)
	}
	key, _ := ExpandPath(repo)
	if cfg.Mappings[key] != "work/bill" {
		t.Errorf("mapping = %q, want %q", cfg.Mappings[key], "work/bill")
	}
}

func TestConfig_SaveCentralRoundTrip(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	repo := t.TempDir()
	cfg := &Config{
		Instances: map[string]Instance{
			"local": {Driver: DriverSQLite, Path: filepath.Join(fakeHome, ".tm", "local.db")},
		},
		DefaultInstance: "local",
		Actor:           "zdenko",
	}
	if err := cfg.SetMapping(repo, "local", "TM"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveCentral(); err != nil {
		t.Fatalf("SaveCentral: %v", err)
	}

	path, err := CentralConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := ConfigFromYAML(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.DefaultInstance != "local" {
		t.Errorf("DefaultInstance = %q, want %q", reloaded.DefaultInstance, "local")
	}
	if reloaded.Actor != "zdenko" {
		t.Errorf("Actor = %q, want %q", reloaded.Actor, "zdenko")
	}
	if len(reloaded.Instances) != 1 {
		t.Errorf("Instances = %v, want one entry", reloaded.Instances)
	}
	if len(reloaded.Mappings) != 1 {
		t.Errorf("Mappings = %v, want one entry", reloaded.Mappings)
	}
	// Runtime-only state must not be serialised.
	if strings.Contains(readFile(t, path), "Resolved") {
		t.Error("Resolved is runtime state and must not be written to config")
	}
}

func TestMergeConfigs_InstancesAndMappings(t *testing.T) {
	base := &Config{
		Instances:       map[string]Instance{"local": {Path: "/a"}},
		Mappings:        map[string]string{"/one": "local/A"},
		DefaultInstance: "local",
	}
	over := &Config{
		Instances: map[string]Instance{"work": {Path: "/b"}},
		Mappings:  map[string]string{"/two": "work/B"},
	}

	got := mergeConfigs(base, over)
	if len(got.Instances) != 2 {
		t.Errorf("Instances = %v, want both merged", got.Instances)
	}
	if len(got.Mappings) != 2 {
		t.Errorf("Mappings = %v, want both merged", got.Mappings)
	}
	// An empty override must not clobber the base's default.
	if got.DefaultInstance != "local" {
		t.Errorf("DefaultInstance = %q, want %q preserved", got.DefaultInstance, "local")
	}
}

func TestMergeConfigs_OverrideWinsPerKey(t *testing.T) {
	base := &Config{
		Instances:       map[string]Instance{"local": {Path: "/base"}},
		Mappings:        map[string]string{"/repo": "local/OLD"},
		DefaultInstance: "local",
	}
	over := &Config{
		Instances:       map[string]Instance{"local": {Path: "/over"}},
		Mappings:        map[string]string{"/repo": "local/NEW"},
		DefaultInstance: "work",
	}
	got := mergeConfigs(base, over)
	if got.Instances["local"].Path != "/over" {
		t.Errorf("instance path = %q, want the override", got.Instances["local"].Path)
	}
	if got.Mappings["/repo"] != "local/NEW" {
		t.Errorf("mapping = %q, want the override", got.Mappings["/repo"])
	}
	if got.DefaultInstance != "work" {
		t.Errorf("DefaultInstance = %q, want the override", got.DefaultInstance)
	}
}

func TestMergeConfigs_DoesNotAliasBaseMaps(t *testing.T) {
	// A merge must not hand back maps that alias the inputs, or writing a
	// mapping through the merged config would mutate the layer it came from.
	base := &Config{
		Instances: map[string]Instance{"local": {Path: "/a"}},
		Mappings:  map[string]string{"/one": "local/A"},
	}
	got := mergeConfigs(base, nil)
	got.Mappings["/two"] = "local/B"
	got.Instances["work"] = Instance{Path: "/b"}

	if len(base.Mappings) != 1 {
		t.Errorf("merging aliased the base Mappings: %v", base.Mappings)
	}
	if len(base.Instances) != 1 {
		t.Errorf("merging aliased the base Instances: %v", base.Instances)
	}
}

func TestResolved_IsZero(t *testing.T) {
	if !(Resolved{}).IsZero() {
		t.Error("zero Resolved should report IsZero")
	}
	if (Resolved{Project: "tm"}).IsZero() {
		t.Error("a Resolved with a project is not zero")
	}
}

// TestConfig_YAMLOmitsEmptyFields keeps the written file tidy: a config with
// no mappings should not emit an empty `mappings:` key.
func TestConfig_YAMLOmitsEmptyFields(t *testing.T) {
	raw, err := yaml.Marshal(Config{DefaultInstance: "local"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, unwanted := range []string{"mappings:", "instances:", "actor:", "backend:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("marshalled config should omit empty %q, got:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "default_instance: local") {
		t.Errorf("default_instance missing from:\n%s", out)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
