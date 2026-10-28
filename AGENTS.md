# AGENTS.md — guidance for contributors (humans and LLM agents)

> Read this **before coding**. It captures the project's purpose, the design
> decisions already made, and the repository layout, so that anyone (or any
> agent) opening this repo cold has what they need without depending on any
> prior conversation history.

## What M3m0s is (in one sentence)

A **single, decentralized** semantic memory service, run as a Docker container
(or a plain static binary, small enough for a small ARM host), reachable by any
agent tool (Claude Code, OpenCode, other agent frameworks, scripts, cron) via
**MCP**, **CLI** or **HTTP** — covering not just code, but also life, company
and client management.

## Why it exists (context for the decision)

The starting point was a comparison document that measured an existing memory
system (chromem-go + OpenAI embeddings, with an extension tied to a single
agent tool) against Claude-Mem, Mem0, Cognee and Zep. That comparison
identified correct technical gaps (no LLM fact extraction, no formatted context
block, no dedup, no temporal facts, no MCP) but prioritized them wrongly for
the real goal: it treated MCP as "low priority" because the original goal was
only "make that tool's memory as good as Mem0/Cognee/Zep".

**The real goal is different:** have ONE memory that any agent tool can use, to
reduce tokens spent rediscovering context and to allow cheaper models with the
same quality. Under that goal, MCP/CLI (access) become P0, not P3.

The current comparison lives in the repo at
**[docs/market-benchmark.md](docs/market-benchmark.md)**: a feature matrix
comparing m3m0s against the 4 competitors, and the known gaps and non-goals.
Update that file when a new decision closes or opens a relevant gap.

## Decisions already made (do not reopen without new grounds)

See `docs/decisions/` for the detail and reasoning of each one. Summary:

1. **Existing memory layers stay separate by function** — M3m0s does not
   replace each project's `MEMORY.md`/`AGENTS.md`/`CLAUDE.md` (each documents
   in its own way: specs, ADRs, briefings, etc.). M3m0s is the "brain" that
   **reads and enriches itself from** those layers (semantic indexing +
   reindexing when the files change), without becoming their editorial source
   of truth.
2. **Stack: Go at the core, TypeScript only where mandatory.** The server
   (HTTP + MCP + CLI) is a single static Go binary — focused on minimal
   footprint (small ARM hosts) and zero external runtime dependency (no Node
   needed on the host). TypeScript only comes in for thin adapters required by
   a tool's own extension API and for the WebUI (React).
3. **Decentralized execution:** distinguish the **M3m0s core** (storage +
   vector search, one central service) from **M3m0s watch** (a lightweight
   agent that runs on each machine/project, watches local memory files via
   fsnotify and sends updates to the core). One binary, two modes
   (`m3m0s serve` / `m3m0s watch <dir>`).
4. **Host:** validate first in Docker on a workstation (bring it up, test it,
   use it for a while) and move to a small always-on host only after it is
   validated.
5. **Name:** M3m0s (it was "Engram" until 15 Jul 2026 — renamed because it
   collided with the real product **getengram.app**, a SaaS for conversation
   memory for agents; see D6 in `docs/decisions/` for the full analysis of that
   product and what is worth taking from it, e.g. secret redaction at
   ingestion and cascade delete). This project has no relation to
   getengram.app. Easy to rename again if it doesn't stick.
6. **License: MIT** (`LICENSE` file at the root).
7. **Embeddings: OpenAI (`text-embedding-3-small`), final.** It was an interim
   default until Phase 4, with local Ollama under evaluation (A6); closed on
   16 Jul 2026 — the maintainer decided not to migrate (no habit of operating
   Ollama, and the OpenAI default is simpler for anyone to configure later).
   See A6 in `docs/decisions/`.
8. **`scope`/`domain` schema: 4 orthogonal axes** —
   `domain` (what the memory is about: `code:<repo>`, `writing:<project>`,
   `personal:<area>`, `client:<name>`; the category names are stored-data
   contract strings kept as-is) + `source` (the technical tool that generated
   it: `claude-code`, `opencode`, `openwebui`, `github-scrape`, etc.) + optional
   `agent` (a named, persistent identity when distinct from the tool) +
   free-form `tags`/`metadata`. Needed because content domain and originating
   tool/agent are independent axes that cross (the same book can be written via
   Claude Code and discussed via Open WebUI; the same named agent can span a
   book, code and data scraped from social networks). See D9 in
   `docs/decisions/` for the reasoning and
   **[docs/memory-schema.md](docs/memory-schema.md)** for the full technical
   mapping (including the derived field `domain_category`, needed because
   chromem-go — reused by D2 — only filters metadata by exact equality, with no
   prefix match).
