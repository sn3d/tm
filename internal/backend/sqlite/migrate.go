package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pressly/goose/v3"
	"github.com/sn3d/tm/internal/client"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsDir is the path inside migrationsFS where the .sql files live.
// Kept as a constant since goose.Up takes it as a string argument.
const migrationsDir = "migrations"

// runMigrations brings the database to the latest schema version. Legacy
// databases created before goose was introduced (i.e. ones whose tables
// exist but whose goose_db_version table does not) are detected and marked
// as already at version 1 so the bootstrap migration is skipped.
// runMigrations brings the database to the latest schema version.
//
// dbPath is used only to derive the placeholder project ticker that
// migration 00006 attributes pre-existing data to — see placeholderTicker.
// Pass "" (or ":memory:") when there is no meaningful directory name.
func runMigrations(db *sql.DB, dbPath string) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	// Silence goose's per-migration stdout chatter; failures surface as
	// returned errors, which is what callers (and tests) care about.
	goose.SetLogger(goose.NopLogger())

	if err := stampLegacyDB(db); err != nil {
		return err
	}
	pending, err := needsProjectsMigration(db)
	if err != nil {
		return err
	}
	// Only a database that already holds tasks needs a placeholder project to
	// attribute them to. A brand-new database must come up EMPTY: seeding a
	// project named after its directory would invent a project the user never
	// asked for — and for the default instance at ~/.tm/local.db that
	// directory is ".tm", so every fresh install would start with a bogus
	// project that then collides with the first real import.
	seed := false
	if pending {
		if seed, err = hasExistingTasks(db); err != nil {
			return err
		}
		// The scratch table is always created so 00006 can reference it
		// unconditionally; it is only POPULATED when there is data to
		// attribute, which is what keeps a fresh database project-free.
		if err := seedPlaceholderProject(db, dbPath, seed); err != nil {
			return err
		}
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	if seed {
		if err := backfillCommentTimestamps(db); err != nil {
			return err
		}
	}
	return nil
}

// hasExistingTasks reports whether the pre-00006 database holds any task
// rows. A database with no tasks has nothing to attribute, so it gets no
// placeholder project.
func hasExistingTasks(db *sql.DB) (bool, error) {
	exists, err := tableExists(db, "tasks")
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil {
		return false, fmt.Errorf("count existing tasks: %w", err)
	}
	return count > 0, nil
}

