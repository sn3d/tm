// Package app is the composition root of TM: it owns the dependency-injection
// switch that maps a client.Config to the concrete backend, and exposes
// NewClient(cfg) as the one-line entry point used by cmd/, internal/mcp, and
// any future entry point.
//
// Here belong backend-selection switch (one case per backend),
// and any future cross-cutting wiring that touches more than one backend.
// Anything tied to a single backend lives in that backend's package; anything
// independent of backends lives in client.
package app
