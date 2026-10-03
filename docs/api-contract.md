# API contract — MCP, CLI and HTTP

Defines the surface of the M3m0s core: how the same internal operations
(`add`, `search`, `recent`, `stats`) are exposed over HTTP, MCP (stdio) and the
CLI, using the schema of **D9** (`docs/memory-schema.md`). The starting point
was the endpoints of the earlier standalone memory server this project
generalized (`POST /memories`, `POST /search`, `GET /recent`, `GET /stats`,
`GET /health`) — reused and extended, not rewritten (D1/D2). Extended in later
phases: Phase 2 (D11) adds `memory_delete`; Phase 5 (D13) adds `memory_update`,
the `extraction` field of `memory_stats`, and (a later elaboration, closing a
gap the WebUI exposed in practice) `memory_get`, which makes the admin WebUI
viable (`docs/implementation-plan.md`).

> Note on domain names: the domain *categories* used in the examples below
> (`codigo`, `escrita`, `pessoal`) are stored-data and API-contract strings
> and are intentionally kept as-is for compatibility with existing data.

## Topology (elaboration of D3)

D3 talked about "at least two modes" (`serve`/`watch`). Fitting MCP into that
design requires a third: MCP over stdio is, by the nature of the protocol, one
process per host session (Claude Code, OpenCode and similar hosts each spawn
their own instance) — it cannot be the same process that owns the central
storage (D3 requires a single storage service, otherwise memory fragments per
machine again). Therefore:

- **`m3m0s serve`** — central daemon (D3 "core"): the only one that talks to
  SQLite directly (D28, `docs/decisions/`). HTTP.
- **`m3m0s watch <dir>`** — local daemon (D3 "watch"): fsnotify, HTTP client of
  `serve`.
- **`m3m0s mcp`** — stdio↔HTTP bridge, no state of its own: translates MCP
  calls into HTTP requests against `serve`. It is the piece D10 assumed to
  exist: what an MCP client configuration registers is this bridge
  (`command: m3m0s, args: [mcp]`), not `serve` directly.
- **`m3m0s add|search|recent|stats`** — one-shot CLI commands, also HTTP
  clients of `serve`.

Every "client" mode (`watch`/`mcp`/CLI verbs) uses the same config:
`M3M0S_URL` (default `http://localhost:19234`) and `M3M0S_API_KEY`.

## `/api/v1` prefix and embedded WebUI (later elaboration, 16 Jul 2026)

Every API operation (the "Operations" section below) lives under `/api/v1/*`
— except `GET /health`, which stays at the root on purpose (infrastructure
probes expect a fixed path with no embedded version). `M3M0S_URL` still points
only at host:port (e.g. `http://localhost:19234`); each client
(`internal/client`, `webui/src/lib/api.ts`) prepends `/api/v1/...` itself, it
is not part of the `M3M0S_URL` value.

`m3m0s serve` also serves the WebUI (Phase 5, D13) from the same
process/port: `webui/dist` (produced by `npm run build`; the Node pipeline stays
separate — D2) is embedded in the binary via `go:embed` (package `webui`,
`webui/embed.go`) and mounted at `/` with an SPA fallback (any non-file path
outside `/api/v1` returns `index.html`, so TanStack Router routes work on
refresh/direct links). Practical consequence: running the WebUI in production
is just `m3m0s serve` — no extra Node/nginx process (aligned with D4, small
self-hosted deployments). In development, `npm run dev` (Vite) still exists
for hot reload, proxying `/api/v1` to an already running `m3m0s serve` — which
is why the WebUI client uses a relative path (`/api/v1/...`) and does not
depend on `VITE_M3M0S_URL` in the common case.

## Auth

- **Direct HTTP:** `Authorization: Bearer <api_key>` header required on every
  route except `/health`. No valid key → `401`. (A SHA-256 hash is stored,
  never the raw key — already planned in Phase 1.)
- **MCP (stdio) and CLI:** no auth of their own — they run as a local process
  of the user (same trust boundary as whoever invoked them), and use the
  configured `M3M0S_API_KEY` to authenticate against `serve` under the hood.
  This only becomes a concern if `M3M0S_URL` ever points outside the local
  network without TLS — out of scope while usage stays on a trusted network.

## Common format: `MemoryEntry`

```json
{
  "id": "mem_8f2a1c9d",
  "content": "plain text, already passed through secret redaction",
  "domain": "codigo:m3m0s",
  "domainCategory": "codigo",
  "source": "claude-code",
  "agent": null,
  "tags": ["chapter-3"],
  "metadata": { "conversationId": "conv_8f2a" },
  "createdAt": "2026-07-15T19:00:00Z",
  "updatedAt": "2026-07-15T19:00:00Z"
}
```

