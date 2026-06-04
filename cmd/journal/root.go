package journal

import "github.com/urfave/cli/v3"

var Cmd = &cli.Command{
	Name:  "journal",
	Usage: "Inspect the audit-log journal of mutations",
	Commands: []*cli.Command{
		listCmd,
	},
}
