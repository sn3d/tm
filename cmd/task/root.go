package task

import "github.com/urfave/cli/v3"

var Cmd = &cli.Command{
	Name:  "task",
	Usage: "Manage tasks",
	Commands: []*cli.Command{
		createCmd,
		listCmd,
		getCmd,
		editCmd,
		commentCmd,
	},
}