This is the public JSON shape (HTTP and the CLI's `--json`). Internally it maps
directly to the columns of the SQLite `entries` table (D28,
`docs/decisions/`) as described in `docs/memory-schema.md` — API callers do
not need to know that.

## Context block format (search)

Markdown ready to inject into a prompt — the `contextBlock` field in every
`search`, and the main text returned by the MCP tool:

```
## Relevant memories

1. [0.87] codigo:m3m0s (claude-code, 2026-07-15)
   Snippet of the content here, truncated at N characters...

2. [0.79] escrita:ai-in-practice-book (openwebui, 2026-07-10)
   Another snippet...

3. [0.73] codigo:m3m0s (watch, 2026-07-16) — AGENTS.md § Decisions already made
   Snippet of a chunk generated by m3m0s watch...
```

The third entry's suffix (` — <file_path> § <heading>`) only appears when the
memory has `metadata.file_path` (every chunk generated by `m3m0s watch`, see
the Watch section below); `§ <heading>` only appears if `metadata.heading` is
also present (a chunk corresponding to a section, not a whole file).

## Operations

### `memory_add` · `POST /api/v1/memories` · `m3m0s add`

Input (MCP tool schema / HTTP body):
```json
{
  "content": "string, required",
  "domain": "string, required, ^[a-z0-9_]+:[a-z0-9._-]+$",
  "source": "string, default \"manual\"",
  "agent": "string, optional",
  "tags": ["string", "..."],
  "metadata": { "free": "..." }
}
```

CLI: `m3m0s add "<content>" --domain codigo:m3m0s [--source manual] [--agent my-agent] [--tag foo --tag bar] [--meta key=value]`

Server pipeline (Phase 1 + Phase 4 extraction/D12, in this order): **1)**
secret redaction on `content` (D6); **2)** if extraction is enabled on the
server (`M3M0S_EXTRACT_API_KEY` set — D12, item 1), call the extractor
(`internal/extract`) on the already redacted content and get back a list of
atomic facts (if extraction fails or is not enabled, the list is `[content]`,
i.e. the original content becomes the only "fact" — identical to pre-Phase-4
behavior); **3)** for each fact in the list: redact secrets again (defense in
depth — D12, item 4) → dedup check (high-similarity search within the same
`domain`) → duplicate found → update the existing entry (`deduped: true`,
existing `id`) → otherwise insert a new one (`mem_<nanoid>`, or
`chunk_<nanoid>` if `idPrefix` was passed), `deduped: false`.

Response — **two possible paths since D26 (`docs/decisions/`, async Add)**,
depending on how long the fact-extraction pipeline takes relative to
`-add-sync-timeout`/`M3M0S_ADD_SYNC_TIMEOUT` (default `300ms`; `<= 0` disables
the async path and always waits for Add to really finish, the pre-D26
behavior):

**Sync path (200)** — finished within the wait timeout. The shape changed in
Phase 4 (D12, item 2, breaking, no compat shim) from singular to plural,
because 1 `add` can produce N entries when extraction is enabled:
```json
{
  "entries": [
    { "entry": { "...MemoryEntry..." }, "deduped": false },
    { "entry": { "...MemoryEntry..." }, "deduped": true }
  ],
  "extracted": true,
  "status": "completed"
}
```
`extracted` tells whether the fact-extraction pipeline ran (`true`) or fell
back to the raw content (`false` — extraction disabled on the server, or it
failed and hit the fallback of D12 item 5). When `extracted: false`, `entries`
always has exactly 1 item. `status` is additive (D26, `omitempty`) — always
`"completed"` in this shape, whether as the direct 200 response or as the
`result` field of a successful poll (see the next section); any pre-D26 client
that already decodes this JSON ignoring unknown fields needs no change.

**Async path (202 Accepted)** — did not finish within the wait timeout. The
pipeline keeps running in the background; the final result becomes available
via polling (the `GET /memories/pending/:id` section below):
```json
{
  "operationId": "op_ab12cd34ef56",
  "status": "pending"
}
```
`status` is always `"pending"` in this response — the final values
(`"completed"`/`"failed"`) only appear in the poll response. The Go client
(`internal/client.Client.Add`) does this polling transparently — the CLI,
`m3m0s watch`, `m3m0s mcp` and `internal/selfindex` call `Add` as usual and
only receive the final result, without needing to know which of the two paths
was used.

### `GET /memories/pending/:id` (D26, no dedicated MCP/CLI equivalent)

Queries the result of a `memory_add` operation that degraded to the async path
(the 202 above). It has no MCP tool or CLI subcommand of its own — it is an
internal implementation detail of the polling in `internal/client.Client.Add`;
using the HTTP API directly outside the Go client also works if manual polling
is needed.

Response (always 200 when the ID exists, regardless of status):
```json
{
  "operationId": "op_ab12cd34ef56",
  "status": "completed",
  "result": { "entries": [ "...same as the memory_add 200..." ], "extracted": true, "status": "completed" }
}
```
- `status: "pending"` — still running; `result`/`error` absent.
- `status: "completed"` — `result` present (same shape as the sync 200 of
  `memory_add`), `error` absent.
- `status: "failed"` — `error` present with the failure message, `result`
  absent.

`404 not_found` when the `id` never existed or is past the result retention
TTL (15 minutes after the operation leaves `pending` — `pendingAddTTL`,
`internal/httpapi/pendingadd.go`); tracking is in-memory only and does not
survive a restart of `m3m0s serve` (D26).

### `memory_search` · `POST /api/v1/search` · `m3m0s search`

Input: `{ "query": string, "limit"?: number = 5, "domain"?: string, "domainCategory"?: string, "source"?: string, "agent"?: string, "tags"?: string[] }`

`domain` restricts the candidate set before the cosine computation (D28);
`domainCategory`/`source`/`agent`/`tags` are a Go post-filter over the already
scored candidates (exact match, implicit AND — see `memory-schema.md`).

Response:
```json
{
  "results": [ { "...MemoryEntry...": "...", "score": 0.87 } ],
  "contextBlock": "## Relevant memories\n\n1. ..."
}
```
The MCP tool returns `contextBlock` as the main text of the result (reducing
the tokens the calling LLM would spend parsing raw JSON — the original goal of
the Phase 1 "formatted context block" item) and embeds `results` as auxiliary
structured content for callers that need the raw data.

CLI: `m3m0s search "<query>" [--limit 5] [--domain codigo:m3m0s] [--json]` —
without `--json` it prints the context block; with it, it prints raw `results`.

### `memory_recent` · `GET /api/v1/recent` · `m3m0s recent`

Input: `limit` (default 10), `domain`, `domainCategory`, `source`, `agent` —
the same exact filters as `search`, without a similarity `query`; ordered by
`createdAt` descending.

Response: `{ "results": [ "...MemoryEntry..." ] }` (no `score`).

### `memory_stats` · `GET /api/v1/stats` · `m3m0s stats`

