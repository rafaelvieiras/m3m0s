# A11 — Feedback loop (`memory_feedback`)

**Status:** Open (preliminary recommendation: do not implement)

**Context (draft, 16 Jul 2026):** a low-priority gap in the original benchmark, which never became a decision nor a phase. Mem0 has `feedback(data)`; no other competitor researched has this natively.

**Preliminary recommendation (not a decision):** probably **not worth implementing** — `memory_update` + manual delete (Phase 5/[D13](D13-webui-admin-scope.md), already done) already solve the same problem that Mem0's `feedback()` solves ("this memory is wrong, I edit or delete it"), with no need for a separate `rating` field that nobody has asked for yet. It is recorded explicitly instead of simply being discarded, so the research is not repeated later — but the probable answer is "do not do it". **Not decided.**
