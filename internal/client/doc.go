// Package client is the core of TM: it defines the domain types (Task,
// Comment, State, TaskID, CommentID), the Backend interface together with
// its per-entity repositories, the Config struct, and the Client facade
// that exposes the operations agents and the CLI invoke.
//
// What belongs here: anything that describes WHAT TM does, independent of
// how persistence is implemented. New domain concepts, new Client methods,
// new Backend interface methods, and new Config fields all go here.
//
// This package must depends on nothing else in internal/. It is
// imported by every other internal package (backend implementations,
// app, cmd, mcp).
package client