No input. Response:
```json
{
  "count": 1234,
  "countByDomainCategory": { "codigo": 800, "escrita": 200, "pessoal": 234 },
  "backend": "sqlite + OpenAI embeddings",
  "model": "text-embedding-3-small",
  "extraction": { "enabled": true, "model": "deepseek/deepseek-v4-flash" },
  "telemetry": {
    "apiKeyCount": 3,
    "memoryUsageMB": 42.7,
    "databaseSizeMB": 118.3,
    "uptimeSeconds": 86412,
    "extractionTokensUsed": 154302,
    "tokensSavedByCompaction": 987654
  }
}
```
`countByDomainCategory` is new relative to the original `/stats` — a direct
benefit of the `domainCategory` field derived in D9. `extraction` is new in
Phase 5 (D13) — it mirrors whether the fact extractor (Phase 4/D12) is on in
the server (`enabled: true` when `M3M0S_EXTRACT_API_KEY`/`-extract-key` is set,
the same condition that turns extraction on in `memory_add`/`watch`, see the
"Fact extraction" section below) and, if on, which `M3M0S_EXTRACT_MODEL` is
configured. `enabled: false` → `model: null` (never an empty string — the
consumer should not have to check both cases to know it is off). It is the
data the WebUI uses for its stats panel to show "fact extraction: on, model X"
or "off" (D13).

**Implementation note:** today the `enabled`/`M3M0S_EXTRACT_MODEL` pair only
exists inside `cmd/m3m0s/serve.go`, at the moment the extractor is built and
passed to the `MemoryStore` via `SetExtractor` —
`internal/httpapi.Server`/`apitypes.StatsResponse` had no way of knowing
whether an extractor is configured nor which model. Phase 5 needed to expose
those two values up to `handleStats` (e.g. a new field in `httpapi.Server` set
in `httpapi.New`, or a `MemoryStore.ExtractionInfo()` method if keeping the
config close to the extractor rather than the HTTP server is preferable).

`telemetry` (a later elaboration after Phase 5) is operational metrics of the
service itself — process/disk footprint and the token savings that fact
extraction provides — complementing `count`/`countByDomainCategory`, which
describe the content of the memories, not the service that stores them:

- `apiKeyCount`: how many API keys are registered (`auth.Store.Count()`).
- `memoryUsageMB`: memory of the Go process (`runtime.MemStats.Sys` — total
  obtained from the OS, not just live heap).
- `databaseSizeMB`: sum of the files in `dataDir` (the SQLite database —
  `m3m0s.db` + `m3m0s.db-wal`/`-shm`, D28 — and any legacy file not yet
  manually removed after the migration to SQLite).
- `uptimeSeconds`: how long this `m3m0s serve` process has been up (counted
  from `httpapi.New`, resets with the process).
- `extractionTokensUsed`: tokens (`prompt_tokens + completion_tokens`) reported
  by the extraction provider (the `usage` field of the chat completions
  response, OpenAI-compatible standard) across all `Extract`/`Classify` calls
  since the process started — `0` when extraction is off. A process counter,
  not persisted (resets together with `uptimeSeconds`). It does not include
  tokens spent on embeddings: the embeddings API does not expose that data to
  the caller (`internal/embed`).
- `tokensSavedByCompaction`: accumulated estimate (a heuristic of ~4
  characters per token, **not** exact tokenization) of how much fact
  extraction shrank the stored content — for each `memory_add` that runs fact
  extraction, it adds `tokens(original content) - tokens(extracted facts)` when
  positive. Unlike `extractionTokensUsed`, this value is persisted (it
  survives a restart) and is the number that best represents the project's
  underlying goal (AGENTS.md: "reduce tokens spent rediscovering context").

### `memory_get` · `GET /api/v1/memories/:id` · `m3m0s get`

Added in an elaboration after Phase 5 (D13): the WebUI (D13 item 1) had no way
to open the detail/edit screen of an entry "cold" (direct link, F5, bookmark)
— it could only show an entry that had already appeared in a `search`/`recent`/
`add` list in the same browser session (TanStack Query cache). `memory_get`
closes that gap: it fetches 1 memory by `id`, with no side effects (it does not
count as an access/touch, and does not change `updatedAt`) — *as originally
specified; since D22 it does count as access reinforcement, see "Memory layers
and automatic forgetting" below.*

**Scope decision — first-class on all 3 surfaces (HTTP + MCP + CLI), not just
HTTP:** unlike `memory_delete` (D11, which stayed deliberately HTTP-only
because it is minimal internal scope just so `watch` can do cascade purges,
with no public UI/tool), `memory_get` is a generic read operation in the same
spirit as `memory_add`/`search`/`recent`/`stats`/`update` — all five of which
are already first-class on the 3 surfaces. There is no concrete reason to
treat "read 1 memory by id" as a special case: it is the same pattern as
`memory_update` (loads the entry from the index, returns the full
`MemoryEntry`), just without the write part. It gets an MCP tool
(`memory_get`) and a CLI verb (`m3m0s get <id>`) for the same reason the other
five did: any MCP host or terminal user may want to inspect 1 specific memory
without going through a search.

Input: only the `id` — in HTTP it comes from the URL, in MCP it is the only
property of the input schema, in the CLI it is the positional argument.

No request body.

CLI: `m3m0s get <id> [--json]` — without `--json` it prints a readable summary
(same format as `m3m0s update`: `id`/`domain`/`updatedAt` on one line, content
truncated below); with `--json` it prints the raw `MemoryEntry`.

MCP tool: `memory_get` — input schema `{ "id": { "type": "string" } }`,
`required: ["id"]`, following the same pattern as the existing `toolDefs` in
`internal/mcpserver/mcp.go`.

