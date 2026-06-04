package filestorage

import (
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/sn3d/tm/internal/client"
)

type commentsRepository struct {
	dir string
}

// Add appends a comment to the given task file. The repository assigns a
// fresh ULID and writes it back into c.ID. Returns an error if the task
// does not exist.
func (cr *commentsRepository) Add(id client.TaskID, c *client.Comment) error {
	tf, err := readTaskFile(cr.dir, id)
	if err != nil {
		return err
	}
	if tf == nil {
		return fmt.Errorf("task %q not found", id)
	}
	c.ID = ulid.Make().String()
	tf.comments = append(tf.comments, *c)
	return writeTaskFile(cr.dir, tf)
}

// GetForTask returns every comment attached to the given task, in insertion
// order. Returns an empty slice when the task has no comments or does not
// exist (matching the sqlite backend's behavior).
func (cr *commentsRepository) GetForTask(id client.TaskID) ([]client.Comment, error) {
	tf, err := readTaskFile(cr.dir, id)
	if err != nil {
		return nil, err
	}
	if tf == nil {
		return []client.Comment{}, nil
	}
	out := make([]client.Comment, len(tf.comments))
	copy(out, tf.comments)
	return out, nil
}
