# A7 — `metadata.source_content` duplicates the original text per extracted fact

**Status:** Closed (16 Jul 2026), see [D21](D21-deduplicated-source-blob-store.md)

Found while explaining the "Extracted from:" field of the WebUI (Phase 5/[D13](D13-webui-admin-scope.md), item 3, 16 Jul 2026): each extracted fact (Phase 4/[D12](D12-llm-fact-extraction.md)) becomes an independent `Entry`/chromem-go document, and each one carries its **own complete copy** of the source text in `metadata.source_content` (`internal/store/store.go:284`, `withSourceContent`) — there is no reference/pointer between entries in chromem-go, only key-value per document. If 1 `Add` generates N facts, the original text is stored N times, not 1 time with N facts pointing to it.

It is not a bug, it is the current design — but the maintainer flagged that this "may scale sharply": the larger the input text (e.g. the digest of `m3m0s hook claude-code`, up to 40 thousand characters) and the more facts it generates, the more this duplication weighs in space. Left as a follow-up on purpose, not decided yet — a possible path is a separate "source document" schema, with facts referencing it by ID instead of copying the text, but that is a schema change (it affects [D9](D9-scope-domain-schema-four-axes.md)) that needs to be thought through calmly, not decided in a hurry. Do not implement without deciding with the maintainer first.

**Closed on 16 Jul 2026, see [D21](D21-deduplicated-source-blob-store.md):** the maintainer asked to tackle this after noticing the database growing fast in production (7MB with few conversations). The path (a blob store by hash, which this draft already sketched) and the migration of the already-existing data were confirmed before implementing.
