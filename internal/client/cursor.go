package client

import "time"

// ActorCursorRepository persists per-actor, per-project "last seen"
// timestamps used by Client.Inbox to compute the "recent changes since I last
// looked" set.
//
// Scope is per (actor, project), not global per actor. A global cursor would
// mean glancing at one project marks every other project's work as seen,
// silently losing an agent's inbox across repos.
type ActorCursorRepository interface {
	// Get returns the actor's last-seen timestamp in the given project. An
	// unseen (actor, project) pair yields the zero time, so all events
	// qualify as "new".
	Get(actor, project string) (time.Time, error)
	// Set overwrites the actor's last-seen timestamp for one project.
	Set(actor, project string, ts time.Time) error
}
