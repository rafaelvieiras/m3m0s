---
title: Introduction
description: What m3m0s is, how its pieces fit together, and where to start.
section: Start
order: 1
---

m3m0s is a self-hosted long-term memory for LLM agents. A single Go binary runs a server that stores what your agents learn and gives it back to any tool over **MCP**, **CLI** or **HTTP**. It also embeds a WebUI for browsing, editing and chatting with your memory.

## The pieces

| Piece | What it does |
|---|---|
| **Server** (`m3m0s serve`) | HTTP API and embedded WebUI in one process. SQLite storage, embeddings from any OpenAI-compatible endpoint. |
| **Hooks and plugins** | Run inside your agent tool. They capture session transcripts and recall relevant memories before each prompt. |
| **MCP bridge** (`m3m0s mcp`) | A stdio MCP server so an agent can search memory explicitly. A stateless bridge to a running server. |
| **CLI** (`m3m0s add`, `search`, `recent`, …) | One-shot commands that talk to the server. |
| **Watcher** (`m3m0s watch <dir>`) | Indexes `AGENTS.md`, `CLAUDE.md`, `MEMORY.md` and `docs/**/*.md` of a project and reindexes them on change. |

## The memory loop

1. **Capture.** At the end of a turn or session, the integration hands the transcript to `m3m0s hook claude-code`. Secrets are redacted, and if you configured an extraction model the text is distilled into short atomic facts.
2. **Store.** Facts are embedded, deduplicated by similarity and stored with a *domain* (for example `code:myproject`). A fact that contradicts an older one marks the older one as expired. A scheduled sweep fades stale facts and keeps a grace period before deleting.
3. **Recall.** On each prompt the integration searches your memory by the prompt text and injects the few facts that clear a relevance floor. See [Recall and tuning](/guide/recall/).

## Where to go next

- New here? Run the [Quickstart](/guide/quickstart/).
- Putting it on a server? Read [Deploy](/guide/deploy/) and [Configuration](/guide/configuration/).
- Connecting a tool: [Claude Code](/guide/claude-code/), [pi](/guide/pi/), [opencode](/guide/opencode/), [Hermes](/guide/hermes/) or [any MCP client](/guide/mcp/).

> m3m0s is early but in daily use by its author. The API is not frozen yet. Design decisions are recorded in the repository under `docs/decisions/`.
