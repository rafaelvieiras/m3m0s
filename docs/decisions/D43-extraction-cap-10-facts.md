# D43 — Raise the extraction cap from 6 to 10 facts

**Status:** Accepted (supersedes the cap number in [D41](D41-bounded-extraction-output.md); the `max_tokens` ceiling and the "short facts" rule stay)

**Context:** D41 accepted that a dense piece may hold more than 6 durable facts and said to raise the cap in the prompt if recall mattered more than latency. Measured on 16 real 7–8k-character pieces from production (`deepseek/deepseek-v4.1-flash`, cap 6 vs. cap 20, extra facts judged by the same model for "durable and not already covered"): cap 6 kept 94 facts; uncapped extraction found 268, of which **120 durable and uncovered were lost (~7.5 per piece, all 16 pieces affected)**. Part of the lost facts is implementation detail, part is real decisions and rules.

**Latency by cap (same pieces):**

| Cap | Mean facts | Median | p90 | Max |
|---|---|---|---|---|
| 6 | 6.0 | 11.6s | 13.2s | 13.4s |
| 10 | 9.4 | 18.7s | 23.2s | 23.2s |
| 12 | 11.1 | 21.1s | 27.7s | 27.8s |

**Decision:** the prompt asks for **at most 10 facts**. `max_tokens=1024` and the 45s budget are unchanged. With a fallback model configured the primary gets ~27s; the 10-fact worst case (23s) fits, 12 would not with margin.

**Trade-off:** ~60% more output per piece, so ~1.6x the extraction latency and tokens. Dense pieces still lose their tail beyond 10; the raw source is kept either way.

**Not done:** re-extracting what was already extracted under the cap of 6 (pieces stay as they are; new ingestion uses the new cap).
