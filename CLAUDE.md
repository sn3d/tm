# TM - Task Manager for Agents

TM is local task manager for agents. Agents do planning and planning should result in tasks for sub agents.

## Folder structure

- `./main.go`: thin entry point — instantiates the root CLI command and runs it
- `./cmd`: CLI command definitions for `tm` and all its subcommands
    - `./cmd/root.go`: builds the root `*cli.Command` and registers top-level subcommands
    - `./cmd/subcommand1.go`: defines `subcommand1` as a `*cli.Command` factory
    - `./cmd/subcommand2/`: `subcommand3` promoted to its own package because it has subsubcommands
        - `./cmd/subcommand2/root.go`: builds the `subcommand3` `*cli.Command` and registers its subsubcommands
        - `./cmd/subcommand2/subsubcommand1.go`: defines `subsubcommand1` as a `*cli.Command` factory
- `./internal`: all internal implementations, not importable from outside the module
    - `./internal/client`: contains the `Client` struct, the main facade for TM
    - `./internal/mcp`: tool methods (mcp-go fashion) for the MCP server; depends on `./internal/client`
- `./Taskfile.yaml`: like a Makefile — defines building, testing, and deploying procedures

## Commands

`task test` - use it for testing
`task build` - use it for building binary

## Used frameworks and libaries

- [urfave v3](https://cli.urfave.org/v3/getting-started/): For CLI, subcommands, arguments, the TM is using urfave library
- [BubbleTea](https://github.com/charmbracelet/bubbletea): For UI in terminal (TUI)
- [MCP-GO](https://github.com/mark3labs/mcp-go): For MCP server 