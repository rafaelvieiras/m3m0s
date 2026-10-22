# D46 — pi capture reuses the Claude Code capture pipeline

**Status:** Accepted

**Context:** [D10](D10-pi-adapter-dropped.md) dropped a TypeScript adapter because pi speaks MCP, but MCP only covers recall: capture needs something that sees the session. pi has an extension API with `agent_end` and `session_shutdown` events and writes a JSONL session file.

**Decision:**
- No new capture code. A small pi extension (`integrations/pi/m3m0s-memory.ts`) runs the existing `m3m0s hook claude-code` with `transcript_path` set to pi's session file, `Stop` after each run and `SessionEnd` at shutdown (detached, so it outlives a quitting pi). Checkpoint, spool, redaction, digests and the durable queue ([D40](D40-durable-ingest-queue.md)) are the ones already used.
- `ParseLine` also accepts pi's `{"type":"message","message":{"role":...}}` lines, keeping only `user`/`assistant` text; pi's system prompt and injected custom messages are skipped, so recalled memories are not captured back.
- `M3M0S_HOOK_SOURCE` overrides the `source` of captured memories (`pi`); default stays `claude-code`.

**Verified:** a `pi -p` run wrote its checkpoint on shutdown, and a manual `SessionEnd` produced 3 extracted facts with `source=pi`, a `code:<project>` domain and the session's model recorded.

**Not done:** capture of pi sessions that end by a crash (only `Stop` gated by growth/text covers them), and a client contract document (to be written after the opencode and Hermes integrations).
