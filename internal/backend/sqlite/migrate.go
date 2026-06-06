package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"
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
func runMigrations(db *sql.DB) error {
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
	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
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
