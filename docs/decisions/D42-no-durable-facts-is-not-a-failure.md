# D42 — "No durable fact" is a settled outcome, not an extraction failure

**Status:** Accepted

**Context:** after [D41](D41-bounded-extraction-output.md) the timeouts were gone (0 `deadline exceeded` in ~18h), but pieces kept falling into raw fallback with `no non-empty fact extracted`. The model had answered well-formed and found nothing durable. The parser treated that as an error, so each occurrence cost a second attempt, an ingest-queue retry (30s/2m/10m) and, for raw entries, a re-extraction by the retroactive job every 2h that always returned the same empty answer (one `codigo:orion-media-share` entry was re-asked five times in a day).

**Decision:**
- `extract.ErrNoFacts` is a sentinel for "valid answer, zero usable facts". `extractChunk` does not retry it.
- The piece is kept raw (nothing is dropped: an empty answer from a model may be wrong) and tagged `metadata.extraction=no-durable-facts` instead of `raw-fallback`.
- `Add` with `RequireExtraction` treats it as satisfied, so the ingest queue does not retry it.
- The retroactive job skips entries tagged `no-durable-facts`. When it finds a raw entry whose every piece settles as no-facts, it tags the entry in place (no delete/reinsert, the ID and relations are kept).

**Not changed:** real failures (timeout, invalid JSON, empty HTTP body) keep the retry and `raw-fallback` path. The prompt still does not offer `{"facts": []}`.

**Trade-off:** an entry wrongly judged empty is no longer re-asked automatically. It stays searchable raw; clearing the tag (memory_update metadata) makes it a candidate again.
