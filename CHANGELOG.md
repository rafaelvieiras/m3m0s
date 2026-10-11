# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Secret redactor: key names with a prefix or suffix (`TEST_USER_PASSWORD=`, `access_token=`,
  `CLIENT_SECRET=`) were missed because `\b` does not match after `_`. It now also knows
  `senha`/`pwd`/`passwd`, narrative forms ("the password is ..."), short low-entropy values,
  `Authorization: Basic|Bearer`, `user:password@host` URLs and more provider formats, keeps the
  key name, and skips placeholders. The high-entropy layer no longer replaces identifiers
  (camelCase, snake_case, branch names, file paths) with `[REDACTED]`.
- Source blobs (D21) are redacted on the write path too.

### Added

- Fact nature (D38): every new fact gets a `kind` (`decision`, `rule`, `preference`, `gotcha`,
  `procedure`, `state`, `noise`, `other`) from the extractor in the same call (`kind_source=llm`) or a
  conservative heuristic (`heuristic`); existing rows are not rewritten and read as `other`. `kind`
  is in the memory JSON, filters `search`/`recent`/MCP (`--kind`), is correctable with `memory_update`
  (`manual`, never overwritten), and its distribution is on the Insights page. Optional backfill
  `m3m0s kind backfill estimate|start|status|cancel` (confirmation, rate limited, resumable);
  `m3m0s kind sample|report` and an extractor golden file for measuring agreement. Groundwork for a
  per-kind retention policy.
- `m3m0s maintenance redact-scan [--apply]`: scans stored entries and source blobs with the
  current redactor. Dry run by default; reports ids/hashes and rule ids only (never values).
  `--apply` re-redacts blobs in place; entries are only listed for manual review.

- Memory health dashboard (D37, migration 00006): an Insights page in the WebUI, `GET
  /api/v1/insights/overview` and `m3m0s insights show [--json]` with totals, distributions
  (domain, tool, age, text length), `valid_until`/pinned/never-accessed counts, soft-deleted
  per day and the forgetting outlook (cold now, waiting for hard-delete). A new manual,
  cancellable, read-only job `insights-hygiene` (`POST /api/v1/insights/hygiene/run|cancel`,
  `GET /api/v1/insights/hygiene`) finds identical and near-identical memories (cosine
  0.98/0.95/0.92, same domain versus across domains and tools, using the in-memory int8
  index and no extra vectors) and low-value candidates in the `codigo` domain, and stores
  one small JSON report. It never changes memories; examples are truncated and exclude
  secrets. Tuning: `M3M0S_INSIGHTS_CPU_PERCENT`, `M3M0S_INSIGHTS_MAX_MINUTES`.
