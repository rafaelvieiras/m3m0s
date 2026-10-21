---
title: Claude Code
description: Capture sessions and recall memory on every prompt in Claude Code, with hooks and an optional read-only MCP server.
section: Connect your tools
order: 6
---

For Claude Code, m3m0s is a set of hooks that all run `m3m0s hook claude-code`. Install the binary and store the credentials first, as described in [CLI and credentials](/guide/cli/).

## What the hooks do

| Event | Role |
|---|---|
| `SessionStart` | Injects a digest of recent memories. |
| `UserPromptSubmit` | Two entries. A synchronous one with `--inject` searches memory by the prompt and injects the relevant hits. An async one captures. |
| `Stop`, `PreCompact`, `SessionEnd` | Capture the new part of the transcript (`PreCompact` and `SessionEnd` can be async). |
| `PostToolUse` | Optional safety net. It does nothing until the transcript has grown. |

Async hooks cannot inject context, which is why the `--inject` entry must be synchronous.

## Configure the hooks

Add this to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "timeout": 15,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] },
      { "hooks": [{ "type": "command", "timeout": 10,
        "command": "m3m0s hook claude-code --inject 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "timeout": 60,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "PreCompact": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "SessionEnd": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ]
  }
}
```

Create the log directory once with `mkdir -p ~/.m3m0s/hooks`, then run `m3m0s doctor` to confirm the events are registered.

> A packaged Claude Code plugin, installed with `/plugin install`, is planned. Until it ships, the settings above are the way to install.

## Optional: read-only MCP server

With capture and recall automatic, the agent only needs to search explicitly now and then. Register the MCP server in read-only mode, so it cannot add or rewrite memories:

```sh
claude mcp add m3m0s --scope user \
  -e M3M0S_URL=https://memory.example.com \
  -e M3M0S_API_KEY=<your API key> \
  -- m3m0s mcp --read-only
```

See [MCP clients](/guide/mcp/) for the tools it exposes.

## Verify

Open a new session in a project you have memories for and ask about something you decided earlier. A matching memory is injected before the model answers. Every injection is logged in `~/.m3m0s/hooks/claude-code.log`.
