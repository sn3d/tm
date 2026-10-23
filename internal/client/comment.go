package client

import "time"

// CommentID is a ULID, already globally unique across projects, so it needs
// no project qualifier.
type CommentID = string

type Comment struct {
	ID      CommentID
	Who     string
	Comment string
	// CreatedAt is explicit rather than derived from the ULID. Ordering by
	// ULID works within one database, but rows merged from several databases
	// (see `tm migrate`) have no meaningful cross-source ULID order.
	CreatedAt time.Time
}

type CommentsRepository interface {
	// Add inserts a comment for the task the ref addresses. The repository
	// assigns c.ID (ULID) and stamps c.CreatedAt when zero, writing both back
	// into the value pointed to.
	Add(ref TaskID, c *Comment) (err error)
	// GetForTask returns every comment on the task, oldest first.
	GetForTask(ref TaskID) (comments []Comment, err error)
}
