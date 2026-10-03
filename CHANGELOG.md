# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0]

Initial public release.

### Added

- Single static Go binary (no CGO) with `serve` (HTTP API), `mcp` (MCP server),
  `hook` (agent hooks), `watch` (file watcher) and CLI commands.
- HTTP API under `/api/v1` with API key authentication; `/health` endpoint.
- MCP server exposing memory tools to any MCP-capable client.
- Memory model with four orthogonal scope/domain axes.
- LLM-based fact extraction from sessions and documents, chunked, with forced
  JSON output and per-chunk retry.
- Deduplication, lightweight knowledge graph and temporal facts
  (`valid_until` set on contradiction).
- Automatic forgetting with decay and `pinned` memories; memory layers.
- Claude Code hooks for automatic capture (`PreCompact`, `SessionEnd`) and
  context injection (`SessionStart`), with secret redaction.
- Asynchronous add with a short timeout.
- In-process job system with history, manual runs and runtime interval control.
- SQLite storage with versioned migrations; deduplicated blob store for source
  content.
- Embedded React WebUI: browse and edit memories, jobs, injection preview and a
  floating chat (RAG questions, saving via tool calling).
- Embedded project documentation, self-indexed at startup.
- Golden queries and `recall@k` memory evaluation.
- Docker image (multi-stage build, non-root, health check).

[Unreleased]: https://github.com/rafaelvieiras/m3m0s/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/rafaelvieiras/m3m0s/releases/tag/v0.1.0
