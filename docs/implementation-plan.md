# Implementation plan

Phases with priority P0 (blocks the core goal) → P3 (nice-to-have).
Each phase must end in something testable before moving on. See
[`docs/decisions/`](./decisions/README.md) for the reasoning behind the
stack/host/name choices (D-numbers below refer to the records there).

## Phase 0 — Foundation (done 15 Jul 2026)

- [x] Repo created, `git init`.
- [x] `README.md`, `AGENTS.md`, `CLAUDE.md`, decision records, this plan.
- [x] First commit.
- [x] Decide A1 (license): MIT — `LICENSE` at the root (see D7).
- [x] Decide A2 (remote repo): created, private for now (see D8).

## Phase 1 — Minimal core (Go) — P0

Goal: a Go binary that replaces the previous `memory-server`, generalized
beyond code, with the highest-impact gaps already closed.

- [x] Port the logic of the previous `memory-server` `main.go`
  (chromem-go + embeddings) to the new m3m0s Go module (`internal/store`) —
  reused (chromem-go v0.7.0, same embedding model), not rewritten from scratch.
- [x] **`/recent` bug:** fixed — `internal/store.MemoryStore` keeps its own
  uncapped index (`index.json`, atomic writes), updated in the same critical
  section as the chromem write, and is the source of truth for
  `/recent`/`/stats` (never again an in-memory list capped at 500 and out of
  sync with a sidecar).
  **Bug "compressed memories lose `domain`/`source`/`agent`":** N/A in this
  phase — the m3m0s core does not port/implement any compression feature
  (that was exclusive to the retired extension's `memory.ts`, not to the
  `main.go` that was ported here). It stays as a principle to guide a possible
  future compression feature (Phase 4/6): never overwrite
  `domain`/`source`/`agent` with a generic value such as `"_compressed"` — if
  compression ever exists, it must preserve or inherit those fields from the
  source memories.
- [x] Replaced `project` (cwd basename) with the 4-axis schema defined in D9
  and detailed technically in [docs/memory-schema.md](./memory-schema.md)
  (`domain` + derived `domain_category`, `source`, optional `agent`,
  `tags`/`metadata_json`) — implemented in `internal/schema`. Still open (does
  not block the phase): whether `domain_category` is validated against a
  closed list, and the migration rule for the existing `memory-server` data
  (which only has `project`) to the new schema — no old data was migrated in
  this session.
- [x] Implemented `add`/`search`/`recent`/`stats` per the contract closed in
  **[docs/api-contract.md](./api-contract.md)** — `internal/httpapi` (HTTP),
  `internal/mcpserver` (`m3m0s mcp` bridge, stateless stdio↔HTTP) and
  `cmd/m3m0s` (CLI `m3m0s add|search|recent|stats`, HTTP client of `serve` via
  `internal/client`). Includes a formatted context block in search
  (`apitypes.BuildContextBlock`) and pre-insert dedup (`add` returns
  `deduped`). Tested end to end manually: a real `serve` + a real `mcp`
  talking HTTP to each other. MCP is the item that unlocks Claude Code and
  OpenCode; agents that already have a generic MCP client need no adapter
  (D10) — just register `m3m0s mcp` in their MCP config (not done yet — that
  is a Phase 3 item).
- [x] **Secret redaction on ingestion** (`internal/redact`, before
  dedup/embed in the `add` pipeline), inspired by getengram.app (see D6):
  specific patterns first (PEM keys, `sk-`/`sk-ant-`/`AKIA`/`ghp_`/`sk_live_`
  prefixes, JWT, connection strings, `password`/`token`/`secret`/`api_key`
  pairs), high-entropy heuristic last — replaces with `[REDACTED]` before
  anything becomes an embedding. Generic PII (outside those patterns) is not
  covered yet. Critical because `m3m0s watch` (Phase 2) will index real
  project files.
- [x] **API key auth** (`internal/auth`: SHA-256 hash stored, never the raw
  key, plus a display prefix) — `m3m0s serve` now requires
  `Authorization: Bearer <api_key>` on every route except `/health`, and
  automatically generates a bootstrap key (printed once to stderr) on first
  boot with no registered key.
