# pi integration

A [pi](https://github.com/earendil-works/pi) extension that gives pi the same memory loop as the Claude Code hooks, by reusing the `m3m0s` binary:

- **Recall** (`before_agent_start`): runs `m3m0s hook claude-code --inject` with the prompt and injects the relevant memories as a hidden custom message ([D44](../../docs/decisions/D44-per-prompt-memory-search.md)).
- **Capture** (`agent_end` as `Stop`, `session_shutdown` as `SessionEnd`): runs `m3m0s hook claude-code` against pi's session file. The transcript parser accepts pi's `message` lines; memories are stored with `source: "pi"` ([D46](../../docs/decisions/D46-pi-capture.md)).

## Install

1. `go install` (or copy) `m3m0s` to `~/go/bin/m3m0s`, and create `~/.m3m0s/hook.env` with `M3M0S_URL` and `M3M0S_API_KEY`.
2. Copy `m3m0s-memory.ts` to `~/.pi/agent/extensions/`. Adjust `BIN` at the top if the binary lives elsewhere.
3. Optional: register the MCP server read-only in `~/.pi/agent/mcp.json`:
   `{"m3m0s": {"command": "sh", "args": ["-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec \"$HOME/go/bin/m3m0s\" mcp --read-only"]}}`

Failures are silent and never block pi; the log is `~/.m3m0s/hooks/claude-code.log`.
