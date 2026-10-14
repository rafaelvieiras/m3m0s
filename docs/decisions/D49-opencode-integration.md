# D49 — opencode integration reuses the hook pipeline

**Status:** Accepted

**Context:** F3 ([#4](https://github.com/rafaelvieiras/m3m0s/issues/4)) asks for clients beyond Claude Code. [D46](D46-pi-capture.md) showed that a client only needs to hand its session text to `m3m0s hook claude-code` and to ask `--inject` for recall; opencode has a plugin API with the same two hook points.

**Decision:**
- `integrations/opencode/m3m0s-memory.js` is an opencode plugin. **Recall:** `chat.message` remembers the user text, `experimental.chat.system.transform` runs `m3m0s hook claude-code --inject` with it and appends the returned block to that turn's system prompt (once per user message). **Capture:** on `session.idle` it reads the session through the plugin client, writes it as JSONL (`message` lines, the same shape D46 parses) to `~/.m3m0s/hooks/opencode/<session>.jsonl` and runs the hook as `Stop`; `dispose` runs it as `SessionEnd`.
- Memories carry `source: "opencode"` (`M3M0S_HOOK_SOURCE`) and the session's model.
- The MCP server is registered read-only ([D45](D45-mcp-read-only-mode.md)) for explicit searches.
- No server change, no new Go code: checkpoint, spool, redaction, digests and the durable queue are shared with Claude Code and pi.

**Verified:** an `opencode run` wrote its transcript, produced a `source=opencode` memory with the model name, and created the per-session recall dedup file. The model answer itself could not be checked in that run (upstream `502` from the provider router, unrelated).

**Not done:** capturing sub-agent sessions separately, and a documented minimal client contract (to be written once Hermes and Open WebUI follow).
