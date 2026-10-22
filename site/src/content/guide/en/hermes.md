---
title: Hermes
description: A memory provider for Hermes agents that recalls before each turn, captures after it, and adds a read-only search tool.
section: Connect your tools
order: 9
---

The Hermes integration is a **memory provider**. It reuses the `m3m0s` binary, so the relevance gates and the secret redaction behave exactly as they do for other tools.

- **Recall** (`prefetch`): runs `m3m0s hook claude-code --inject` with the user message. Hermes wraps the result in its own `<memory-context>` fence.
- **Capture** (`sync_turn` as `Stop`, `on_session_end` as `SessionEnd`): appends each turn to a per-session transcript and hands it to `m3m0s hook claude-code`. Memories carry `source: "hermes"`, filed under `M3M0S_HOOK_DOMAIN` (default `personal:hermes`).
- **Tool** `m3m0s_search` (read-only): `query`, `limit`, `domain`, or `recent` to list the newest memories of a domain.

Cron and webhook turns are skipped. Failures are silent and never block the agent.

## Install

Hermes allows one external memory provider.

### With the Hermes CLI

The binary must be on `PATH`, or at `<plugin dir>/bin/m3m0s`:

```sh
hermes plugins install rafaelvieiras/m3m0s/integrations/hermes/m3m0s-memory
hermes memory setup   # pick m3m0s-memory
```

### Manual install

The steps below assume the agent runs in a container with a data volume mounted at `<data>`.

1. Build a static binary for the agent's platform:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o m3m0s ./cmd/m3m0s
```

2. Copy the plugin and the binary into the data volume, owned by the agent user:

```sh
cp -r integrations/hermes/m3m0s-memory <data>/plugins/m3m0s-memory
install -m 755 m3m0s <data>/plugins/m3m0s-memory/bin/m3m0s
```

3. Add the credentials to `<data>/.env`:

```sh
M3M0S_URL=https://memory.example.com
M3M0S_API_KEY=<your API key>
M3M0S_HOOK_DOMAIN=personal:hermes   # optional
```

4. Select the provider in `<data>/config.yaml` and restart the agent:

```yaml
memory:
  provider: m3m0s-memory
```

`hermes memory status` should show it as active. State (checkpoints, spool, transcripts) lives in `<data>/m3m0s/`.
