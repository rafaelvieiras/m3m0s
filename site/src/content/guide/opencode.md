---
title: opencode
description: A plugin for opencode that appends relevant memories to each turn and captures sessions when they go idle.
section: Connect your tools
order: 8
---

The opencode plugin gives [opencode](https://opencode.ai) the same memory loop as the Claude Code hooks.

- **Recall:** each user message is searched against m3m0s, and the relevant hits are appended to that turn's system prompt.
- **Capture:** on `session.idle` (as `Stop`) and on shutdown (`SessionEnd`) the session text is written to `~/.m3m0s/hooks/opencode/<session>.jsonl` and handed to `m3m0s hook claude-code`. Memories carry `source: "opencode"`.

## Install

1. Install the binary and store the credentials: [CLI and credentials](/guide/cli/). Set `M3M0S_BIN` if the binary is not at `~/go/bin/m3m0s` or on `PATH`.
2. Add the npm package to the `plugin` array of `opencode.json`. opencode installs it on start:

```json
{
  "plugin": ["m3m0s-opencode"]
}
```

### Manual install

Or put the file in opencode's plugins folder:

```sh
mkdir -p ~/.config/opencode/plugins
curl -fsSL https://raw.githubusercontent.com/rafaelvieiras/m3m0s/main/integrations/opencode/m3m0s-memory.js \
  -o ~/.config/opencode/plugins/m3m0s-memory.js
```

Then add it to `opencode.json`:

```json
{
  "plugin": ["./plugins/m3m0s-memory.js"]
}
```

## Optional: read-only MCP

So the agent can also search explicitly:

```json
{
  "mcp": {
    "m3m0s": {
      "type": "local",
      "enabled": true,
      "command": ["sh", "-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec m3m0s mcp --read-only"]
    }
  }
}
```

## Troubleshooting

Failures are silent and never block opencode. The log is `~/.m3m0s/hooks/claude-code.log`. Run `m3m0s doctor` to check credentials and the connection.
