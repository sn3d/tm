package client

import "time"

// ActorCursorRepository persists per-actor "last seen" timestamps used by
// Client.Inbox to compute the "recent changes since I last looked" set.
type ActorCursorRepository interface {
	// Get returns the actor's last-seen timestamp. An unseen actor yields the
	// zero time (so all events qualify as "new").
	Get(actor string) (time.Time, error)
	// Set overwrites the actor's last-seen timestamp.
	Set(actor string, ts time.Time) error
}
