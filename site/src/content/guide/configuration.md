---
title: Configuration
description: Environment variables for the server and the clients.
section: Deploy
order: 4
---

The server is configured with environment variables (or the matching `m3m0s serve` flags). With Docker Compose, put them in `.env`.

## Server

| Variable | Default | Purpose |
|---|---|---|
| `OPENAI_API_KEY` | none (keyword-only) | Key for embeddings (`text-embedding-3-small`). Without it the server runs keyword-only: full-text search and exact-match dedup. |
| `M3M0S_OPENAI_BASE_URL` | OpenAI | Alternative OpenAI-compatible embeddings endpoint. |
| `M3M0S_PORT` | `19234` | HTTP port. |
| `M3M0S_DATA_DIR` | `~/.m3m0s/data` (`/data` in the image) | Where the SQLite database lives. |
| `M3M0S_HOST_PORT` | `19234` | Host port published by `compose.yaml`. |
| `M3M0S_ADD_SYNC_TIMEOUT` | `300ms` | How long `add` waits before answering `202` and finishing in the background. |
| `GOGC` | `50` | Go GC target. Lower values trade CPU for a lower memory peak. |
| `GOMEMLIMIT` | not set | Optional soft memory limit, for example `600MiB`. |

## Fact extraction and chat (optional)

Extraction turns one input into several short atomic facts. It stays off until you set a key.

| Variable | Default | Purpose |
|---|---|---|
| `M3M0S_EXTRACT_API_KEY` | none (off) | Enables LLM fact extraction. |
| `M3M0S_EXTRACT_BASE_URL` | `https://openrouter.ai/api/v1` | OpenAI-compatible endpoint for extraction. |
| `M3M0S_EXTRACT_MODEL` | `deepseek/deepseek-v4-flash` | Extraction model. |
| `M3M0S_CHAT_API_KEY` | extraction key | Enables the WebUI chat. |
| `M3M0S_CHAT_BASE_URL`, `M3M0S_CHAT_MODEL` | OpenRouter, `deepseek/deepseek-v4-flash` | Chat provider and model. |

## Background jobs

| Variable | Default | Purpose |
|---|---|---|
| `M3M0S_RETRO_EXTRACT_INTERVAL` | `2h` | Retroactive extraction job interval. |
| `M3M0S_FORGET_SWEEP_INTERVAL` | `12h` | Forgetting sweep interval. |
| `M3M0S_RETENTION_MODEL` | `legacy` | Score the sweep uses: `legacy` (one 30-day half-life) or `per-kind` (half-life by nature of fact, opt-in). |
| `M3M0S_REEMBED_BATCH`, `M3M0S_REEMBED_RPS` | `64`, `5` | Batch size and request rate of the re-embedding job. |
| `M3M0S_JOBS_TICK` | `30s` | Scheduler heartbeat. |
| `M3M0S_INSIGHTS_CPU_PERCENT` | `60` | CPU duty cycle of the memory-health analysis. |
| `M3M0S_INSIGHTS_MAX_MINUTES` | `30` | Wall-clock limit of that analysis. |

## Ingestion filter

Automatic capture drops or tags tooling boilerplate before storing it.

| Variable | Default | Purpose |
|---|---|---|
| `M3M0S_FILTER_MODE` | `default` | Global mode: `default`, `off`, `tag` or `drop`. |
| `M3M0S_FILTER_MODES` | none | Per-rule modes that win over the global one, for example `diff-trivia-fact=drop`. |
| `M3M0S_FILTER_RULES` | none | JSON file of rules that replace, add or disable built-in ones. |

## Clients

These are read by the CLI, the MCP bridge, the watcher and the hooks.

| Variable | Default | Purpose |
|---|---|---|
| `M3M0S_URL` | `http://localhost:19234` | Server URL. |
| `M3M0S_API_KEY` | none | API key. |
| `M3M0S_HOOK_DOMAIN` | derived from the working directory | Domain captured memories are filed under. |

Recall tuning variables are on the [Recall and tuning](/guide/recall/) page.

## Embedding provider at runtime

The embedding model, endpoint, dimensions and key can also be changed without a restart, from the WebUI (Embedding page) or the CLI:

```sh
m3m0s embedding show
m3m0s embedding estimate --model text-embedding-3-large --dimensions 1024
echo "$NEW_KEY" | m3m0s embedding set --model text-embedding-3-large --api-key-stdin
m3m0s embedding reembed start --watch
```

The key is never accepted as an argument and never printed. If a change leaves stored vectors in another model or size, `set` shows how many and an approximate cost, and asks you to confirm.
