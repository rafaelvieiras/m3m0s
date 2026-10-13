# Decision records

A lightweight decision log (the equivalent of ADRs, without the heavy format yet — a full ADR format should be adopted once the Go core is stable and structural changes start happening frequently).

## Convention

- One file per record, named `D<N>-<short-kebab-title>.md` for decisions and `A<N>-<short-kebab-title>.md` for open questions ("A" items).
- Each file starts with an H1 (`# D15 — <title>`) followed by a one-line status: **Accepted**, **Closed (date)** or **Open**.
- IDs are stable and never reused. D-numbers follow the order in which decisions were made; A-numbers are questions that were open at some point and may be closed by a decision (the file then points to the closing `D` record).
- Records are historical: they keep the context, alternatives and reasoning as of the date they were written. Later elaborations and corrections are appended to the record they refine instead of creating a new one. Field and endpoint names are those in use at the time of writing; the API contract in [`../api-contract.md`](../api-contract.md) is the source of truth for the current shape.
- A1–A5 were closed early (see D7, D8, D9, D10 and D11) and have no file of their own.

## Index

| ID | Title | Status | File |
|----|-------|--------|------|
| D1 | Existing memory layers stay separate by function | Accepted | [D1-existing-memory-layers-stay-separate.md](D1-existing-memory-layers-stay-separate.md) |
| D2 | Stack: Go core, TypeScript only where required | Accepted | [D2-stack-go-core-typescript-only-where-required.md](D2-stack-go-core-typescript-only-where-required.md) |
| D3 | Decentralized execution: core vs watch | Accepted | [D3-decentralized-execution-core-vs-watch.md](D3-decentralized-execution-core-vs-watch.md) |
| D4 | Host: Docker on the desktop first, a small ARM host later | Accepted | [D4-host-docker-on-desktop-first.md](D4-host-docker-on-desktop-first.md) |
| D5 | Name: M3m0s (was "Engram" until 15 Jul 2026) | Accepted | [D5-name-m3m0s.md](D5-name-m3m0s.md) |
| D6 | Analysis of getengram.app (external reference, 15 Jul 2026) | Accepted | [D6-getengram-analysis.md](D6-getengram-analysis.md) |
| D7 | License: MIT | Accepted | [D7-license-mit.md](D7-license-mit.md) |
| D8 | Remote repo created on GitHub as private | Accepted | [D8-private-github-repo.md](D8-private-github-repo.md) |
| D9 | `scope`/`domain` schema: 4 orthogonal axes | Accepted | [D9-scope-domain-schema-four-axes.md](D9-scope-domain-schema-four-axes.md) |
| D10 | `pi` adapter: dropped, `pi` already has a generic MCP client (closes A5) | Accepted | [D10-pi-adapter-dropped.md](D10-pi-adapter-dropped.md) |
| D11 | `m3m0s watch` mechanism (closes A4) | Accepted | [D11-watch-mechanism.md](D11-watch-mechanism.md) |
| D12 | LLM fact extraction (Phase 4) | Accepted | [D12-llm-fact-extraction.md](D12-llm-fact-extraction.md) |
| D13 | Scope of the admin WebUI (Phase 5) | Accepted | [D13-webui-admin-scope.md](D13-webui-admin-scope.md) |
| D14 | Auto-capture via hook (Claude Code, Phase 8) | Accepted | [D14-claude-code-hook-auto-capture.md](D14-claude-code-hook-auto-capture.md) |
| D15 | Phase 7 mechanism (light KG + temporal fact, closes A8) | Accepted | [D15-light-kg-temporal-fact.md](D15-light-kg-temporal-fact.md) |
| D16 | In-process job system (periodic routines) | Accepted | [D16-in-process-jobs.md](D16-in-process-jobs.md) |
| D17 | Automatic memory injection via `SessionStart` (closes the "read" side left pending by Phase 8/D14) | Accepted | [D17-sessionstart-memory-injection.md](D17-sessionstart-memory-injection.md) |
| D18 | Fact extraction split into chunks (closes the root cause of the recurring raw fallback, found while investigating D17) | Accepted | [D18-chunked-fact-extraction.md](D18-chunked-fact-extraction.md) |
| D19 | Forced JSON + per-chunk retry in extraction (found in production after D18 was already running) | Accepted | [D19-forced-json-retry.md](D19-forced-json-retry.md) |
| D20 | Turn off `reasoning` on the extraction model (found in production minutes after D19 was running) | Accepted | [D20-disable-extraction-model-reasoning.md](D20-disable-extraction-model-reasoning.md) |
| D21 | Deduplicated blob store for `metadata.source_content` (closes A7) | Accepted | [D21-deduplicated-source-blob-store.md](D21-deduplicated-source-blob-store.md) |
| D22 | Memory layers / automatic forgetting, inspired by ai-memory (closes A9) | Accepted | [D22-memory-layers-automatic-forgetting.md](D22-memory-layers-automatic-forgetting.md) |
| D23 | Floating LLM chat in the WebUI (ask via RAG, save via tool-calling) | Accepted | [D23-webui-floating-llm-chat.md](D23-webui-floating-llm-chat.md) |
| D24 | `m3m0s watch` pointed at the project's own repo + per-chunk/file failure resilience | Accepted | [D24-watch-on-own-repo.md](D24-watch-on-own-repo.md) |
| D25 | Documentation embedded in the binary + self-indexing at boot (production, replaces D24 outside the dev desktop) | Accepted | [D25-embedded-docs-self-indexing.md](D25-embedded-docs-self-indexing.md) |
| D26 | Async add with a short timeout (`POST /memories` stops blocking the caller for seconds) | Accepted | [D26-async-add-short-timeout.md](D26-async-add-short-timeout.md) |
| D27 | In-process job visibility and control in the WebUI (history, manual run, runtime interval) | Accepted | [D27-webui-jobs-visibility-control.md](D27-webui-jobs-visibility-control.md) |
| D28 | Replace the hand-rolled JSONs + chromem-go with SQLite and versioned migrations | Accepted | [D28-sqlite-with-versioned-migrations.md](D28-sqlite-with-versioned-migrations.md) |
| D29 | Context-injection preview in the WebUI, before implementing new hooks/integrations | Accepted | [D29-injection-context-preview.md](D29-injection-context-preview.md) |
| D30 | Memory evaluation with a small scope: golden queries + `recall@k` (closes A10) | Accepted | [D30-memory-evaluation-recall-at-k.md](D30-memory-evaluation-recall-at-k.md) |
| D31 | In-memory int8 vector index with exact re-ranking | Accepted | [D31-int8-vector-index-exact-rerank.md](D31-int8-vector-index-exact-rerank.md) |
| D32 | Hybrid search, optional embeddings and an injection token budget | Accepted | [D32-hybrid-search-and-token-budget.md](D32-hybrid-search-and-token-budget.md) |
| D33 | Provenance fields: `model` and optional `project` | Accepted | [D33-provenance-model-and-project.md](D33-provenance-model-and-project.md) |
| D34 | Project scope: layered recall, overrides and session detection | Accepted | [D34-project-scope-layered-recall.md](D34-project-scope-layered-recall.md) |
| D35 | Accounts: onboarding, sessions and CLI-issued password reset | Accepted | [D35-accounts-onboarding-sessions-reset.md](D35-accounts-onboarding-sessions-reset.md) |
| D36 | Runtime embedding configuration and vector spaces | Accepted | [D36-runtime-embedding-config.md](D36-runtime-embedding-config.md) |
| D37 | Memory health dashboard: live overview and a read-only hygiene job | Accepted | [D37-insights-dashboard.md](D37-insights-dashboard.md) |
| D38 | Fact nature (`kind`): closed enum from the extractor, a heuristic fallback, stored in metadata | Accepted | [D38-fact-kind.md](D38-fact-kind.md) |
| D39 | Per-kind retention: a half-life by nature of fact (revises D22), shadow mode first | Accepted (shadow; default `legacy`) | [D39-per-kind-retention.md](D39-per-kind-retention.md) |
| D40 | Durable ingest queue with a bounded worker pool (extends D26) | Accepted | [D40-durable-ingest-queue.md](D40-durable-ingest-queue.md) |
| D41 | Bound the extraction output (at most 6 short facts, `max_tokens` ceiling) | Accepted | [D41-bounded-extraction-output.md](D41-bounded-extraction-output.md) |
| D42 | "No durable fact" is a settled outcome, not an extraction failure | Accepted | [D42-no-durable-facts-is-not-a-failure.md](D42-no-durable-facts-is-not-a-failure.md) |
| A6 | Embeddings provider | Closed (16 Jul 2026) | [A6-embeddings-provider.md](A6-embeddings-provider.md) |
| A7 | `metadata.source_content` duplicates the original text per extracted fact | Closed (16 Jul 2026), see [D21](D21-deduplicated-source-blob-store.md) | [A7-source-content-duplication.md](A7-source-content-duplication.md) |
| A8 | Phase 7 (light KG + temporal fact) | Closed (16 Jul 2026), see [D15](D15-light-kg-temporal-fact.md) | [A8-light-kg-temporal-fact.md](A8-light-kg-temporal-fact.md) |
| A9 | Compression of old low-value memories | Closed (17 Jul 2026), see [D22](D22-memory-layers-automatic-forgetting.md) | [A9-old-memory-compression.md](A9-old-memory-compression.md) |
| A10 | Memory evaluation (`recall@k`) | Closed (23 Jul 2026), see [D30](D30-memory-evaluation-recall-at-k.md) | [A10-memory-evaluation.md](A10-memory-evaluation.md) |
| A11 | Feedback loop (`memory_feedback`) | Open (preliminary recommendation: do not implement) | [A11-memory-feedback.md](A11-memory-feedback.md) |
