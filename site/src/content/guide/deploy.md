---
title: Deploy
description: Run m3m0s on a server with Docker Compose or the plain binary, behind TLS, with backups and upgrades.
section: Deploy
order: 3
---

m3m0s stores whatever you send it, including conversation transcripts. Put it behind TLS and a trusted network boundary before exposing it to the internet, and read the [Security](/guide/security/) page first.

## Docker Compose

The repository ships a `compose.yaml` that builds the image from the root `Dockerfile` (a multi-stage build: WebUI with Vite, then a static Go binary with the WebUI embedded). The image is pure Go with no cgo, so it also builds and runs on small ARM boards.

```sh
cp .env.example .env
docker compose up -d --build
```

- The port published on the host is `M3M0S_HOST_PORT` (default `19234`).
- Data lives in the named volume `m3m0s-data`, mounted at `/data`. It holds the SQLite database and the API key file. A named volume (not a bind mount) keeps the right owner for the non-root user inside the image.
- The image has a built-in healthcheck on `GET /health`.
- Secrets come from `.env`. Keep it out of version control.

Images are also published to `ghcr.io/rafaelvieiras/m3m0s`, tagged by commit (`sha-<commit>`). If you deploy through a GitOps tool, point it at that image instead of building on the host.

## Behind a reverse proxy

Terminate TLS in front of the container. With Caddy, for example:

```caddyfile
memory.example.com {
    reverse_proxy localhost:19234
}
```

Nothing else is needed: the WebUI and the API share the same port.

## Without Docker

Download the archive for your architecture from the [releases page](https://github.com/rafaelvieiras/m3m0s/releases) and run it:

```sh
M3M0S_DATA_DIR=/var/lib/m3m0s OPENAI_API_KEY=sk-... m3m0s serve
```

A minimal systemd unit:

```ini
[Unit]
Description=m3m0s memory server
After=network-online.target

[Service]
User=m3m0s
EnvironmentFile=/etc/m3m0s.env
Environment=M3M0S_DATA_DIR=/var/lib/m3m0s
ExecStart=/usr/local/bin/m3m0s serve
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

To build from source with the WebUI embedded, run `npm ci && npm run build` in `webui/` first, then `go build ./cmd/m3m0s`.

## Small hosts

On a board with little RAM, set a soft memory limit so the Go runtime collects earlier, for example `GOMEMLIMIT=600MiB`. Keep it above the live size, which is about 250 MB for 75,000 memories. `GOGC` defaults to 50, which already trades a little CPU for a lower peak.

## Backups

All state is in the data directory. For a consistent copy, stop the container, archive the volume and start it again:

```sh
docker compose stop m3m0s
docker run --rm -v m3m0s-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/m3m0s-data.tgz -C /data .
docker compose start m3m0s
```

Memory can contain secrets that were stored before a redaction rule existed. Treat backups as sensitive. `m3m0s maintenance redact-scan` reports those cases, and `--apply` re-redacts the stored source blobs (a dry run by default).

## Upgrading

```sh
git pull
docker compose up -d --build
```

The database schema is migrated on start through versioned migrations. Take a backup first when you cross several releases.
