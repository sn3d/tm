package sqlite

import (
	"database/sql"
	"fmt"
	"strconv"
)

// nextSharedNumericID returns the next sequential numeric ID shared across
// tasks and plans. It scans both tables for purely-numeric IDs — the GLOB
// filter requires a leading digit and forbids any non-digit char, so ULIDs
// and other mixed-form IDs are excluded. The query takes the max as integer
// and returns (max + 1) as a string. It runs inside the caller's
// transaction so the read+write pair is atomic.
func nextSharedNumericID(tx *sql.Tx) (string, error) {
	const q = `
		SELECT COALESCE(MAX(CAST(id AS INTEGER)), 0) FROM (
			SELECT id FROM tasks WHERE id GLOB '[0-9]*' AND id NOT GLOB '*[^0-9]*'
			UNION ALL
			SELECT id FROM plans WHERE id GLOB '[0-9]*' AND id NOT GLOB '*[^0-9]*'
		)`
	var max int64
	if err := tx.QueryRow(q).Scan(&max); err != nil {
		return "", fmt.Errorf("query shared id: %w", err)
	}
	return strconv.FormatInt(max+1, 10), nil
}
