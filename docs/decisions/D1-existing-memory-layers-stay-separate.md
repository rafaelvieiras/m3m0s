# D1 — Existing memory layers stay separate by function

**Status:** Accepted

**Context:** the maintainer already has 3 memory layers today: per-project `MEMORY.md`/`AGENTS.md`/`CLAUDE.md` files (curated markdown, each project with its own pattern — specs, ADRs, briefings), Claude Code's private memory (`~/.claude/projects/.../memory/`, per machine), and `pi`'s chromem-go store (semantic, but tied to `pi`).

**Decision:** M3m0s does not replace any of them. It is a new layer that **reads** the others (semantic indexing of each project's memory/doc files) to enrich its own search, and it helps tools consume fewer tokens — but editing and curation stay where they already are.

**Why:** each project already documents things its own way (one uses docs/ADRs/briefings, another uses `MEMORY.md`, others may use different specs). Forcing a migration of everything into a central vector database would throw away a system that already works and is human-readable without any tooling.
