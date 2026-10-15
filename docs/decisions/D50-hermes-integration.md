# D50 — Hermes integration reuses the hook pipeline

**Status:** Accepted

**Context:** F3 ([#4](https://github.com/rafaelvieiras/m3m0s/issues/4)). Hermes agents (always-on, Telegram/email) have no working directory and run in a container, but expose a `MemoryProvider` interface (`prefetch`, `sync_turn`, `on_session_end`, tool schemas).

**Decision:**
- A Python `MemoryProvider` (`integrations/hermes/m3m0s-memory`, stdlib only; selected with `memory.provider`) shells out to a static `m3m0s` binary kept in the plugin directory, exactly like [D46](D46-pi-capture.md)/[D49](D49-opencode-integration.md): recall through `--inject` in `prefetch` (Hermes fences it as `<memory-context>`), capture through the transcript path, plus a read-only `m3m0s_search` tool. An earlier `pre_llm_call` hook plugin appended context unfenced and was replaced. No server change, no duplicated gate logic.
- `M3M0S_HOOK_DOMAIN` (new, validated with `schema.ValidateDomain`, falls back to `code:<cwd>`) pins the domain of captured memories, because a Hermes agent has no meaningful cwd. The plugin defaults it to `personal:hermes`.
- Configuration lives in the agent's own `.env`, and state in `<data>/m3m0s` on the persistent volume, so a container recreate keeps the checkpoints and the spool.

**Verified:** (hook version, then provider)  inside the running container, as the agent user, recall returned the compose-location memories, a generic prompt returned nothing, and a simulated turn plus session end produced a `source=hermes`, `personal:hermes` memory (then deleted). `hermes plugins list` shows the plugin enabled. Enabled on one agent only; the second agent (family-facing) is left without it on purpose.

**Not done:** per-user recall isolation (all platforms share one memory, as in the other clients).

**Tool guidance:** the tool description tells the model that injected memories already answer most questions, and `recent` ignores nothing silently: a query switches to a normal search.