Server pipeline: loads the existing entry by `id` from its own index (the same
mechanism `memory_update`/`memory_delete` already use to resolve an id — no new
search logic) — not found → `404`. It does not recompute the embedding nor
update `updatedAt`.

Response (200): the full `MemoryEntry` (same shape as `memory_update`/
`memory_add`/`memory_search`, a single object — never wrapped in `entries[]`,
since `memory_get` always acts on exactly 1 entry).

Errors: `404 not_found` (id does not exist — the same code already used by
`DELETE /api/v1/memories/:id` and `PATCH /api/v1/memories/:id`).

### `memory_update` · `PATCH /api/v1/memories/:id` · `m3m0s update`

Added in Phase 5 (D13, item 1): there was no way to edit an already stored
memory — only insert (`add`, with automatic dedup), query
(`search`/`recent`/`stats`) or delete (`memory_delete`, minimal scope).
`memory_update` closes that gap as a **first-class operation on all 3
surfaces** (HTTP + MCP + CLI) — unlike `memory_delete`, which remains
deliberately HTTP-only (minimal scope for `watch`'s cascade purge, D11, with no
MCP tool or CLI verb). Editing is **partial**: only the fields sent in the body
change, the rest of the entry stays the same.

**Editable fields:** `content`, `domain`, `source`, `agent`, `tags`,
`metadata`. `id` and `createdAt` never change; `updatedAt` always changes to
the update timestamp, even if no value actually changed (the server does not
diff field by field to decide whether anything "really changed").

**"Field sent" vs. "field omitted" semantics (important for implementers):**
the body is interpreted by **key presence**, not by zero value — a `PATCH`
without the `"tags"` key keeps the current tags, but `"tags": []` **clears**
all tags (likewise `"metadata": {}` clears all metadata, and `"agent": null`
removes the agent). This cannot be expressed by decoding straight into plain
pointers with `encoding/json` (`*string`/`*[]string` cannot tell "key absent"
from "key present with value `null`") — the HTTP implementation must first
decode into `map[string]json.RawMessage` (or equivalent) to check which keys
came in the body, and only then apply each present field onto a copy of the
existing entry.

Input (HTTP body / MCP tool schema — all fields optional except `id`, which in
HTTP comes from the URL and in MCP/CLI is its own argument/property):
```json
{
  "content": "string, optional",
  "domain": "string, optional, same pattern as memory_add",
  "source": "string, optional",
  "agent": "string or null, optional",
  "tags": ["string", "..."],
  "metadata": { "free": "..." }
}
```
`tags` and `metadata`, when sent, **replace the whole value** (no key-by-key/
item-by-item merge) — the same "whole field" granularity as the rest of the
PATCH, just applied inside a field that is internally a collection. Direct
consequence for whoever implements the WebUI: to edit 1 key of `metadata`
without losing the others (e.g. changing a tag while keeping
`metadata.source_content` — see the "Fact extraction" section below — present),
the client must send the **complete, already merged** `metadata` object, not
just the diff.

MCP tool: `memory_update` — input schema = the same JSON body above plus `id`
(string, required), following the same pattern as the existing `toolDefs` in
`internal/mcpserver/mcp.go`.

CLI: `m3m0s update <id> [--content "..."] [--domain codigo:m3m0s] [--source manual] [--agent my-agent] [--tag foo --tag bar] [--meta key=value] [--clear-tags] [--clear-metadata] [--clear-agent]`

The CLI uses the stdlib `flag` package, which cannot natively tell "flag not
passed" from "flag passed with an empty value" — so the fields that can be
cleared (`tags`, `metadata`, `agent`) get a dedicated boolean clear flag
(`--clear-tags`/`--clear-metadata`/`--clear-agent`, mutually exclusive with
`--tag`/`--meta`/`--agent` in the same command), instead of trying to express
"empty" via `--tag` with no argument. For `content`/`domain`/`source` (never
"clearable" — only replaceable, always needing a non-empty value if sent), the
implementation must use `fs.Visit` (or equivalent) to know whether the flag was
actually passed before including the corresponding key in the PATCH body —
looking only at the value (`""` as default) cannot tell "leave alone" from
"sent empty by mistake" (the latter is invalid anyway, see Errors below).

Server pipeline:

**1)** loads the existing entry by `id` — not found → `404`.

**2)** decodes the body, identifies the present fields — no recognized field
present → `400`.

**3)** if `content` is present: it must be non-empty (same rule as
`memory_add`) → goes through secret redaction again (`internal/redact`, D6,
same discipline as `add`) → the redacted `content` replaces the entry's.
**Re-embedding is mandatory:** since the text changed, the stored vector
embedding becomes stale (semantic search would compare against a vector that no
longer represents the current text) — the server must recompute the embedding
and rewrite the whole row (`sqlstore.UpsertEntry`, D28, the same upsert
mechanism `Add`/dedup already uses), not just overwrite the metadata. **No fact
extraction (D12) runs here, even with `M3M0S_EXTRACT_API_KEY` enabled on the
server** — unlike `memory_add`, `memory_update` is a manual, deliberate
overwrite of ONE specific entry (the user has already decided the final text),
not a raw input that should turn into N new facts; running extraction here
would generate extra unrequested entries and diverge from the `id` the caller
asked to edit. For the same reason, **`memory_update` also does not run dedup**
(`DedupSimilarityThreshold`) — it makes no sense to merge the entry being
edited with another "similar" entry just because the new text came to
resemble it; the PATCH's target `id` is always the one updated, never another.

**4)** if `domain` is present: validates against
`internal/schema.ValidateDomain` (same regex as `add`) → invalid → `400`;
valid → replaces `domain` **and** recomputes `domainCategory`
(`schema.DomainCategory(domain)`) — it is a derived field (D9/
`memory-schema.md`) and cannot go out of sync with the new `domain`.

