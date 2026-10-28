# Benchmark vs. market — Claude-Mem, Mem0, Cognee, Zep

> **Read this with care.** The competitor columns are a good-faith summary of
> each project's public documentation as of mid-2026. These projects move
> fast, so some cells are probably out of date, and anyone who maintains one
> of them is welcome to correct this file with a pull request. The m3m0s
> column describes this repository at the time of writing and is the part we
> can vouch for. This is a positioning aid, not a ranking: the projects make
> different trade-offs for different users.

## 1. What m3m0s optimizes for

One memory shared by every agent tool, self-hosted, light to run: a single
static Go binary backed by one SQLite file, reachable over MCP, CLI and HTTP,
with a built-in web UI. Heavier graph features and enterprise governance are
deliberately out of scope.

## 2. Feature matrix

✅ supported · ⚠ supported in a simpler or narrower form · ❌ not offered ·
— not applicable or unknown to us.

| Feature | m3m0s | Claude-Mem | Mem0 | Cognee | Zep |
|---|---|---|---|---|---|
| Auto-capture from agent sessions | ✅ Claude Code, pi, opencode, Hermes, Open WebUI (hooks/plugins/filter) | ✅ hooks | ✅ `add()` | ✅ `remember()` | ✅ `graph.add()` |
| LLM extraction into atomic facts | ✅ ([D12](decisions/D12-llm-fact-extraction.md)) | ✅ | ✅ | ✅ entity extraction | ✅ full graph |
| Scope isolation | ✅ domain / source / agent / tags ([D9](decisions/D9-scope-domain-schema-four-axes.md)) | ✅ per project | ✅ user/agent/run | ✅ datasets | ✅ users and groups |
| Vector search | ✅ in-process int8 index with exact re-rank ([D31](decisions/D31-int8-vector-index-exact-rerank.md)) | ✅ | ✅ | ✅ | ✅ |
| Keyword search | ✅ SQLite FTS5, hybrid by default ([D32](decisions/D32-hybrid-search-and-token-budget.md)) | ✅ | ⚠ | ⚠ | ✅ hybrid |
| Typo-tolerant queries | ✅ ([D51](decisions/D51-query-term-correction.md)) | — | — | — | — |
| Knowledge graph | ⚠ light `related_to` links, no graph database ([D15](decisions/D15-light-kg-temporal-fact.md)) | ❌ | ⚠ optional | ✅ core feature | ✅ core feature, temporal |
| Temporal facts (expiry, contradictions) | ✅ ([D15](decisions/D15-light-kg-temporal-fact.md)) | ❌ | ❌ | ❌ | ✅ |
| Forgetting / retention | ✅ decay and per-kind half-life ([D22](decisions/D22-memory-layers-automatic-forgetting.md), [D39](decisions/D39-per-kind-retention.md)) | ⚠ | ✅ | ❌ | ❌ |
| Prompt-ready context block and per-prompt recall | ✅ ([D17](decisions/D17-sessionstart-memory-injection.md), [D44](decisions/D44-per-prompt-memory-search.md)) | ✅ | ❌ raw results | ❌ | ✅ |
| MCP | ✅ `m3m0s mcp`, with a read-only mode ([D45](decisions/D45-mcp-read-only-mode.md)) | ✅ | ✅ | ✅ | ✅ |
| Embedding provider | ✅ any OpenAI-compatible endpoint, switchable at runtime ([D36](decisions/D36-runtime-embedding-config.md)) | ⚠ | ✅ | ✅ | ✅ |
| Works without an embedding key | ✅ keyword-only mode | — | — | — | — |
| Self-hosting | ✅ one container or static binary, SQLite | ⚠ multi-component | ⚠ heavier | ⚠ several stores | ❌ platform is cloud-only (the Graphiti engine is open source) |
| Small ARM boards | ✅ pure Go, no cgo, ARM builds | — | — | — | — |
| Web UI | ✅ search, edit, jobs, insights, chat | ✅ | ❌ | ❌ | ✅ |
| Accounts and sessions | ✅ ([D35](decisions/D35-accounts-onboarding-sessions-reset.md)) | — | — | — | ✅ |
| Secret redaction on ingest | ✅ automatic ([`internal/redact`](../internal/redact)) | ✅ `<private>` tags | ❌ | ❌ | ✅ |
| Memory health / hygiene reports | ✅ ([D37](decisions/D37-insights-dashboard.md)) | ⚠ | ❌ | ❌ | ❌ |

## 3. Known gaps and non-goals

| Item | Status | Note |
|---|---|---|
| Rich knowledge graph and self-improvement loops | Non-goal | The graph is deliberately light (no LLM in the relation step). |
| Enterprise governance, audit logs, SLA | Non-goal | m3m0s targets individuals and small teams. |
| Manual privacy markers such as `<private>` | Not implemented | Only automatic redaction exists; "sensitive but not detectable" text cannot be marked by hand. |
| Built-in language support | English-only built-in word lists | Memory content in any language works through the embedding model; the stoplists and heuristics are English ([D48](decisions/D48-english-domain-categories.md)). |
| Automated retrieval evaluation | Small scope | Golden queries with `recall@k`, run by hand ([D30](decisions/D30-memory-evaluation-recall-at-k.md)). |
| Feedback loop (`memory_feedback`) | Open, not recommended | Manual update and delete cover the use case ([A11](decisions/A11-memory-feedback.md)). |

---

See [`docs/decisions/`](./decisions/README.md) for the reasoning behind each
row.
