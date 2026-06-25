// Package board hosts the `tm board` command — an interactive kanban
// view of tasks grouped into Backlog / In Progress / Review / Done.
//
// The runtime is bubbletea; the model + rendering live in
// internal/tui/board. This file is the cmd wiring: parse flags, build
// the load and move closures, hand them to the Model, run tea.Program.
package board

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sn3d/tm/internal/app"
	"github.com/sn3d/tm/internal/client"
	"github.com/sn3d/tm/internal/tui"
	boardview "github.com/sn3d/tm/internal/tui/board"
	"github.com/urfave/cli/v3"
)

var Cmd = &cli.Command{
	Name:  "board",
	Usage: "Show tasks as an interactive kanban board (Backlog / In Progress / Review / Done)",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "mine",
			Usage: "Show only tasks assigned to the current actor.",
		},
		&cli.StringFlag{
			Name:  "parent",
			Usage: "Show only children of this parent task (e.g. a planning task).",
		},
		&cli.BoolFlag{
			Name:  "archived",
			Usage: "Show only archived tasks (default hides them).",
		},
		&cli.BoolFlag{
			Name:  "all",
			Usage: "Show every task including archived ones.",
		},
		app.ActorFlag,
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		cfg := ctx.Value(client.CfgKey).(*client.Config)
		app.ApplyActor(cfg, command)
		c, err := app.NewClient(cfg)
		if err != nil {
			return err
		}

		if command.Bool("archived") && command.Bool("all") {
			return fmt.Errorf("--archived and --all are mutually exclusive")
		}
		archived := client.ArchivedActive
		switch {
		case command.Bool("archived"):
			archived = client.ArchivedOnly
		case command.Bool("all"):
			archived = client.ArchivedAll
		}

		// load is captured at construction so the model can re-invoke it
		// after every action without re-parsing flags or rebuilding the
		// client. The closure honours --mine, --parent, --archived/--all.
		parent := command.String("parent")
		hasParent := command.IsSet("parent")
		mine := command.Bool("mine")
		actor := cfg.Actor

		load := func() ([]client.Task, error) {
			var (
				tasks []client.Task
				err   error
			)
			if hasParent {
				tasks, err = c.GetTasksByParent(parent, archived)
			} else {
				tasks, err = c.ListTasks(archived)
			}
			if err != nil {
				return nil, err
			}
			if mine {
				tasks = filterAssigned(tasks, actor)
			}
			return tasks, nil
		}

		// move is the state-only transition. Reassignment is intentionally
		// out of scope for the v1 interactive board (see internal/tui/board
		// docs and the design doc).
		move := func(id client.TaskID, to client.TaskState) error {
			t, err := c.GetTask(id)
			if err != nil {
				return err
			}
			return c.EditTask(id, client.EditTaskInput{
				Subject:       t.Subject,
				Description:   t.Description,
				State:         to,
				AssignedAgent: t.AssignedAgent,
				DependsOn:     t.DependsOn,
				ParentID:      t.ParentID,
				Labels:        t.Labels,
				Mode:          t.Mode,
			})
		}

		// loadDetail fetches task + comments + child tasks in one shot.
		// Children come from GetTasksByParent with archived=All so the
		// detail view shows even archived children — opting in to see
		// them is the whole point of opening a parent.
		loadDetail := func(id client.TaskID) (boardview.TaskDetail, error) {
			t, err := c.GetTask(id)
			if err != nil {
				return boardview.TaskDetail{}, err
			}
			comments, err := c.GetTaskComments(id)
			if err != nil {
				return boardview.TaskDetail{}, err
			}
			subtasks, err := c.GetTasksByParent(id, client.ArchivedAll)
			if err != nil {
				return boardview.TaskDetail{}, err
			}
			return boardview.TaskDetail{Task: *t, Comments: comments, Subtasks: subtasks}, nil
		}

		// addComment posts a comment authored by the configured actor.
		addComment := func(id client.TaskID, body string) error {
			return c.AddTaskComment(id, cfg.Actor, body)
		}

		// editDescription uses tea.ExecProcess so $EDITOR runs in the
		// parent terminal (alt-screen suspended, restored on return).
		// We write `current` to a temp file, edit, read back, and patch
		// the task via EditTask. The signal messages are typed in the
		// board package so its Update knows how to handle them.
		editDescription := func(id client.TaskID, current string) tea.Cmd {
			tmp, err := os.CreateTemp("", "tm-desc-*.md")
			if err != nil {
				return func() tea.Msg { return boardview.ErrorMsg{Err: err} }
			}
			path := tmp.Name()
			if _, err := tmp.WriteString(current); err != nil {
				_ = tmp.Close()
				_ = os.Remove(path)
				return func() tea.Msg { return boardview.ErrorMsg{Err: err} }
			}
			_ = tmp.Close()

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			ed := exec.Command(editor, path) // #nosec G204 — $EDITOR is user-controlled
			return tea.ExecProcess(ed, func(execErr error) tea.Msg {
				defer os.Remove(path)
				if execErr != nil {
					return boardview.ErrorMsg{Err: execErr}
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return boardview.ErrorMsg{Err: err}
				}
				next := string(raw)
				t, err := c.GetTask(id)
				if err != nil {
					return boardview.ErrorMsg{Err: err}
				}
				if err := c.EditTask(id, client.EditTaskInput{
					Subject:       t.Subject,
					Description:   next,
					State:         t.State,
					AssignedAgent: t.AssignedAgent,
					DependsOn:     t.DependsOn,
					ParentID:      t.ParentID,
					Labels:        t.Labels,
					Mode:          t.Mode,
				}); err != nil {
					return boardview.ErrorMsg{Err: err}
				}
				return boardview.DescriptionEditedMsg{}
			})
		}

		// archive matches `tm archive` CLI behaviour: cascade to descendants
		// by default. The cascaded count flows through so the board footer
		// can report "archived (+ N descendants)" — mirroring what the CLI
		// prints — instead of leaving the user guessing which children
		// silently disappeared from the next reload.
		archive := func(id client.TaskID) (int, error) {
			return c.ArchiveTask(id, true)
		}

		resolver := tui.NewResolver(cfg.Styling)
		model := boardview.NewModel(resolver, load, move, loadDetail, addComment, editDescription, archive)
		_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
		return err
	},
}

// filterAssigned keeps only the tasks owned by actor. Empty actor returns
// the input unchanged so a caller without a resolved identity doesn't get
// a silently empty board.
func filterAssigned(in []client.Task, actor string) []client.Task {
	if actor == "" {
		return in
	}
	out := make([]client.Task, 0, len(in))
	for _, t := range in {
		if t.AssignedAgent == actor {
			out = append(out, t)
		}
	}
	return out
}
