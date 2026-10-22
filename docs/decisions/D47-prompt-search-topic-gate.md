# D47 — Topic gate for per-prompt search

**Status:** Accepted (refines [D44](D44-per-prompt-memory-search.md))

**Context:** after D44 went live, generic prompts pulled unrelated memories from other projects at high cosine: "pode fazer merge e deploy" returned three `bhay-e-mota` facts at 0.75–0.77, above any sensible floor and above the relevant hits of topical prompts (0.64–0.72). Cosine cannot separate them: a prompt with no topic is close to every generic workflow sentence. Filtering by `kind` does not work either — many correct hits ("o compose do m3m0s fica em ...") are classified `state`.

**Decision:**
- **Prompt gate:** extract the prompt's *specific terms* (lowercase, accent-folded words of 3+ characters or containing a digit, minus a PT/EN stoplist that includes function words and workflow vocabulary such as merge, deploy, commit, seguir, continuar, validar). A prompt with none (a bare "go ahead" or "merge and deploy" in either language) triggers no search at all — no embedding call, no injection.
- **Hit gate:** a hit is injected only if its content mentions at least one specific term. The cosine floor of D44 still applies.
- `M3M0S_PROMPT_LEXICAL=off` disables both gates.

**Measured (production, 6 prompts):** generic workflow prompts (a bare "go ahead", "merge and deploy") now inject nothing, while prompts naming a topic (a feature phase, the extraction model, where the compose file lives, a hook timeout bug) keep their relevant hits.

**Trade-off:** a purely paraphrased match with no shared word is no longer injected; the memory is still reachable through `memory_search`. Precision over recall, because an injected irrelevant memory costs tokens on every prompt and misleads.

**Not done:** per-project term weighting and learning the stoplist from usage.
