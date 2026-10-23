package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sn3d/tm/internal/client"
)

type projectsRepository struct {
	db *sql.DB
}

// Save inserts or updates a project. The ticker is canonicalised here as well
// as by the caller — the table carries a CHECK (ticker = lower(ticker))
// constraint, so an uncanonicalised value would be rejected by the database
// rather than silently stored.
//
// CreatedAt is stamped on insert and preserved across updates; UpdatedAt is
// refreshed on every save. Both are written back into p.
func (pr *projectsRepository) Save(p *client.Project) error {
	canon, err := client.CanonicalTicker(p.Ticker)
	if err != nil {
		return fmt.Errorf("save project: %w", err)
	}
	p.Ticker = canon
	if p.DisplayName == "" {
		p.DisplayName = canon
	}

	now := time.Now().UTC()
	existing, err := pr.GetByTicker(canon)
	if err != nil {
		return err
	}
	if existing != nil && !existing.CreatedAt.IsZero() {
		p.CreatedAt = existing.CreatedAt
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	var archivedAt sql.NullString
	if p.ArchivedAt != nil {
		archivedAt = sql.NullString{String: p.ArchivedAt.Format(time.RFC3339Nano), Valid: true}
	}

	const upsert = `
		INSERT INTO projects (ticker, display_name, name, created_at, updated_at, archived_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(ticker) DO UPDATE SET
			display_name = excluded.display_name,
			name         = excluded.name,
			updated_at   = excluded.updated_at,
			archived_at  = excluded.archived_at`
	if _, err := pr.db.Exec(upsert, p.Ticker, p.DisplayName, p.Name,
		p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano),
		archivedAt); err != nil {
		return fmt.Errorf("upsert project %q: %w", p.Ticker, err)
	}
	return nil
}

// GetByTicker returns the project, or (nil, nil) when absent. The input is
// canonicalised, so "TM" and "tm" resolve to the same row.
func (pr *projectsRepository) GetByTicker(ticker string) (*client.Project, error) {
	canon, err := client.CanonicalTicker(ticker)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	const q = `SELECT ticker, display_name, name, created_at, updated_at, archived_at
		FROM projects WHERE ticker = ?`
	var (
		p           client.Project
		createdStr  string
		updatedStr  string
		archivedRaw sql.NullString
	)
	err = pr.db.QueryRow(q, canon).Scan(&p.Ticker, &p.DisplayName, &p.Name,
		&createdStr, &updatedStr, &archivedRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query project %q: %w", canon, err)
	}
	if err := hydrateProject(&p, createdStr, updatedStr, archivedRaw); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetAll returns projects ordered by ticker. Archived projects are excluded
// unless includeArchived is set — archive is what hides a project from
// pickers and lists, chosen over delete because destroying a project would
// destroy its tasks, comments and journal.
func (pr *projectsRepository) GetAll(includeArchived bool) ([]client.Project, error) {
	q := `SELECT ticker, display_name, name, created_at, updated_at, archived_at FROM projects`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY ticker`

	rows, err := pr.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	out := make([]client.Project, 0)
	for rows.Next() {
		var (
			p           client.Project
			createdStr  string
			updatedStr  string
			archivedRaw sql.NullString
		)
		if err := rows.Scan(&p.Ticker, &p.DisplayName, &p.Name,
			&createdStr, &updatedStr, &archivedRaw); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		if err := hydrateProject(&p, createdStr, updatedStr, archivedRaw); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project rows: %w", err)
	}
	return out, nil
}

// TaskCounts returns active (non-archived) task counts keyed by canonical
// ticker, for the `tm init` picker and `tm project list`. Projects with no
// tasks are absent from the map rather than present with a zero — callers
// read a missing key as zero anyway.
func (pr *projectsRepository) TaskCounts() (map[string]int, error) {
	const q = `SELECT project_id, COUNT(*) FROM tasks
		WHERE archived_at IS NULL GROUP BY project_id`
	rows, err := pr.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query task counts: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var (
			ticker string
			count  int
		)
		if err := rows.Scan(&ticker, &count); err != nil {
			return nil, fmt.Errorf("scan task count row: %w", err)
		}
		out[ticker] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task count rows: %w", err)
	}
	return out, nil
}

func hydrateProject(p *client.Project, createdStr, updatedStr string, archivedRaw sql.NullString) error {
	var err error
	if p.CreatedAt, err = parseSQLTime(createdStr); err != nil {
		return fmt.Errorf("parse created_at for project %q: %w", p.Ticker, err)
	}
	if p.UpdatedAt, err = parseSQLTime(updatedStr); err != nil {
		return fmt.Errorf("parse updated_at for project %q: %w", p.Ticker, err)
	}
	if archivedRaw.Valid && archivedRaw.String != "" {
		parsed, perr := parseSQLTime(archivedRaw.String)
		if perr != nil {
			return fmt.Errorf("parse archived_at for project %q: %w", p.Ticker, perr)
		}
		p.ArchivedAt = &parsed
	} else {
		p.ArchivedAt = nil
	}
	return nil
}
