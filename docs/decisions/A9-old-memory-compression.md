# A9 — Compression of old low-value memories

**Status:** Closed (17 Jul 2026), see [D22](D22-memory-layers-automatic-forgetting.md)

**Context (draft, 16 Jul 2026):** [D6](D6-getengram-analysis.md) records the intention ("M3m0s wants compression of old low-value memories, a deliberate difference" from getengram, which does not compress) — but this never became a checklist item in any phase of the plan, it just stayed as a loose sentence inside the analysis of a competitor. The same kind of gap the WebUI had until Phase 5 existed ([D13](D13-webui-admin-scope.md): "the React WebUI planned in [D2](D2-stack-go-core-typescript-only-where-required.md) never became a checklist item in any phase").

**What the competitors do:** the old `pi` (`memory.ts`) had `maybeCompress()` — >100 memories → an LLM condenses 5 at a time — but the original comparison already pointed to real bugs in it (it lost `domain`/`source`/`agent`, turning into a generic `"_compressed"`; see the note already recorded in Phase 1 of the plan about not repeating that mistake). Claude-Mem "compresses" in the observation→facts/concepts/narrative sense (already covered by Phase 4/[D12](D12-llm-fact-extraction.md)). Zep does temporal invalidation (an old fact is not deleted, it is marked invalid — it overlaps with [A8](A8-light-kg-temporal-fact.md) above). None of the 4 competitors has an isolated feature equivalent to "summarize N old memories into 1" outside extraction — it is a pattern specific to the old `pi` that M3m0s did not port.

**Open questions to close this:**
- Trigger: by age (`createdAt` > X days), by count per `domain` (>N memories in the same domain), or manual (`m3m0s compact`)?
- Provenance preservation: already solved in principle (note in Phase 1) — the compressed memory inherits `domain`/`source`/`agent` from the source memories, never overwriting with a generic value.
- Destructive or not: delete the original memories after compressing (chromem-go has no soft-delete today), or keep both versions?

**Closed on 17 Jul 2026, see [D22](D22-memory-layers-automatic-forgetting.md).** It did not become "summarize N old memories into 1" as the old `pi` did (`maybeCompress()`) — it became automatic forgetting inspired by ai-memory (soft-delete + hard-delete with a grace period, `internal/store/decay.go`), which solves the same practical goal ("low-value memories do not grow the database forever") without the provenance bugs that the original comparison pointed out in `pi`. Trigger: age + access reinforcement (a retention formula), not count per domain nor a manual `m3m0s compact` — but `dryRun=true` covers the manual preview case. Destructive: yes, with a reversible grace period (`SoftDeleteGraceDays`).