- Embedding settings in the WebUI and CLI (D36, issue #39 phase 3): an Embedding page
  (provider, model, dimensions, enable, write-only API key) with the 409 confirmation
  flow (stale counts plus approximate token/cost estimate), and a re-embedding panel with
  start, cancel, progress bar and ETA. New `m3m0s embedding show|set|estimate|reembed
  start|status|cancel [--watch]`; the key is read from stdin or an environment variable,
  never from an argument, and is never printed.
- Re-embedding job (D36, issue #39 phase 2): after a model or dimensions change, a manual,
  cancellable, resumable job re-embeds the rows outside the active vector space in
  batches (rate limited, with retry and backoff), with progress on the Jobs page;
  `POST /api/v1/embedding-config/reembed/estimate` returns pending rows, approximate
  tokens and, for known OpenAI models, the estimated cost before confirming.
- Accounts (D35, migration 00004): first-run onboarding in the WebUI, username/password
  sign-in with revocable server-side sessions (HttpOnly cookie, CSRF header, login
  throttling), a password reset page reached from a single-use link, and
  `m3m0s user list|add|reset|delete` (API key only). API keys keep working
  unchanged for hooks, MCP and the CLI.
- Project scope (D34): `project=X` recalls global memory plus that project's,
  `scope=project-only|global` narrows it, project entries can override a
  contradicted global entry inside the project only, and `PATCH project` promotes
  or moves entries. `m3m0s project set|unset|list|show` maps directories to
  projects (`~/.m3m0s/projects.json`); the hooks and the MCP server use it, and
  sessions with no project recall only global memory. `GET /stats` adds
  `countByProject`.
- Provenance fields (D33, migration 00003): optional `model` and `project` on entries,
  accepted and returned by the API, MCP tools, CLI and WebUI, and usable as exact
  filters in search, recent and the injection preview. The Claude Code hook records
  the model from the transcript and the project from `M3M0S_PROJECT`;
  `M3M0S_INJECT_SOURCE`/`M3M0S_INJECT_PROJECT` filter SessionStart injection.
  Dedup only compares entries of the same project.
- `m3m0s eval <golden.json>`: recall@k, mean injected tokens and latency per
  ranking mode over a golden dataset (D30).
- SQLite FTS5 index over memory content (migration 00002, backfilled on first start).
- On-disk capture spool (`~/.m3m0s/spool`): payloads the server does not accept
  are saved and replayed (oldest first, idempotent) by the next capture hook or
  by `m3m0s doctor --replay`; capped at 500 files, 30-day retention, dead-letter
  directory for payloads the server keeps rejecting.
- `m3m0s doctor`: checks credentials, server, API key, Claude Code hooks, spool
  and recent hook failures.
- Timestamps in hook log lines.
- `Stop`, `UserPromptSubmit` and `PostToolUse` capture events as a safety net
  for abrupt session ends: they only stat the transcript until it grew by 16 KB
  and holds 4000+ characters of new text.
- `doctor` reports whether `m3m0s` is configured as an MCP server in Claude Code
  and opencode, and which optional hook events are missing.
- Ingestion boilerplate filter (issue #53, D12 addendum): the automatic capture path (the
  Claude Code hook) no longer turns tooling text into memories. Versioned, data-driven rules
  (id, scope `segment|fact`, regex anchored on a full template, action `drop|tag`) ship as
  defaults: the extractor's own prompt and JSON schema, session-continuation and compaction
  boilerplate and commit-message generator prompts are dropped; diff arithmetic (byte
  sizes, insertion counts) is only tagged `metadata.filtered_by=<rule id>` for review.
  Segment rules run before extraction (saving tokens), fact rules after it. Modes
  `off|tag|drop` globally and per rule, and an operator rules file, via
  `M3M0S_FILTER_MODE`, `M3M0S_FILTER_MODES` and `M3M0S_FILTER_RULES`. Explicit writes
  (`memory_add`, API, chat, watch) and pinned content are never filtered. Per-rule
  seen/dropped/tagged counters appear in `GET /api/v1/insights/overview`, the Insights
  page and the server log. `m3m0s filter replay` shows the hits over a stored database
  (opened read-only) or text files without writing anything. `POST /api/v1/memories`
  accepts `capture` and answers with `filtered`.

### Changed

- Hybrid search (D32): the default `mode` fuses the vector ranking with the 3 best
  FTS5 keyword hits; `vector` and `keyword` remain selectable. Recall on the
  maintainer's 38-query golden set: recall@1 0.71 → 0.84, recall@3 0.84 → 0.90,
  recall@5 and @10 unchanged. Search falls back to keyword if the embedding
  provider fails.
- `Add` embeds the content once instead of twice.
- The OpenAI key is optional: without it the server runs keyword-only.
- `search` accepts `mode` and `maxTokens`; responses carry `estimatedTokens`.
  The SessionStart injection is capped at `M3M0S_INJECT_MAX_TOKENS` (default
  1000, `0` = no cap) and the WebUI preview reports its token estimate.
- Search and dedup use an in-memory int8 vector index with exact re-ranking
  (D31): 20x faster and 140x less allocation per query on a 27k-entry domain.
- Long session digests are split into chunks at message boundaries instead of
  being truncated at 40000 characters.

### Fixed

- Memory: the in-memory vector index no longer reallocates (and briefly doubles) its
  ~120 MB backing array on the first insert after boot; vectors now live in fixed-size
  blocks. Cuts the post-boot RSS peak by ~80 MB on a 78k-entry index.
- Re-embedding progress no longer ends as "62/81" when rows leave the pending set during
  the run (deleted, or re-embedded by a normal edit): they are now reported as `skipped`
  and `done + errors + skipped == total` after a clean run.
- Search no longer fails for a domain with more than ~32,700 entries (SQLite's
  bound-variable limit).
- A failed capture no longer loses data: the per-session checkpoint advanced
  before the POST, so a failure dropped those transcript lines for good.

## [0.1.0]

Initial public release.

### Added

- Single static Go binary (no CGO) with `serve` (HTTP API), `mcp` (MCP server),
  `hook` (agent hooks), `watch` (file watcher) and CLI commands.
- HTTP API under `/api/v1` with API key authentication; `/health` endpoint.
- MCP server exposing memory tools to any MCP-capable client.
- Memory model with four orthogonal scope/domain axes.
- LLM-based fact extraction from sessions and documents, chunked, with forced
  JSON output and per-chunk retry.
- Deduplication, lightweight knowledge graph and temporal facts
  (`valid_until` set on contradiction).
- Automatic forgetting with decay and `pinned` memories; memory layers.
- Claude Code hooks for automatic capture (`PreCompact`, `SessionEnd`) and
  context injection (`SessionStart`), with secret redaction.
- Asynchronous add with a short timeout.
- In-process job system with history, manual runs and runtime interval control.
- SQLite storage with versioned migrations; deduplicated blob store for source
  content.
- Embedded React WebUI: browse and edit memories, jobs, injection preview and a
  floating chat (RAG questions, saving via tool calling).
- Embedded project documentation, self-indexed at startup.
- Golden queries and `recall@k` memory evaluation.
- Docker image (multi-stage build, non-root, health check).

[Unreleased]: https://github.com/rafaelvieiras/m3m0s/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/rafaelvieiras/m3m0s/releases/tag/v0.1.0
