// Package project hosts the `tm project` subcommands: the registry-level
// operations on projects themselves, as opposed to the task operations that
// run inside one project.
package project

import "github.com/urfave/cli/v3"

var Cmd = &cli.Command{
	Name:  "project",
	Usage: "Manage projects in the current instance",
	Commands: []*cli.Command{
		listCmd,
		newCmd,
		renameCmd,
		archiveCmd,
		showCmd,
	},
}
