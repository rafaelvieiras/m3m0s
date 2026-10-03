# Security Policy

m3m0s stores long-term memory for LLM agents, which can include sensitive
text, session transcripts and credentials mentioned in conversations. We take
reports seriously.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private vulnerability reporting:
go to the repository's **Security** tab and choose **Report a vulnerability**
(GitHub Security Advisories). Include:

- affected version or commit, and how you run m3m0s (Docker or binary);
- a description of the issue and its impact;
- steps or a minimal proof of concept, using synthetic data only.

Response is best effort: we aim to acknowledge reports within a few days and to
keep you updated until the issue is fixed or closed. Please allow reasonable
time for a fix before any public disclosure.

## Supported versions

Only the latest `0.x` release receives security fixes. The project is
pre-1.0; older releases are not patched.

## Scope

In scope:

- API key authentication and authorization (`/api/v1`, MCP);
- secret redaction (`internal/redact`) letting secrets reach storage or logs;
- hooks and transcript capture (what is captured, sent and persisted);
- stored memory data (SQLite database, blob store, API key file);
- the embedded WebUI (XSS, CSRF, auth bypass);
- the container image and default configuration.

Out of scope: vulnerabilities in third-party providers (embedding or LLM APIs),
issues requiring prior full access to the host or the data directory, and
deployments exposed without authentication contrary to the documentation.

## What NOT to post publicly

Never include in public issues, PRs, discussions or logs:

- API keys or tokens (m3m0s keys, provider keys, anything from `.env`);
- session transcripts or real memory contents;
- database files (`*.db`, `*.db-wal`) or the data directory;
- internal hostnames, IPs or other details of your deployment.

If you accidentally exposed a secret, rotate it immediately.