**5)** `source`/`agent`/`tags`/`metadata` present: replace the corresponding
value on the entry (see the whole-replacement semantics above), with no
additional validation beyond "if `source` was sent, it cannot be an empty
string" (same spirit as `content`).

**6)** `updatedAt = now`; `createdAt` preserved from the original value.

**7)** writes: `sqlstore.UpsertEntry` always re-embeds (there is no content
diff to decide whether to recompute; even an update that only changed `tags`
ends up re-embedding. Acceptable cost at personal/small-host scale, but worth
recording as a possible future optimization: only re-embed when `content`
actually changed) and updates the in-memory index (`internal/store`), in the
same critical section/consistency discipline that `Add`/`Delete` already
follow.

Response (200): the **complete** updated `MemoryEntry` (same format as
`memory_add`/`memory_search`, a single object — not wrapped in `entries[]`,
because `memory_update` always acts on exactly 1 entry, never generating N like
`add` with extraction enabled).

Errors: `404 not_found` (id does not exist — the same code already used by
`DELETE /api/v1/memories/:id`); `400 no_fields_to_update` (body with no
recognized field); `400 invalid_domain` (new domain outside the pattern, same
code as `add`); `400 content_required` (content present but empty, same code as
`add`); `400 source_required` (source present but empty).

### `GET /health` (HTTP only — no MCP/CLI equivalent, outside the `/api/v1` prefix)

`{ "status": "ok" }`, no auth. Use: container/orchestrator healthcheck. It
stays outside `/api/v1` on purpose — infrastructure/orchestrator probes expect
a fixed path with no embedded API version (the "Architecture correction"
elaboration in D13, `docs/decisions/`).

### `memory_delete` · `DELETE /api/v1/memories/:id` · `DELETE /api/v1/memories?domain=X&filePath=Y`

Added in Phase 2 (D11), with minimal scope: just enough for `m3m0s watch`'s
cascade purge — it is not an arbitrary manual-delete tool exposed like the
other 4 (hence no `m3m0s delete` CLI verb nor `memory_delete` MCP tool — the
caller is `watch` itself, via direct HTTP against `serve`, authenticated with
the same `M3M0S_API_KEY`).

Two forms:
- **By ID:** `DELETE /api/v1/memories/:id` → removes one specific memory.
  `204` if removed, `404` if the ID does not exist.
- **By domain + file_path:** `DELETE /api/v1/memories?domain=<domain>&filePath=<path>`
  → removes **all** chunks whose `metadata.file_path` exactly matches
  `filePath` within that `domain` (the real use case: a file was reindexed —
  delete the old chunks before inserting the new ones — or a file was deleted
  — delete everything that came from it). Response:
  `{ "deletedCount": N }`, `200` even if `N == 0`.

Server pipeline: removes the row from SQLite (`sqlstore.DeleteEntry`/
`DeleteEntries`, D28) and from the in-memory index (`internal/store`) in the
same critical section — the same consistency discipline `Add` already follows.

## Watch (Phase 2, D11)

How a file watched by `m3m0s watch <dir>` becomes a searchable memory:

- **`domain`:** inferred as `codigo:<basename(dir)>` from the watched root,
  overridable via `m3m0s watch <dir> --domain <category:instance>`. `source` is
  always `"watch"`, `agent` is absent.
- **Chunking:** each file is split by level-1 or level-2 Markdown header
  (`#`/`##`) — 1 section = 1 chunk = 1 `memory_add`. A file without headers (or
  too short to have its own section) becomes 1 single chunk with the whole
  content. IDs of these chunks use the `chunk_` prefix instead of `mem_`
  (reserved since D6/`docs/memory-schema.md`, unused until Phase 2).
- **Provenance:** every chunk carries `metadata.file_path` (path relative to
  the watched root, e.g. `"docs/decisions/README.md"`) and, when applicable,
  `metadata.heading` (the section header text). This is what shows up in the
  context block (see the section above).
