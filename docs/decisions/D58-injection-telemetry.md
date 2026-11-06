# D58 — Injection telemetry, separate from reads

**Status:** Accepted

**Context:** `access_count`/`last_accessed_at` (D22) is bumped by `Get` and by the primary results of a `Search`. Per-prompt recall ([D44](D44-per-prompt-memory-search.md)) goes through `Search`, so an entry injected automatically into a prompt counted as "accessed". The session-start digest ([D17](D17-session-start-injection.md), [D57](D57-session-digest-kind-priority.md)) goes through `Recent`, which counted nothing. Neither matches what the metric is read as.

The practical effect: the WebUI and `/insights/overview` report "95% never accessed" as if the memory were dead, while part of that population is injected on every session and part is never served at all. There is no way to tell "the memory is not useful" from "the memory is never served".

**Decision:**

- Clients declare the intent of a call: `purpose: "inject"` on `POST /search` (body field) and `GET /recent` (query param). Anything else, including the default, is a deliberate read.
- The store keeps two independent counters per entry: `access_count`/`last_accessed_at` for reads, and `inject_count`/`last_injected_at` for injections. A call bumps exactly one of them; an injection never bumps the read counter.
- `Recent` bumps `inject_count` only when `purpose=inject`; the CLI, the WebUI and MCP `memory_recent` stay plain reads of the listing (they are not injected into a model by the server).
- The hook sends `purpose=inject` for both mechanisms it owns (per-prompt search and the session digest).
- **Only reads feed decay.** `touchInjection` never touches `access_count`, so an entry that is injected constantly cannot extend its own life — that would be a self-reinforcing loop with no evidence that the entry helped.
- `/insights/overview` totals gain `injected`, `never_injected` and `never_injected_pct` next to the existing access fields, which gives the four-way split the metric could not express before: never served, served but never read, read but never served (manual lookups), and both.

**Consequences:** `inject_count` is written on every digest and every prompt injection, which is one `UPDATE metadata_json` per returned entry — the same cost `touchAccess` already paid on search, and the digest touches at most 15 rows per session.

**Not done:** recording which prompt or session caused an injection (the counters are aggregated per entry), any usefulness signal (see [A11](A11-memory-feedback.md)), and feeding `inject_count` into ranking — deliberately, since that would reward being shown rather than being right.
