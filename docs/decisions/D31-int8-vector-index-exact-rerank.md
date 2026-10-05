# D31 — In-memory int8 vector index with exact re-ranking

**Status:** Accepted

**Context:** [D28](D28-sqlite-replaces-json-and-chromem.md) kept exact brute-force cosine search, reading every embedding of the domain from SQLite on each `Search` and each `Add` (dedup). Measured on a synthetic single domain of 27,000 entries (1536 dimensions, the size of the largest real domain): **865 ms per query, 538 MB allocated per query, 269 MB peak heap**. It also failed outright for a domain above ~32,700 entries, because the candidate IDs were bound into one `IN (...)` statement (SQLite's variable limit).

**Decision:** keep a compact vector index in memory (`internal/store/vecindex.go`):
- vectors are L2-normalized and quantized to int8 with one scale per row (4x smaller than float32: about 105 MB for 68k entries);
- built at boot by streaming the `embedding` column once (`sqlstore.ForEachEmbedding`), then kept in sync on add, update and delete;
- a query ranks candidates with an int8 dot product (domain filtered by a per-row domain code; all domains in a single pass when no domain is given) and keeps the best 200 (at least 10x the requested limit);
- the survivors are re-scored with the **exact** float32 vectors read from SQLite (`EmbeddingsByIDs`, now chunked below the variable limit), so quantization error never reaches the returned scores;
- when filters (category, source, tags, expired, soft-deleted) leave fewer results than requested, the candidate window doubles until satisfied or the index is exhausted.

**Result (same synthetic data, same machine):** 27k entries: 42 ms and 3.8 MB allocated per query (20x faster, 140x less allocation); 68k entries in one domain: 86 ms (previously an error). Recall of the exact top-10 within the index's top-100 is at least 0.99 in `TestVecIndexTopMatchesExactRanking`.

**Alternatives considered:** an HNSW/ANN index (more code and a build step for a dataset that a linear int8 scan handles in tens of milliseconds); `sqlite-vec` (needs cgo, breaks the static binary, see D28); float16 or float32 resident vectors (2x or 4x the memory on a small host).

**Not covered here:** hybrid keyword + vector ranking, optional embedding provider, and the injection token budget (remaining items of the F5 issue).
