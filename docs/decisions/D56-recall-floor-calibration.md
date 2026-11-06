# D56 — Recall floor calibration (refines D44, D47)

**Status:** Accepted

**Context:** [D44](D44-per-prompt-memory-search.md) set the per-prompt cosine floor at 0.60 from 4 production prompts, with "little margin" already noted at the time. By early November the store had ~85k entries and 95% of them had never been read, so it was worth re-measuring instead of trusting the small sample.

A sample of 15 realistic prompts (in the author's language, phrased the way he actually asks, across several unrelated projects), run through `m3m0s hook claude-code --inject` against production, gave:

| Floor | Prompts injected | Notes |
|---|---|---|
| 0.60 | 8/15 | a memory-retention question peaked at 0.588, a deployment question at 0.59 and a hardware-troubleshooting one at 0.57 — all dropped |
| 0.55 | 12/15 | the three misses above are injected; the two remaining misses had no matching memory in the store |
| 0.50 | 14/15 | starts pulling wrong-project noise (two of the prompts matched a hit from an unrelated project above 0.60) |

The foreign-project miss was separate: a question about deploying project A, asked while the working directory was project B, peaked at 0.642. The 0.60+0.10 foreign floor dropped it even though the prompt named project A explicitly.

**Decision:**

- `promptDefaultScore` is **0.55** (still overridable with `M3M0S_PROMPT_MIN_SCORE`).
- The +0.10 penalty for another `code:` project does **not** apply when the prompt mentions that project by name (accent-folded, case-insensitive substring of the domain suffix). The penalty exists to keep unrelated auth/token talk from dragging a different repo's notes in; a named project is a deliberate cross-project read, so it uses the plain floor.
- The D47 lexical gate is unchanged: a hit still has to share a topic-bearing term with the prompt, which is what keeps 0.55 from importing generic boilerplate.

**Consequences:** roughly 50% more prompts get context (8/15 → 12/15 on the sample), at the same 3-entry/500-token cap, so the token cost per a *matching* prompt does not change. Precision is slightly lower; the observed wrong-project hit at 0.50 is why the floor stops there. The measurement is manual and small — the eval harness of [D30](D30-memory-evaluation-recall-at-k.md) (`scripts/synthetic-golden.py`, `internal/eval`) is the tool to turn this into a repeatable number.

**Not done:** per-kind or per-domain weighting in the score, reranking, and learning the floor from feedback (see [A11](A11-memory-feedback.md)).
