package client

type CommentID = string

type Comment struct {
	ID      CommentID
	Who     string
	Comment string
}

type CommentsRepository interface {
	// Add inserts a comment for the given task. The repository assigns
	// c.ID and writes it back into the value pointed to.
	Add(id TaskID, c *Comment) (err error)
	GetForTask(id TaskID) (comments []Comment, err error)
}
