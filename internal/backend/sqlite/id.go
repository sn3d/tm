package sqlite

import (
	"database/sql"
	"fmt"
	"strconv"
)

// nextProjectNumericID returns the next sequential numeric ID within one
// project. Numbering restarts per project, which is what TM/#1 and BILL/#1
// coexisting implies — and it is what makes the composite primary key able to
// fold many old databases together without renumbering anything.
//
// The GLOB filter requires a leading digit and forbids any non-digit char, so
// ULIDs and other mixed-form IDs are excluded from the max. The query runs
// inside the caller's transaction so the read+write pair stays atomic.
func nextProjectNumericID(tx *sql.Tx, project string) (string, error) {
	const q = `
		SELECT COALESCE(MAX(CAST(id AS INTEGER)), 0) FROM tasks
		WHERE project_id = ?
		  AND id GLOB '[0-9]*' AND id NOT GLOB '*[^0-9]*'`
	var max int64
	if err := tx.QueryRow(q, project).Scan(&max); err != nil {
		return "", fmt.Errorf("query next id for project %q: %w", project, err)
	}
	return strconv.FormatInt(max+1, 10), nil
}