- **Hash diff:** `watch` keeps its own local state (outside `serve` — it is not
  the core's responsibility), mapping
  `file_path → {content hash, list of generated chunk IDs}`. On a file change
  detected via fsnotify, it recomputes the hash; if equal to the stored one, it
  does nothing. If it changed, it runs `DELETE
  /api/v1/memories?domain=X&filePath=Y` (removes the old chunks) and then the
  `memory_add` calls for the new chunks.
- **Cascade purge:** a deleted file (fsnotify remove event) or a root removed
  from the watch → `DELETE /api/v1/memories?domain=X&filePath=Y` for each file
  that had been indexed, and `watch` clears the corresponding entry from its
  local state.
- **Allowlist/denylist:** the default allowlist (A4) is `AGENTS.md`,
  `CLAUDE.md`, `MEMORY.md`, `docs/**/*.md`, overridable/extendable via `--allow`
  (additional globs). The denylist covers common build/dependency directories
  (`node_modules/**`, `dist/**`, `target/**`, `.git/**`, `vendor/**` as the
  default), overridable via `--deny`. The denylist always wins over the
  allowlist if both match the same path.

## Fact extraction (Phase 4, D12)

Enabled in `m3m0s serve` by config, not per request — the same pattern
`-openai-base` already uses to point embeddings at an alternative endpoint:

| Env var | Flag | Default | Required? |
|---|---|---|---|
| `M3M0S_EXTRACT_API_KEY` | `-extract-key` | — | No — absent = extraction off, `Add` behaves as in Phase 1/2/3 (1 input = 1 entry, `extracted: false`). |
| `M3M0S_EXTRACT_BASE_URL` | `-extract-base` | `https://openrouter.ai/api/v1` | No |
| `M3M0S_EXTRACT_MODEL` | `-extract-model` | `deepseek/deepseek-v4-flash` (adjustable, no guarantee it is the exact OpenRouter slug at deploy time — check `openrouter.ai/models` before running in production) | No |

Applies to both entry points that generate memory — `memory_add` (direct) and
each chunk generated by `m3m0s watch` (D12, item 1) — with no differentiation
by `domain`. The extraction prompt includes the input's `domain` as context for
the model, asking for atomic, self-contained facts reusable in future search,
as a JSON array of strings. The detail of the full pipeline (redaction before/
after, fallback on error) is in the `memory_add` section above and in D12
(`docs/decisions/`).

**Retroactive traceability: `metadata.source_content` (Phase 5, D13, item
3).** When `extracted: true`, each `entry` inside `entries[]` carries
`entry.metadata.source_content` = the original text (already redacted, D6) that
generated that fact — the same `content` that was passed to the extractor, not
the fact itself. This is what lets the WebUI show "extracted from: ..." at any
time after the `add` (D13) — otherwise that relationship only exists in the
immediate response of `POST /memories` and is lost afterwards unless persisted
in some field. When `extracted: false` (extraction disabled on the server, or
it failed and hit the fallback — D12, item 5), the entry does **not** carry
`metadata.source_content`: the entry's own `content` already IS the original,
so the field would be redundant.

`source_content` follows the same key-inside-`metadata_json` convention
already used by `m3m0s watch` for `file_path`/`heading` (see
`docs/memory-schema.md`) — it is not a first-class field of `MemoryEntry`, so
it **needs no change to the `schema.Entry` struct** (`Metadata` is already a
free-form `map[string]string`, it fits without a schema change); the real
change is in `internal/store.Add`, which now injects this key into each fact's
metadata before writing, only when `extracted == true` (the already redacted
`content` of the whole input, not the `in.Metadata` shared among the N facts of
one `add` — each fact inherits the same `source_content`, since they all came
from the same source text).

**Dedup and `source_content`: the most recent wins.** When an extracted fact is
deduplicated against an existing entry (`deduped: true`), the stored
`source_content` becomes the one from the **most recent** fact (the input that
triggered this `add`), overwriting the old `source_content` — the same rule
that already applies to the rest of the dedup merge (`mergeForDedup`:
`content`, `source`, `tags`, `metadata` follow "the most recent prevails or is
merged", never "keep the original"). Keeping the old `source_content` would
leave provenance pointing at a source text that no longer corresponds to the
entry's current `content` (which was also overwritten with the most recent
text) — inconsistent with the rest of the dedup pipeline.

**Collision with manually sent `metadata`:** if the `memory_add` request already
arrives with `metadata.source_content` filled in by the caller (unlikely, but
possible), the server overwrites it with the real value computed by the
pipeline whenever `extracted == true` — the key is reserved by extraction in
that case, not a free field. Via `memory_update` (see the section above),
however, `source_content` is **not** managed automatically: since `metadata` is
a whole replacement when present in the PATCH, manually editing an extracted
entry without including `source_content` in the new metadata object erases that
provenance — expected behavior (manual editing is deliberate and overrides
everything), not a bug; the WebUI should preserve the field in the payload if
it wants to keep it visible after the edit.

## Light KG and temporal facts (Phase 7, D15)

Mechanism settled in `docs/decisions/` (D15) — no new infrastructure (no Neo4j,
D6 still holds), reusing the same similarity search that dedup (D12) already
performs per fact inside `Add`.

**Two new fields, inside `metadata` (no change to `schema.Entry`, same
convention as `file_path`/`heading`/`source_content`):**
- `related_to` — comma-separated string with the IDs of related memories.
- `valid_until` — RFC3339 timestamp; present and in the past = memory
  considered expired.

**How `related_to` is populated (automatic, no LLM):** the similarity query
that already runs for dedup (floor `DedupSimilarityThreshold = 0.95`) gets a
second, lower floor, `RelatedSimilarityThreshold = 0.65`. The highest-similarity
candidate within the same `domain` that falls in the `[0.65, 0.95)` band (not a
dedup, but not irrelevant either) becomes `related_to` of the new fact — and
vice versa. It works even with fact extraction (Phase 4/D12) off on the server,
because it does not depend on an LLM.

**How `valid_until` is populated (needs an LLM, optional):** when there is a
candidate in the band above **and** the extractor configured on the server also
implements the `Classifier` interface
(`Classify(ctx, newFact, existingFact string) (string, error)` — today only
`*extract.Extractor`, see `internal/extract`), 1 extra call to the same
extraction LLM asks whether the new fact `contradicts`/`related`/`unrelated` the
candidate. Only `contradicts` (exact answer, unambiguous) marks the existing
candidate with `valid_until = now` — any other result, including classifier
unavailable, network error/timeout, or an unrecognized answer, falls back to
the safe default (`related`, never invalidates anything — same spirit as D12
item 5).

**Search behavior (`memory_search`/`memory_recent`), on by default, no new
parameter to enable it:**
1. Entries with `metadata.valid_until` in the past are excluded from the
   result by default.
2. For each top-K result, if the entry has `related_to`, the related entry (or
   entries) is (are) included in the result as well (cap +2 per result, without
   duplicating an ID already in the list). In the `contextBlock`, an entry
   brought in by expansion appears marked as "↳ related to `<id>`" instead of
   the normal numbered `N. [score] ...` format — making it clear to the
   consuming LLM that it did not come directly from the query, but from a
   relation.

No opt-out flag in this v1 (`search`/`recent` keep the same public signature) —
if this behavior proves noisy in practice, it becomes opt-in/opt-out later with
real usage data, not guessing now.

## Memory layers / automatic forgetting (D22, closes A9)

Mechanism settled in `docs/decisions/` (D22), inspired by the memory tiers of
`akitaonrails/ai-memory`, adapted to m3m0s's flat `Entry` model (no explicit
tiers — 1 retention formula decides eligibility and coldness, see
`internal/store/decay.go`).

