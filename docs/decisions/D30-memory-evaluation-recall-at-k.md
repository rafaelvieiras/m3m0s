# D30 — Memory evaluation with a small scope: golden queries + `recall@k` (closes A10)

**Status:** Accepted

**Context (23 Jul 2026):** [A10](A10-memory-evaluation.md) was recorded as low priority, conditioned on reopening [A6](A6-embeddings-provider.md) (embeddings) — but the maintainer reconsidered the argument: m3m0s has already gone through real changes that affect retrieval with no objective measurement ([D28](D28-sqlite-with-versioned-migrations.md) replaced the whole vector layer, from chromem-go to cosine in Go over SQLite; [D18](D18-chunked-fact-extraction.md)–[D20](D20-disable-extraction-model-reasoning.md) changed how facts are extracted and become searchable), and the only validation until today was "eyeballing" in the WebUI. `recall@k` becomes the read-side counterpart of what the tests already cover on the write side — it does not depend on reopening [A6](A6-embeddings-provider.md) to have value.

**Decision:** implement with a small scope, not a comprehensive evaluation framework:
- **A small golden dataset (10–20 queries)**, covering the domains already in use today (`codigo:m3m0s`, and the maintainer's other real domains as they get populated) — not a generic synthetic dataset.
- Each golden query points to the free-text query + the expected `Entry` `id`(s) as the result — not expected text (it avoids coupling to exact wording, which changes with re-extraction).
- A command/endpoint that runs the dataset against the real search and reports `recall@k` (small k, e.g. 3 and 5) — the surface is still to be decided at implementation (likely: a CLI subcommand, since it is a development/regression tool, not a business operation that `pi`/scripts need to call — the same restricted rationale as [D23](D23-webui-floating-llm-chat.md)/[D29](D29-injection-context-preview.md)).
- No automated CI nor deploy gate in this v1 — run manually before changes to embedding/extraction/chunking, not on every commit.

**Out of scope for this decision:** automatic curation of the dataset (growing by itself with real usage), metrics beyond `recall@k` (precision, MRR), any integration with [A11](A11-memory-feedback.md) (the feedback loop, which remains not recommended).
