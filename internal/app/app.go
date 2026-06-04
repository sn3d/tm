// Package app is the composition root: it wires a *client.Client to the
// concrete backend selected by Config. Entry points (cmd/, internal/mcp, ...)
// should construct their client via app.NewClient instead of building the
// backend themselves.
package app

import (
	"fmt"

	"github.com/sn3d/tm/internal/backend/filestorage"
	"github.com/sn3d/tm/internal/backend/sqlite"
	"github.com/sn3d/tm/internal/client"
)

// NewClient builds a fully-wired *client.Client from cfg. The backend is
// chosen by cfg.Backend; per-backend options are read from cfg.Options.
// An empty cfg falls back to defaults (sqlite backend in ~/.tm/), so
// callers can pass &client.Config{} when no configuration file exists.
func NewClient(cfg *client.Config) (*client.Client, error) {
	applyDefaults(cfg)
	b, err := newBackend(cfg)
	if err != nil {
		return nil, err
	}
	return client.New(b, client.WithActor(cfg.Actor)), nil
}

func applyDefaults(cfg *client.Config) {
	if cfg.Backend == "" {
		cfg.Backend = "sqlite"
	}
	if cfg.ProjectRoot != "" {
		if cfg.Options == nil {
			cfg.Options = map[string]string{}
		}
		if _, set := cfg.Options["project_root"]; !set {
			cfg.Options["project_root"] = cfg.ProjectRoot
		}
	}
}

func newBackend(cfg *client.Config) (client.Backend, error) {
	switch cfg.Backend {
	case "sqlite":
		return sqlite.NewBackendFromOptions(cfg.Options)
	case "filestorage":
		return filestorage.NewBackendFromOptions(cfg.Options)
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}
