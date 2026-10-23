package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	_ "modernc.org/sqlite"
)

// legacyV5Schema is the pre-projects schema (goose version 5) that the
// per-directory databases under ~/.tm/projects/ are at.
const legacyV5Schema = `
CREATE TABLE tasks (
    id             TEXT PRIMARY KEY,
    subject        TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL DEFAULT 'todo',
    assigned_agent TEXT NOT NULL DEFAULT '',
    parent_id      TEXT NOT NULL DEFAULT '',
    labels         TEXT NOT NULL DEFAULT '[]',
    mode           TEXT NOT NULL DEFAULT 'standard',
    created_at     TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL DEFAULT '',
    archived_at    TEXT NULL
);
CREATE TABLE task_deps (
    task_id       TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    PRIMARY KEY (task_id, depends_on_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE TABLE comments (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    who     TEXT NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT ''
);
CREATE TABLE events (
    id      TEXT PRIMARY KEY,
    ts      TEXT NOT NULL,
    actor   TEXT NOT NULL DEFAULT '',
    kind    TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE actor_cursors (
    actor        TEXT PRIMARY KEY,
    last_seen_at TEXT NOT NULL
);
CREATE TABLE goose_db_version (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL,
    is_applied INTEGER NOT NULL,
    tstamp TIMESTAMP DEFAULT (datetime('now'))
);
INSERT INTO goose_db_version (version_id, is_applied) VALUES (0,1),(1,1),(2,1),(3,1),(4,1),(5,1);
`

// writeLegacyDB creates a legacy per-directory sqlite database with the given
// tasks. Each task is (id, subject, description).
func writeLegacyDB(t *testing.T, dir string, tasks [][3]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "tm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyV5Schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, tk := range tasks {
		if _, err := db.Exec(
			`INSERT INTO tasks (id, subject, description, state, created_at, updated_at)
			 VALUES (?, ?, ?, 'todo', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z')`,
			tk[0], tk[1], tk[2]); err != nil {
			t.Fatalf("insert task %s: %v", tk[0], err)
		}
	}
}

