// Package app is the composition root: it wires a *client.Client to the
// sqlite backend for the project and instance that scope resolution picked.
// Entry points (cmd/, internal/mcp, ...) should construct their client via
// app.NewClient instead of building the backend themselves.
package app

import (
	"fmt"

	"github.com/sn3d/tm/internal/backend/sqlite"
	"github.com/sn3d/tm/internal/client"
)

// NewClient builds a fully-wired *client.Client bound to cfg.Resolved's
// project, reading the database cfg.Resolved names.
//
// Scope resolution must have run first (the root command's Before hook does
// it). A config with no resolved project is an error rather than a guess —
// no command silently writes to a project it inferred.
func NewClient(cfg *client.Config) (*client.Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no configuration loaded")
	}
	if cfg.Resolved.Project == "" {
		return nil, fmt.Errorf("no TM project selected; run 'tm init' here or in a parent directory, or pass --project TICKER")
	}
	b, err := newBackend(cfg)
	if err != nil {
		return nil, err
	}
	return client.New(b,
		client.WithActor(cfg.Actor),
		client.WithProject(cfg.Resolved.Project),
	), nil
}

// NewInstanceClient builds a Client for registry-level work (`tm project`,
// `tm init --list`, `tm migrate`, cross-project reads) where a database is
// needed but no single project is bound. The returned Client errors on any
// single-project operation, which is the intended guard.
func NewInstanceClient(cfg *client.Config) (*client.Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no configuration loaded")
	}
	b, err := newBackend(cfg)
	if err != nil {
		return nil, err
	}
	opts := []client.Option{client.WithActor(cfg.Actor)}
	// Bind the project when one happens to be resolved, so a command can
	// use both the registry and the current project without rebuilding.
	if cfg.Resolved.Project != "" {
		opts = append(opts, client.WithProject(cfg.Resolved.Project))
	}
	return client.New(b, opts...), nil
}

func newBackend(cfg *client.Config) (client.Backend, error) {
	// Config.Backend remains the seam where a future github / jira backend
	// plugs in. Only sqlite is implemented; filestorage was removed once all
	// markdown moved into SQLite tables.
	switch cfg.Backend {
	case "", "sqlite":
		return sqlite.NewBackend(resolveDBPath(cfg))
	default:
		return nil, fmt.Errorf("unknown backend %q (only %q is implemented)", cfg.Backend, "sqlite")
	}
}

// resolveDBPath prefers the resolved instance path, falling back to an
// explicit db_path option (used by tests wiring a client directly).
func resolveDBPath(cfg *client.Config) string {
	if cfg.Resolved.DBPath != "" {
		return cfg.Resolved.DBPath
	}
	return cfg.Options["db_path"]
}