**Eligibility (who decays by default):** only memories with
`metadata.source_content_ref` present internally (= went through fact
extraction, Phase 4/D12) **and** without `metadata.pinned: "true"`. Manual raw
content, or content that hit the extraction fallback (D12, item 5), is exempt
by default — treated as more deliberate.

**Reinforcement:** every `memory_get` and every PRIMARY `memory_search` hit
(not those brought in by 1-hop `related_to` expansion) increments
`metadata.access_count` and updates `metadata.last_accessed_at` — frequently
queried memories resist forgetting better. `memory_recent` does not reinforce.

### `memory_forget_sweep` · `POST /api/v1/memories/forget-sweep` · `m3m0s forget-sweep [--dry-run]`

Optional body: `{ "dryRun": boolean }` (default `false`, an empty body is
equivalent to `dryRun: false`).

Evaluates every eligible memory against the retention formula; below the floor
(`ColdRetentionThreshold`), it becomes a soft-delete candidate. Memories
already soft-deleted for more than `SoftDeleteGraceDays` (30 days) become
hard-delete candidates. With `dryRun: true`, it only reports — nothing changes.
With `dryRun: false` (or absent), it really soft-deletes and hard-deletes.

Response (`200`), same shape in dry-run and real execution (`softDeleted`/
`hardDeleted` come back empty in dry-run):
```json
{
  "dryRun": false,
  "evaluated": 42,
  "softDeleteCandidates": ["mem_abc", "mem_def"],
  "hardDeleteCandidates": ["mem_ghi"],
  "softDeleted": ["mem_abc", "mem_def"],
  "hardDeleted": ["mem_ghi"]
}
```

Also runs as a scheduled job (`internal/jobs`, D16) — **always**, even without
fact extraction enabled in this run (eligibility only depends on the entry
already having `source_content_ref` from some earlier boot). Interval
configurable via `-forget-sweep-interval`/`M3M0S_FORGET_SWEEP_INTERVAL`
(default `12h`).

### `memory_restore` · `POST /api/v1/memories/:id/restore` · `m3m0s restore <id>`

Reverts a soft-delete, at any time before the corresponding hard-delete runs.
Returns the full, already restored `MemoryEntry` (`200`), or `404 not_found` —
the same code both for "id never existed" and "id exists but is not
soft-deleted".

**Effect on `memory_get`/`memory_search`/`memory_recent`:** a soft-deleted
memory (`metadata.deleted_at` present) is treated as nonexistent by default in
all 3 — `memory_get` returns `404 not_found`, `memory_search`/`memory_recent`
omit it from the result (including from the 1-hop `related_to` expansion). A
`memory_add` with content identical to a soft-deleted memory does **not** merge
into it by dedup — it creates a new entry, never silently reviving behind
`memory_restore`'s back.

**`GET /api/v1/stats`** gains `telemetry.softDeletedCount` — how many memories
are soft-deleted, awaiting hard-delete by a future sweep.

## WebUI floating chat (D23)

Settled in `docs/decisions/` (D23) — a conversational memory assistant
exclusive to the WebUI (no MCP tool or CLI subcommand, unlike the other
operations in this spec). Answering questions is always automatic (RAG via
`store.Search`, no tool at all); saving a new memory goes through
function-calling (`add_memory`), triggered by the LLM only when the user asks
explicitly.

### `POST /api/v1/chat`

Requires authentication (same `withAuth` as the other routes). Unavailable
(`503 chat_disabled`) when the server has neither `M3M0S_CHAT_API_KEY` nor
`M3M0S_EXTRACT_API_KEY` configured (`-chat-key` falls back to the value of
`-extract-key` as a convenience shortcut).

Body:
```json
{
  "messages": [
    { "role": "user", "content": "what have I recorded about the client Acme?" }
  ],
  "domain": "cliente:acme"
}
```
`messages` is the full conversation history so far (`role`: `user` or
`assistant` — the system prompt is built by the server, never supplied by the
caller). `domain` is optional: it scopes the context search (RAG) by domain and
becomes the default suggestion for `add_memory` when the user does not specify
another.

Response (`200`):
```json
{
  "reply": "You recorded 3 memories about the client Acme: ...",
  "toolCalls": [],
  "contextEntryIds": ["mem_abc", "mem_def"]
}
```
`toolCalls` comes back empty (never `null`) when the LLM answered without
invoking `add_memory`; each item, when present, carries `name`, `arguments`
(the raw JSON sent by the LLM) and `resultSummary` (readable text of what
happened — success with the generated ID(s), or an error). `contextEntryIds`
lists the IDs of the memories retrieved via RAG and injected into the prompt of
this call (empty if the search found nothing or failed — best-effort, never
takes down the response).

**Save-a-memory flow** ("remember that I like drip coffee"): the LLM decides to
call `add_memory({content, domain, tags?})`; the server runs `store.Add` with
`source` always fixed to `"webui-chat"` (never controlled by the LLM), feeds
the result back as a `role: tool` message and makes a 2nd call to the provider
to generate the final natural-language reply — at most 2 tool-calling rounds
per request.

Errors: `503 chat_disabled` (no key configured), `400 invalid_json`/
`400 empty_messages` (malformed body), `502 chat_upstream_failed` (provider
unavailable/timeout).

## In-process jobs (D27)

Settled in `docs/decisions/` (D27) — visibility/control, from the WebUI, of the
periodic jobs that already run inside `m3m0s serve` (`internal/jobs`, D16):
today `forget-sweep` (D22) and `retroactive-extraction` (D12/D16).
**WebUI-only in this v1**, with no MCP tool or CLI subcommand (the same
restricted scope as D23) — the CLI/external agents have no operational need to
trigger jobs manually today. The 3 routes below require authentication (same
`withAuth` as the others).

A job is identified by the `name` it was registered with in the `Scheduler`
(`"forget-sweep"`/`"retroactive-extraction"` today — any future registered job
appears automatically, without changing this contract).

