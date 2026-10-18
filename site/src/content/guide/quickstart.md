---
title: Quickstart
description: Run the server with Docker Compose, create your account and store your first memory in about five minutes.
section: Start
order: 2
---

You need Docker with Compose v2.24 or later, and an OpenAI-compatible API key for embeddings. Without a key the server still runs, in keyword-only mode (full-text search and exact-match dedup).

## 1. Start the server

```sh
git clone https://github.com/rafaelvieiras/m3m0s.git
cd m3m0s
cp .env.example .env        # set OPENAI_API_KEY
docker compose up -d
curl http://localhost:19234/health
docker compose logs         # the first start prints a generated API key
```

> Save the API key from the log. It is shown once and is not printed again.

## 2. Create your account

Open `http://localhost:19234/`. A fresh instance runs a first-time setup: paste the API key from the log (it proves you control the instance) and create the first account with a username and a password. From then on you sign in with that account.

Accounts are for people using the WebUI. Hooks, MCP and the CLI keep using API keys.

## 3. Get the CLI

The CLI is the same binary as the server. Download an archive for Linux (`amd64`, `arm64` or `armv7`) from the [releases page](https://github.com/rafaelvieiras/m3m0s/releases), or build it:

```sh
go install github.com/rafaelvieiras/m3m0s/cmd/m3m0s@latest
```

Point it at the server:

```sh
export M3M0S_URL=http://localhost:19234
export M3M0S_API_KEY=<the key from the first start>

m3m0s add "We use SQLite with versioned migrations" --domain code:myproject
m3m0s search "which database do we use?" --domain code:myproject
```

## 4. Connect a tool

Pick the tool you use and follow its page: [Claude Code](/guide/claude-code/), [pi](/guide/pi/), [opencode](/guide/opencode/), [Hermes](/guide/hermes/) or [an MCP client](/guide/mcp/). They all read the same two settings, `M3M0S_URL` and `M3M0S_API_KEY`.

## Forgot the password?

`m3m0s user reset <username>` prints a single-use link, valid for 30 minutes, that opens a page to choose a new password. Run it with `M3M0S_URL` and `M3M0S_API_KEY` set, or inside the container. `m3m0s user add`, `list` and `delete` manage further accounts, and they all share the same memory.
