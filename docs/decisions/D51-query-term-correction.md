# D51 — Query-term correction (typo-tolerant search)

**Status:** Accepted (refines [D44](D44-per-prompt-memory-search.md) and [D47](D47-prompt-topic-gate.md))

**Context:** a project name typed with one extra letter ("playtheadtv") returned no automatic recall for an agent: the vector search still found the right memories, but below the cosine floor of D44, and the topic gate of D47 required a shared word. The keyword side (FTS5 `unicode61`) only matches exact tokens, so it could not help. Every client that searches (hooks, MCP, CLI, WebUI) is exposed to the same failure.

**Decision:**
- **Vocabulary:** migration `00009` adds `entries_fts_vocab`, an `fts5vocab` view over the keyword index (distinct tokens with document counts; nothing stored). The store caches it for 5 minutes and adds the words of every domain name, so a project that is rarely named in its memories is still known.
- **Correction:** each query word of 5+ characters is compared (optimal-string-alignment distance: insert, delete, substitute, swap) with vocabulary terms of similar length; budget 1 edit up to 7 characters, 2 beyond. A candidate must appear in at least 3 entries, and when the typed word itself is in the vocabulary the candidate must be at least 8x more frequent, so a rare-but-correct word is left alone and a typo that was captured once is still repaired.
- **Search:** the corrected text is embedded; the keyword side searches the typed and the corrected words (OR). `POST /search` returns `correctedQuery` and prefixes the context block with a note, so a human or a model sees what was searched.
- **Prompt gate (D47):** a hit now ties to the prompt when it contains a prompt term exactly or as a word within the same edit budget. Terms under 5 characters stay exact.
- No new setting: the behavior only repairs words the store does not know; queries with correct spelling are unchanged.

**Trade-offs:**
- A correct but rare word that is one edit from a very common one can be replaced (mitigated by the 8x dominance rule; the typed word is still searched).
- Names shorter than 5 characters are never corrected.
- The vocabulary is refreshed every 5 minutes, not on each write.

**Not done:** phonetic matching, per-user vocabularies, and surfacing "did you mean" in the WebUI.
