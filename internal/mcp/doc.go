// Package mcp exposes a TM client over the Model Context Protocol via stdio,
// so MCP-aware agents (e.g. Claude Desktop, Claude Code) can manipulate tasks
// and comments through the same surface the CLI uses.
//
// What belongs here: the MCP server wrapper, tool registrations, and the
// thin handler functions that translate MCP tool calls into client.Client
// method invocations. Nothing in this package should re-implement domain
// logic — every handler delegates to the client.
//
// Dependencies: imports client. Construction of the underlying client is
// the caller's responsibility (mcp.NewServer takes a *client.Client), so
// this package does not import app or any backend.
package mcp