### `GET /api/v1/jobs`

Lists all registered jobs, with history. `history` is always non-null (it may
be empty), most recent first, capped at the last ~20 runs.

Response (`200`):
```json
{
  "jobs": [
    {
      "name": "forget-sweep",
      "intervalSeconds": 43200,
      "running": false,
      "lastRunAt": "2026-07-18T09:00:00-03:00",
      "lastDurationMs": 812,
      "lastSuccess": true,
      "lastSummary": "42 evaluated, 3 soft-deleted, 1 hard-deleted",
      "nextRunAt": "2026-07-18T21:00:00-03:00",
      "history": [
        {
          "startedAt": "2026-07-18T09:00:00-03:00",
          "durationMs": 812,
          "success": true,
          "summary": "42 evaluated, 3 soft-deleted, 1 hard-deleted"
        }
      ]
    }
  ]
}
```
`lastRunAt`/`lastDurationMs`/`lastSuccess`/`lastSummary`/`lastError` are
absent/null when the job has never run in this process instance (`history` is
empty in that case). `lastError`/`error` (per `history` item) only appears on an
unsuccessful run — an error returned by the job or a recovered panic.

### `POST /api/v1/jobs/:name/run`

Triggers the job immediately, outside the normal scheduling cycle — with the
same overlap protection the automatic scheduling already has: if the job is
already running, it returns `409 job_already_running` instead of queueing or
running in parallel. `404 job_not_found` if `name` does not match any
registered job.

Response (`200`); the run is dispatched in the background (it does not wait for
completion — follow it via `GET /api/v1/jobs`, which the WebUI does with short
polling):
```json
{ "started": true }
```

### `PATCH /api/v1/jobs/:name`

Body:
```json
{ "intervalSeconds": 3600 }
```
Changes the job's interval at runtime, without restarting the server — the next
run is recomputed from now. The change is persisted in
`<M3M0S_DATA_DIR>/jobs-config.json` and survives a restart (the saved value
wins over the flag/env default on the process's next start).

Response (`200`): the updated `JobStatus`, same shape as an item of
`GET /api/v1/jobs`.

Errors: `400 invalid_interval` (`intervalSeconds` absent or `<= 0`),
`404 job_not_found`.

## Context injection preview (D29)

Scope restricted to the WebUI (same rationale as D23) — no dedicated MCP tool or
CLI subcommand. It exists to show, BEFORE implementing a new hook or
integration for another tool, exactly what would be injected today for a real
domain, in one of the 3 mechanisms that actually exist in m3m0s — never a
fictional mechanism.

### `GET /api/v1/preview/injection`

Input (query params): `domain` (optional — empty searches across all domains)
and `mode` (`hook` | `mcp` | `manual`, default `hook`).

- `hook`: reproduces exactly `internal/hook.RunSessionStart` (D14/D17) —
  fetches up to `SessionStartFetchLimit` (40) recent memories of the domain,
  filters by `InjectableEntries` (only entries with `metadata.source_content`,
  cut at `SessionStartRecentLimit`, 15) and wraps the resulting
  `apitypes.BuildContextBlock` in the same JSON envelope
  (`hookSpecificOutput.additionalContext`) that Claude Code interprets. It is
  the only automatic mechanism that exists today — no other tool has an
  equivalent hook yet.
- `mcp`: reproduces exactly what the MCP tool `memory_recent` returns today
  (`internal/mcpserver`, `dispatchTool`) — up to 10 recent memories of the
  domain, without the `InjectableEntries` filter, wrapped in the same
  `content: [{ type: "text", text: "..." }]` format of an MCP tool result. A
  generic MCP client only sees this if the model itself decides to call the
  tool during the conversation — nothing is injected automatically at session
  start.
- `manual`: raw `apitypes.BuildContextBlock` (up to 15 recent memories of the
  domain, no extraction filter, no envelope at all) — the text one would paste
  by hand into the system prompt of a tool with neither a hook nor an MCP
  integration (scripts, cron, a new agent not yet plugged in).

Response:
```json
{
  "mode": "hook",
  "domain": "codigo:m3m0s",
  "mechanism": "Claude Code SessionStart hook (D14/D17): ...",
  "entriesFetched": 40,
  "entriesUsed": 12,
  "raw": "{\n  \"hookSpecificOutput\": { ... }\n}"
}
```
`mechanism` is a short description, ready to display in the WebUI, of what the
mode represents. `entriesFetched` is how many memories came from the database
before any filter; `entriesUsed` is how many survived in the final `raw`
(identical to `entriesFetched` in the `mcp`/`manual` modes, which do not
filter).

Errors: `400 invalid_mode` (`mode` outside `hook`/`mcp`/`manual`).

## Errors (common format across the 3 surfaces)

```json
{ "error": { "code": "invalid_domain", "message": "domain must match ^[a-z0-9_]+:[a-z0-9._-]+$" } }
```
HTTP uses the corresponding status code (`400`/`401`/`404`/`500`); MCP returns
it as a tool error; the CLI prints `message` to stderr and exits with a
non-zero code.

## Out of scope for this spec (explicitly deferred)

- **Arbitrary manual delete** (e.g. a generic tool/CLI to delete any memory on
  user request) — what Phase 2 adds (`memory_delete`, see the section above) is
  minimal scope just for `watch`'s cascade purge, not a general delete feature.
  Broad manual delete waits for real demand.
- **`scope_` prefix** mentioned in the original draft of the plan — stopped
  making sense as its own entity after D9 (`domain` is a string field on the
  memory itself, not a separate record with an ID). Implicitly removed;
  recorded here so as not to confuse readers of the old plan.
- **Pagination/rate limiting** on `/recent` and `/search` beyond a simple
  `limit` — no real need at the current scale (small self-hosted deployments).
