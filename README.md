# TM — Task Manager for Agents

TM is a local task manager for agents. Agents do the planning, and planning should result in tasks for sub-agents.

It ships as:
- a CLI (`tm`) for managing tasks from the terminal
- an MCP stdio server (`tm mcp`) that exposes those tasks to MCP-aware clients like Claude Code


## Installation

You can install `tm` easily by running command:

```
brew install sn3d/tap/tm
```

## Add the TM MCP server to Claude Code

After installatin, you can register it with Claude Code using `claude mcp add`:

```sh
claude mcp add tm -- "$(pwd)/bin/tm" mcp
```

To make it available across all your projects, register it at user scope:

```sh
claude mcp add --scope user tm -- "$(pwd)/bin/tm" mcp
```

Verify it's wired up:

```sh
claude mcp list
```

You should see `tm` in the list. Inside Claude Code, the TM tools will then be available to the agent.

### Auto-approve TM tools

By default, Claude Code asks for confirmation every time it invokes a TM tool. To pre-approve every tool exposed by the `tm` server, add a permission rule to your user-level settings at `~/.claude/settings.json`:

```json
{
  "permissions": {
    "allow": [
      "mcp__tm"
    ]
  }
}
```

The rule `mcp__tm` whitelists every tool on the `tm` MCP server. (Per-tool rules use the form `mcp__tm__<tool-name>`; wildcards like `mcp__tm__*` are not supported.)

Restart any running Claude Code sessions for the change to take effect. Run `/permissions` inside Claude Code to verify the rule is loaded.

## Configuration

TM reads `taskmanager.yaml` from the current working directory if present. When the file is missing, TM falls back to built-in defaults.


## Building from source code

```sh
task build
```

The binary is placed at `./bin/tm`.


