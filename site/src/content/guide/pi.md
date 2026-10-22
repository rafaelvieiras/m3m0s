---
title: pi
description: An extension for the pi coding agent that recalls memory before each run and captures sessions afterwards.
section: Connect your tools
order: 7
---

The pi extension gives pi the same memory loop as the Claude Code hooks, by calling the `m3m0s` binary.

- **Recall** (`before_agent_start`): runs `m3m0s hook claude-code --inject` with the prompt and appends the relevant memories as a hidden custom message. The block is fenced in `<memory-context>` with a note that it is recalled memory and not new user input.
- **Capture** (`agent_end`, `session_shutdown`): runs `m3m0s hook claude-code` against pi's session file. Memories are stored with `source: "pi"`.

Nothing is injected on compaction. Recall happens per prompt.

## Install

1. Install the binary and store the credentials: [CLI and credentials](/guide/cli/). The extension looks for `m3m0s` at `~/go/bin/m3m0s` first and then on `PATH`.
2. Install the package from npm:

```sh
pi install npm:m3m0s-pi
```

3. Restart pi. Set `M3M0S_BIN` if the binary lives somewhere else.

### Manual install

Copy the extension into pi's extensions folder instead:

```sh
mkdir -p ~/.pi/agent/extensions
curl -fsSL https://raw.githubusercontent.com/rafaelvieiras/m3m0s/main/integrations/pi/m3m0s-memory.ts \
  -o ~/.pi/agent/extensions/m3m0s-memory.ts
```


Then restart pi.

## Optional: read-only MCP

Register the server in `~/.pi/agent/mcp.json`:

```json
{
  "m3m0s": {
    "command": "sh",
    "args": ["-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec m3m0s mcp --read-only"]
  }
}
```

## Troubleshooting

Failures are silent and never block pi. Read `~/.m3m0s/hooks/claude-code.log` and run `m3m0s doctor`. If a memory from another project shows up, see [Recall and tuning](/guide/recall/).
