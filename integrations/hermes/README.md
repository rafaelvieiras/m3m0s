# Hermes integration

A [Hermes](https://github.com/NousResearch/hermes-agent) plugin that gives an agent long-term memory from m3m0s by reusing the `m3m0s` binary ([D50](../../docs/decisions/D50-hermes-integration.md)):

- **Recall** (`pre_llm_call`): runs `m3m0s hook claude-code --inject` with the user message and returns the relevant memories as `{"context": ...}`.
- **Capture** (`post_llm_call` as `Stop`, `on_session_end` as `SessionEnd`): appends each turn to a per-session transcript and hands it to `m3m0s hook claude-code`. Memories carry `source: "hermes"` and the domain from `M3M0S_HOOK_DOMAIN` (default `personal:hermes`).

## Install (inside the agent's data volume)

1. Build a static binary: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o m3m0s ./cmd/m3m0s`.
2. Copy `m3m0s-memory/` to `<data>/plugins/m3m0s-memory/` and the binary to `<data>/plugins/m3m0s-memory/bin/m3m0s` (owned by the agent user, mode 755).
3. Add to `<data>/.env`: `M3M0S_URL`, `M3M0S_API_KEY` and optionally `M3M0S_HOOK_DOMAIN`.
4. Add `m3m0s-memory` to `plugins.enabled` in `<data>/config.yaml`, then restart the agent. `hermes plugins list` should show it as enabled.

State (checkpoints, spool, transcripts) lives in `<data>/m3m0s/`. Failures are silent and never block the agent. Cron and webhook turns are skipped.
