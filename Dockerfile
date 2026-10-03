# syntax=docker/dockerfile:1

# Multi-stage packaging of M3m0s: builds the WebUI (Vite) and the Go binary
# with the dist embedded via //go:embed (webui/embed.go), and ships a lean
# final image. The build does NOT depend on the webui/dist committed in the
# repo (it only exists so go:embed never breaks a plain `go build` outside
# Docker, docs/decisions/ D13): the builder-web stage generates the real dist
# and the builder-go stage overwrites whatever came from the repo before
# compiling.
#
# No explicit multi-arch cross-compilation set up here — the image is built
# for the host architecture. m3m0s has no C dependency (chromem-go is pure Go,
# CGO_ENABLED=0 already guarantees a static binary), so the image can also run
# on small ARM boards: adding --platform=$BUILDPLATFORM/TARGETARCH for
# cross-builds is trivial if needed.

ARG GO_VERSION=1.25

# ---------------------------------------------------------------------------
# Stage 1 — builder-web: generates the production WebUI build with Vite.
# ---------------------------------------------------------------------------
FROM node:lts-slim AS builder-web
WORKDIR /app/webui

# Dependency layer: only the manifest, which rarely changes, so it caches well.
COPY webui/package.json webui/package-lock.json ./
RUN npm ci

# Frontend source and build. vite.config.ts uses the default outDir (./dist,
# next to this directory) — the same path that webui/embed.go embeds via
# `//go:embed all:dist`.
COPY webui/ ./
RUN npm run build

# ---------------------------------------------------------------------------
# Stage 2 — builder-go: compiles the static binary with the WebUI embedded.
# ---------------------------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS builder-go
WORKDIR /app

# Go dependency layer.
COPY go.mod go.sum ./
RUN go mod download

# Application Go code (webui/ comes in separately below, only what go:embed
# needs — the rest of the Vite project, node_modules etc., is not part of the
# Go module and does not need to be here).
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY webui/embed.go ./webui/

# selfdocs.go (module root) + the AGENTS.md and docs/ files it embeds via
# go:embed (D25, docs/decisions/) — self-indexing of the project's own
# documentation at m3m0s serve boot, so it works in any deployment without
# depending on the host having the files (unlike m3m0s watch, D24). The
# whole docs/ tree is copied, including subfolders such as docs/decisions/.
COPY selfdocs.go ./
COPY AGENTS.md ./
COPY docs/ ./docs/

# Overwrites the placeholder/dist committed in the repo with the REAL dist
# from stage 1, ensuring //go:embed all:dist captures the production build,
# not a potentially stale artifact from the host.
COPY --from=builder-web /app/webui/dist/ ./webui/dist/

# Static binary (no CGO — chromem-go and the rest of the core are pure Go).
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /m3m0s ./cmd/m3m0s

# ---------------------------------------------------------------------------
# Stage 3 — final: minimal image, no Node/Go toolchain.
# ---------------------------------------------------------------------------
FROM alpine:3.24 AS final

# Dedicated non-root user.
RUN addgroup -S m3m0s && adduser -S -G m3m0s -H -h /nonexistent m3m0s

# Only the compiled binary.
COPY --from=builder-go /m3m0s /m3m0s

# Persistent data directory (chromem-go index + apikeys.json). Created and
# chowned to the non-root user BEFORE `USER` — when a NAMED volume is mounted
# at /data for the first time, Docker copies the owner and permissions of the
# image directory into the new volume (avoids the pitfall of a volume being
# created root:root and the non-root process failing to write).
RUN mkdir -p /data && chown m3m0s:m3m0s /data
VOLUME /data

USER m3m0s

EXPOSE 19234

# Healthcheck via busybox wget (present in alpine). GET /health is
# deliberately outside the /api/v1 prefix (docs/api-contract.md) — it is the
# fixed path that orchestrators/healthchecks expect.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:19234/health || exit 1

ENTRYPOINT ["/m3m0s"]
CMD ["serve"]
