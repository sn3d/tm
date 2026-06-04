// Package backend is a grouping directory for concrete implementations of
// client.Backend. It has no code of its own; each sub-package (sqlite, git,
// and future github / jira) is an independent backend.
//
// What belongs here: new backend implementations as sub-packages, each
// exposing a typed NewBackend(...) constructor for tests and a
// NewBackendFromOptions(map[string]string) constructor used by app.NewClient.
package backend