// needsProjectsMigration reports whether migration 00006 still has to run,
// i.e. the projects table does not exist yet. Used to decide whether to seed
// the placeholder ticker and run the post-migration ULID backfill, both of
// which are one-shot work that must not repeat on later opens.
func needsProjectsMigration(db *sql.DB) (bool, error) {
	exists, err := tableExists(db, "projects")
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// seedPlaceholderProject writes the derived ticker into a scratch table that
// migration 00006 reads. Passing it through a table rather than string
// interpolation keeps the .sql file a static asset (and keeps the value out
// of the SQL text entirely).
//
// A fresh database gets a placeholder too, which `tm init` then renames or
// supersedes; carrying one costs a single row and means 00006 never has to
// branch on "was there data".
func seedPlaceholderProject(db *sql.DB, dbPath string, populate bool) error {
	const create = `CREATE TABLE IF NOT EXISTS tm_migration_placeholder (
		ticker TEXT NOT NULL, display_name TEXT NOT NULL)`
	if _, err := db.Exec(create); err != nil {
		return fmt.Errorf("create migration placeholder table: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM tm_migration_placeholder`); err != nil {
		return fmt.Errorf("reset migration placeholder: %w", err)
	}
	if !populate {
		// Empty table: 00006's INSERT ... SELECT yields no rows, so the
		// database comes up with no projects.
		return nil
	}
	ticker, display := placeholderTicker(dbPath)
	if _, err := db.Exec(
		`INSERT INTO tm_migration_placeholder (ticker, display_name) VALUES (?, ?)`,
		ticker, display); err != nil {
		return fmt.Errorf("seed migration placeholder: %w", err)
	}
	return nil
}

// placeholderTicker derives the ticker that migration 00006 attributes
// existing data to, guessed from the database's directory name:
// ~/.tm/projects/-Users-zdenkovrabel-unravela-tm/tm.db -> "tm".
//
// Guessing keeps upgrading zero-touch — every existing database works
// immediately after the binary is updated, with no mandatory command first.
// The trade-off accepted is that a user who never runs `tm project rename`
// keeps a derived ticker forever, which is tolerable because a derived
// ticker is usually the right one anyway.
//
// Returns the canonical ticker and the display name (identical here; the
// user's preferred casing is unknowable from a sanitised path).
func placeholderTicker(dbPath string) (ticker, display string) {
	const fallback = "proj"
	if dbPath == "" || dbPath == ":memory:" {
		return fallback, fallback
	}
	dir := filepath.Base(filepath.Dir(dbPath))
	if derived, ok := client.DeriveTicker(dir); ok {
		return derived, derived
	}
	// Fall back to the file's own stem before giving up entirely.
	stem := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	if derived, ok := client.DeriveTicker(stem); ok {
		return derived, derived
	}
	return fallback, fallback
}

// backfillCommentTimestamps fills task_comments.created_at for rows migration
// 00006 copied over with an empty timestamp.
//
// Comment IDs are ULIDs, whose first 10 Crockford-base32 characters encode a
// 48-bit millisecond timestamp. Decoding that is impractical in SQL, so it
// happens here. Any id that is not a parseable ULID (hand-edited data, a
// legacy non-ULID id) falls back to the owning task's updated_at, and then to
// now, so the NOT NULL column always ends up with a usable value.
func backfillCommentTimestamps(db *sql.DB) error {
	rows, err := db.Query(`SELECT c.id, COALESCE(t.updated_at, '')
		FROM task_comments c
		LEFT JOIN tasks t ON t.project_id = c.project_id AND t.id = c.task_id
		WHERE c.created_at = ''`)
	if err != nil {
		return fmt.Errorf("scan comments needing timestamps: %w", err)
	}
	type pending struct{ id, taskUpdated string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.taskUpdated); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan comment row: %w", err)
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate comment rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close comment rows: %w", err)
	}
	if len(todo) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin comment backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range todo {
		ts := commentTimestamp(p.id, p.taskUpdated, now)
		if _, err := tx.Exec(`UPDATE task_comments SET created_at = ? WHERE id = ?`, ts, p.id); err != nil {
			return fmt.Errorf("backfill created_at for comment %q: %w", p.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit comment backfill: %w", err)
	}
	return nil
}

// commentTimestamp resolves one comment's created_at, preferring the ULID's
// own embedded time.
func commentTimestamp(id, taskUpdated, now string) string {
	if parsed, err := ulid.ParseStrict(id); err == nil {
		return ulid.Time(parsed.Time()).UTC().Format(time.RFC3339Nano)
	}
	if taskUpdated != "" {
		return taskUpdated
	}
	return now
}

// stampLegacyDB brings a pre-goose database up to the 00001_init.sql schema
// (in case it predates columns like plan_id/created_at/updated_at) and then
// marks it as already at version 1 so goose.Up skips the init migration.
//
// Detection: a "legacy" database is one where the tasks table exists but
// goose_db_version does not. On a fresh database (no tasks table) this is
// a no-op and goose.Up handles everything from scratch.
func stampLegacyDB(db *sql.DB) error {
	if hasGoose, err := tableExists(db, "goose_db_version"); err != nil {
		return err
	} else if hasGoose {
		return nil
	}
	hasTasks, err := tableExists(db, "tasks")
	if err != nil {
		return err
	}
	if !hasTasks {
		return nil
	}
	if err := reconcileLegacyColumns(db); err != nil {
		return err
	}
	// Run 00001_init.sql against the legacy DB. Every CREATE inside uses
	// IF NOT EXISTS, so this is a no-op for tables the old `allSchemas`
	// loop already created and a backfill for anything that was missing
	// (e.g. a partial legacy state).
	if err := applyInitSchemaIdempotent(db); err != nil {
		return err
	}
	// EnsureDBVersion creates goose_db_version (and seeds version 0 if the
	// table was just created). We ignore the returned current version
	// because we're about to override it.
	if _, err := goose.EnsureDBVersion(db); err != nil {
		return fmt.Errorf("ensure goose version table: %w", err)
	}
	const stamp = `INSERT INTO goose_db_version (version_id, is_applied, tstamp)
		VALUES (?, 1, CURRENT_TIMESTAMP)`
	if _, err := db.Exec(stamp, 1); err != nil {
		return fmt.Errorf("stamp legacy db at version 1: %w", err)
	}
	return nil
}

// applyInitSchemaIdempotent reads 00001_init.sql out of the embedded FS and
// executes only its CREATE statements (the Up section minus the goose
// directives). Used to bring legacy DBs up to the init schema before they
// get stamped at version 1.
func applyInitSchemaIdempotent(db *sql.DB) error {
	raw, err := migrationsFS.ReadFile(migrationsDir + "/00001_init.sql")
	if err != nil {
		return fmt.Errorf("read init migration: %w", err)
	}
	// Strip the -- +goose directives; everything between Up and Down is the
	// schema body, and every statement in it is idempotent.
	sqlText := extractGooseUp(string(raw))
	if _, err := db.Exec(sqlText); err != nil {
		return fmt.Errorf("apply init schema to legacy db: %w", err)
	}
	return nil
}

// extractGooseUp returns the SQL between `-- +goose Up` and `-- +goose Down`
// markers in a goose migration file, with `-- +goose Statement{Begin,End}`
// directives stripped so the body is plain SQL.
func extractGooseUp(text string) string {
	_, body, ok := strings.Cut(text, "-- +goose Up")
	if !ok {
		return ""
	}
	if down, _, found := strings.Cut(body, "-- +goose Down"); found {
		body = down
	}
	var out strings.Builder
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-- +goose Statement") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// reconcileLegacyColumns adds columns that 00001_init.sql defines but the
// oldest pre-goose databases may be missing (in particular, plan_id was
// added imperatively by tm before timestamps and goose existed). Each
// statement uses an idempotent ADD COLUMN: SQLite rejects duplicates with
// "duplicate column name", which we swallow so already-current databases
// pass through cleanly.
func reconcileLegacyColumns(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE tasks ADD COLUMN plan_id TEXT NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			msg := err.Error()
			// "duplicate column" → already present, fine.
			// "no such table" → legacy DB never had this table; goose.Up
			// will create it from 00001_init.sql.
			if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "no such table") {
				continue
			}
			return fmt.Errorf("reconcile legacy column: %w", err)
		}
	}
	return nil
}

// tableExists checks sqlite_master for a table by name. Returns false with
// no error when the table is absent.
func tableExists(db *sql.DB, name string) (bool, error) {
	const q = `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`
	var one int
	err := db.QueryRow(q, name).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table %q: %w", name, err)
	}
	return true, nil
}
