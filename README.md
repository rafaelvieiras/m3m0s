# M3m0s

A self-hosted, long-term semantic memory service for LLM agents: one small Go
binary that stores what your agents learn and serves it back to any tool over
**MCP**, **CLI** or **HTTP**, with a built-in web UI for browsing, editing and
chatting with your memory.

M3m0s exists because every agent tool keeps its own isolated memory — or none.
The result is context re-explained every session (wasted tokens), nothing that
travels between tools, and no way to use cheaper models because each session
has to rediscover everything from scratch. Memory systems that already tackle
this — Claude-Mem, Mem0, Cognee, Zep — each take a different trade-off: Claude-Mem
is tied to a single tool and a Node/Bun + ChromaDB stack; Mem0 and Cognee are
heavier to self-host (extra databases, optional graph stores); Zep's full
platform is cloud-only. M3m0s aims at the opposite corner: **one static binary
backed by a single SQLite file**, light enough for a small ARM board, speaking
the protocols every agent tool already supports, and keeping the memory
*shared* instead of per-tool. A feature-by-feature comparison is in
[`docs/market-benchmark.md`](docs/market-benchmark.md).

- **Server** (`m3m0s serve`): HTTP API + embedded WebUI in one process. SQLite
  storage (pure Go, no cgo) with versioned migrations; embeddings from any
  OpenAI-compatible endpoint; cosine search in Go.
- **MCP bridge** (`m3m0s mcp`): stdio MCP server for hosts such as Claude Code
  or OpenCode; a stateless bridge to a running `serve`.
- **CLI** (`m3m0s add|search|recent|get|update|stats|forget-sweep|restore`):
  one-shot commands, also clients of `serve`.
- **Watcher** (`m3m0s watch <dir>`): indexes `AGENTS.md`, `CLAUDE.md`,
  `MEMORY.md` and `docs/**/*.md` of a project and reindexes them when they
  change.
- **Hooks** (`m3m0s hook claude-code`): automatic capture and context
  injection for Claude Code sessions.

## Status

Early but functional: the core runs in daily use by its author. The API is not
yet frozen. Design decisions are recorded in
[`docs/decisions/`](docs/decisions/README.md) and the phased plan is in
[`docs/implementation-plan.md`](docs/implementation-plan.md).

Working today:

- **Storage and search**: SQLite store with versioned migrations; vector
  search over OpenAI-compatible embeddings (brute-force cosine in Go, filtered
  by domain); a 4-axis scope schema (`domain` / `source` / `agent` /
  `tags`+`metadata`), see [`docs/memory-schema.md`](docs/memory-schema.md).
- **Ingestion quality**: secret redaction on ingestion, optional LLM fact
  extraction (one input becomes several atomic facts, chunked and retried),
  deduplication by similarity, deduplicated storage of the original source text.
- **Temporal facts and light knowledge links**: `related_to` links by
  similarity, `valid_until` set when a new fact contradicts an old one
  (LLM-classified, safe fallback).
- **Layers and forgetting**: retention formula with reinforcement on access,
  soft-delete with a grace period, restore, pinning, and a scheduled sweep.
- **Interfaces**: MCP tools, CLI, authenticated HTTP API under `/api/v1`
  ([`docs/api-contract.md`](docs/api-contract.md)), async `add` with a pollable
  operation when extraction is slow.
- **WebUI** (embedded in the binary): search and browse by scope, stats and
  telemetry, add with extraction preview, edit/delete, an in-process jobs
  panel (history, run now, change interval), a context-injection preview, and a
  floating chat that answers from your memory and can save new memories.
- **Claude Code hooks**: `SessionStart` (inject recent memory),
  `PreCompact` and `SessionEnd` (capture the transcript delta).
- **Self-documentation**: the project's own docs are embedded in the binary and
  indexed into memory at boot.

Not done yet (planned direction): a tool-agnostic memory shared by many LLM
clients, with integrations beyond Claude Code and more hooks; more reliable
capture; project scopes layered over the current domains; hybrid search
(full-text + vector); and a self-correction loop for memories that turn out to
be wrong or stale. Open questions are tracked as issues.

