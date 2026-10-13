# D41 — Bound the extraction output (at most 6 short facts, `max_tokens` ceiling)

**Status:** Accepted

**Context:** after [D40](D40-durable-ingest-queue.md) made ingestion durable, extraction kept failing with `read response: context deadline exceeded` (~0.7–0.9 per minute in production, with both `deepseek/deepseek-v4-flash` and `deepseek/deepseek-v4.1-flash`). Reasoning was already off ([D20](D20-disable-extraction-model-reasoning.md)), so the cause was not thinking time. A streamed call over a real 8,000-character piece showed it: time to first token 1.4s (v4) / 6.6s (v4.1), then **~33s / ~46s of pure generation**. The prompt asked for "atomic, self-contained facts" with no cap on how many, and the request had no `max_tokens`, so the model wrote as much as it liked, at the slow tail speed of the provider. Same-piece retries (queue attempts 1–3) failed identically: the piece is consistently slow, not unlucky.

**Decision:**
- The system prompt asks for **at most 6 facts**, most important first, **one short sentence each (~25 words)**, and to skip noise (boilerplate, diff trivia, descriptions of the text) instead of emitting it as `noise` facts. `noise` is no longer offered as a kind in the extraction prompt (the parser still accepts it; the classification prompt is unchanged).
- The extraction call sets `max_tokens=1024` (`extractMaxTokens`). The expected output is ~250 tokens; the ceiling only stops a runaway generation from holding an ingest worker. A reply cut by it is invalid JSON and takes the usual raw-fallback path.

**Measured (same four ~8k-char pieces, real model):** v4.1-flash 45s timeout on every piece → 11–15s, 6 facts, ~250 output tokens; v4-flash ~34s → 12–25s.

**Trade-off:** a dense piece can hold more than 6 durable facts; the extra ones are not extracted. Pieces are at most 8,000 characters ([D18](D18-chunked-fact-extraction.md)), so the loss is bounded per piece, and the raw source of an extracted piece is still recorded. If recall turns out to matter more than latency, raise the cap in the prompt, not `max_tokens`.

**Rejected:** giving the first attempt a smaller share of the budget so the retry can run. The failure is long generation, which a retry in the remaining time cannot beat, and the existing test deliberately forbids retrying after a genuine timeout.

**Not done:** streaming the response (would show progress, not make generation faster), splitting pieces smaller than 8,000 characters (more calls, same total output).
