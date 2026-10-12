# D40 — Durable ingest queue with a bounded worker pool (extends D26)

**Status:** Proposed

**Extends:** [D26](D26-async-add-short-timeout.md) (async add with a short timeout), [D12](D12-llm-fact-extraction.md)/[D18](D18-chunked-fact-extraction.md)/[D19](D19-forced-json-retry.md) (extraction). Issue #65.

**Context:** since D26 `POST /memories` answers within ~300 ms (200 or 202), but the work itself — fact extraction, then `addOne` per fact — runs in an unbounded goroutine per request and its result lives in an in-memory tracker. Production on 2026-10-12 showed three consequences when the extraction provider became slow:

1. **No durability.** A restart mid-extraction loses the operation. Since the hook treats a 202 as delivered (#66), the capture is gone with no spool copy.
2. **No backpressure.** Every request and every piece starts its own provider call; a spool replay plus live captures plus the retroactive job produce dozens of concurrent calls, which makes a slow provider slower.
3. **Failure becomes noise.** A piece that times out is stored raw immediately and stays raw until the retroactive job (every 2 h, 25 per run) repairs it.

**Decision, in 2 stages:**

1. **Bounded pool, no schema change.** `Add` hands the extraction to a pool of N workers (`M3M0S_INGEST_WORKERS`, default 2) fed by an in-memory queue of bounded size. A piece that fails is retried with backoff (30 s, 2 min, 10 min) before it is stored raw; the raw entry is tagged `metadata.extraction=raw-fallback`. The 300 ms wait and the 200/202 contract of D26 are unchanged.
2. **Durable queue.** The accepted request (already redacted and boilerplate-filtered, which are cheap and synchronous) is written to `ingest_queue(id, payload, status, attempts, next_attempt_at, error, created_at)` in SQLite *before* answering. `operationId` is the row id; `GET /memories/pending/:id` reads the table, so an operation survives a restart. Workers claim rows, extract (primary model, then `M3M0S_EXTRACT_FALLBACK_MODEL`), write the facts and mark the row done. After the last attempt the piece is stored raw with the `raw-fallback` tag.

**Invariants:**

- Redaction (D21/#59) and the ingest filter (#60) run before enqueue; the queue never holds an unredacted secret.
- One worker at a time per domain, so dedup and the contradiction classifier (D15) see entries in arrival order.
- The queue is bounded (`M3M0S_INGEST_QUEUE_MAX`, default 5,000 rows); above it `POST` answers `503` with `Retry-After`, and the hook keeps its own spool.
- Rows older than 7 days in `failed` are dead-lettered, not retried forever.
- `GET /api/v1/insights/overview` exposes queue depth, oldest age, retries and raw-fallback count.

**Trade-offs accepted:** a captured fact is searchable only after extraction (seconds to minutes) — fine for automatic capture; an explicit `memory_add` keeps the short synchronous wait of D26. Until stage 2 ships, the hook's "202 = delivered" rule (#66) has a restart window; it is reverted if stage 2 is delayed.

**Out of scope:** changing the extraction prompt or model choice; a separate queue service (SQLite is enough at this volume).

**Acceptance:** killing the process mid-extraction loses no accepted capture; a replay of 100 payloads never exceeds N concurrent provider calls; a provider outage of an hour ends with zero raw entries once it recovers and the retries run.
