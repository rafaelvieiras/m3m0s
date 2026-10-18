# Hermes integration

A [Hermes](https://github.com/NousResearch/hermes-agent) **memory provider** that gives an agent long-term memory from m3m0s by reusing the `m3m0s` binary ([D50](../../docs/decisions/D50-hermes-integration.md)):

- **Recall** (`prefetch`): runs `m3m0s hook claude-code --inject` with the user message; Hermes wraps the result in its own `<memory-context>` fence. Misspelled terms are repaired server-side ([D51](../../docs/decisions/D51-query-term-correction.md)).
- **Capture** (`sync_turn` as `Stop`, `on_session_end` as `SessionEnd`): appends each turn to a per-session transcript and hands it to `m3m0s hook claude-code`. Memories carry `source: "hermes"` and the domain from `M3M0S_HOOK_DOMAIN` (default `personal:hermes`).
- **Tool** `m3m0s_search` (read-only): `query`, `limit`, `domain`, or `recent` (newest memories of a domain; a query always takes precedence). Wraps `m3m0s search`/`recent`.

## Install

With the Hermes CLI (the binary must be on `PATH`, or at `<plugin dir>/bin/m3m0s`):

```sh
hermes plugins install rafaelvieiras/m3m0s/integrations/hermes/m3m0s-memory
hermes memory setup   # pick m3m0s-memory
```

Manual install, inside the agent's data volume:

1. Build a static binary: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o m3m0s ./cmd/m3m0s`.
2. Copy `m3m0s-memory/` to `<data>/plugins/m3m0s-memory/` and the binary to `<data>/plugins/m3m0s-memory/bin/m3m0s` (owned by the agent user, mode 755).
3. Add to `<data>/.env`: `M3M0S_URL`, `M3M0S_API_KEY` and optionally `M3M0S_HOOK_DOMAIN`.
4. Set `memory.provider: m3m0s-memory` in `<data>/config.yaml` (Hermes allows one external provider), then restart the agent. `hermes memory status` should show it as active.

State (checkpoints, spool, transcripts) lives in `<data>/m3m0s/`. Failures are silent and never block the agent. Cron and webhook turns are skipped.
