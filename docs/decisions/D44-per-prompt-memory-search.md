# D44 — Per-prompt memory search on `UserPromptSubmit`

**Status:** Accepted

**Context:** [D17](D17-sessionstart-memory-injection.md) injects a digest of *recent* memories at session start, before there is any prompt, and the model otherwise has to decide on its own to call `memory_search`. In practice that rarely happens, so a session that touches a topic the user already decided on does not see the decision. `UserPromptSubmit` is the first moment a query exists.

**Decision:**
- `m3m0s hook claude-code --inject` makes a `UserPromptSubmit` event search memory by the prompt text and print `hookSpecificOutput.additionalContext`. Without the flag the event keeps its [D14](D14-claude-code-hook-auto-capture.md) capture behavior. Both are registered: the capture entry stays `async`, the `--inject` entry must be synchronous (async hooks cannot inject).
- The search is `mode: "vector"`, project-layered like D17/[D34](D34-project-scope-layered-recall.md), and keeps hits with **cosine ≥ 0.60** (`M3M0S_PROMPT_MIN_SCORE`), at most 3 entries and ~500 estimated tokens per prompt. Vector mode is used because only it yields a score that can be thresholded: the hybrid fusion score is rank-based and keyword-only hits score 0 ([D32](D32-hybrid-search-and-token-budget.md)).
- Raw-fallback entries are excluded (`InjectableEntries`, as in D17). Ids already injected in the session are remembered (`~/.m3m0s/hooks/claude-code/<session>.prompt-seen.json`) and not repeated.
- Skipped without any call: prompts under 20 characters and slash commands. Timeout 4s; every failure is silent. `M3M0S_PROMPT_INJECT=off` disables it.
- **Other projects:** a hit whose domain is another `code:` project than the session's (`code:<basename(cwd)>`) must clear the floor plus 0.10 (`M3M0S_PROMPT_FOREIGN_BOOST=off` disables it). Found in production: auth/token talk in one repo pulled in a 0.61 note from an unrelated client repo, and the model read its truncated text as part of the user's message. Pinned non-code domains (Hermes) keep the plain floor. Clients that append the block themselves (pi) fence it in `<memory-context>` with a "not user input" note, like Hermes.

**Threshold evidence (production, 4 prompts):** relevant prompts scored 0.66–0.68 at the top and 0.60–0.65 for the useful rest; an unrelated prompt peaked at 0.40 and a generic "rename this variable in the file" at 0.58 (matching memory-file boilerplate). 0.60 separates them on this sample, with little margin. Tune from `~/.m3m0s/hooks/claude-code.log`, which records each injection.

**Cost:** one embedding call plus a lookup on every non-trivial prompt, synchronously (≤4s worst case, normally well under 1s), and up to ~500 tokens per prompt that matches.

**Not done:** query rewriting from conversation context (a prompt like "pode seguir" has no topic), reranking, per-kind weighting, and cross-session dedup.