9. **`m3m0s watch` scope:** by default only memory/doc files (`AGENTS.md`,
   `CLAUDE.md`, `MEMORY.md`, `docs/**/*.md`); source code stays out for now.
10. **Dedicated adapter for a specific agent tool: dropped.** That tool already
    runs with a generic MCP client adapter and real MCP servers in use today —
    a generic MCP client already exists, it is not hypothetical. Once the
    M3m0s core exposes MCP over stdio (Phase 1), registering it in the tool's
    MCP configuration is enough, the same mechanism used by the other servers.
    See D10 in `docs/decisions/`.
11. **`m3m0s watch` mechanism (Phase 2):** automatic domain
    `code:<root-basename>` (overridable via `--domain`), chunking by Markdown
    header (`metadata.file_path`/`metadata.heading` as provenance),
    `memory_delete` with minimal scope (by ID and by domain+file_path, only for
    cascade purge) reopens what Phase 1 had left out of scope, 1 `watch`
    process per watched root. See D11 in `docs/decisions/`.
12. **LLM fact extraction (Phase 4):** applies to `memory_add` and
    `m3m0s watch`, both — not selective by domain, turned on/off on the server
    via `M3M0S_EXTRACT_API_KEY`. 1 input → N atomic facts, each becomes its own
    `Entry` (`AddResponse` becomes plural, breaking change with no shim).
    Provider: OpenRouter (not OpenAI), cheap configurable model
    (`M3M0S_EXTRACT_MODEL`). Secret redaction runs before AND after extraction
    (the content now leaves the process for one more provider). Auto-capture via
    a hook was **out of scope** for this decision, deferred on purpose. See D12
    in `docs/decisions/`.
13. **Admin WebUI (Phase 5):** at the time there was no UI at all — CLI, HTTP
    API and MCP were the only 3 channels, none with a visual surface; the React
    WebUI planned in D2 had never become a checklist item in any phase. v1
    scope settled with the maintainer: search/navigation by the 4 schema axes
    (D9) + stats panel, validation of fact extraction (shows original input vs.
    the N facts generated by Phase 4/D12), manual delete (endpoint already
    exists) and editing of an existing entry (new endpoint). **Redesigned, not
    ported 1:1** from the earlier prototype UI, which was not usable. Auth via a
    login screen (API key pasted once, stored in `localStorage`,
    `Authorization: Bearer` — the same model the backend already has, no server
    change). Each extracted fact now carries `metadata.source_content` for
    retroactive traceability. See D13 in `docs/decisions/`.
14. **WebUI embedded in the Go binary + API under `/api/v1` (architecture
    correction, 16 Jul 2026):** D13's original design ("separate build, served
    separately") required two manually run processes to use the WebUI —
    corrected: the Node *build* stays separate (`npm run build` →
    `webui/dist`), but `m3m0s serve` embeds that output via `go:embed` and
    serves API + static files from the same process/port. Every API route lives
    under `/api/v1/*`; `GET /health` stays outside the prefix (infrastructure
    probe). See the "Architecture correction" elaboration at the end of D13 in
    `docs/decisions/`.
15. **Auto-capture via hook in Claude Code (Phase 8, 16 Jul 2026):** the item
    D12 had explicitly left out of scope, resumed after Phase 4 was validated in
    production. Claude Code first (not other tools — no native hook), trigger
    `PreCompact`+`SessionEnd` (`"async": true`, does not block the session),
    global registration in `~/.claude/settings.json`, new subcommand
    `m3m0s hook claude-code` that reads the JSONL transcript and forwards the
    new text to `POST /api/v1/memories` (the Phase 4 fact extraction does the
    rest). Accepted risk: transcript JSONL parsing is not a stable public
    Claude Code API. See D14 in `docs/decisions/`.
16. **Light KG + temporal facts (Phase 7, 16 Jul 2026):** closes the
    conditional criterion that blocked Phase 7 (A8). `related_to` is populated
    by embedding similarity (a second floor, `RelatedSimilarityThreshold =
    0.65`, below the dedup floor) — no LLM, the same query dedup (D12) already
    runs. `valid_until` needs semantic judgment: new `Classifier` interface
    (optional type-assertion over the `Extractor`, same pattern as `Model()`) —
    only `contradicts` marks the old memory as expired, any other case
    (unavailable, error, ambiguous) falls back to the safe default `related`.
    `search`/`recent` now filter expired entries and expand 1-hop by default,
    with no new parameter. See D15 in `docs/decisions/`.
