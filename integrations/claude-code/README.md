# Claude Code plugin

Packages the m3m0s hooks and the read-only MCP server as a Claude Code plugin, so they no longer have to be copied into `settings.json` by hand:

- **Hooks:** `SessionStart`, `UserPromptSubmit` (capture plus per-prompt recall, [D44](../../docs/decisions/D44-per-prompt-memory-search.md)), `Stop`, `PreCompact`, `SessionEnd` and `PostToolUse`, all running `m3m0s hook claude-code`.
- **MCP:** `m3m0s mcp --read-only` (search tools only).

## Install

1. Install the `m3m0s` binary somewhere on `PATH` (release archives or `go install`), and create `~/.m3m0s/hook.env` with `M3M0S_URL` and `M3M0S_API_KEY`.
2. In Claude Code:

```
/plugin install m3m0s-memory --marketplace rafaelvieiras/m3m0s
```

or from the shell: `claude plugin marketplace add rafaelvieiras/m3m0s` then `claude plugin install m3m0s-memory@m3m0s`.

If you already configured these hooks in `settings.json`, remove them first to avoid running everything twice. Failures are silent and never block Claude Code; the log is `~/.m3m0s/hooks/claude-code.log`.
