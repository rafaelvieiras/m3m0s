# D2 — Stack: Go core, TypeScript only where required

**Status:** Accepted

**Decision:** the server (HTTP + MCP + CLI) is written in Go, as a single static binary. TypeScript is used only for (a) adapters for tools whose own extension API does not speak MCP (today: `pi`) and (b) the WebUI (reusing the React app that already existed in the legacy `pi` `memory-server` extension).

**Why:** an explicit focus on minimal memory/CPU use (the target is a Raspberry Pi-class device) and minimal external dependencies — a Go binary does not need a Node runtime installed on the host.
