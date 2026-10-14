# D45 — Read-only MCP mode

**Status:** Accepted

**Context:** with automatic capture ([D14](D14-claude-code-hook-auto-capture.md)) and per-prompt injection ([D44](D44-per-prompt-memory-search.md)), agents no longer need to write or maintain memory through MCP. The MCP server instructions already say not to call `memory_add` for routine progress, but a tool that exists will eventually be called (a wrong `memory_update`, a `memory_forget_sweep`, a `memory_restore`).

**Decision:** `m3m0s mcp --read-only` (or `M3M0S_MCP_READ_ONLY=1`) hides `memory_add`, `memory_update`, `memory_forget_sweep` and `memory_restore` from `tools/list` and rejects them in `tools/call` with an error result. `memory_search`, `memory_get`, `memory_recent` and `memory_stats` stay. Default is unchanged (all tools), so existing registrations keep working; clients opt in.

**Not done:** a server-side read-only API key scope. This is a client-side convenience, not a security boundary: anything holding the API key can still call the HTTP API.
