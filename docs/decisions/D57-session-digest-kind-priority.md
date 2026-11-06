# D57 — Session-start digest prefers rules and preferences (refines D17)

**Status:** Accepted

**Context:** [D17](D17-session-start-injection.md) builds the injected digest from `GET /recent` alone: the 15 most recent extracted memories of the session's domain, capped at ~1000 estimated tokens. That was calibrated when the store was small.

In production the capture rate is tens of thousands of facts per day, so a `/recent` window only ever covers the last minutes: the digest shows whatever the previous session happened to write, and the project's standing rules and the user's preferences — the entries that actually transfer between sessions — never appear unless nothing newer exists.

**Decision:**

- The digest is assembled from one plain `/recent` fetch plus one fetch per **priority kind**, `M3M0S_INJECT_KINDS` (default `rule,preference`), all in parallel under the same 5 s ceiling.
- Merge order is the configured kind order, then the plain fetch as filler; duplicates are dropped by id. Each priority kind is capped at `(SessionStartRecentLimit - digestFillerReserve) / len(kinds)` (6 with the default two kinds), so `digestFillerReserve = 3` slots always stay for plain recency — a digest made only of standing rules loses the "what were we doing" thread that `/recent` exists to provide.
- Entries still go through `InjectableEntries` (raw fallbacks dropped, hard cap of 15) and `FitTokenBudget`, exactly as in D17, so the token ceiling of D17 is unchanged.
- A failed kind fetch is skipped, not fatal: the digest is best-effort. `M3M0S_INJECT_KINDS=off` restores the single-`/recent` behaviour.
- The WebUI injection preview (D29, `mode=hook`) builds the digest with the same function, so what the preview shows is what the hook injects.

**Why not reorder a bigger `/recent` window in the client:** reaching an older rule would need thousands of entries at this capture rate, and the store has no `kind + recency` index, so the filter costs a full scan per call.

**Not done:** ordering by importance inside a kind (there is no importance field; the order stays the server's recency), weighting recalled-but-unused entries down, and any per-project rule selection beyond the D34 layered scope.
