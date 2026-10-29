# Connecting MCP clients over HTTP

`m3m0s serve` exposes the memory tools as a remote MCP server (D54):

- URL: `http(s)://<host>:<port>/mcp` (local default `http://localhost:19234/mcp`)
- Header: `Authorization: Bearer <M3M0S_API_KEY>`
- Transport: Streamable HTTP (plain JSON responses, no SSE, no sessions)
- Read-only for one client: append `?read_only=1` to the URL (D45).

The key is a normal m3m0s API key. Over a network, put the server behind TLS; and if you use a reverse proxy, add a route for `/mcp`, since a proxy that forwards only `/api/v1*` and `/health` does not expose it.

Quick check:

```sh
curl -s -X POST http://localhost:19234/mcp \
  -H "Authorization: Bearer $M3M0S_API_KEY" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

## Claude Code

```sh
claude mcp add --transport http m3m0s http://localhost:19234/mcp \
  --header "Authorization: Bearer $M3M0S_API_KEY"
```

## Open WebUI (native MCP)

In the admin settings, add an external tool server: type **MCP (Streamable HTTP)**, URL `http://<m3m0s-host>:19234/mcp`, auth **Bearer** with the key. The server is reached from the Open WebUI backend, so use an address that container can resolve (not `localhost` when Open WebUI runs in Docker).

## pi (pi-mcp-adapter)

In `~/.pi/agent/mcp.json`:

```json
{
  "mcpServers": {
    "m3m0s": {
      "url": "http://localhost:19234/mcp",
      "headers": { "Authorization": "Bearer ${M3M0S_API_KEY}" }
    }
  }
}
```

If your adapter version does not expand environment variables in `headers`, paste the key literally.

## opencode (remote)

In `opencode.json`:

```json
{
  "mcp": {
    "m3m0s": {
      "type": "remote",
      "url": "http://localhost:19234/mcp?read_only=1",
      "headers": { "Authorization": "Bearer {env:M3M0S_API_KEY}" }
    }
  }
}
```

Drop `?read_only=1` if the agent should be able to write memories.

## Troubleshooting

- `401`: missing or wrong key. `403`: the request carries an `Origin` that is neither the server's own nor in `M3M0S_MCP_ALLOWED_ORIGINS`. `405`: the client tried `GET`/`DELETE`; use a client that supports POST-only Streamable HTTP.
- Tools return `read-only` errors: `M3M0S_MCP_READ_ONLY=1` is set on the server, or the URL has `?read_only=1`.
