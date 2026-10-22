---
title: CLI and credentials
description: Install the m3m0s binary on the machine where your agents run, store the credentials once, and check that capture works.
section: Connect your tools
order: 5
---

Every integration reuses the `m3m0s` binary, so install it on each machine where you run an agent. It is the same binary as the server.

## Install the binary

Download the archive for your architecture from the [releases page](https://github.com/rafaelvieiras/m3m0s/releases) (Linux `amd64`, `arm64`, `armv7`) and put `m3m0s` on your `PATH`. Or build it with Go:

```sh
go install github.com/rafaelvieiras/m3m0s/cmd/m3m0s@latest
```

This installs to `~/go/bin/m3m0s`. Make sure that directory is on the `PATH` of the tool that launches the hooks.

## Store the credentials

Hooks and plugins read the server URL and the API key from the environment, and fall back to `~/.m3m0s/hook.env`. Create that file once and every integration picks it up:

```sh
mkdir -p ~/.m3m0s
cat > ~/.m3m0s/hook.env <<'ENV'
M3M0S_URL=https://memory.example.com
M3M0S_API_KEY=<your API key>
ENV
chmod 600 ~/.m3m0s/hook.env
```

An optional `M3M0S_HOOK_DOMAIN` line changes the domain captured memories are filed under. Without it the domain is derived from the working directory (`code:<folder name>`).

## Check it works

```sh
m3m0s doctor
```

`doctor` checks the credentials, the connection to the server, the Claude Code hook registration and the spool of captures that could not be sent. It exits with status 1 when something fails and prints how to fix it. `m3m0s doctor --replay` resends the pending spool.

Hooks themselves always exit 0, because they must never break the host agent. Their log is `~/.m3m0s/hooks/claude-code.log`.

## Everyday commands

```sh
m3m0s add "Deploys go through the staging branch first" --domain code:myproject
m3m0s search "how do we deploy?" --domain code:myproject
m3m0s search "retry policy" --kind rule
m3m0s recent --limit 10
m3m0s get <id>
m3m0s update <id> --kind gotcha
m3m0s stats
```

The full list of operations, for MCP, CLI and HTTP, is in `docs/api-contract.md` in the repository.

## Watch a project's docs

```sh
m3m0s watch ~/projects/myproject
```

The watcher indexes `AGENTS.md`, `CLAUDE.md`, `MEMORY.md` and `docs/**/*.md` and reindexes them when they change.