## Quickstart (Docker)

```sh
cp .env.example .env        # set OPENAI_API_KEY (used for embeddings)
docker compose up -d
curl http://localhost:19234/health
docker compose logs          # first start prints a generated API key — save it,
                             # it is not shown again
```

Then open `http://localhost:19234/`, paste the API key on the login screen, and
start adding memories. Data lives in a named volume (`m3m0s-data`), so it
survives restarts.

Point the clients at the server:

```sh
export M3M0S_URL=http://localhost:19234
export M3M0S_API_KEY=<the key from the first start>

m3m0s add "We use SQLite with versioned migrations" --domain codigo:myproject
m3m0s search "which database do we use?" --domain codigo:myproject
```

Register the MCP bridge in your agent host as a stdio server with
`command: m3m0s`, `args: ["mcp"]` (and the two environment variables above). The
full operation list for MCP, CLI and HTTP is in
[`docs/api-contract.md`](docs/api-contract.md).

To run without Docker: `go build ./cmd/m3m0s` (build the WebUI first with
`npm run build` in `webui/` if you want it embedded), then `m3m0s serve`.

## Configuration

Set through environment variables (or the matching `m3m0s serve` flags).

| Variable | Default | Purpose |
|---|---|---|
| `OPENAI_API_KEY` | — (required) | Key for embeddings (`text-embedding-3-small`). |
| `M3M0S_OPENAI_BASE_URL` | OpenAI | Alternative OpenAI-compatible embeddings endpoint. |
| `M3M0S_PORT` | `19234` | HTTP port. |
| `M3M0S_DATA_DIR` | `~/.m3m0s/data` (`/data` in the Docker image) | Where the SQLite database lives. |
| `M3M0S_HOST_PORT` | `19234` | Host port published by `compose.yaml`. |
| `M3M0S_EXTRACT_API_KEY` | — (extraction off) | Enables LLM fact extraction. |
| `M3M0S_EXTRACT_BASE_URL` | `https://openrouter.ai/api/v1` | OpenAI-compatible endpoint for extraction. |
| `M3M0S_EXTRACT_MODEL` | `deepseek/deepseek-v4-flash` | Extraction model. |
| `M3M0S_CHAT_API_KEY` | falls back to the extraction key | Enables the WebUI chat. |
| `M3M0S_CHAT_BASE_URL` / `M3M0S_CHAT_MODEL` | OpenRouter / `deepseek/deepseek-v4-flash` | Chat provider and model. |
| `M3M0S_ADD_SYNC_TIMEOUT` | `300ms` | How long `add` waits before answering `202` and finishing in the background (`<= 0` disables). |
| `M3M0S_RETRO_EXTRACT_INTERVAL` | `2h` | Retroactive extraction job interval. |
| `M3M0S_FORGET_SWEEP_INTERVAL` | `12h` | Forgetting sweep interval. |
| `M3M0S_JOBS_TICK` | `30s` | Scheduler heartbeat. |
| `M3M0S_URL` | `http://localhost:19234` | Server URL used by the CLI, MCP bridge, watcher and hooks. |
| `M3M0S_API_KEY` | — | API key used by those clients. |

## Security note

M3m0s stores whatever you send it — including conversation transcripts — and
you configure it with provider API keys. Text is passed through secret
redaction before storage and before it is sent to an extraction provider, but
redaction is best-effort. Put the server behind TLS and a trusted network
boundary before exposing it, keep your `.env` out of version control, and read
[`SECURITY.md`](SECURITY.md) for details and how to report a vulnerability.

## Development

Build, test and run commands, the repository layout and the project's
conventions (English only, human-owned commits, no personal or infrastructure
data) are in [`AGENTS.md`](AGENTS.md); contribution workflow is in
[`CONTRIBUTING.md`](CONTRIBUTING.md). Quick check:

```sh
go vet ./... && go test ./...
```

## Name

M3m0s has no relation to [getengram.app](https://getengram.app/). The project
was called "Engram" in its first days and was renamed to avoid the collision
(see [`docs/decisions/`](docs/decisions/README.md), D5/D6).

## License

MIT — see [`LICENSE`](LICENSE).