- [x] IDs prefixed by entity type — `mem_<12 chars>` (`internal/idgen`)
  instead of the current `mem-<unixnano>`. `chunk_`/`scope_` remain out of
  scope for Phase 1 (see docs/api-contract.md, "Out of scope" section).

**Phase done criterion:** from inside Claude Code, call an m3m0s MCP tool and
see a search result coming from the same backend the previous agent extension
fed.

## Phase 2 — `m3m0s watch` (active reindexing) — P0/P1

Goal: the core stops depending only on manual `memory_add` / tool-call
auto-capture — it watches the projects' memory/doc files and updates itself
when they change. Full mechanism designed and closed with the maintainer in
D11 before coding — see also the "Watch" section of `docs/api-contract.md`.

- [x] `m3m0s watch <dir>` mode — fsnotify over the watched root, 1 process
  per root (D11, item 4). `domain` inferred as `codigo:<basename(dir)>`
  (`codigo` is the stored domain-category name for code projects),
  overridable via `--domain`; `source` always `"watch"`.
  (`internal/watch`, `cmd/m3m0s/watch.go`).
- [x] Default allowlist confirmed in A4 (`AGENTS.md`, `CLAUDE.md`,
  `MEMORY.md`, `docs/**/*.md`) via `--allow` (additional globs) + denylist
  (`node_modules/**`, `dist/**`, `target/**`, `.git/**`, `vendor/**` as
  default) via `--deny`, denylist always beats allowlist. Source code stays
  out of the default for now (may become per-project opt-in later). Glob
  matcher with `**` support implemented by hand (`internal/watch/match.go`),
  no new dependency.
- [x] Chunking by Markdown header (`#`/`##`, 1 section = 1 chunk; a file
  without headers = 1 single chunk) — D11, item 2. The IDs of these chunks use
  the `chunk_` prefix (`internal/chunker`, via a new optional `idPrefix` field
  propagated from `apitypes.AddRequest` → `store.AddInput` →
  `schema.NewEntry`).
- [x] Content diff (SHA-256 hash per file, kept in the local `watch` state in
  `~/.m3m0s/watch/<hash>.json`) so nothing is reindexed without a real change.
- [x] Provenance: each chunk carries `metadata.file_path` +
  `metadata.heading` (when applicable) and shows up in the search context
  block (`apitypes.BuildContextBlock`, suffix ` — <file_path> § <heading>`).
- [x] `memory_delete` with minimal scope in `serve` (`DELETE /memories/:id`
  and `DELETE /memories?domain=X&filePath=Y`) — prerequisite for the cascade
  purge below. D11, item 3 (reopens what Phase 1 had left out of scope, with
  deliberately restricted scope).
