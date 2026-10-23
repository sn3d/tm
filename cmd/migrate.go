package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/backend/sqlite"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/mdimport"
	"github.com/sn3d/tm/internal/scope"
	"github.com/sn3d/tm/internal/tui"
	"github.com/urfave/cli/v3"
)

// migrateCmd folds the pre-registry per-directory databases under
// ~/.tm/projects/ (and any filestorage markdown directories) into the target
// instance's single database.
//
// Source databases are left untouched on disk. Nothing is deleted, and
// re-running is safe: import is keyed on (project, id) and an
// already-imported project is detected and skipped.
var migrateCmd = &cli.Command{
	Name:  "migrate",
	Usage: "Fold the legacy per-directory databases under ~/.tm/projects/ into the current instance",
	Description: `Scans ~/.tm/projects/ for the per-directory sqlite databases and
filestorage markdown directories TM used before projects were entities,
then imports each as a project in the target instance.

Tickers are derived from each directory's trailing path segment,
canonicalised, truncated to 15 characters and de-duplicated with a numeric
suffix. Task IDs are preserved: the composite (project_id, id) primary key
makes collisions impossible.

Source data is never modified or deleted. Re-running is safe.`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Print the plan and exit without writing anything.",
		},
		&cli.BoolFlag{
			Name:  "yes",
			Usage: "Skip the confirmation prompt (required when not on a terminal).",
		},
		&cli.BoolFlag{
			Name:  "include-empty",
			Usage: "Import databases with no tasks too (skipped by default; many are test leftovers).",
		},
		&cli.StringFlag{
			Name:  "source",
			Usage: "Directory to scan instead of ~/.tm/projects (useful for testing).",
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}
		return runMigrate(cfg, command, cwd, os.Stdin)
	},
}

// sourceKind distinguishes the two legacy storage shapes migrate can read.
type sourceKind int

const (
	sourceSQLite sourceKind = iota
	sourceFilestorage
)

func (k sourceKind) String() string {
	if k == sourceFilestorage {
		return "markdown"
	}
	return "sqlite"
}

// plannedSource is one legacy directory and the project it will become.
type plannedSource struct {
	Dir       string // absolute path of the legacy directory
	Name      string // its base name, e.g. "-Users-zdenkovrabel-unravela-tm"
	Kind      sourceKind
	Ticker    string // derived, de-duplicated, canonical
	TaskCount int
	// RepoPath is the folder the sanitised name decodes back to, when that
	// directory actually exists. Only then is a mapping written — the
	// sanitisation is lossy, so an unverified guess must not become config.
	RepoPath string
	Skip     bool
	SkipWhy  string
}

func runMigrate(cfg *client.Config, command *cli.Command, cwd string, stdin *os.File) error {
	sourceDir := command.String("source")
	if sourceDir == "" {
		var err error
		if sourceDir, err = scope.LegacyProjectsDir(); err != nil {
			return err
		}
	}

	resolved, err := scope.ResolveInstanceOnly(cwd, cfg, ScopeOverride(command))
	if err != nil {
		return err
	}
	cfg.Resolved = resolved

	fmt.Printf("Scanning %s …\n\n", sourceDir)
	sources, err := scanSources(sourceDir, command.Bool("include-empty"))
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		fmt.Println(color.HiBlackString("Nothing to migrate."))
		return nil
	}

	importable := 0
	for _, s := range sources {
		if !s.Skip {
			importable++
		}
	}
	fmt.Printf("Found %d legacy source(s), %d to import.\n", len(sources), importable)
	fmt.Printf("Target instance: %s (%s)\n\n", resolved.Instance, resolved.DBPath)
	printPlan(sources)

	if command.Bool("dry-run") {
		fmt.Println()
		fmt.Println(color.HiBlackString("--dry-run: nothing was written."))
		return nil
	}
	if importable == 0 {
		fmt.Println()
		fmt.Println(color.HiBlackString("Nothing to import."))
		return nil
	}
	if !command.Bool("yes") {
		ok, err := confirm(stdin)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println(color.HiBlackString("Aborted; nothing was written."))
			return nil
		}
	}

	return importAll(cfg, sources)
}

