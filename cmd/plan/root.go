package plan

import "github.com/urfave/cli/v3"

var Cmd = &cli.Command{
	Name:  "plan",
	Usage: "Manage plans",
	Commands: []*cli.Command{
		createCmd,
		listCmd,
		getCmd,
		editCmd,
		commentCmd,
	},
}
