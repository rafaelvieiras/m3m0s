# opencode integration

A plugin that gives [opencode](https://opencode.ai) the same memory loop as the Claude Code hooks, by reusing the `m3m0s` binary:

- **Recall:** each user message is searched against m3m0s and the relevant hits are appended to that turn's system prompt ([D44](../../docs/decisions/D44-per-prompt-memory-search.md), [D47](../../docs/decisions/D47-prompt-search-topic-gate.md)).
- **Capture:** on `session.idle` (as `Stop`) and on shutdown (`SessionEnd`) the session text is written to `~/.m3m0s/hooks/opencode/<session>.jsonl` and handed to `m3m0s hook claude-code`; memories carry `source: "opencode"` ([D49](../../docs/decisions/D49-opencode-integration.md)).

## Install

From npm (needs the `m3m0s` binary on `PATH` or at `~/go/bin/m3m0s`, plus `~/.m3m0s/hook.env`): add `"m3m0s-opencode"` to the `plugin` array of `opencode.json`.

Manual install:

1. Install `m3m0s` to `~/go/bin/m3m0s` (or set `M3M0S_BIN`) and create `~/.m3m0s/hook.env` with `M3M0S_URL` and `M3M0S_API_KEY`.
2. Copy `m3m0s-memory.js` to `~/.config/opencode/plugins/` and add `"./plugins/m3m0s-memory.js"` to the `plugin` array of `opencode.json`.
3. Optional: register the MCP server read-only so the agent can search explicitly:

```json
"m3m0s": {
  "type": "local",
  "enabled": true,
  "command": ["sh", "-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec \"$HOME/go/bin/m3m0s\" mcp --read-only"]
}
```

Failures are silent and never block opencode; the log is `~/.m3m0s/hooks/claude-code.log`.
