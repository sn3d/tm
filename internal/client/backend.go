package client

// Backend is a concrete persistence implementation that exposes per-entity
// repositories. Implementations include sqlite, and (planned) github / jira.
type Backend interface {
	Tasks() TasksRepository
	Comments() CommentsRepository
	Events() EventsRepository
	ActorCursors() ActorCursorRepository
}