17. **In-process job system (16 Jul 2026):** `internal/jobs` — a scheduler with
    1 goroutine + a short `time.Ticker` (default 30s, `-jobs-tick`), no OS cron
    nor external lib, never overlaps runs of the same job. First job:
    retroactive extraction (`internal/store/retroextract.go`) — finds memories
    without `metadata.source_content` (never went through fact extraction,
    D12/D13, whether because it was off at `Add` time or because of D12 item
    5's fallback after a network timeout) and runs extraction on them, in
    batches of 25, every ~2h (`-retro-extract-interval`), only when extraction
    is enabled. See D16 in `docs/decisions/`.
18. **Automatic memory injection via `SessionStart` hook (16 Jul 2026):**
    closes the "read" side that Phase 8/D14 had left pending (D14 only solved
    writing, `PreCompact`/`SessionEnd`). Same subcommand
    `m3m0s hook claude-code`, now dispatching by `hook_event_name`:
    `SessionStart` calls `RunSessionStart` (`internal/hook/sessionstart.go`),
    which fetches `GET /api/v1/recent` filtered by the current cwd's domain and
    writes to stdout the `additionalContext` that Claude Code injects into the
    session — only when the payload's `source` is `"startup"` or `"clear"` (a
    genuinely context-free session; `resume`/`compact` already have their own
    history/summary). Context timeout of 5s (not the generic client's 30s)
    because this hook is synchronous and blocks the start of every Claude Code
    session, in every project — never only those using m3m0s.
    `injectableEntries` filters to only inject entries with
    `metadata.source_content` (already through fact extraction, D12) — never
    the raw-content fallback when extraction times out (D12 item 5), found in
    production while testing this very mechanism. See D17 in
    `docs/decisions/`.
19. **Fact extraction sliced into pieces (16 Jul 2026):** closes the root cause
    of the recurring raw fallback that motivated D17 — `PreCompact`/`SessionEnd`
    digests of 30-40 thousand characters almost always blew through the fixed
    20s timeout of `internal/extract` (1 non-streamed call, no retry, for the
    whole digest). `internal/store/extractchunk.go`: `splitForExtraction`
    splits large content into pieces of up to 8,000 characters (on paragraph
    boundaries); `extractPieces` calls `Extract` on each piece **concurrently**
    (not sequentially — avoids multiplying the worst-case latency), and a piece
    that fails falls back to raw only for itself, without taking down the
    others. Used by both `Add` and `retroExtractOne` (D16) — a giant entry that
    partially fails in retroactive extraction now "shrinks" each round instead
    of retrying the same atomic call forever. See D18 in `docs/decisions/`.
20. **Forced JSON + per-piece retry in extraction (16 Jul 2026):** found in
    production running D18 — slicing isolates failures per piece fine, but the
    per-piece success rate was still low (in a round of 6 pieces, only 1
    succeeded), and the causes were mixed, not just timeout: empty response and
    the model returning loose prose instead of the requested JSON array.
    `internal/extract/extract.go`: `Extract` now sends
    `response_format: {"type": "json_object"}` (only in `Extract`, never in
    `Classify`) and asks the prompt for a `{"facts": [...]}` object;
    `parseFacts` tries that format first and falls back to the bare array (the
    old format) if it yields no facts, so as not to break against a provider
    that ignores `response_format`. `internal/store/extractchunk.go`: new
    `extractChunk` tries up to `extractionMaxAttempts = 2` times per piece,
    total span bounded by `extractionChunkBudget = 25s` — a budget, not a blind
    count, so a retry does not double the worst-case latency of a genuine
    timeout beyond the 30s of the CLI/MCP/watch HTTP client. See D19 in
    `docs/decisions/`.
21. **Turn off `reasoning` on the extraction model (16 Jul 2026):** found
    minutes after D19 was in production — 11 of 11 pieces failed with
    `context deadline exceeded` on the first digest after the redeploy,
    returning to the pre-D19 pattern. A direct test against OpenRouter isolated
    the root cause of D18 *and* D19 at once: `DefaultModel`
    (`deepseek/deepseek-v4-flash`) is a reasoning model — a trivial repetitive
    prompt spent 475 `reasoning` tokens and 8.5s before answering; real content
    almost always blows the 20s of `internal/extract` because of that, not
    because of volume (D18) nor response format (D19).
    `internal/extract/extract.go`: new field `Reasoning *reasoningConfig` in
    `chatRequest` (`{"reasoning": {"enabled": false}}`), set in `Extract` AND
    `Classify` (both use the same slow model and neither needs visible
    reasoning). The same test with reasoning off: 1.43s, 0 reasoning tokens.
    D18/D19 remain valid as per-piece failure isolation and format
    reinforcement, respectively — D20 attacks the most probable cause, it does
    not replace the other two. See D20 in `docs/decisions/`.
22. **Deduplicated blob store for `metadata.source_content` (16 Jul 2026,
    closes A7):** the maintainer noticed the database growing fast (7MB with
    few conversations) — measured in production, `source_content` was ~71x
    larger than the facts' `content`, with the same source text copied across
    up to 14 different entries (1 digest generating N facts = N copies of the
    text that generated them). `internal/store/sourceblob.go`: a sidecar
    `sourceblobs.json` (same tmp+rename discipline as `index.json`) indexed by
    `sha256(text)`. `internalizeMetadata` (write) and `externalizeEntry` (read,
    on every public output — `Get`/`Search`/`Recent`/`Add`/`Update`) bridge the
    two: the API/WebUI contract changes nothing, only what stays in
    `index.json`/chromem-go internally. `internal/store/migrate.go`
    (`MigrateSourceContentBlobs`) migrated the 220 existing entries on every
    `m3m0s serve` boot, idempotently, reusing the already computed embedding
    (`chromem-go GetByID`) so as not to spend a new network call. GC of orphan
    blobs (dedup overwriting a reference, or delete) was left out on purpose —
    a residue much smaller than the eliminated duplication, intersects with A9
    (undecided at the time, closed in item 23 below). See D21 in
    `docs/decisions/`.
23. **Memory layers / automatic forgetting, inspired by ai-memory (17 Jul 2026,
    closes A9):** the maintainer explicitly asked to port the "memory tiers"
    concept from `akitaonrails/ai-memory` (analyzed the same day in a
    comparative benchmark) — closes the 2 questions A9 had left open since the
    foundation, confirmed before coding: destructive with a grace period
    (soft-delete → hard-delete after `SoftDeleteGraceDays = 30` days, revertible
    via `Restore` within the window) and scope limited to LLM-extracted facts
    (Phase 4/D12 — `metadata.source_content_ref` present); raw manual/fallback
    content is exempt by default. `internal/store/decay.go`: a single retention
    formula (`exp(-age/30d) + 0.5·log1p(access_count)·
    exp(-days_since_access/45d)`) replaces their 4 explicit tiers
    (working/episodic/semantic/procedural do not map cleanly onto m3m0s's flat
    `Entry` model) — below `ColdRetentionThreshold = 0.15` it becomes a
    soft-delete candidate in `ForgetSweep` (dry-run available). `Get` and
    primary `Search` hits reinforce (`access_count`/`last_accessed_at`,
    rewriting only metadata in chromem-go without spending a new embedding,
    same technique as D21); `metadata.pinned=true` explicitly exempts,
    mirroring ai-memory's "pinned". Soft-deleted becomes invisible by default
    (`Get`→404, outside `Search`/`Recent`, and `findDuplicate` does not revive
    by dedup). New `forget-sweep` job in the scheduler (D16), always running
    (not only with extraction enabled) every 12h by default. New symmetric
    surfaces across the 3 layers: `POST /memories/forget-sweep`,
    `POST /memories/:id/restore`, MCP `memory_forget_sweep`/`memory_restore`,
    CLI `forget-sweep`/`restore`. See D22 in `docs/decisions/`.
24. **Floating LLM chat in the WebUI (17 Jul 2026):** the maintainer asked for
    a chat inside the WebUI that answers questions about memory (automatic RAG,
    no command at all) and inserts new memories with the LLM's help (via
    tool-calling, not a manual command) — a floating panel on the right,
    expandable/closable, persistent across route changes (not across reloads),
    with `/clean` and command autocomplete. New package `internal/chat`: an
    OpenAI-compatible client (OpenRouter, same provider as D12) with support
    for `tools`/`tool_choice`, decoupled from `internal/extract` and
    `internal/store`, `reasoning: {enabled: false}` always on (D20) but without
    forcing `response_format: json_object` (D19 is only for Extract — here the
    response is natural text). Handler `POST /api/v1/chat`
    (`internal/httpapi/chat.go`) orchestrates: searches the user's last message
    via `store.Search` (best-effort, never takes down the response) and injects
    the result into the system prompt via `apitypes.BuildContextBlock` (reused
    from D17); exposes 1 single tool `add_memory` that the LLM triggers only
    when the user explicitly asks to save something, running `store.Add` with
    `source` always fixed to `"webui-chat"` (never controlled by the LLM); loop
    of up to 2 tool-calling rounds. `-chat-key`/`M3M0S_CHAT_API_KEY` falls back
    to the value of `-extract-key`/`M3M0S_EXTRACT_API_KEY` if absent — whoever
    already has extraction on gets the chat for free with the same key; with no
    key in either, it degrades to 503 (never 500). Scope deliberately
    restricted to the WebUI: no MCP tool nor CLI subcommand (unlike D22) —
    CLI/external agents already have `memory_add`/`memory_search` directly.
    Frontend: `ChatWidget` (`webui/src/components/chat/ChatWidget.tsx`) mounts
    once in `AppShell.tsx`, outside the `<Outlet/>`, survives a route change
    without remounting; state in local `useState` (does not persist reloads, on
    purpose in this v1); non-empty `toolCalls` in the response invalidates the
    TanStack Query caches (`recent`/`search`/`stats`). No streaming (the project
    has no SSE infrastructure) — synchronous response with a "typing…"
    indicator. See D23 in `docs/decisions/`.