// writeMarkdownDir creates a filestorage-shaped directory with one task.
func writeMarkdownDir(t *testing.T, dir, id, subject, body string) {
	t.Helper()
	tasksDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nid: \"" + id + "\"\nstate: todo\nassigned_agent: \"\"\n---\n\n# " +
		subject + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(tasksDir, id+"--"+id+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// targetConfig builds a config whose single instance points at a fresh
// database inside a temp dir, so nothing touches the user's real ~/.tm.
func targetConfig(t *testing.T) *client.Config {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, ".tm", "local.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	return &client.Config{
		Instances: map[string]client.Instance{
			"local": {Driver: client.DriverSQLite, Path: dbPath},
		},
		DefaultInstance: "local",
		Resolved: client.Resolved{
			Instance: "local",
			DBPath:   dbPath,
		},
	}
}

func TestScanSources_DerivesTickersAndSkipsEmpty(t *testing.T) {
	root := t.TempDir()
	writeLegacyDB(t, filepath.Join(root, "-Users-me-unravela-tm"), [][3]string{
		{"1", "first", ""}, {"2", "second", "body"},
	})
	writeLegacyDB(t, filepath.Join(root, "-Users-me-billing"), [][3]string{
		{"1", "invoice", ""},
	})
	// An empty database: many of the 48 real ones are test leftovers.
	writeLegacyDB(t, filepath.Join(root, "-private-tmp-tm-smoke"), nil)
	writeMarkdownDir(t, filepath.Join(root, "-Users-me-notes"), "1", "a note", "note body")
	// A directory that is neither shape must be ignored entirely.
	if err := os.MkdirAll(filepath.Join(root, "random-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := scanSources(root, false)
	if err != nil {
		t.Fatalf("scanSources: %v", err)
	}

	byName := map[string]plannedSource{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if _, present := byName["random-dir"]; present {
		t.Error("a directory with neither tm.db nor tasks/*.md must be ignored")
	}

	tm := byName["-Users-me-unravela-tm"]
	if tm.Ticker != "tm" {
		t.Errorf("ticker for the tm dir = %q, want %q", tm.Ticker, "tm")
	}
	if tm.TaskCount != 2 {
		t.Errorf("task count = %d, want 2", tm.TaskCount)
	}
	if tm.Kind != sourceSQLite {
		t.Errorf("kind = %v, want sqlite", tm.Kind)
	}

	bill := byName["-Users-me-billing"]
	if bill.Ticker != "billing" {
		t.Errorf("ticker for the billing dir = %q, want %q", bill.Ticker, "billing")
	}

	notes := byName["-Users-me-notes"]
	if notes.Kind != sourceFilestorage {
		t.Errorf("markdown dir kind = %v, want filestorage", notes.Kind)
	}
	if notes.Ticker != "notes" {
		t.Errorf("markdown ticker = %q, want %q", notes.Ticker, "notes")
	}

	empty := byName["-private-tmp-tm-smoke"]
	if !empty.Skip {
		t.Error("an empty database should be skipped by default")
	}
	if empty.SkipWhy != "empty" {
		t.Errorf("skip reason = %q, want %q", empty.SkipWhy, "empty")
	}
}

func TestScanSources_IncludeEmpty(t *testing.T) {
	root := t.TempDir()
	writeLegacyDB(t, filepath.Join(root, "-tmp-empty"), nil)

	got, err := scanSources(root, true)
	if err != nil {
		t.Fatalf("scanSources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sources, want 1", len(got))
	}
	if got[0].Skip {
		t.Error("--include-empty should keep an empty database")
	}
}

func TestScanSources_MissingRootIsNotAnError(t *testing.T) {
	got, err := scanSources(filepath.Join(t.TempDir(), "nope"), false)
	if err != nil {
		t.Fatalf("a missing legacy root should not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d sources, want none", len(got))
	}
}

func TestUniqueTicker_DeDuplicates(t *testing.T) {
	used := map[string]bool{}

	first := uniqueTicker("-Users-a-unravela-tm", used)
	used[first] = true
	if first != "tm" {
		t.Fatalf("first ticker = %q, want %q", first, "tm")
	}

	// A second directory deriving the same ticker gets a numeric suffix
	// rather than colliding on the primary key.
	second := uniqueTicker("-Users-b-other-tm", used)
	used[second] = true
	if second != "tm2" {
		t.Errorf("second ticker = %q, want %q", second, "tm2")
	}

	third := uniqueTicker("-Users-c-third-tm", used)
	if third != "tm3" {
		t.Errorf("third ticker = %q, want %q", third, "tm3")
	}
}

func TestUniqueTicker_SuffixRespectsLengthLimit(t *testing.T) {
	// A 15-char base plus a suffix must still fit in 15 characters, or the
	// projects table's CHECK would reject it.
	used := map[string]bool{"abcdefghijklmno": true}
	got := uniqueTicker("abcdefghijklmnopqrs", used)
	if len(got) > client.TickerMaxLen {
		t.Errorf("ticker %q is %d chars, over the %d limit", got, len(got), client.TickerMaxLen)
	}
	if _, err := client.CanonicalTicker(got); err != nil {
		t.Errorf("de-duplicated ticker %q is invalid: %v", got, err)
	}
}

func TestUniqueTicker_FallsBackToProj(t *testing.T) {
	// A directory name with nothing usable falls back rather than failing.
	got := uniqueTicker("---", map[string]bool{})
	if got != "proj" {
		t.Errorf("ticker = %q, want %q", got, "proj")
	}
}

// TestImportAll_SQLiteAndMarkdown is the end-to-end migrate test: two legacy
// sqlite databases and one markdown directory fold into one instance, with
// IDs preserved and both #1 rows coexisting.
func TestImportAll_SQLiteAndMarkdown(t *testing.T) {
	root := t.TempDir()
	writeLegacyDB(t, filepath.Join(root, "-Users-me-unravela-tm"), [][3]string{
		{"1", "tm first", "tm body one"},
		{"7", "tm seventh", ""},
	})
	writeLegacyDB(t, filepath.Join(root, "-Users-me-billing"), [][3]string{
		{"1", "bill first", ""},
	})
	writeMarkdownDir(t, filepath.Join(root, "-Users-me-notes"), "3", "a note", "note body")

	cfg := targetConfig(t)
	sources, err := scanSources(root, false)
	if err != nil {
		t.Fatalf("scanSources: %v", err)
	}
	if err := importAll(cfg, sources); err != nil {
		t.Fatalf("importAll: %v", err)
	}

	c, err := app.NewInstanceClient(cfg)
	if err != nil {
		t.Fatalf("NewInstanceClient: %v", err)
	}

	projects, err := c.Projects().GetAll(true)
	if err != nil {
		t.Fatalf("GetAll projects: %v", err)
	}
	gotTickers := map[string]bool{}
	for _, p := range projects {
		gotTickers[p.Ticker] = true
	}
	for _, want := range []string{"tm", "billing", "notes"} {
		if !gotTickers[want] {
			t.Errorf("missing imported project %q (got %v)", want, gotTickers)
		}
	}

	// IDs are preserved: the composite key makes collisions impossible, so
	// tm/#1 and billing/#1 coexist and tm/#7 keeps its number.
	tm1, err := c.Backend().Tasks().GetByID(client.TaskRef{Project: "tm", ID: "1"})
	if err != nil || tm1 == nil {
		t.Fatalf("tm/#1: %v (got %+v)", err, tm1)
	}
	if tm1.Subject != "tm first" {
		t.Errorf("tm/#1 subject = %q, want %q", tm1.Subject, "tm first")
	}
	// The description moved into task_descriptions and reads back via GetByID.
	if tm1.Description != "tm body one" {
		t.Errorf("tm/#1 description = %q, want %q", tm1.Description, "tm body one")
	}

	tm7, err := c.Backend().Tasks().GetByID(client.TaskRef{Project: "tm", ID: "7"})
	if err != nil || tm7 == nil {
		t.Fatalf("tm/#7 should keep its original number: %v (got %+v)", err, tm7)
	}

	bill1, err := c.Backend().Tasks().GetByID(client.TaskRef{Project: "billing", ID: "1"})
	if err != nil || bill1 == nil {
		t.Fatalf("billing/#1: %v (got %+v)", err, bill1)
	}
	if bill1.Subject != "bill first" {
		t.Errorf("billing/#1 subject = %q, want %q (projects must not bleed)", bill1.Subject, "bill first")
	}

	// The markdown task came through the preserved filestorage parser.
	note, err := c.Backend().Tasks().GetByID(client.TaskRef{Project: "notes", ID: "3"})
	if err != nil || note == nil {
		t.Fatalf("notes/#3 (markdown import): %v (got %+v)", err, note)
	}
	if note.Subject != "a note" {
		t.Errorf("notes/#3 subject = %q, want %q", note.Subject, "a note")
	}
	if note.Description != "note body" {
		t.Errorf("notes/#3 description = %q, want %q", note.Description, "note body")
	}
}

// TestImportAll_LeavesSourcesUntouched is a stated guarantee: nothing is
// deleted and the source databases are not modified.
func TestImportAll_LeavesSourcesUntouched(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "-Users-me-unravela-tm")
	writeLegacyDB(t, srcDir, [][3]string{{"1", "first", ""}})
	srcDB := filepath.Join(srcDir, "tm.db")

	before, err := os.ReadFile(srcDB)
	if err != nil {
		t.Fatal(err)
	}

	cfg := targetConfig(t)
	sources, err := scanSources(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := importAll(cfg, sources); err != nil {
		t.Fatalf("importAll: %v", err)
	}

	after, err := os.ReadFile(srcDB)
	if err != nil {
		t.Fatalf("source database should still exist: %v", err)
	}
	if len(before) != len(after) {
		t.Errorf("source database changed size %d -> %d; migrate must not modify sources",
			len(before), len(after))
	}
	// The source must still be at its original schema version — migrate works
	// on a copy precisely so 00006 does not run against the original.
	db, err := sql.Open("sqlite", srcDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("query source version: %v", err)
	}
	if version != 5 {
		t.Errorf("source database migrated to version %d; it must stay at 5", version)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("source database gained a projects table; migrate must not touch it")
	}
}

// TestImportAll_ReRunIsSafe covers the stated re-run guarantee: an
// already-imported project is detected and skipped rather than double-written.
func TestImportAll_ReRunIsSafe(t *testing.T) {
	root := t.TempDir()
	writeLegacyDB(t, filepath.Join(root, "-Users-me-unravela-tm"), [][3]string{
		{"1", "first", ""}, {"2", "second", ""},
	})

	cfg := targetConfig(t)
	sources, err := scanSources(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := importAll(cfg, sources); err != nil {
		t.Fatalf("first importAll: %v", err)
	}
	if err := importAll(cfg, sources); err != nil {
		t.Fatalf("second importAll should be safe: %v", err)
	}

	c, err := app.NewInstanceClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := c.Backend().Tasks().GetAll("tm")
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("re-running migrate duplicated rows: got %d tasks, want 2", len(tasks))
	}
}

// TestImportAll_PreservesDepsAndParents checks the deferred-dependency pass:
// edges are written only after every task exists, since task_deps has a
// foreign key onto tasks.
func TestImportAll_PreservesDepsAndParents(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "-Users-me-unravela-tm")
	writeLegacyDB(t, srcDir, [][3]string{
		{"1", "parent", ""},
		{"2", "child", ""},
		{"3", "dependent", ""},
	})
	// Wire up a parent link and a dependency in the source.
	db, err := sql.Open("sqlite", filepath.Join(srcDir, "tm.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET parent_id='1' WHERE id='2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO task_deps (task_id, depends_on_id) VALUES ('3','1')`); err != nil {
		t.Fatal(err)
	}
	cid := ulid.Make().String()
	if _, err := db.Exec(
		`INSERT INTO comments (id, task_id, who, comment) VALUES (?, '1', 'alice', 'a note')`,
		cid); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	cfg := targetConfig(t)
	sources, err := scanSources(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := importAll(cfg, sources); err != nil {
		t.Fatalf("importAll: %v", err)
	}

	c, err := app.NewInstanceClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parentRef := client.TaskRef{Project: "tm", ID: "1"}

	child, err := c.Backend().Tasks().GetByID(client.TaskRef{Project: "tm", ID: "2"})
	if err != nil || child == nil {
		t.Fatalf("child: %v", err)
	}
	if child.ParentID != parentRef {
		t.Errorf("child ParentID = %+v, want %+v (re-qualified to the new ticker)", child.ParentID, parentRef)
	}

	dependent, err := c.Backend().Tasks().GetByID(client.TaskRef{Project: "tm", ID: "3"})
	if err != nil || dependent == nil {
		t.Fatalf("dependent: %v", err)
	}
	if len(dependent.DependsOn) != 1 || dependent.DependsOn[0] != parentRef {
		t.Errorf("DependsOn = %+v, want [%+v]", dependent.DependsOn, parentRef)
	}

	comments, err := c.Backend().Comments().GetForTask(parentRef)
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(comments) != 1 || comments[0].Comment != "a note" {
		t.Errorf("comments = %+v, want the imported note", comments)
	}
	// The comment keeps its original ULID so re-imports stay idempotent.
	if comments[0].ID != cid {
		t.Errorf("comment ID = %q, want the original ULID %q", comments[0].ID, cid)
	}
}

// TestImportAll_WritesFolderMappings checks that a reconstructible repo path
// gains a mapping, and an unverifiable one does not.
func TestImportAll_WritesFolderMappings(t *testing.T) {
	root := t.TempDir()

	// A directory whose sanitised name decodes back to a real path.
	realRepo := t.TempDir()
	sanitized := sanitizeForTest(realRepo)
	writeLegacyDB(t, filepath.Join(root, sanitized), [][3]string{{"1", "mapped", ""}})

	// And one that decodes to a path that does not exist.
	writeLegacyDB(t, filepath.Join(root, "-nonexistent-path-ghost"), [][3]string{{"1", "ghost", ""}})

	cfg := targetConfig(t)
	sources, err := scanSources(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := importAll(cfg, sources); err != nil {
		t.Fatalf("importAll: %v", err)
	}

	key, _ := client.ExpandPath(realRepo)
	if _, ok := cfg.Mappings[key]; !ok {
		t.Errorf("expected a mapping for the reconstructible repo %q; got %v", key, cfg.Mappings)
	}
	// The sanitisation is lossy, so an unverified guess must not become
	// config.
	for folder := range cfg.Mappings {
		if folder == "/nonexistent/path/ghost" {
			t.Error("a mapping was written for a path that does not exist")
		}
	}
}

// sanitizeForTest mirrors scope.SanitizePath without importing it, keeping
// this test independent of that helper's internals.
func sanitizeForTest(p string) string {
	out := make([]rune, 0, len(p))
	for _, r := range p {
		if r == filepath.Separator {
			out = append(out, '-')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
