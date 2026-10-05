# D32 — Hybrid search, optional embeddings and an injection token budget

**Status:** Accepted

**Context:** after [D31](D31-int8-vector-index-exact-rerank.md) made vector search fast, the rest of the search issue was quality and cost: search was vector-only (exact identifiers and names are blurred by embeddings), a missing or failing embedding provider broke search and add, and injection had no token budget.

**Decision:**
- **FTS5 keyword index** (`entries_fts`, migration 00002, `unicode61` with accent folding, kept in sync by triggers). Queries are built as an OR of quoted terms (max 24), ranked by bm25.
- **Search modes** `hybrid` (default), `vector`, `keyword`. Hybrid is the exact-cosine-ordered vector list fused by reciprocal rank (k=60) with only the **3 best accepted keyword hits**. Expired, soft-deleted and filtered-out entries are removed from both lists before fusion so they never hold rank positions; the int8 index only selects candidates, the vector list is re-sorted by exact cosine before fusing.
- **Optional embeddings:** with no key the server runs keyword-only (entries stored without a vector, exact-content dedup, FTS search, no `score`). If the provider fails at search time, hybrid degrades to keyword; explicit `vector` returns the error.
- `Add` embeds the content once (the dedup lookup vector is reused for storage).
- **Token budget:** `maxTokens` on search and `M3M0S_INJECT_MAX_TOKENS` (default 1000) on the SessionStart hook, using a 4-characters-per-token estimate over the rendered snippet; lower-ranked results are dropped first. Responses and the injection preview expose `estimatedTokens`. "Tokens actually used" cannot be measured reliably from the server side, so the metric is injected tokens, reported by `m3m0s eval` as mean tokens per query.

**Evidence (`m3m0s eval`, 38 golden queries over 7 real domains: 18 paraphrases written from sampled memories, 20 more with half exact-term queries; recall of the single expected id):**

| mode | recall@1 | recall@3 | recall@5 | recall@10 |
|---|---|---|---|---|
| vector | 0.711 | 0.842 | 0.947 | 0.974 |
| keyword | 0.711 | 0.895 | 0.895 | 0.921 |
| hybrid | 0.842 | 0.895 | 0.947 | 0.974 |

Fusing deeper keyword lists (all hits, or 8+) improved recall@1 but cost recall@5 and @10 — weak keyword hits displaced correct vector hits — so keyword only breaks ties near the top: depth 3 matched or beat vector at every k.

**Caveat:** the dataset is small and the depth was chosen on it, so differences of one query (0.026) are noise. The dataset is private (real memories) and lives outside the repository; its format is documented in [evaluation](../evaluation.md). Grow it before retuning.

**Not done:** a local or pluggable embedding provider beyond the existing OpenAI-compatible base URL setting; per-client budget configuration other than the hook environment variable.
