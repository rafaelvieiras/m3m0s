# D3 — Decentralized execution: core vs watch

**Status:** Accepted

**Decision:** the binary has (at least) two modes: `m3m0s serve` (stores data and serves vector search, runs in one central place) and `m3m0s watch <dir>` (a lightweight agent that runs on each machine/project, watches memory files via fsnotify and sends updates to `serve`).

**Why:** the maintainer has multiple machines (desktop, laptop, a small ARM host) and multiple projects spread across them; indexing must happen close to where the files live, but storage/search must be a single service (otherwise memory fragments per machine again, which is the original problem).

**Later elaboration:** MCP over stdio required a third mode (`m3m0s mcp`, a stateless bridge) in addition to `serve`/`watch` — see the full topology and the `add`/`search`/`recent`/`stats` contract in **[docs/api-contract.md](../api-contract.md)**.
