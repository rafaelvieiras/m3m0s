---
title: Security
description: What m3m0s stores, how it protects it, and how to run it safely.
section: Reference
order: 12
---

m3m0s stores whatever you send it, including session transcripts, and you configure it with provider API keys. Treat the server and its backups like any other system that holds that kind of text.

## What protects your data

- **API keys** guard the HTTP API, the MCP bridge and the CLI. WebUI accounts have a username and a password, and a reset is a single-use 30-minute link.
- **Secret redaction** runs on ingestion, and again before text is sent to an extraction provider. It is best-effort: it cannot catch every format.
- **`m3m0s maintenance redact-scan`** reports secrets that were stored before a redaction rule existed. `--apply` re-redacts the stored source blobs. It is a dry run by default.
- **Read-only MCP mode** keeps agents from rewriting memory. It is a convenience, not a boundary: anything with the API key can call the HTTP API.
- **Providers see what you send them.** Text goes to the embedding, extraction and chat endpoints you configure, and nowhere else is configured by default.

## Run it safely

1. Put the server behind TLS and a trusted network boundary before exposing it. A private network or VPN is better than the open internet.
2. Keep `.env` and `~/.m3m0s/hook.env` out of version control, with mode `600` for the latter.
3. Treat backups and the data volume as sensitive.
4. Use an embedding and extraction provider you trust with the text your agents see, or point `M3M0S_OPENAI_BASE_URL` at a local endpoint.

## Reporting a vulnerability

Do not open a public issue. Use GitHub private vulnerability reporting: open the repository's **Security** tab and choose **Report a vulnerability**. Include the affected version, how you run m3m0s, and steps to reproduce with synthetic data only. See [`SECURITY.md`](https://github.com/rafaelvieiras/m3m0s/blob/main/SECURITY.md) for scope and supported versions.

Never post API keys, transcripts, real memory contents, database files or internal hostnames in public issues or logs. If a secret leaked, rotate it right away.
