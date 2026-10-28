# D55 — Open WebUI integration as a Filter Function

**Status:** Accepted

**Context:** Open WebUI is a chat front end used with many models. Like the coding agents ([D44](D44-per-prompt-memory-search.md), [D49](D49-opencode-integration.md), [D50](D50-hermes-integration.md)), it benefits most from memory that is injected and captured automatically, because a model rarely decides on its own to call a memory tool. Open WebUI has three extension points that could carry it: Tools (model-called), MCP connections, and Filter Functions (run on every request and response).

**Decision:**
- A single-file Python **Filter Function** (`integrations/openwebui/m3m0s_memory_filter.py`, importable in Admin > Functions) talks to the HTTP API directly. Unlike the Hermes/opencode/pi clients it does not shell out to the `m3m0s` binary, because the Open WebUI container has no such binary and a filter can use `urllib` from the standard library (empty `requirements:`).
- `inlet` searches with the last user message (`POST /api/v1/search`, `mode: "vector"`, `maxTokens: 600`, 3 results, cosine floor 0.60 as in D44, prompts under 20 characters skipped) and adds the result to the system prompt inside a `<memory-context>` fence marked as not user input. A previous fence is stripped first, so regenerating never duplicates it. The topic gate of [D47](D47-prompt-search-topic-gate.md) lives in the hook, not in the API, and is not reimplemented here; the cosine floor is the only relevance gate.
- `outlet` posts the finished turn (`User: ...` / `Assistant: ...`) to `POST /api/v1/memories` with `source: "openwebui"`, `domain: "personal:openwebui"` and `model` set, from a background task so the reply is not delayed. Turns under 40 characters are skipped; everything else is left to the server's extraction gate ([D12](D12-llm-fact-extraction.md); no client-side filtering). Redaction is the server's job ([D6](D6-getengram-analysis.md)); the client does not duplicate it.
- Every failure (server down, timeout, 4xx/5xx, bad JSON) is logged and swallowed; the chat is never blocked or failed.
- Valves: `m3m0s_url`, `api_key` (password), `domain`, `capture_enabled`, `inject_enabled`, `max_tokens`, `limit`, `min_score`, `timeout_seconds`, `capture_timeout_seconds`, `allowed_users`.

**Why a Filter, not MCP or Tools:** a Tool or MCP server is only used when the model chooses to call it, which is the failure mode D44 was written to fix, and it cannot capture turns. A filter does both deterministically. A native MCP Streamable HTTP endpoint is being added separately (D54); it can complement the filter by giving the model on-demand search, and the two do not conflict (the filter injects the obvious hits, the tool covers follow-up lookups).

**Caveats:**
- **Shared memory, no per-user isolation.** Open WebUI can have many users but m3m0s has one memory per server and API key ([D35](D35-accounts-onboarding-sessions-reset.md) accounts are for the admin UI; keys are not scoped, see [D50](D50-hermes-integration.md)). Without a limit, every Open WebUI user would read and write the owner's memory. The `allowed_users` valve (comma-separated names, e-mails or ids; empty means everyone) restricts both injection and capture. Deployments with other users should set it. The filter also should be enabled only on models the owner is comfortable sending memory to, since injected memories go to whichever provider serves that model.
- Valves are admin-wide: the API key is stored in Open WebUI's database.
- Regenerate or edit in Open WebUI can produce several captures of nearly the same turn; server-side dedup ([D12](D12-llm-fact-extraction.md)) handles repeats.

**Not done:** per-user domains or keys, a topic gate in the API, streaming-aware capture of tool-call turns, and publishing to the Open WebUI community site.