// scanSources enumerates the legacy directories and assigns each a ticker.
func scanSources(root string, includeEmpty bool) ([]plannedSource, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}

	// used tracks tickers already handed out so duplicates get a numeric
	// suffix rather than colliding on the primary key.
	used := map[string]bool{}
	out := make([]plannedSource, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		s := plannedSource{Dir: dir, Name: e.Name()}

		dbPath := filepath.Join(dir, "tm.db")
		switch {
		case fileExists(dbPath):
			s.Kind = sourceSQLite
			count, cerr := countLegacyTasks(dbPath)
			if cerr != nil {
				s.Skip, s.SkipWhy = true, "unreadable: "+cerr.Error()
			}
			s.TaskCount = count
		case mdimport.IsFilestorageDir(dir):
			s.Kind = sourceFilestorage
			tasks, terr := mdimport.ReadDir(dir)
			if terr != nil {
				s.Skip, s.SkipWhy = true, "unreadable: "+terr.Error()
			}
			s.TaskCount = len(tasks)
		default:
			continue // not a TM data directory
		}

		if !s.Skip && s.TaskCount == 0 && !includeEmpty {
			s.Skip, s.SkipWhy = true, "empty"
		}
		if !s.Skip {
			s.Ticker = uniqueTicker(e.Name(), used)
			used[s.Ticker] = true
		}
		// The sanitised name reverses to a path only approximately, so a
		// mapping is written only when the decoded directory really exists.
		if repo := scope.UnsanitizePath(e.Name()); repo != "" && dirExists(repo) {
			s.RepoPath = repo
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// uniqueTicker derives a ticker from a legacy directory name, de-duplicating
// with a numeric suffix. Truncation keeps room for the suffix so "proj1" can
// never overflow the 15-character limit.
func uniqueTicker(dirName string, used map[string]bool) string {
	base, ok := client.DeriveTicker(dirName)
	if !ok {
		base = "proj"
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("%d", i)
		trimmed := base
		if len(trimmed)+len(suffix) > client.TickerMaxLen {
			trimmed = trimmed[:client.TickerMaxLen-len(suffix)]
		}
		cand := trimmed + suffix
		if !used[cand] {
			return cand
		}
	}
}

// countLegacyTasks opens a legacy database read-only and counts its tasks,
// WITHOUT running migrations — the source must stay exactly as it is.
func countLegacyTasks(dbPath string) (int, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func printPlan(sources []plannedSource) {
	bold := color.New(color.Bold).Sprint
	fmt.Println(tui.PadRight(bold("SOURCE"), 44) +
		tui.PadRight(bold("KIND"), 10) +
		tui.PadRight(bold("TICKER"), 18) + bold("TASKS"))
	for _, s := range sources {
		ticker := s.Ticker
		if s.Skip {
			ticker = color.YellowString("SKIP")
		}
		line := tui.PadRight(tui.Truncate(s.Name, 42), 44) +
			tui.PadRight(s.Kind.String(), 10) +
			tui.PadRight(ticker, 18) +
			fmt.Sprintf("%d", s.TaskCount)
		if s.Skip && s.SkipWhy != "" {
			line += color.HiBlackString("  (" + s.SkipWhy + ")")
		}
		fmt.Println(line)
	}
}

func confirm(stdin *os.File) (bool, error) {
	fmt.Print("\nProceed? [y/N] ")
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		// A closed stdin (piped, no input) must not be read as consent.
		return false, fmt.Errorf("cannot read confirmation; pass --yes to proceed non-interactively")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// importer carries the per-run state of a migrate: the target client and the
// dependency links deferred until every task exists.
//
// State lives on a struct rather than package vars so two runs in one process
// (the tests do exactly this) cannot leak deps into each other.
type importer struct {
	target *client.Client
	// pending holds dependency lists to write in a second pass. A task may
	// reference a sibling not yet imported, and task_deps has a foreign key
	// onto tasks, so the edges go in only after every node exists.
	pending []pendingDep
}

// pendingDep is one task's deferred dependency list.
type pendingDep struct {
	project string
	ref     client.TaskID
	deps    []client.TaskID
}

// importAll opens the target instance once and imports each planned source.
func importAll(cfg *client.Config, sources []plannedSource) error {
	target, err := app.NewInstanceClient(cfg)
	if err != nil {
		return err
	}
	imp := &importer{target: target}

	imported, skipped := 0, 0
	for _, s := range sources {
		if s.Skip {
			continue
		}
		existing, err := target.Projects().GetByTicker(s.Ticker)
		if err != nil {
			return err
		}
		if existing != nil {
			// A project already present means a previous run imported it.
			// Re-importing would double-write rows, so skip rather than merge.
			fmt.Printf("%s %s already imported as %s; skipping\n",
				color.YellowString("•"), s.Name, existing.Label())
			skipped++
			continue
		}
		n, err := imp.importSource(s)
		if err != nil {
			return fmt.Errorf("import %s: %w", s.Name, err)
		}
		fmt.Printf("%s %s -> %s (%d tasks)\n", color.GreenString("✓"), s.Name, s.Ticker, n)
		imported++

		if s.RepoPath != "" {
			if err := cfg.SetMapping(s.RepoPath, cfg.Resolved.Instance, s.Ticker); err != nil {
				return err
			}
		}
	}

	// Dependency edges go in last, once every task across every imported
	// project exists.
	if err := imp.flushPending(); err != nil {
		return fmt.Errorf("link dependencies: %w", err)
	}

	if imported > 0 {
		if err := cfg.SaveCentral(); err != nil {
			return err
		}
	}
	fmt.Printf("\n%s Imported %d project(s), skipped %d.\n", color.GreenString("✓"), imported, skipped)
	fmt.Println(color.HiBlackString("Source data was not modified."))
	return nil
}

// importSource dispatches on the legacy storage shape.
func (imp *importer) importSource(s plannedSource) (int, error) {
	project := &client.Project{Ticker: s.Ticker, DisplayName: s.Ticker}
	if err := imp.target.Projects().Save(project); err != nil {
		return 0, err
	}
	switch s.Kind {
	case sourceFilestorage:
		return imp.importMarkdown(s)
	default:
		return imp.importSQLite(s)
	}
}

// importSQLite copies tasks, descriptions and comments out of a legacy
// database. The source is opened through sqlite.NewBackend on a COPY of the
// file, so migration 00006 can run against the copy without touching the
// original — source databases are left untouched on disk.
func (imp *importer) importSQLite(s plannedSource) (int, error) {
	tmp, err := os.CreateTemp("", "tm-migrate-*.db")
	if err != nil {
		return 0, fmt.Errorf("create scratch copy: %w", err)
	}
	scratch := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(scratch)

	if err := copyFile(filepath.Join(s.Dir, "tm.db"), scratch); err != nil {
		return 0, err
	}
	srcBackend, err := sqlite.NewBackend(scratch)
	if err != nil {
		return 0, fmt.Errorf("open scratch copy: %w", err)
	}
	// The copy has been migrated to the current schema, so its data is
	// reachable through the normal repositories. Its placeholder project is
	// whatever 00006 derived; read the tasks out from under it.
	srcProjects, err := srcBackend.Projects().GetAll(true)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, sp := range srcProjects {
		tasks, err := srcBackend.Tasks().GetAll(sp.Ticker)
		if err != nil {
			return 0, err
		}
		for i := range tasks {
			t := tasks[i]
			srcRef := t.ID
			// GetAll does not join the description; fetch it explicitly so
			// the body is not silently dropped on import.
			body, err := srcBackend.Descriptions().Get(srcRef)
			if err != nil {
				return 0, err
			}
			t.Description = body
			// IDs are preserved — the composite key makes collisions
			// impossible, which is what makes folding many databases
			// together tractable.
			t.ID = client.TaskRef{Project: s.Ticker, ID: srcRef.ID}
			t.ParentID = requalify(t.ParentID, s.Ticker)
			for j := range t.DependsOn {
				t.DependsOn[j] = requalify(t.DependsOn[j], s.Ticker)
			}
			if err := imp.saveTask(s.Ticker, &t); err != nil {
				return 0, err
			}
			comments, err := srcBackend.Comments().GetForTask(srcRef)
			if err != nil {
				return 0, err
			}
			for _, cm := range comments {
				c := cm
				if err := imp.target.Backend().Comments().Add(t.ID, &c); err != nil {
					return 0, fmt.Errorf("import comment %q: %w", cm.ID, err)
				}
			}
			count++
		}
	}
	return count, nil
}

// importMarkdown parses each .md file through the preserved filestorage
// parser. This import path is the one piece of filestorage that had to
// outlive the backend: deleting it before this worked would have orphaned
// any checked-in markdown tasks.
func (imp *importer) importMarkdown(s plannedSource) (int, error) {
	tasks, err := mdimport.ReadDir(s.Dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, mt := range tasks {
		ref := client.TaskRef{Project: s.Ticker, ID: mt.ID}
		t := client.Task{
			ID:            ref,
			Subject:       mt.Subject,
			Description:   mt.Description,
			State:         mt.State,
			AssignedAgent: mt.AssignedAgent,
			ParentID:      bareRef(s.Ticker, mt.ParentID),
			Labels:        mt.Labels,
			Mode:          mt.Mode,
			CreatedAt:     mt.CreatedAt,
			ArchivedAt:    mt.ArchivedAt,
		}
		for _, d := range mt.DependsOn {
			if r := bareRef(s.Ticker, d); !r.IsZero() {
				t.DependsOn = append(t.DependsOn, r)
			}
		}
		if err := imp.saveTask(s.Ticker, &t); err != nil {
			return 0, err
		}
		for _, mc := range mt.Comments {
			c := client.Comment{ID: mc.ID, Who: mc.Who, Comment: mc.Body}
			if err := imp.target.Backend().Comments().Add(ref, &c); err != nil {
				return 0, fmt.Errorf("import comment %q: %w", mc.ID, err)
			}
		}
		count++
	}
	return count, nil
}

// saveTask writes a task with its dependency list deferred to flushPending.
func (imp *importer) saveTask(project string, t *client.Task) error {
	deps := t.DependsOn
	t.DependsOn = nil
	if err := imp.target.Backend().Tasks().Save(project, t); err != nil {
		return err
	}
	if len(deps) > 0 {
		imp.pending = append(imp.pending, pendingDep{project: project, ref: t.ID, deps: deps})
	}
	return nil
}

// flushPending re-saves each task with its dependency list now that every
// referenced task exists. A dep whose target did not survive the import is
// dropped rather than failing the whole migration on one dangling reference —
// the alternative is losing every other task to one bad edge.
func (imp *importer) flushPending() error {
	for _, p := range imp.pending {
		t, err := imp.target.Backend().Tasks().GetByID(p.ref)
		if err != nil {
			return err
		}
		if t == nil {
			continue
		}
		kept := make([]client.TaskID, 0, len(p.deps))
		for _, d := range p.deps {
			dt, err := imp.target.Backend().Tasks().GetByID(d)
			if err != nil {
				return err
			}
			if dt != nil {
				kept = append(kept, d)
			}
		}
		t.DependsOn = kept
		if err := imp.target.Backend().Tasks().Save(p.project, t); err != nil {
			return err
		}
	}
	imp.pending = nil
	return nil
}

// requalify moves a same-project reference onto the new ticker, preserving
// the bare number. The zero ref stays zero.
func requalify(ref client.TaskID, ticker string) client.TaskID {
	if ref.IsZero() {
		return ref
	}
	return client.TaskRef{Project: ticker, ID: ref.ID}
}

// bareRef builds a reference from a bare id string, returning the zero ref
// for an empty id.
func bareRef(ticker, id string) client.TaskID {
	if id == "" {
		return client.TaskRef{}
	}
	return client.TaskRef{Project: ticker, ID: id}
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
