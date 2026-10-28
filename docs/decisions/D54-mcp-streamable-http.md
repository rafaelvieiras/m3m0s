# D54 — Native MCP Streamable HTTP endpoint (builds on D10, D45)

**Status:** Accepted

**Context:** MCP was stdio only: `m3m0s mcp` is a process the host spawns, which forwards to `serve` over HTTP ([D10](D10-pi-adapter-dropped.md), [D45](D45-mcp-read-only-mode.md)). Clients that cannot spawn a local binary (Open WebUI, which runs in a container) or that prefer a remote server (`pi-mcp-adapter` with `url`, opencode remote, Claude Code `--transport http`) had no way in. The integrations of [D49](D49-opencode-integration.md) and [D50](D50-hermes-integration.md) register the stdio bridge; F3 ([#4](https://github.com/rafaelvieiras/m3m0s/issues/4)) wants more clients.

**Decision:**

- `m3m0s serve` serves `POST /mcp`, the MCP Streamable HTTP transport (spec revision 2025-03-26 and later), on the same port as the API.
- **Plain JSON, no SSE.** Each POST carries one JSON-RPC message (batches are accepted) and gets `application/json`, or `202` for notifications. `GET` and `DELETE` answer `405`: the server has nothing to push, which the spec allows. No `Mcp-Session-Id`: the endpoint is stateless, so it needs no session store and survives restarts and proxies.
- **One tool set, two transports.** `internal/mcpserver` now has a transport-agnostic `Handler` (`HandleMessage`) that owns the tool definitions, the dispatch, the read-only rules and the protocol replies. The stdio loop and the HTTP handler are thin wrappers; there is no second copy of any tool schema. The refactor also moved the former package globals (`readOnly`, `sessionProject`) into the `Handler`, and adds `ping` and protocol version negotiation to both transports.
- **Tools run in-process.** The HTTP handler builds an `internal/client` over a transport that hands requests to the server's own mux (`client.NewWithHTTPClient`), with the caller's Bearer token. Every tool therefore goes through the real `/api/v1` handlers, auth included, instead of a parallel code path into the store.
- **Auth:** API key Bearer only, same keys as `/api/v1`. Browser sessions and the CSRF header do not apply to a machine interface.
- **Origin check:** a request with an `Origin` header is rejected with `403` unless it matches the request `Host` or `M3M0S_MCP_ALLOWED_ORIGINS` (comma-separated, `*` = any). Requests without `Origin` (all non-browser clients) pass. This is the spec's DNS-rebinding guard. The global CORS middleware also lists the `Mcp-Session-Id` and `Mcp-Protocol-Version` headers.
- **Read-only ([D45](D45-mcp-read-only-mode.md)) is identical on both transports:** `M3M0S_MCP_READ_ONLY=1` or `m3m0s serve -mcp-read-only` for the whole endpoint, or `/mcp?read_only=1` on a single client URL (it can only narrow). Same four tools hidden and rejected.
- **Default project:** a remote connection has no working directory, so recall defaults to global memory; the model passes `project` to widen.
- The stdio bridge stays: it needs no network exposure and keeps the per-session project detection ([D34](D34-project-scope-layered-recall.md)).

**Public exposure:** the endpoint exists on every `serve`, but the reverse proxy decides what is reachable. Where only `/api/v1*` and `/health` are routed, `/mcp` is not exposed until a route is added. The API key is the only credential, so expose it over TLS only.

**Not done:** SSE streaming and `GET /mcp` (no server-initiated messages exist yet), sessions, resources/prompts, OAuth (clients use a static key header), and a read-only API key scope (still a client-side convenience, as in D45).