25. **`m3m0s watch` pointed at the repo itself + per-chunk/per-file failure
    resilience (17 Jul 2026):** found while testing the D23 chat — it could not
    even say which LLM model was running, because the project's own
    documentation (`AGENTS.md`/`docs/**/*.md`) had never been indexed into its
    own memory (`m3m0s watch`, Phase 2/D11, existed but was never pointed at
    this repo's real root). Run as a user-level service (restarting on failure)
    against this repo, domain `code:m3m0s`. On first run, it found a real bug:
    a large chunk of `AGENTS.md` blew the 30s HTTP client timeout (D17) while
    the server was still extracting facts (D12/D18/D19) from the chunk — since
    `ReconcileFile` only saved the file's state (`state.Set`) after ALL chunks
    had gone well, each timeout took down the whole process (an unhandled error
    bubbling up to `Run`) and the service manager restarted it, reprocessing the
    file from scratch and spending extraction tokens again, never finishing.
    Fixed with the same failure-isolation rationale as D18, one layer up:
    `internal/client.NewFromEnvWithTimeout` gives `m3m0s watch` specifically
    120s (CLI/MCP keep the interactive 30s of D17); `ReconcileFile`/`Sync`
    (`internal/watch/watch.go`) no longer abort on the first chunk/file that
    fails — they log and continue, leaving the next scan to retry only what
    remained pending. See D24 in `docs/decisions/`.
26. **Documentation embedded in the binary + self-indexing at boot
    (17 Jul 2026):** the maintainer pointed out that the mechanism of item 25
    (D24) only works in a development workstation — a real standalone Docker
    deployment has neither the `.md` files nor an external `watch` running.
    Confirmed: embed in the binary, the same pattern already used for the WebUI
    (item 14, D13). A `selfdocs` package at the repo ROOT (not `internal/` —
    `go:embed` cannot reach files outside the directory of the `.go` file that
    carries the directive, and `AGENTS.md`/`docs/` live at the root), the import
    path is the module root itself. The embed covers `AGENTS.md`, `docs/*.md`
    and `docs/decisions/*.md`, so those files must stay plain Markdown.
    `internal/selfindex` orchestrates: reuses `internal/chunker.Split` and calls
    `store.MemoryStore.Add`/`DeleteByFilePath` DIRECTLY (no HTTP — unlike
    `internal/watch`, which by D3 is always an external client; here it is the
    process's own bootstrap). Domain `code:m3m0s`/source `"selfdocs"` (the
    same domain as D24, on purpose — does not duplicate under different domains
    if both mechanisms run together). Idempotence across restarts via a sha256
    hash per file in `<M3M0S_DATA_DIR>/selfdocs-state.json`; an equal hash skips
    without spending extraction tokens again. Per-chunk/per-file failure
    isolation (same rationale as D24). Runs in a separate goroutine (does not
    block boot nor `/health`). Always on, with no flag to turn it off — it is
    the product's own knowledge. See D25 in `docs/decisions/`.
27. **Async Add with a short timeout (18 Jul 2026):** prepares the ground for a
    future feature of automatic memory ingestion as the user keeps sending
    messages, where blocking the caller for several seconds on each
    `memory_add` (LLM fact extraction, D12/D18/D19, up to 25s per piece) would
    be unacceptable. `POST /api/v1/memories`
    (`internal/httpapi/server.go:handleAdd`) always dispatches `store.Add` in a
    goroutine with `context.Background()` — never `r.Context()`, otherwise
    extraction would be cancelled as soon as the handler responds and the
    connection closes — and only waits up to `-add-sync-timeout`/
    `M3M0S_ADD_SYNC_TIMEOUT` (default `300ms`, `<= 0` disables the async path
    entirely) before degrading to **202 Accepted** with a pollable
    `operationId` at **`GET /api/v1/memories/pending/:id`**
    (`internal/httpapi/pendingadd.go`, `pendingAddTracker` — in-memory only,
    15min TTL, does not survive a process restart, the same practical effect as
    a synchronous Add interrupted by a crash today). `AddResponse` gains an
    additive `status` field (`omitempty`, breaks no client).
    `internal/client.Client.Add` keeps its exact public signature — CLI/
    `m3m0s watch`/`m3m0s mcp`/`internal/selfindex` change nothing — doing the
    polling internally (200ms interval, honoring the caller's `ctx`) when it
    receives 202, via a new internal variant (`doRaw`) that exposes the raw HTTP
    status code without affecting the `do()` used by the other methods. See D26
    in `docs/decisions/`.
28. **In-process job visibility and control in the WebUI (18 Jul 2026):** the
    job system (`internal/jobs`, item 17/D16) gave no visibility once the server
    was up — no way to know when `forget-sweep`/`retroactive-extraction` last
    ran, what happened, no manual trigger, no changing the interval without
    restarting the process with a different flag. `Job.Run` now returns
    `(summary string, err error)`; `Scheduler` gains a circular history (last 20
    runs per job, same mutex as before), `Status()` (snapshot for the API),
    `TriggerNow(name)` (runs outside the cycle, the same overlap protection as
    `runDue` via `CompareAndSwap`) and `SetInterval(name, interval)` (changes
    the interval at runtime, recomputing `nextRun`). `internal/jobs/persist.go`
    writes the interval override to `<dataDir>/jobs-config.json` (tmp+rename,
    same discipline as `index.json`/`telemetry.json`) — survives a restart. 3
    new routes under `/api/v1/jobs` (`GET`, `POST .../run`, `PATCH /:name`),
    `internal/httpapi/jobs.go`. A **generic** `/jobs` page in the WebUI
    (`JobsPage.tsx`): iterates the job list the API returns, not hardcoded for
    any job name — `forget-sweep`/`retroactive-extraction` appear for free
    today, any future job appears without changing the page. A card per job:
    status, interval editor (seconds/minutes/hours), last run, compact history,
    "run now" button. `useJobs()` uses a short `refetchInterval` (no SSE
    infrastructure in the project, D23) to reflect runs without a manual F5. Out
    of scope: cancelling an in-flight run; MCP/CLI surface for jobs (WebUI-only,
    same restricted rationale as D23). See D27 in `docs/decisions/`.
29. **Replace the hand-rolled JSONs + chromem-go with SQLite and versioned
    migrations (18 Jul 2026):** the maintainer noticed that every m3m0s schema
    evolution was done by hand, with no migration mechanism — 6 JSON files
    (`index.json`, `sourceblobs.json` D21, `telemetry.json`, `jobs-config.json`
    D27, `apikeys.json`, `selfdocs-state.json` D25), each hand-rolled, with no
    version field; worse, each `Entry` lived in two independent representations
    (`index.json` + a chromem-go document) that had to be synchronized by hand
    on every schema change. Confirmed, the maintainer chose the widest scope:
    replace everything, including vector search — reopens the part of D2 that
    chose chromem-go as the vector layer (the rest of D2, Go core/static binary,
    does not change). `internal/sqlstore` (new package): SQLite via
    `modernc.org/sqlite` (pure Go, no cgo — keeps the Dockerfile's
    `CGO_ENABLED=0`) with versioned migrations via `pressly/goose/v3` (embedded
    by `go:embed`). Vector search without a native extension (`sqlite-vec`
    would require cgo): embedding in a `BLOB` column, cosine computed in Go
    (`internal/store/similarity.go`) over the set already filtered by domain —
    the same brute-force strategy chromem-go already used. `internal/store`
    keeps the usual in-memory index (a fast source for `/recent`/`/stats`), now
    reloaded from SQLite at boot; every write becomes 1 row upsert (before:
    rewriting the whole `index.json` on every change). Legacy data import
    (`internal/store/importlegacy.go`) runs only once at boot
    (`cmd/m3m0s/serve.go`, before anything else touches the database) — reads
    the 6 old JSONs + the legacy chromem-go embeddings (kept in `go.mod` only
    for this read path), without automatically deleting the original files.
    `internal/store/migrate.go` (D21) removed — the equivalent effect became
    part of the import. See D28 in `docs/decisions/`.
30. **Context injection preview in the WebUI (20 Jul 2026):** before
    implementing new auto-capture hooks/integrations for other tools, the
    maintainer wanted to see what the initial injected prompt would look like
    in each — asked for a visual preview in the WebUI, not an injection feature
    itself. Scope settled: generic by **injection mechanism** (not by tool name,
    so as not to invent mechanics that no tool has yet), reusing
    `apitypes.BuildContextBlock` as is, fed by a real domain chosen in a text
    field. 3 modes, the 3 mechanisms that exist today: `hook` (reproduces
    `internal/hook.RunSessionStart`, D14/D17, exactly — the only automatic
    mechanism that exists), `mcp` (reproduces what the MCP `memory_recent` tool
    returns — only what a generic MCP client sees IF the model decides to call
    the tool, never automatic) and `manual` (a raw block, to paste by hand into
    a tool with neither a hook nor MCP). `GET /api/v1/preview/injection` (new
    `internal/httpapi/preview.go`) and a `/preview` page in the WebUI. See D29
    in `docs/decisions/`.
31. **Memory evaluation with small scope: golden queries + `recall@k`
    (23 Jul 2026, closes A10):** A10 was conditioned on reopening A6
    (embeddings), but the maintainer reconsidered on the grounds of memory
    quality assurance — m3m0s has already undergone real changes to the
    retrieval path with no objective measurement (D28 swapped the vector layer,
    D18–D20 changed extraction), so the value of `recall@k` does not depend on
    reopening A6. Scope deliberately kept small: a golden dataset of 10–20
    queries (free text → expected `Entry` `id`(s), not expected text, so as not
    to couple to exact wording), covering the real domains already in use; a
    command/endpoint that runs the dataset and reports `recall@k` (small k, e.g.
    3 and 5); run manually before changes to embedding/extraction/chunking, with
    no CI nor deploy gate in this v1. See D30 in `docs/decisions/`.

## Open decisions (see docs/decisions/ for context)

- **A11** — Feedback loop (`memory_feedback`): a low-priority gap from the
  original benchmark. The preliminary recommendation (16 Jul 2026) is **not to
  implement** — `memory_update`/manual delete (Phase 5) already cover the same
  use case — but it is recorded explicitly instead of discarded.

Open decisions are tracked as GitHub issues titled "Decide …"; when one closes,
a record is added under `docs/decisions/` (see "Repository conventions" below).

## Implementation plan

See **[docs/implementation-plan.md](docs/implementation-plan.md)** — phases
with P0/P1/P2/P3 priority, derived from the revised priority table of the
original analysis.

## Repo structure

```
AGENTS.md               this file — entry point
CLAUDE.md               "@AGENTS.md" (Claude Code only auto-loads CLAUDE.md)
README.md               public pitch, project status
docs/
  decisions/              decisions made + open, with reasoning (index in README.md,
                          one file per record: D1..D30, A6..A11)
  implementation-plan.md  phase plan (P0..P3)
  memory-schema.md        technical spec of the memory schema (D9/A3), including
                          the mapping to the real storage (SQLite, D28)
  api-contract.md         MCP/CLI/HTTP contract of the core operations
  market-benchmark.md     comparison vs. Claude-Mem/Mem0/Cognee/Zep
                          (feature matrix, known gaps and non-goals)
cmd/m3m0s/               single binary: dispatcher serve/mcp/watch/hook/add/search/...
webui/                   admin WebUI (Phase 5, D13) — Vite/React/TS, talks to the
                          `m3m0s serve` API under /api/v1; separate Node build
                          (`npm run build`), but the output (`webui/dist`) is
                          embedded in the Go binary via go:embed and served by
                          `m3m0s serve` itself (item 14 above)
selfdocs.go              embeds AGENTS.md + docs/*.md + docs/decisions/*.md into the
                          binary (item 26, D25)
internal/
  schema/                 Entry model + mapping to the persisted map[string]string
  redact/                 secret redaction at ingestion
  sqlstore/               SQLite (modernc.org/sqlite) + versioned migrations
                          (goose) — the only layer that speaks SQL directly (item 29, D28)
  embed/                  OpenAI-compatible embeddings HTTP call (item 29, D28)
  store/                  MemoryStore — in-memory index + internal/sqlstore as
                          persistence, cosine vector search in Go (item 29, D28)
  auth/                   API keys (SHA-256 hash, never the raw key) — api_keys
                          table via internal/sqlstore since D28
  httpapi/                HTTP server (m3m0s serve)
  apitypes/               request/response types shared between HTTP and client
  client/                 HTTP client used by CLI/mcp/watch
  mcpserver/              MCP: stdio↔HTTP bridge (m3m0s mcp) + native Streamable HTTP endpoint (POST /mcp, D54), one shared tool set
  idgen/                  short prefixed IDs (mem_...)
  jobs/                   in-process routine scheduler (no OS cron, item 17
                          above) — 1st consumer: retroactive extraction in
                          internal/store/retroextract.go; custom intervals
                          (D27) in the job_intervals table since D28
```

When the Go core changes structurally in a relevant way, follow the same
discipline: it becomes an entry in `docs/decisions/` (the equivalent of a
lightweight ADR), not just a silent commit.

## Build and test

```sh
go vet ./... && go test ./...       # Go core (also what PRs must pass)
cd webui && npm ci && npm run build # WebUI -> webui/dist (embedded by go:embed)
go build ./cmd/m3m0s                # single binary
docker compose up -d                # run the whole thing (see README.md)
```

## Working on this project

1. Read this file in full, then `docs/decisions/` and
   `docs/implementation-plan.md`.
2. Check the real state of the repo (`git log`, `ls`) — this file describes
   the intent and the decisions to date; the code may have moved on.
3. Do not re-decide the items under "Decisions already made" without an
   explicit new reason — they were settled after analysis, they are not
   guesses.
4. The items under "Open decisions" are exactly that: open an issue (or
   comment on the existing one) before assuming an answer.

## Repository conventions (open source)

This is an open-source project. These rules apply to every contributor, human
or LLM, and to every artifact in the repo:

- **English only.** Code, comments, identifiers, UI strings, log/error
  messages, test names, docs, commit messages, issues and PRs are all written
  in English. The only exception is test data that deliberately exercises
  non-English input (e.g. accent handling or multilingual behavior), and it
  must be explained by an English comment. Domain category names are English
  (`code`, `writing`, `personal`, `client`); the old Portuguese names
  (`codigo`, `escrita`, `pessoal`, `cliente`) are still accepted on input and
  rewritten (D48).
- **Commits are human-owned.** Every commit is authored by, and is the
  responsibility of, one or more humans. Never add AI/LLM/tool co-authorship
  or attribution to a commit or PR — no `Co-Authored-By:` trailer naming a
  tool or model, no "Generated with ..." footer — even if your tooling adds
  one by default. A `Co-Authored-By:` trailer for another human is fine.
  Messages are short and imperative, in English (optionally with a
  conventional prefix such as `fix(store):` or `docs:`).
- **No personal or infrastructure data.** Nothing in the repo (files, tests,
  fixtures, comments, commit messages, docs) may reveal a developer's or
  operator's personal details or setup: real names or e-mail addresses
  (git authorship and `LICENSE` are the only places), absolute home
  directories (`/home/...`, `/Users/...`), hostnames, IPs, ports or paths of
  someone's private infrastructure, hardware/OS anecdotes stated as a local
  fact ("on my machine ..."), private notes or vault references, credentials,
  API keys, tokens or the contents of `.env*` files. Use placeholders
  (`example.com`, `<path>`) and environment variables. If a quirk matters,
  state it as a condition ("on setups with X ...") and link the upstream
  issue.
- **Self-check before committing:**
  `git grep -nIP '[áéíóúâêôãõçÁÉÍÓÚÂÊÔÃÕÇ]'` and
  `git grep -nIE '/home/|/Users/'` should print nothing except deliberate
  multilingual test data and stored-data contract strings.
- **Issues** follow a fixed shape: **Problem** (what is wrong or missing, with
  evidence), **Proposal** (the intended change), **Open questions**, and
  **Acceptance** (how we know it is done). Open design decisions are filed as
  issues titled "Decide …"; when one is closed, add a record under
  `docs/decisions/` (new `D<n>` or `A<n>` file plus an entry in the index) so
  the reasoning stays in the repo.
- **Pull requests** link their issue with `Closes #N`, keep to one logical
  change, and must pass `go vet ./... && go test ./...` before review. Changes
  that alter the API surface update `docs/api-contract.md` in the same PR;
  structural changes add or update a decision record.
