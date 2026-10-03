# A10 — Memory evaluation (`recall@k`)

**Status:** Closed (23 Jul 2026), see [D30](D30-memory-evaluation-recall-at-k.md)

**Context (draft, 16 Jul 2026):** a low-priority gap in the original benchmark, which never became a decision nor a phase. Mem0 has a built-in `evaluate()`; no other competitor researched has this natively.

**Proposal:** a fixed set of known questions ("golden queries") with the expected memory as the answer, a command/endpoint that runs these queries and reports recall@k — useful mainly to validate future changes to embedding/extraction without a silent regression in search quality.

**Real priority (original assessment):** low — it would only pay off if the maintainer anticipated touching embedding/extraction again (e.g. reopening [A6](A6-embeddings-provider.md)).

**Closed on 23 Jul 2026, see [D30](D30-memory-evaluation-recall-at-k.md).** Reconsidered under the argument of memory quality assurance, not just protection against reopening [A6](A6-embeddings-provider.md): m3m0s already suffered real changes in the retrieval path without objective measurement ([D28](D28-sqlite-with-versioned-migrations.md), [D18](D18-chunked-fact-extraction.md)–[D20](D20-disable-extraction-model-reasoning.md)), so the value of `recall@k` does not depend on [A6](A6-embeddings-provider.md). The scope was closed small on purpose — a golden dataset of 10–20 queries across the real domains in use, with no comprehensive evaluation framework.
