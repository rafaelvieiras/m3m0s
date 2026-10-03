# Benchmark vs. market — Claude-Mem, Mem0, Cognee, Zep

> Origin: a comparison done on 14 Jul 2026, during the analysis phase of an
> earlier agent memory system, before m3m0s existed. This file is the
> **versioned-in-repo** version of that comparison, updated on 18 Jul 2026 to
> reflect m3m0s after D1–D27. Re-run/update whenever a new decision closes or
> opens a relevant gap — there is no need to re-import the original, what
> matters from it is already here.

## 1. The 9 gaps of the original benchmark — what changed

| Gap (14 Jul) | Original priority | Status today (18 Jul) | Where it was resolved |
|---|---|---|---|
| LLM-based extraction (raw text → atomic facts) | 🔴 high | ✅ done | D12 (Phase 4), sliced/retried in D18–D20 |
| Formatted context block (string ready for the prompt) | 🔴 high | ✅ done | `BuildContextBlock` — used in the `SessionStart` hook (D17) and in the chat (D23) |
| Light knowledge graph (`related_to` between memories) | 🟡 medium | ✅ done | D15 (Phase 7) — embedding similarity, no LLM, no Neo4j |
| Deduplication (same fact saved several times) | 🟡 medium | ✅ done | D12 (similarity floor) + D21 (blob store dedup of `source_content`) |
| Temporal fact (`valid_until`, memories that expire) | 🟡 medium | ✅ done | D15 — `Classifier` detects `contradicts`, marks the old one expired |
| MCP server (expose memory as an MCP tool) | 🟢 low | ✅ done | Phase 1/3 — `m3m0s mcp`, stdio↔HTTP bridge |
| Multi-embedding (Ollama/Cohere besides OpenAI) | 🟢 low | ❌ rejected | A6/D8 — evaluated and dropped; OpenAI is definitive |
| Memory evaluation (automated `recall@k`) | 🟢 low | ✅ decided, small scope | D30 (closes A10) — golden dataset of 10–20 queries, manual, no CI |
| Feedback loop (`memory_feedback(id, rating)`) | 🟢 low | ⏳ recorded, not recommended | A11 — manual `memory_update`/delete already cover the use case |

## 2. Full matrix, updated

Same structure as the original comparison (section 2 of the benchmark), with
the "Ours" column changed from the earlier system to m3m0s today. ⚠ marks
where the feature exists but is simpler than the competitors', or covers only
part of the original use case.

