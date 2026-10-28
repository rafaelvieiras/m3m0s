# Open WebUI integration

A [Filter Function](https://docs.openwebui.com/features/extensibility/plugin/functions/filter) that gives Open WebUI chats long-term memory from m3m0s ([D55](../../docs/decisions/D55-openwebui-filter.md)). It talks to the HTTP API directly and uses only the Python standard library plus `pydantic` (bundled with Open WebUI).

- **Recall** (`inlet`): searches `POST /api/v1/search` with the last user message (`mode: "vector"`, `maxTokens` 600, up to 3 hits with cosine >= 0.60, prompts under 20 characters skipped) and adds the result to the system prompt inside a `<memory-context>` fence. A previous block is removed first, so regenerating a reply does not duplicate it.
- **Capture** (`outlet`): sends the finished turn to `POST /api/v1/memories` with `source: "openwebui"`, `domain: "personal:openwebui"` and the chat model, in a background task. The server redacts secrets and decides what becomes facts ([D12](../../docs/decisions/D12-llm-fact-extraction.md)); the filter only skips turns under 40 characters.
- Failures (server down, timeout, HTTP errors) are logged and never fail or delay the chat.

## Install

1. In Open WebUI go to Admin > Functions > New Function (or import), paste the contents of `m3m0s_memory_filter.py`, save and enable it.
2. Open the function's valves and set `m3m0s_url` (reachable from the Open WebUI server or container, not from your browser) and `api_key`.
3. Enable the filter globally, or attach it to chosen models.
4. If anyone besides you uses this Open WebUI, set `allowed_users`.

## Valves

| Valve | Default | Meaning |
|---|---|---|
| `m3m0s_url` | `http://localhost:19234` | m3m0s base URL, without `/api/v1` |
| `api_key` | empty | Bearer token (password field); empty for a keyless server |
| `domain` | `personal:openwebui` | domain of captured memories |
| `capture_enabled` | `true` | send finished turns |
| `inject_enabled` | `true` | inject relevant memories |
| `max_tokens` | `600` | budget of the injected block |
| `limit` | `3` | max memories injected per prompt |
| `min_score` | `0.60` | cosine floor ([D44](../../docs/decisions/D44-per-prompt-memory-search.md)); `0` disables it |
| `timeout_seconds` | `4` | search timeout |
| `capture_timeout_seconds` | `30` | background capture timeout |
| `allowed_users` | empty | comma-separated Open WebUI names, e-mails or ids; empty means everyone |

## Caveats

- m3m0s has one shared memory per server. Without `allowed_users`, every Open WebUI user reads and writes it. Injected memories are sent to whichever provider serves the chat model.
- The API key lives in Open WebUI's database as a valve.
- For on-demand search by the model itself, the native MCP endpoint (D54, a separate change) can be used alongside the filter.

## Tests

No Open WebUI install is needed; the tests run the filter against a fake local HTTP server.

```sh
pip install pytest pydantic
python -m pytest integrations/openwebui/tests
```
