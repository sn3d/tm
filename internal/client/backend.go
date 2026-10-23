package client

// Backend is a concrete persistence implementation that exposes per-entity
// repositories. sqlite is the only implementation; the interface is the seam
// where a future github / jira backend plugs in.
//
// One Backend corresponds to one DB *instance*, which holds many projects —
// that is what makes cross-project reads (InboxAll, JournalAll) a single
// connection rather than a fan-out.
type Backend interface {
	Projects() ProjectsRepository
	Tasks() TasksRepository
	Descriptions() DescriptionsRepository
	Comments() CommentsRepository
	Events() EventsRepository
	ActorCursors() ActorCursorRepository
}
