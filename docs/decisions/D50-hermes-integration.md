# D50 — Hermes integration reuses the hook pipeline

**Status:** Accepted

**Context:** F3 ([#4](https://github.com/rafaelvieiras/m3m0s/issues/4)). Hermes agents (always-on, Telegram/email) have no working directory and run in a container, but expose plugin hooks that carry the user message (`pre_llm_call`) and the full turn (`post_llm_call`, `on_session_end`).

**Decision:**
- A Python plugin (`integrations/hermes/m3m0s-memory`, stdlib only) shells out to a static `m3m0s` binary kept in the plugin directory, exactly like [D46](D46-pi-capture.md)/[D49](D49-opencode-integration.md): recall through `--inject`, capture through the transcript path. No server change, no duplicated gate logic.
- `M3M0S_HOOK_DOMAIN` (new, validated with `schema.ValidateDomain`, falls back to `code:<cwd>`) pins the domain of captured memories, because a Hermes agent has no meaningful cwd. The plugin defaults it to `personal:hermes`.
- Configuration lives in the agent's own `.env`, and state in `<data>/m3m0s` on the persistent volume, so a container recreate keeps the checkpoints and the spool.

**Verified:** inside the running container, as the agent user, recall returned the compose-location memories, a generic prompt returned nothing, and a simulated turn plus session end produced a `source=hermes`, `personal:hermes` memory (then deleted). `hermes plugins list` shows the plugin enabled. Enabled on one agent only; the second agent (family-facing) is left without it on purpose.

**Not done:** per-user recall isolation (all platforms share one memory, as in the other clients).
