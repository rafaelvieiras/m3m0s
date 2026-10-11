// Command m3m0s is the single M3m0s binary: `serve` (central daemon),
// `mcp` (stdio↔HTTP bridge) and the CLI verbs (`add`/`search`/`recent`/
// `stats`/...), all HTTP clients of `serve` — following the topology
// described in docs/api-contract.md.
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	ctx := context.Background()

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "mcp":
		err = runMCP(ctx, os.Args[2:])
	case "watch":
		err = runWatch(ctx, os.Args[2:])
	case "hook":
		err = runHook(ctx, os.Args[2:])
	case "eval":
		err = runEval(ctx, os.Args[2:])
	case "doctor":
		err = runDoctor(ctx, os.Args[2:])
	case "user":
		err = runUser(ctx, os.Args[2:])
	case "embedding":
		err = runEmbedding(ctx, os.Args[2:])
	case "insights":
		err = runInsights(ctx, os.Args[2:])
	case "kind":
		err = runKind(ctx, os.Args[2:])
	case "maintenance":
		err = runMaintenance(ctx, os.Args[2:])
	case "filter":
		err = runFilter(ctx, os.Args[2:])
	case "project":
		err = runProject(os.Args[2:])
	case "add":
		err = runAdd(ctx, os.Args[2:])
	case "search":
		err = runSearch(ctx, os.Args[2:])
	case "recent":
		err = runRecent(ctx, os.Args[2:])
	case "stats":
		err = runStats(ctx, os.Args[2:])
	case "update":
		err = runUpdate(ctx, os.Args[2:])
	case "get":
		err = runGet(ctx, os.Args[2:])
	case "forget-sweep":
		err = runForgetSweep(ctx, os.Args[2:])
	case "restore":
		err = runRestore(ctx, os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: m3m0s <command> [flags]

commands:
  serve    starts the central daemon (HTTP) — the only one that talks to the database
  mcp      stdio<->HTTP bridge for MCP hosts (Claude Code, OpenCode, ...)
  watch    watches a project root and reindexes memory/doc files
  hook     auto-capture hooks for agent tools (e.g. "claude-code")
  eval     runs a golden dataset and prints recall@k per ranking mode (D30)
  doctor   checks server, key, hooks and the capture spool (--replay resends it)
  embedding runtime embedding config and re-embedding (show|set|estimate|reembed)
  insights memory health summary: totals, forgetting outlook, last hygiene analysis (show)
  kind     fact nature tag: labelling sample, agreement report, backfill (sample|report|backfill)
  maintenance offline repair jobs on the data directory (redact-scan)
  filter   ingestion boilerplate rules: replay them over stored text without writing (replay)
  project  maps directories to project scopes (show|list|set|unset)
  user     accounts for the WebUI: list, add, reset (password link), delete
  add      stores a memory
  search   semantic search
  recent   lists recent memories
  stats    overall counts
  update        partially edits an existing memory by id
  get           fetches 1 existing memory by id (records an access reinforcement)
  forget-sweep  memory layers: evaluates/soft-deletes cold facts, hard-deletes expired soft-deletes
  restore       reverts the soft-delete of a memory by id

"watch", "hook", "add", "search", "recent", "stats", "update", "get",
"forget-sweep", "restore", "embedding", "insights" and "kind" talk to an already running "serve" via M3M0S_URL
(default http://localhost:19234) and M3M0S_API_KEY. "maintenance" opens the data directory
(--data / M3M0S_DATA_DIR) directly instead.

Use "m3m0s <command> -h" to see the flags of each one.
`)
}
