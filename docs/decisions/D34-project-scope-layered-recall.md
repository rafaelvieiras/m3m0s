# D34 — Project scope: layered recall, overrides and session detection

**Status:** Accepted (supersedes the "exact `project` filter" of [D33](D33-provenance-model-and-project.md))

**Context:** with `project` stored ([D33](D33-provenance-model-and-project.md)), the remaining questions of issue #9 were the recall rule, what wins on conflict, how an entry moves between scopes, and how each client knows its project.

**Decision:**
- **Recall rule.** There is still one memory per user; `project` is a layer on top of it.
  - `project=X` recalls **global entries plus X's** (layered). Other projects' entries are never returned.
  - `scope=project-only` (with `project`) recalls only X's; `scope=global` recalls only entries with no project.
  - No `project` and no `scope` applies no project filter (administrative view: the API default, the CLI, the WebUI).
  - 1-hop `related_to` expansion obeys the same rule, so a link cannot smuggle another project's memory into a scoped recall.
- **Clients default to the safe side.** The SessionStart hook and the MCP `memory_search`/`memory_recent` use the session's project when there is one (layered) and `scope=global` when there is none, so a session never recalls project-only memory it is not in. An explicit `project` or `scope` always wins.
- **Conflict: the project wins, inside the project only.** When a new project entry is a near-duplicate of a global one, or is in the related band and the classifier answers `contradicts`, it records `metadata.overrides = <global id>`. Layered recall drops the overridden global entry when the overriding project entry is in scope. The global entry is **not** invalidated (`valid_until` is never set), so every other project and the global recall still see it. Dedup itself stays per project.
- **Promotion and demotion are updates.** `PATCH {"project": null}` promotes a project entry to global, `{"project": "slug"}` moves a global one into a project (or one project to another). Changing the scope drops the entry's `overrides`, which were decided for the old scope.
- **Session detection, nothing in the repositories.** `M3M0S_PROJECT` wins; otherwise the longest directory prefix of the working directory listed in `~/.m3m0s/projects.json` (`m3m0s project set|unset|list|show`). An invalid `M3M0S_PROJECT` resolves to global instead of falling through to another project. The hook tags captures with the resolved project, so a mapped directory's sessions are captured as project memory and no longer feed the global memory (opt-in per directory).
- `GET /stats` adds `countByProject`.

**Consequences:** project facts stop polluting other projects' recall and can contradict global ones without destroying them. Mapping a directory changes where its captures go, which is why it is explicit. Not done: automatic promotion suggestions, and detecting the project from a git remote (the explicit mapping is predictable and works outside git).
