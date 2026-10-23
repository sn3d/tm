# TM — Task Manager for Agents

TM is a local task manager for agents. Agents do the planning, and planning should result in tasks for sub-agents.

It ships as:
- a CLI (`tm`) for managing tasks from the terminal
- an MCP stdio server (`tm mcp`) that exposes those tasks to MCP-aware clients like Claude Code

Tasks live in **projects**. One agent can see and manage work across every project, so a single agent isn't confined to one repository.


## Installation

You can install `tm` easily by running command:

```
brew install sn3d/tap/tm
```

## Getting started

Map a folder to a project:

```sh
cd ~/code/my-repo
tm init
```

On a terminal this shows a picker listing the projects in your instance, with an option to create a new one. Pick or create, and TM records the folder → project mapping.

Non-interactively (agents, CI, scripts), every path has a flag:

```sh
tm init --new TM --name "Task Manager"   # create a project and map this folder
tm init --project TM                     # map to an existing project
tm init --list                           # list projects; write nothing
tm init --force --project OTHER          # remap an already-mapped folder
```

Bare `tm init` in a folder that's already mapped just prints the mapping and exits — safe to run speculatively.

Then work with tasks:

```sh
tm create "Fix the login redirect"
tm list
tm get TM/#42
tm board                      # interactive kanban board
tm inbox                      # your open work + what changed since you last looked
```

## Projects and task IDs

A project is identified by a **ticker** — up to 15 characters, case-insensitive, so `TM`, `tm` and `Tm` all mean the same project.

Tasks are addressed as `TICKER/#id`:

```sh
tm get TM/#42        # fully qualified
tm get tm/42         # the '#' is optional, case doesn't matter
tm get 42            # bare id — resolves to the current folder's project
tm get '#42'         # same
```

TM always *prints* the fully qualified form, so an ID copied from output is unambiguous about which project it belongs to.

Managing projects:

```sh
tm project list
tm project new BILL --name "Billing service"
tm project show                  # the current folder's project
tm project rename TM --name "…"  # the ticker itself is immutable
tm project archive OLD           # hides it; data is kept
```

### Working across projects

```sh
tm inbox --all              # every project in the instance
tm journal list --all
tm list -p BILL             # act on another project without leaving this folder
tm board -p BILL
```

`--project` / `-p` and `--instance` are global flags that override the folder mapping; `$TM_PROJECT` and `$TM_INSTANCE` do the same via the environment.

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

MCP tools take an optional `project` parameter and default to the project mapped to the working directory. Task IDs appear in the `TICKER/#id` string form.

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

TM keeps one central config at `~/.tm/config.yaml`. It holds the database instances, the default instance, and the folder → project mappings:

```yaml
instances:
  local:
    driver: sqlite
    path: ~/.tm/local.db
  work:
    driver: sqlite
    path: ~/Dropbox/tm/work.db

default_instance: local

mappings:
  /Users/me/code/my-repo:   local/TM
  /Users/me/code/billing:   work/BILL

actor: me
```

`tm init` creates this file on first run, with a `local` instance at `~/.tm/local.db`.

An **instance** is one database. A project lives in exactly one instance, which keeps separate contexts (work vs personal) genuinely apart. Mappings are resolved by walking up from the working directory, so subdirectories of a mapped repo resolve to the same project, and several folders may point at one project.

A folder with no mapping is an error rather than a guess — no command silently writes to a project you didn't choose:

```
$ tm list
Error: no TM project mapped for /Users/me/code/newrepo

Run 'tm init' here or in a parent directory, or pass --project TICKER.
```

Per-repo `taskmanager.yaml` and `taskmanager.local.yaml` are still read for preferences (`actor`, `styling` overrides). They no longer determine which project a folder belongs to — that lives in the central registry.

## Migrating from an earlier version

Older versions of TM kept one database per directory under `~/.tm/projects/`. `tm migrate` folds those into your current instance:

```sh
tm migrate --dry-run     # show the plan, change nothing
tm migrate
```

It derives a ticker per database, preserves task IDs, comments, descriptions and dependencies, writes a folder mapping for each imported project, and can also import markdown task files from older checked-in `.tm/` directories.

Source databases are opened read-only and left untouched, so nothing is deleted and re-running is safe.

**Back up `~/.tm/` before upgrading.** The new schema is applied automatically the first time the new binary opens a database, and an upgraded database can't be read by an older `tm`.

## Building from source code

```sh
task build
```

The binary is placed at `./bin/tm`.
