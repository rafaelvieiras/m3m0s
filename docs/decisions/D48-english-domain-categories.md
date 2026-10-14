# D48 — English domain categories and English-only built-in word lists

**Status:** Accepted

**Context:** the repository policy is English only, but the domain *category* names (`codigo`, `escrita`, `pessoal`, `cliente`) were Portuguese strings baked into code, API examples and ~90k stored entries, and the prompt topic gate ([D47](D47-prompt-search-topic-gate.md)) shipped a Portuguese stoplist. Both were exceptions "kept for compatibility".

**Decision:**
- Categories are `code`, `writing`, `personal`, `client`. Migration `00008_english_domain_categories.sql` rewrites `entries.domain`/`domain_category` (and queued `ingest_queue.domain`); its Down reverses it. Other category names are untouched.
- The old names are still **accepted on input**: `schema.CanonicalDomain`/`CanonicalCategory` rewrite them in `Add`, `Update` and the `Search`/`Recent` filters, so old hook configs, scripts and golden sets keep working and never create `codigo:*` entries again.
- Defaults follow: hook and `watch` infer `code:<basename>`, the self-index domain is `code:m3m0s`, and the insights noise scan runs on the `code` category.
- The built-in prompt stoplist is English only. Extra generic words in any language go in `~/.m3m0s/prompt-stopwords.txt` (or the file named by `M3M0S_PROMPT_STOPWORDS`), read at start, accents folded.

**Not done:** the Portuguese patterns in the default ingest-filter rules, the redaction keywords (`senha`, `segredo`, `chave`) and the hygiene noise rules stay: they match content people actually write and are product behavior, not naming.

**Rollback:** the previous binary opens the migrated database; it would simply not find `codigo:*` domains until the migration is reverted (`goose down`) or its default domain is configured explicitly.