| Feature | m3m0s (today) | Claude-Mem | Mem0 | Cognee | Zep |
|---|---|---|---|---|---|
| Auto-capture | ⚠ Claude Code only | ✅ 7 hooks | ✅ `add()` | ✅ `remember()` | ✅ `graph.add()` |
| Session summary | ⚠ digest → extraction, no narrative | ✅ LLM, 5 handlers | ❌ manual | ✅ `cognify()` | ✅ automatic context |
| Compression | ✅ sliced concurrent extraction | ✅ facts+concepts+narrative | ❌ | ✅ summaries | ✅ temporal invalidation |
| Scope isolation | ✅ 4 axes: domain/source/agent/tags | ✅ per project | ✅ user/agent/run/app | ✅ datasets+node_sets | ✅ user graph+groups |
| LLM extraction | ✅ atomic facts | ✅ title/subtitle/narrative/facts/concepts/type | ✅ `infer=True` | ✅ entity extraction | ✅ full graph |
| Knowledge graph | ⚠ light — `related_to`, no Neo4j | ❌ (SQLite) | ⚠ optional, Neo4j | ✅ core feature | ✅ core feature, temporal |
| Vector search | ✅ chromem-go | ✅ ChromaDB | ✅ built-in | ✅ pluggable | ✅ vector store |
| Text search (FTS) | ❌ | ✅ FTS5 | ❌ | ❌ | ✅ hybrid |
| Temporal fact | ✅ `valid_until`/`contradicts` | ❌ | ❌ | ❌ | ✅ `valid_at`/`invalid_at` |
| Context block | ✅ `BuildContextBlock` | ✅ CLAUDE.md + timeline | ❌ raw strings | ❌ | ✅ prompt-ready |
| MCP support | ✅ `m3m0s mcp` | ✅ native | ✅ official | ✅ official | ✅ official |
| Self-hosting | ✅ Docker, single Go binary | ⚠ Node/Bun+SQLite+ChromaDB | ⚠ heavy OSS | ⚠ Docker/K8s, 3 DBs | ❌ cloud only * |
| Small ARM boards (e.g. Raspberry Pi) | ⏳ light enough, migration to a small ARM host pending (Phase 6) | ⚠ ~100-200MB | ❌ | ❌ | ❌ |
| Web UI | ✅ search/stats/jobs/chat (D13/D27/D23) | ✅ real-time SSE viewer | ❌ | ❌ | ✅ dashboard |
| Timeline / reports | ⚠ job history, no narrative | ✅ `/timeline-report` | ❌ | ❌ | ❌ |
| Token economics | ✅ `ExtractionTokensUsed`/`TokensSaved` in `/stats` | ✅ discovery_tokens+ROI | ❌ | ❌ | ❌ |
| Privacy controls | ⚠ automatic secret redaction, no manual tag | ✅ `<private>` tags | ❌ | ❌ | ✅ enterprise |
| Multilingual | — | ✅ 28 languages | ❌ | ❌ | ❌ |
| Forgetting / tiers | ✅ decay + soft-delete (D22) | ⚠ working+archive | ✅ conversation→session→user→org | ❌ | ❌ |
| Async add | ✅ configurable timeout + poll (D26) | — | — | — | — |
| Jobs with panel/control | ✅ history, manual trigger, runtime interval (D27) | — | — | — | — |
| Chat with tool-calling | ✅ RAG + `add_memory` via LLM (D23) | — | — | — | — |
| Tool validator / subagents | — out of scope (belonged to the earlier system) | ❌ single-agent | ❌ | ❌ | ❌ |

\* Zep: Graphiti (the temporal KG engine) is open-source, but the full
platform is cloud-only.

## 3. What is still missing — even if it makes no sense for us

| Item | Category | Note |
|---|---|---|
| Multi-embedding provider | Rejected — A6/D8 | Ollama evaluated and dropped; with no habit of operating Ollama, OpenAI is simpler. Closed, do not reopen without a new reason. |
| Full-text search (FTS) | Never addressed | chromem-go has no native FTS; it never became a decision, it was just left out for not being prioritized. |
| Memory evaluation (`recall@k`) | Decided, small scope — D30/A10 | Golden dataset of 10–20 queries, manual command, no CI/deploy gate — not implemented yet. |
| Feedback loop (`memory_feedback`) | Open — A11 | Preliminary recommendation is not to implement — manual `memory_update`/delete already cover the use case. |
| Manual privacy tags (`<private>`) | Never addressed | There is only automatic secret redaction (`internal/redact`), no manual marking of "sensitive but not detectable". |
| Multilingual (28 languages) | Out of scope — personal use | Developed for personal use with a single language; a real gap only if the product goes beyond own use. |
| Self-improvement loop (Cognee `improve()`) | Out of scope — different architecture | m3m0s's KG is deliberately light (D15, no LLM in the relation) — there is no graph rich enough for this kind of loop. |
| Governance/audit logs/SLA (Zep) | Out of scope — enterprise | Personal/solo use has no such requirements. |
| Deploy on a small ARM host | Planned, not executed — D4/Phase 6 | Still being validated on a workstation; Phase 6 (definitive deploy) has not been started. |

---

Sources: the original comparison of memory systems (14 Jul 2026) ·
[`docs/decisions/`](./decisions/README.md) (D1–D30, A6/A11) ·
[`docs/implementation-plan.md`](./implementation-plan.md) (Phases 0–10) ·
`AGENTS.md`.
