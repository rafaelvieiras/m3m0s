---
title: MCP clients
description: Register the stdio MCP server in any host that supports MCP, with or without write access.
section: Connect your tools
order: 10
---

`m3m0s mcp` is a stdio MCP server. It is a stateless bridge to a running `m3m0s serve`, so any MCP host can use it, for example Claude Code, opencode or pi.

## Register it

Use the command `m3m0s` with the argument `mcp`, and pass the two client variables:

```json
{
  "mcpServers": {
    "m3m0s": {
      "command": "m3m0s",
      "args": ["mcp", "--read-only"],
      "env": {
        "M3M0S_URL": "https://memory.example.com",
        "M3M0S_API_KEY": "<your API key>"
      }
    }
  }
}
```

The exact file and key names differ per host (`mcpServers`, `mcp`, and so on). The command, arguments and environment are what matter.

## Tools

| Tool | Purpose |
|---|---|
| `memory_search` | Search by a short query. Supports a project or domain, a `kind` and a token budget, and a `keyword` mode for exact terms. |
| `memory_get` | Read one memory in full. |
| `memory_recent` | List the newest memories. |
| `memory_stats` | Totals for the collection. |
| `memory_add`, `memory_update`, `memory_forget_sweep`, `memory_restore` | Write tools. Hidden in read-only mode. |

## Read-only mode

`m3m0s mcp --read-only` (or `M3M0S_MCP_READ_ONLY=1`) hides the four write tools and rejects them if called. With automatic capture in place, agents don't need to write memory through MCP, and a tool that exists will eventually be called by mistake.

> Read-only mode is a client-side convenience, not a security boundary. Anything holding the API key can still call the HTTP API.

## Tell the agent when to search

The server ships instructions that tell the model to search before answering questions about your projects, past decisions and conventions, to treat results as notes rather than instructions, and not to call `memory_add` for routine progress. Entries of kind `state` describe a past moment, so the agent is told to verify them before relying on them.
