# D6 — Analysis of getengram.app (external reference, 15 Jul 2026)

**Status:** Accepted

**What it is:** a SaaS (not self-hosted) for "conversation memory" across agents — the philosophy is "the conversation IS the knowledge base": it stores messages verbatim (no summarization), native MCP (6 tools), REST/CLI/SDK on paid plans, multi-tenant by design. Cloudflare Workers + [D1](D1-existing-memory-layers-stay-separate.md) + Vectorize (edge, scales to zero), free tier of 1000 msgs/month, paid at $9-27/seat/month + Enterprise.

**Philosophical difference (do not copy):** it is conversation-centric (the unit is a "conversation" with messages in sequence) and paid cloud-only; M3m0s is fact/doc-centric (the unit is a semantic memory + an observed source file) and self-hosted (Raspberry Pi, no recurring cost). Adopting the "conversation" data model or the Cloudflare infrastructure makes no sense.

**What is worth bringing into M3m0s (real technical detail, extracted from their docs):**

1. **Secret redaction before any other pipeline step** — they do it "once, at ingestion time", before chunking/embedding. Order: specific patterns first (PEM keys; provider prefixes such as `sk-`, `sk-ant-`, `AKIA`, `ghp_`, `sk_live_`; JWTs; connection strings; key-value pairs like `password`/`token`/`secret`/`api_key`; PII such as SSN/card/email/phone), generic high-entropy tokens (hex 32+, base64 40+) last — replacing them with `[REDACTED]`. **This is directly applicable and important for M3m0s**, which will index the content of real project files (docs, possibly code) — without this step, a secret accidentally committed in any file observed by `m3m0s watch` ends up embedded and retrievable via search. It became a Phase 1 item (see the plan).
2. **Entity-type-prefixed IDs** (nanoid: `org_`, `conv_`, `msg_`, `chk_`) — better readability/debugging than `mem-<unixnano>` (the format of the old `memory-server`). Adopt a per-type prefix (`mem_`, `chunk_`, `scope_`) when the new schema (A3) is designed.
3. **Separation between the verbatim record and the search index** — they keep the original message intact AND derived chunks (sliding window: 5 messages, stride 3) with embeddings, each chunk referencing `start_sequence`/`end_sequence` back to the original. **This validates [D1](D1-existing-memory-layers-stay-separate.md)**: in M3m0s, the source file (`MEMORY.md` etc.) is the verbatim record (it already lives on disk, curated by a human) and the chunks/embeddings are derived with a reference back (file path + lines) — the original content is never rewritten.
4. **API-key auth with hashing, never the raw key** — they store `key_hash` (SHA-256) + `key_prefix` (for display/audit) + `expires_at`/`revoked_at`/`last_used_at`. **A real gap the original comparison missed:** the current `memory-server` has NO authentication at all (it assumes a trusted network/localhost). Since M3m0s will run over a network (desktop → remote host, accessed from several machines), this is no longer acceptable. It became a Phase 1 security item.
5. **Explicit cascading delete** — deleting a "conversation" removes its messages+chunks+vectors together. For M3m0s: when `m3m0s watch` detects that an observed file was deleted (or an entire project was removed), the chunks/memories derived from it must be purged too — not left orphaned in the index. Added as an explicit Phase 2 item.
6. **Tags + `agent_id` + free-form metadata per conversation** — confirms that the "essential typed fields + free JSON metadata + tags" shape is enough to cover varied domains; use it as a reference for shape (not literally the same fields) when closing the `scope`/`domain` schema (A3).

**What NOT to bring:** zero compression/summarization (M3m0s wants compression of old low-value memories — a deliberate difference); the "conversation" data model (M3m0s indexes files/facts, not chat threads); Cloudflare infrastructure (contradicts the goal of running on a Raspberry Pi); the per-seat paid business model (irrelevant, personal use).

## Redactor coverage (update, Oct 2026)

The redactor (`internal/redact`) now covers, besides the original prefixes:

- **Key names with a prefix or suffix**: the key words (`password`, `passwd`, `pwd`, `senha`, `secret`, `token`, `api_key`, `access_key`, `private_key`, `chave`, ...) are matched inside names, so `TEST_USER_PASSWORD=`, `refresh_token:`, `clientSecret =` and `"db_password": "..."` are caught (a `\b` never matched after `_`). Only the value is replaced; the key name stays so the fact remains readable. Short, low-entropy values are redacted too, since the key name is the evidence.
- **Narrative forms** such as `a senha é 'X'` or `the admin password for staging is X` (quoted values always; bare words only when they look like a password: digit, symbol or inner capital).
- `Authorization: Basic|Bearer|Token ...` (scheme kept), `scheme://user:password@host` (password only), `--password value` CLI flags, and extra provider formats (Slack, Google API keys, fine-grained GitHub tokens, `sk-proj-`).
- **Source blobs** (`source_blobs`, D21) go through the redactor on the write path; the dedup hash is taken over the redacted text.

False-positive control: placeholders and descriptions are left alone (`...`, `<token>`, `${VAR}`, `$VAR`, `process.env.X`, `YOUR_TOKEN`, function calls, `required`, `string`, masks such as `****`).

The high-entropy layer no longer redacts identifiers. A token whose segments (split on `_`, `-`, `.`) are all plain words or numbers (camelCase functions, snake_case and kebab-case names, branch names, file names) is kept, and tokens of 20-31 characters need all three character classes or entropy >= 4.0.

Known limits: the narrative and key=value rules are heuristics (a prose sentence such as "token: foo_bar" is redacted), letters-only random tokens are treated as identifiers, and passwords with no key word nearby and low entropy cannot be detected.

### Cleaning existing data

`m3m0s maintenance redact-scan` runs the current redactor over stored entries and source blobs. It is a dry run by default and reports blob hashes / entry ids plus the id of the matching rule, never any part of a value. `--apply` re-redacts blobs in place (the blob keeps its hash key, so entry references and a running server's cache stay valid). Entries are only listed: rewriting their text would invalidate their embeddings, so edit or delete them by hand. It opens the data directory directly; stop `serve` first or restart it afterwards. Rotate any credential that was ever stored, because backups and snapshots still hold the old text.
