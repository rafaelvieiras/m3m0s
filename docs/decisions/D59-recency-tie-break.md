# D59 — Recency breaks near-ties in search results (refines D32)

**Status:** Accepted

**Context:** Ranking is pure similarity ([D32](D32-hybrid-search-and-token-budget.md)): `score` is the exact cosine, and nothing else influences order. In production this surfaced as a correctness problem rather than a relevance one — a question about the current state ("what changed this week") came back with months-old entries ahead of the current ones, because the same fact is captured repeatedly and the older copies sit a few thousandths of a cosine apart from the newer ones.

**Decision:**

- After ranking, `/search` re-orders **runs of consecutive results whose scores fall inside one 0.01 band**, newest first. A larger gap between two results is never overridden, so the ranking itself still decides what is relevant and a durable old rule keeps its place above a newer irrelevant hit.
- Entries without a score (keyword-only hits in hybrid mode) end the run and keep their position: they cannot be compared on cosine.
- The band is `store.RecencyWindow` (0.01), overridable with `M3M0S_SEARCH_RECENCY_WINDOW` (`0` disables it).
- `score` in the response is still the exact cosine similarity; only the order can change. No field, no score rewriting, so clients that re-sort by `score` see exactly the pre-D59 behaviour.

**Why not a score boost by age:** a weight on age is a policy that eventually ranks a new, vaguely-related memory above the correct old one, and it makes `score` mean two different things depending on configuration. The band only decides within noise.

**Not done:** a time-scoped query mode ("only results from this week"), which is the honest answer to "what changed this week" — it needs an explicit `since`/`until` filter rather than a ranking trick. Logged here as the next step if the band turns out not to be enough.
