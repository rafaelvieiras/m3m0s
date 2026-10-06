# D33 — Provenance fields: `model` and optional `project`

**Status:** Accepted

**Context:** entries did not say which model wrote them nor which project they belonged to, so memories written by several tools could not be audited, filtered or deleted in bulk. Issue #3 proposed `source_tool`, `model` and `project`.

**Decision:**
- **No `source_tool`.** `source` has always been the writing tool (the hook sets `claude-code`, the suggested list names `opencode`, `openwebui`...). A second column would duplicate it and invite two values for one fact. Clients just have to set `source` consistently.
- **`model`** (nullable, no whitespace, max 128 chars) and **`project`** (nullable slug, `^[a-z0-9][a-z0-9._-]{0,63}$`), added by migration 00003 as nullable columns. Existing entries stay `NULL`: unknown model, global project. Nothing is rewritten.
- **One memory per user stays.** `project` is a scope filter on that memory, not a separate instance or a per-project key. Search, recent and the injection preview filter by exact `source`/`model`/`project`; with no `project` filter every entry matches. Combining "global plus this project" in one recall, promotion between scopes and per-client project detection remain with the project-scope issue (#9).
- **Dedup is per project.** The dedup/related lookup only considers entries of the same project, so a project fact never merges into or overwrites the global one (and vice versa). A dedup hit keeps the newest non-empty `model`.
- **Clients set the fields.** The server does not infer them from the API key (per-profile API keys were dropped: there is one memory per person). The Claude Code hook sets `model` from the transcript and `project` from `M3M0S_PROJECT` (an invalid slug is ignored so a capture is never rejected); the SessionStart hook can filter injection with `M3M0S_INJECT_SOURCE` and `M3M0S_INJECT_PROJECT`.
- API, MCP tools, CLI (`--model`, `--project`, `--clear-model`, `--clear-project`) and the WebUI (filters, badges, edit form) accept and return the fields. `PATCH` follows the key-presence rule: `null` clears.

**Update:** the exact `project` filter described above was replaced by layered recall in [D34](D34-project-scope-layered-recall.md): `project=X` now means global plus X, and the old behavior is `scope=project-only`.

**Consequences:** bad output from one tool or model can be found and removed with a filter. The injection budget can be applied per source. The `project` filter is exact on purpose: until #9 defines precedence, mixing scopes in one recall would hide the rule in code.
