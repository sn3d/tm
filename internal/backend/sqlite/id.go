package sqlite

import (
	"database/sql"
	"fmt"
	"strconv"
)

// nextSharedNumericID returns the next sequential numeric ID for tasks. The
// counter is named "shared" for historical reasons — before plans were
// collapsed into tasks the counter was shared across both tables so IDs were
// guaranteed unique across them. Post-collapse the counter scans only
// tasks, and former plan IDs are preserved as task IDs by the collapse
// migration. The GLOB filter requires a leading digit and forbids any
// non-digit char, so ULIDs and other mixed-form IDs are excluded. The query
// takes the max as integer and returns (max + 1) as a string. It runs
// inside the caller's transaction so the read+write pair is atomic.
func nextSharedNumericID(tx *sql.Tx) (string, error) {
	const q = `
		SELECT COALESCE(MAX(CAST(id AS INTEGER)), 0) FROM tasks
		WHERE id GLOB '[0-9]*' AND id NOT GLOB '*[^0-9]*'`
	var max int64
	if err := tx.QueryRow(q).Scan(&max); err != nil {
		return "", fmt.Errorf("query shared id: %w", err)
	}
	return strconv.FormatInt(max+1, 10), nil
}