- [x] **Cascade purge**: watched file deleted (real-time fsnotify event, or
  detected in the initial `Sync` if it was deleted while `watch` was not
  running) → chunks/memories derived from it are removed from the index via
  `memory_delete` by `domain`+`filePath`, they do not stay orphaned (inspired
  by getengram.app's cascade delete, see D6). **Not implemented in this
  phase:** purging a whole root when the `watch` process is simply stopped
  (killing the process deletes nothing, it just stops watching) — there is
  currently no `m3m0s unwatch` or equivalent; if needed, it waits for real
  demand.

**Phase done criterion:** edit a line of a project's `MEMORY.md` and, within
seconds, an m3m0s search for that content already reflects the change —
without calling `memory_add` manually.

## Phase 3 — Multi-tool integration — P1

- [x] Validated the real `m3m0s serve` (real OpenAI embeddings, not the fake
  E2E suite): `add`/`search`/`delete` via HTTP against
  `text-embedding-3-small`, running on `:19235` (`:19234` was already taken by
  the old `memory-server`'s `chromem-server`, retired right after — see item
  below). MCP handshake (`initialize`/`tools/list`/`tools/call`) validated
  manually via raw JSON-RPC over stdio against the real binary — still to be
  validated inside Claude Code/OpenCode as actual MCP hosts (not just the raw
  transport).
- [x] Registered the m3m0s core in the MCP config of another agent that
  already has a generic MCP client (no TypeScript adapter, see D10), entry
  `"m3m0s"` pointing at the `m3m0s mcp` binary with `M3M0S_URL`/
  `M3M0S_API_KEY` of the instance on `:19235`. Still to validate
  handshake/listing/real call *from inside that agent* (the manual test above
  covered the transport, not the host).
- [x] Retired the old `memory-server` — the maintainer decided to bring this
  forward (the old key saw little use, no need to wait for full validation of
  the item above). The old container was stopped (not removed — restart policy
  disabled, data intact in the bind mount, reversible by starting it again);
  the old extension files were moved (not deleted) out of the active path, so
  the agent no longer loads the old extension/tool. Nothing was deleted — only
  turned off/moved out of the active path.

## Phase 4 — Retrieval quality — P2

Full mechanism designed and closed with the maintainer in D12, based on how
Claude-Mem/Mem0/Cognee/Zep solve fact extraction (research summarized in
[market-benchmark.md](./market-benchmark.md)) — not on assumption.
Auto-capture via hook is deliberately left out of this phase (D12, "out of
scope").

- [x] `internal/extract`: manual chat completions client (no new dependency)
  against OpenRouter's OpenAI-compatible API — `Extract(ctx, content, domain)
  ([]string, error)`, prompt asking for atomic self-contained facts as a JSON
  array. Config via `M3M0S_EXTRACT_API_KEY`/`M3M0S_EXTRACT_BASE_URL`/
  `M3M0S_EXTRACT_MODEL` (D12, item 3). Parser tolerant of text/markdown around
  the JSON array.
- [x] `internal/store.Add`: pipeline becomes redact → extract (if enabled,
  otherwise `[content]`) → per fact: redact again → dedup → insert-or-merge
  (D12, items 1/2/4/5). Fallback: extraction fails/times out/invalid JSON →
  inserts the original redacted content as 1 entry, logs the error, never
  silently loses data. `MemoryStore.SetExtractor` toggles it (nil = disabled);
  `internal/store` does not import `internal/extract` directly (`Extractor`
  interface, injected by `cmd/m3m0s/serve.go`).
- [x] `apitypes.AddResponse` becomes `{entries: [...], extracted: bool}`
  (breaking, no shim — D12, item 2) — propagated in `internal/httpapi`,
  `internal/client` (via the same type, no signature change), `cmd/m3m0s`
  (CLI `add`), `internal/mcpserver` (`memory_add`).
- [x] `internal/watch.ReconcileFile`: consumes `resp.Entries` (plural) instead
  of `resp.Entry` — each file chunk can now generate N facts, all IDs go into
  the same `ChunkIDs` list of the local state (which already supported a
  list).
- [x] Unit tests (`internal/extract`, `internal/store` with a fake extractor
  covering on/off/fallback/dedup-between-facts/redaction, `internal/watch`
  with multiple entries per chunk, `AddResponse` serialization) + new E2E
  (`e2e/extract_test.go`) with a fake OpenAI-compatible chat completions
  `httptest.Server` covering: extraction enabled generating N entries,
  extraction disabled (behavior identical to Phase 1/2/3), fallback on
  extractor error, `watch` reindexing a file whose chunk generates multiple
  facts (cascade purge covering all of them, not just the first).
- [x] Docs: `docs/api-contract.md` (new `AddResponse` shape, "Fact
  extraction" section) and the D12 record were closed before implementation —
  no new detail came up that required adjustment.
- [ ] Re-evaluate A6 (embeddings provider) — whether something cheaper/local
  can be used without losing search quality. Outside the scope of D12 (which
  only covered fact extraction) — separate decision, still open.

## Phase 5 — Admin WebUI — P1

Goal: give the maintainer a visual way to search, validate and administer the
memory — until now this could only be done by reading raw JSON via the CLI/HTTP
API. Scope closed with the maintainer in D13 on 15 Jul 2026, starting from the
React WebUI of the old `memory-server` — reused as a technical starting point
(D2), but redesigned, not ported 1:1: the maintainer confirmed the old
version "was not very usable".

**Frontend stack** (closed on 16 Jul 2026, see the D13 elaboration): Vite +
React (D2) as the base, Tailwind + shadcn/ui for the UI, TanStack Query for
cache/fetch against `serve`, TanStack Router for routes, TanStack Form in the
add/edit forms, TanStack Virtual for long search/recent lists, Zod for
client-side schema validation (mirroring `docs/api-contract.md`), route lazy
loading via `React.lazy`.

- [x] Login screen: paste the API key once, store it in `localStorage`, send it
  as `Authorization: Bearer` on every call (D13, item 2) — zero backend change,
  same auth model that already existed (D6/Phase 1). Validated against the
  real `GET /stats` (there is no "who am I" endpoint in the contract — `/stats`
  already requires auth and serves as a probe).
- [x] Search/browsing with filters over the 4 schema axes (D9): `domain`,
  `domainCategory`, `source`, `agent`, `tags` — the old one only filtered by
  `project` (a field that no longer exists). `recent` and semantic search
  (`POST /search`) on truly separate routes (`/recent`, `/search`, with
  filters in the URL via `validateSearch`), reusing the existing endpoints.
  Virtualized list via TanStack Virtual.
- [x] Stats panel: count per `domainCategory` (`GET /stats` already returns
  `countByDomainCategory`, shown as bars), embedding backend/model, and fact
  extractor status (on/off, which `M3M0S_EXTRACT_MODEL`) via the contract's
  `extraction` field (backend already implemented before this frontend
  session).
- [x] Extraction validation: the "Add" screen (TanStack Form + Zod) shows side
  by side the original text and the N atomic facts the LLM extraction (Phase
  4/D12) generated before closing the form — uses the `POST /memories`
  response (`entries[]`, `extracted`), including a per-fact `deduped`
  indicator.
- [x] Retroactive traceability (D13, item 3): `metadata.source_content` was
  already implemented in the backend (`internal/store.Add`) before this
  frontend session; the detail screen shows an "Extracted from: ..." section
  when the field is present.
- [x] Manual delete from the UI — confirmation `AlertDialog` on the detail
  screen, uses `DELETE /memories/:id` (D11), no new backend work.
- [x] Editing an existing entry — `PATCH /memories/:id` (already implemented
  in the backend before this frontend session, see `docs/api-contract.md`).
  Form (TanStack Form + Zod) pre-populated with the full entry (including the
  whole `metadata`, so `source_content` is not lost by accident) and sends
  only the fields actually touched, following the contract's key-presence
  semantics.

Frontend in `webui/` (Vite + React + TS, full stack from the paragraph
above). `npm run build`/`npm run lint`/`npm run test` (Vitest, Zod schemas)
passing without errors.

- [x] `GET /memories/:id` (later elaboration, closing the gap the frontend
  session exposed in practice): the detail/edit screens could initially only
  show an entry that had already appeared in a search/recent/add list in this
  browser session (TanStack Query cache seeded from those responses) — a
  direct detail link or an F5 on the page did not work. `memory_get` closes
  this as a first-class operation on all 3 surfaces (HTTP + MCP + CLI, same
  pattern as `memory_update`), see `docs/api-contract.md`. `useCachedEntry`
  became `useEntry` (`useMemoryApi.ts`): a normal `useQuery` against
  `GET /memories/:id` with `initialData` from the TanStack Query cache to
  avoid a loading flash when the entry was already seen in the session, with
  fallback to the real fetch when it was not.

- [x] WebUI embedded in the binary + API under `/api/v1` (architecture
  correction, 16 Jul 2026, see the final D13 elaboration): the original design
  ("separate build, served separately") required starting two processes by
  hand (`m3m0s serve` + `npm run dev`) just to open the WebUI, which breaks
  the done criterion below in practice. Fixed: `webui/dist` (generated by
  `npm run build`) embedded in `m3m0s serve` via `go:embed`, served at `/`
  with SPA fallback; every API route moved to `/api/v1/*` (`GET /health`
  deliberately stays out). The release build becomes just `npm run build &&
  go build ./cmd/m3m0s` — a single binary again, with no extra Node process in
  production. Validated end to end via Playwright against the real Docker
  instance on 16 Jul 2026 (semantic search, recent, detail, stats, zero
  console errors) — in the process, found and fixed a real bug (nil `metadata`
  serializing as JSON `null`, breaking the WebUI's Zod validation).

**Phase done criterion:** the maintainer can, without touching a terminal
(once `m3m0s serve` is already running — a single process, no separate `npm
run dev`), search a memory, see which file/fact it came from, check whether
fact extraction produced something sensible, delete a wrong entry, and edit an
existing entry — all through the WebUI.

## Phase 6 — Definitive deploy — P2

- [x] Multi-stage Dockerfile: WebUI build (`npm run build`) → Go build
  embedding `webui/dist` via `go:embed` (Phase 5) → final image with only the
  static binary, no Node/Go toolchain in the runtime image. `compose.yaml`
  builds locally (no registry yet), `.env` (gitignored) only with
  `OPENAI_API_KEY`/`M3M0S_EXTRACT_*`, named volume `m3m0s-data:/data`
  (owner/permissions inherited from the image, no uid hack — unlike a
  bind-mount that needs it because of an rclone FUSE mount, a reason that does
  not apply here). Healthcheck via `GET /health`.
- [ ] Run in Docker on a workstation for a validation period — container came
  up on 16 Jul 2026, `docker compose ps` shows `healthy`; the item stays open
  until a period of real use has passed (it is not a one-off test).
- [x] **Issue found on 16 Jul 2026 while validating Phase 8, resolved the same
  day:** the real `.env` used by `docker-compose.yml` did not have
  `M3M0S_EXTRACT_API_KEY` — fact extraction (Phase 4/D12) was **off** in the
  Docker instance (boot log confirmed: "fact extraction: disabled"), despite
  having been validated in production before the move to Docker (Phase 6) —
  lost in that migration. Resolved: the OpenRouter key was provided and set in
  `M3M0S_EXTRACT_API_KEY`, container restarted, boot log confirms "fact
  extraction: enabled (model=deepseek/deepseek-v4-flash)".
- [ ] Then, deploy on a small ARM host behind a reverse proxy, as a permanent
  service (container managed via GitOps) — resolve the real footprint on 1 GB
  of RAM shared with other services before migrating.

## Phase 7 — Light KG + temporal fact — P3, mechanism closed in D15 (16 Jul 2026)

Goal: relate memories to each other and let a new fact invalidate an old one —
with no new infra (no Neo4j, D6 still holds). It stopped being conditional
("only if Phase 4 shows it is needed") on 16 Jul 2026 — see D15/A8 and the
"Light KG and temporal fact" section of `docs/api-contract.md` for the full
design before implementing.

- [x] `RelatedSimilarityThreshold = 0.65` in `internal/store` — second
  similarity floor (below `DedupSimilarityThreshold = 0.95`), reuses the same
  per-fact dedup query, no new call.
- [x] `related_to` written to `metadata` (comma-separated string) for the
  highest-similarity candidate in the `[0.65, 0.95)` band within the same
  `domain` — works without an LLM, even with extraction (Phase 4) off.
- [x] `Classifier` interface (`Classify(ctx, newFact, existingFact string)
  (string, error)`) in `internal/store`, optional type assertion over the
  configured `Extractor` (same pattern as `modelProvider`/`Model()`, D12) —
  implemented in `*extract.Extractor` (`internal/extract`).
- [x] `valid_until` (RFC3339) written to the existing candidate only when
  `Classify` answers exactly `contradicts` — any other case (classifier
  absent, error, unrecognized answer) falls back to the safe `related`, never
  invalidates.
- [x] `search`/`recent`: filtering of `valid_until` in the past and 1-hop
  expansion via `related_to` (cap +2 per result) on by default, no new
  parameter — `Search`/`Recent` in `internal/store` gain a `now time.Time`
  (same pattern as `Add`/`Update`), only 2 call sites in `internal/httpapi`
  changed.
- [x] `apitypes.BuildContextBlock`: entries brought in by expansion marked as
  "↳ related to `<id>`", not in the normal numbered format.
- [x] Unit tests (`internal/store/related_test.go`: similarityBand, metadata
  helpers, linkRelated via fake Classifier — contradicts/unrelated/safe
  fallback on error/unexpected answer/no Classifier, expandRelated with
  cap+seen+expired, Recent filtering expired and expanding 1-hop, addOne end
  to end with controlled embedding; `internal/extract`: `Classify` —
  trim/lowercase, prompt with the 2 facts, HTTP error; `internal/apitypes`:
  contextBlock does not number an expanded entry). `go build ./...`,
  `go vet ./...` (including `-tags e2e`) and `go test ./...` passing on
  16 Jul 2026 — Phase 7 implementation complete.

## Phase 8 — Auto-capture via hook (Claude Code) — P1

Goal: close the item D12 (Phase 4) explicitly left out of scope — today every
memory depends on someone (the model, via an MCP tool call, or `watch`)
deciding to capture it; this phase makes Claude Code capture on its own at the
end of each session/compaction. Full design closed with the maintainer on
16 Jul 2026, see D14.

- [x] `m3m0s hook claude-code`: new Go subcommand that reads the hook's JSON
  payload from stdin (`session_id`/`transcript_path`/`cwd`/`hook_event_name`),
  parses the JSONL transcript extracting only `type: "text"` blocks of
  `user`/`assistant` messages (ignores `tool_use`/`tool_result`/`thinking`),
  from the local checkpoint (`~/.m3m0s/hooks/claude-code/<session_id>.json` —
  last line already processed), concatenates into a digest with a defensive
  size limit (to avoid 1 giant compaction producing 1 giant extraction call).
- [x] Domain: `codigo:<basename(cwd)>`, same rule as `m3m0s watch` (D11), no
  special case. `source=claude-code`, no `agent`.
- [x] The digest goes to `POST /api/v1/memories` via `internal/client` (same
  client as the CLI/MCP) — fact extraction from Phase 4/D12 handles the rest,
  the hook does not extract anything by itself.
- [x] Credentials (`M3M0S_URL`/`M3M0S_API_KEY`): read from the process
  environment or from a dedicated local file (`~/.m3m0s/hook.env`) — never
  embedded in plain text in the hook command inside `~/.claude/settings.json`.
- [x] Registration of the `PreCompact` + `SessionEnd` hooks in
  `~/.claude/settings.json` (**global**, outside this repo — a change shared
  across all of a user's projects in Claude Code, made only after validating
  the subcommand in isolation), `"async": true` so the session is not
  blocked.
- [x] Unit tests of the transcript parser (lines with `type`/`content`
  varying between plain string and array of blocks, mix of
  `text`/`tool_use`/`tool_result`/`thinking`) and of the incremental
  checkpoint (`PreCompact` followed by `SessionEnd` in the same session does
  not duplicate capture).

**Phase done criterion:** compacting or closing a Claude Code session in any
project (not just m3m0s) generates a new memory in m3m0s with no manual action
— verifiable by searching for the session's content right afterwards via
`m3m0s search`/WebUI.

**Production validation (16 Jul 2026) and 2 bugs found/fixed:**

1. The first real `PreCompact` (fired when compacting the very session that
   built this) generated no memory, even though Claude Code reported
   "completed successfully". Cause: `~/.m3m0s/hooks/` did not exist (cleanup
   of earlier tests had deleted the whole tree) — the
   `2>>~/.m3m0s/hooks/claude-code.log` redirection of the registered command
   fails when the parent directory does not exist, which kills the whole
   command *before* the `m3m0s` binary even runs; the `|| true` in the command
   masks this failure, so "completed successfully" does not mean "captured".
   Fixed by recreating the directory — but it is a real fragility of the
   mechanism (not of the subcommand itself) worth hardening later: the hook
   should not depend on a pre-existing directory to even log its own error.
2. With the directory fixed, `Add` still failed with
   `dedup query: ... 400 Bad Request` from OpenAI's embeddings API. Root
   cause, in `internal/store`: neither the dedup query (`findDuplicate`) nor
   storage (`upsertChromem`) limited the text sent to `embedFunc`, and the
   hook's digest can reach 40 thousand characters (~the defensive limit of
   item 1 above) — well above the 8191 tokens `text-embedding-3-small`
   accepts. This had never shown up before because, with fact extraction
   (Phase 4/D12) on, all content reaching `Add` already comes chopped into
   short atomic facts; since extraction was **off** in the Docker instance at
   the time of this test (`M3M0S_EXTRACT_API_KEY` missing from the `.env` used
   by `docker-compose.yml` — resolved the same day, see the Phase 6 bullet
   above), the raw 40k-character digest went whole into embedFunc. Fixed with
   a cap (`maxEmbedChars = 12000`) applied only to the text sent to embedFunc
   (both in the dedup query and in storage, via a pre-computed
   `Document.Embedding`) — the stored/returned `Content` stays intact, without
   truncating what the user sees. Validated end to end against the real Docker
   instance after the fix: the capture created a real entry
   (`domain=codigo:m3m0s`, `source=claude-code`), removed right after via
   `DELETE /api/v1/memories/{id}`.
3. **Found on 16 Jul 2026, in the first real capture in production — 2
   problems, fixed the same day:**
   - **Extraction failed on this specific capture:** error
     `parse facts array: invalid character 'a' looking for beginning of
     value` (the message was in Portuguese in the original code) —
     signature of an item in the JSON array returned by the model without
     quotes (e.g. `[fact1, loose text]`), probably because the input was close
     to the 40k-character cap. It fell back (D12, item 5, as designed) and
     stored the raw digest as 1 single entry, instead of atomic facts — no
     data loss, but without the expected summary. Not fixed yet (it is not a
     regression, it is the fallback working; consider later a retry with a
     reinforced prompt or a more tolerant parser if it happens again).
   - **Truncation kept the wrong content:** this was the first `PreCompact`
     fired in that session, so the local checkpoint (which only existed zeroed)
     processed the whole transcript accumulated until then (line 0 to ~4300,
     several work phases), not just the last compaction window.
     `BuildDigest` truncated keeping the **start** of the text — reasonable in
     a normal compaction window, but in this case it discarded precisely the
     most recent work and kept only old, already-resolved conversation.
     **Fixed:** `BuildDigest` now truncates keeping the **end** (most
     recent) — more correct both for this case and for the common case of 1
     normal compaction.

## Phase 9 — Memory tiers / automatic forgetting — P2, mechanism closed in D22 (17 Jul 2026)

Goal: close A9 (left open since the foundation) — low-value memories cannot
grow the database forever. Explicit request from the maintainer to port the
"memory tiers" concept from `akitaonrails/ai-memory` (analyzed the same day in
a comparative benchmark), adapted to m3m0s's flat `Entry` model. Full design
(retention formula, eligibility, destructive with grace period) closed with
the maintainer via `AskUserQuestion` before coding — see D22 and the "Memory
tiers / automatic forgetting" section of `docs/api-contract.md`.

- [x] `internal/store/decay.go`: `retentionScore` (age + access reinforcement,
  half-lives of 30d/45d), `isDecayEligible` (only extracted facts, D12, not
  pinned), `touchAccess` (reinforcement on `Get`/primary hits of `Search`,
  rewriting only metadata in chromem-go without spending a new embedding),
  `ForgetSweep` (soft-delete + hard-delete after `SoftDeleteGraceDays = 30`,
  optional `dryRun`), `Restore`.
- [x] Soft-deleted excluded by default from `Get` (`ErrNotFound`)/`Search`/
  `Recent`/`expandRelated`, and treated as "no duplicate" in `findDuplicate` —
  never revived by silent dedup.
- [x] `Get` gained the `now time.Time` parameter (internal change, does not
  affect the HTTP/MCP/CLI contract) to support access reinforcement.
- [x] New symmetric surfaces on the 3 layers: `POST
  /api/v1/memories/forget-sweep`, `POST /api/v1/memories/:id/restore`, MCP
  tools `memory_forget_sweep`/`memory_restore`, CLI
  `m3m0s forget-sweep [--dry-run]`/`m3m0s restore <id>`.
- [x] `GET /api/v1/stats` gains `telemetry.softDeletedCount`.
- [x] `forget-sweep` job registered in the scheduler (`internal/jobs`, D16) —
  always runs, regardless of whether fact extraction is enabled in this run.
  Interval configurable via `-forget-sweep-interval`/
  `M3M0S_FORGET_SWEEP_INTERVAL` (default 12h).
- [x] Tests (`internal/store/decay_test.go`): `retentionScore`/
  `isDecayEligible` as pure functions, `ForgetSweep` dry-run vs. real,
  exemption of pinned/non-extracted, hard-delete only after the grace period,
  `Restore` (success and the 2 `ErrNotFound` cases), soft-deleted excluded
  from `Search`/`Recent`/`Get`, `findDuplicate` ignoring a soft-deleted
  candidate, access reinforcement via `Get` and via a primary `Search` hit
  (distinct from an entry only brought in by 1-hop expansion). `go build
  ./...`, `go vet ./...` (including `-tags e2e`) and `go test ./... -race`
  passing.

**Phase done criterion:** an old extracted fact that is never queried again
disappears from search/recent on its own after a `forget-sweep` (manual or
scheduled), remains recoverable via `m3m0s restore <id>` while within the
grace period, and a `dryRun: true` shows the candidates without changing
anything — verifiable both via automated tests and manually against the real
`serve`.

## Phase 10 — Floating LLM chat in the WebUI — P2, closed in D23 (17 Jul 2026)

Goal: conversational assistant inside the WebUI — ask about the memory without
needing any command (automatic RAG) and save new memories with the LLM's help
(via tool-calling, not a manual form). Explicit request from the maintainer: a
floating panel on the right side, expandable/closable, persistent across route
changes, `/clean` command and command autocomplete. See D23 and the "WebUI
floating chat" section of `docs/api-contract.md`.

- [x] New package `internal/chat`: OpenAI-compatible chat completions client
  (OpenRouter) with support for `tools`/`tool_choice`/`tool_calls`, decoupled
  from `internal/extract` and `internal/store`. `reasoning: {enabled: false}`
  always on (D20); no forced `response_format: json_object` (unlike `Extract`,
  D19 — here the response is natural text).
- [x] `internal/httpapi/chat.go`: `POST /api/v1/chat` handler — best-effort RAG
  via `store.Search` + `apitypes.BuildContextBlock` (reused from D17) injected
  into the system prompt; 1 `add_memory` tool executing `store.Add` with
  `source` always fixed to `"webui-chat"`; loop of up to 2 tool-calling rounds;
  degrades to `503 chat_disabled` without a configured key.
- [x] `internal/apitypes`: `ChatRequest`/`ChatMessage`/`ChatResponse`/
  `ChatToolCallResult`.
- [x] `cmd/m3m0s/serve.go`: `-chat-key`/`M3M0S_CHAT_API_KEY` (falls back to the
  value of `-extract-key`/`M3M0S_EXTRACT_API_KEY` if absent), `-chat-base`/
  `M3M0S_CHAT_BASE_URL`, `-chat-model`/`M3M0S_CHAT_MODEL`.
- [x] Tests: `internal/chat/chat_test.go` (Complete with/without tool call,
  tools/tool_choice only when there are tools, reasoning off, HTTP error/empty
  choices, token accumulation); `internal/httpapi/chat_test.go` (503 without
  key, 400 without messages, RAG-only flow, full tool-calling flow executing a
  real `store.Add` and confirming the saved memory via `store.Recent`, 502 on
  provider failure). `go build ./...`, `go vet ./...` (including `-tags e2e`)
  and `go test ./... -race` passing, no regression.
- [x] Frontend (`webui/`): `sendChatMessage` in `lib/api.ts` + Zod schemas in
  `lib/schemas.ts`; `useChatMessage` hook (`hooks/useMemoryApi.ts`)
  invalidating `recent`/`search`/`stats` when `toolCalls` comes back non-empty;
  `ChatWidget` (`components/chat/ChatWidget.tsx`) mounted once in
  `AppShell.tsx`, outside the `<Outlet/>` — survives route changes;
  `components/chat/chat-commands.ts` with the `/clean` command (100% local,
  no API call) and keyboard-navigable autocomplete. `tsc -b`, `vite build`,
  `oxlint` and `vitest run` passing, no regression.
- [x] Scope deliberately restricted to the WebUI: no MCP tool or CLI
  subcommand (unlike Phase 9) — the CLI/external agents already have direct
  `memory_add`/`memory_search`, simpler than going through an intermediary LLM.

**Out of scope for this phase** (justified in D23, revisit only if it becomes
a real need): response streaming (no SSE infra in the project — synchronous
response with a "typing…" indicator); conversation persistence across page
reloads (only across route changes); syncing the chat's `domain` with the
current page's filter (the WebUI currently has no concept of a current domain
shared across routes).

**Phase done criterion:** asking about something already recorded in memory
returns an answer grounded in the retrieved context (RAG), with no command
needed; explicitly asking to save something creates a new memory (visible in
`/recent`/search right afterwards, with no manual reload); `/clean` clears the
conversation without a network round trip; the panel survives navigating
between Search/Recent/Stats/Add without losing history — verifiable both via
automated tests and manually against the real `serve` with
`M3M0S_CHAT_API_KEY` set.
