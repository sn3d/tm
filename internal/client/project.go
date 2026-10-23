package client

import "time"

// Project is a first-class entity identified by its ticker. Before this
// existed a "project" was just a directory path hashed into a storage
// location, which made cross-project views impossible.
type Project struct {
	// Ticker is the canonical lowercase identifier and the primary key.
	Ticker string
	// DisplayName preserves the user's preferred casing ("TM", "WebApp") so
	// canonicalising the ticker never loses presentation intent.
	DisplayName string
	// Name is the human-readable long form ("Task Manager for Agents").
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	// ArchivedAt hides the project from pickers and lists when non-nil.
	// Archive over delete: destroying a project would destroy its tasks,
	// comments and journal.
	ArchivedAt *time.Time
}

// Label renders the project for UI, preferring the user's casing.
func (p Project) Label() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Ticker
}

// ProjectsRepository persists projects. Every method canonicalises the ticker
// it is given, so callers may pass user input directly.
type ProjectsRepository interface {
	// Save inserts or updates a project. The ticker must already be
	// canonical (use CanonicalTicker); the database enforces this with a
	// CHECK constraint. CreatedAt is stamped on insert and preserved across
	// updates; UpdatedAt is refreshed on every save.
	Save(p *Project) error
	// GetByTicker returns the project, or (nil, nil) when absent. The input
	// ticker is canonicalised, so "TM" and "tm" resolve to the same row.
	GetByTicker(ticker string) (*Project, error)
	// GetAll returns projects ordered by ticker. Archived projects are
	// excluded unless includeArchived is set.
	GetAll(includeArchived bool) ([]Project, error)
	// TaskCounts returns active (non-archived) task counts keyed by
	// canonical ticker, for the `tm init` picker and `tm project list`.
	TaskCounts() (map[string]int, error)
}
