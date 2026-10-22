# Contributing to m3m0s

Thanks for your interest. m3m0s is a long-term memory service for LLM agents
(HTTP API + MCP + CLI + embedded WebUI). Contributions of all sizes are welcome.

## Ground rules

- **English only.** Code, comments, identifiers, UI strings, log/error
  messages, test names, docs, commit messages, issues and PRs. The only
  exception is test data that deliberately exercises non-English input, and it
  must be explained by a comment.
- **No personal or infrastructure data.** Nothing in the repo may reveal
  someone's real name or e-mail (authorship and `LICENSE` excepted), home
  directories, hostnames, IPs, ports, private notes, credentials, API keys,
  tokens or `.env` contents. Use placeholders (`example.com`, `/path/to/repo`)
  and environment variables.
- **Never paste real memory data** (database files, transcripts, API keys) in
  issues, PRs or tests. Use synthetic fixtures.
- Be kind. See the [Code of Conduct](CODE_OF_CONDUCT.md).

## Setup

Requirements:

- Go (the version in `go.mod`, currently 1.25.x)
- Node.js (current LTS) and npm, for the WebUI in `webui/`
- Docker, optional (to build and run the container image)

```sh
git clone https://github.com/rafaelvieiras/m3m0s.git
cd m3m0s
cp .env.example .env   # fill in the keys you need; never commit .env
go run ./cmd/m3m0s serve
```

The repository tracks only a placeholder `webui/dist/index.html`, so
`go build` works on a fresh clone. To get the real WebUI embedded:

```sh
cd webui && npm ci && npm run build
```

## Commit and PR titles

PRs are squash-merged, so the **PR title** becomes the commit on `main` and
must follow [Conventional Commits](https://www.conventionalcommits.org/); a CI
check enforces it. Releases and the changelog are generated from these titles.

```
<type>(<optional scope>): <subject>
```

- Types: `feat` (minor bump), `fix` and `perf` (patch bump), `refactor`,
  `docs`, `test`, `build`, `ci`, `chore`, `revert`.
- Breaking change: add `!` after the type (`feat(api)!: ...`) or a
  `BREAKING CHANGE:` footer (major bump).
- The subject starts in lowercase and has no trailing period.
- Do not edit `CHANGELOG.md` or version numbers by hand: release-please opens
  a release PR with the next version and the changelog; merging it tags and
  publishes the release.
- Scopes name the area: `hook`, `search`, `mcp`, `ingest`, `webui`, `pi`,
  `opencode`, `hermes`, `claude-code`.

## Checks before opening a PR

```sh
gofmt -l .                 # must print nothing
go vet ./...
go test ./...
cd webui && npm ci && npm run build   # also: npm run lint, npm test
```

CI runs the same checks plus a Docker image build.

## Project layout

See [`AGENTS.md`](AGENTS.md) for the project map, conventions and how the
packages under `internal/` fit together. Design decisions live in
[`docs/decisions/`](docs/decisions/README.md).

## Workflow

1. **Open or pick an issue.** Use the templates (bug, feature, decision). For
   anything non-trivial, agree on the approach in the issue first.
2. **Branch** from `main` (e.g. `fix/hook-spool`, `feat/hybrid-search`).
3. **Open a PR** against `main` with `Closes #N` in the description and fill in
   the checklist. Keep PRs focused; one logical change per PR.
4. A maintainer reviews. CI must be green.

Issues use the structure **Problem / Proposal / Open questions / Acceptance**.

## Commit conventions

- English, short, imperative: `Fix hook retry on connection refused`.
- An optional conventional prefix is fine: `feat:`, `fix:`, `docs:`,
  `chore:`, optionally scoped (`fix(hook): ...`).
- Commits are human-owned. Do not add AI/LLM/tool co-author trailers or
  "Generated with ..." footers. A `Co-Authored-By:` for another human is fine.

## Decision records

Architectural or behavior-defining choices are recorded in `docs/decisions/`.
To propose one, open an issue titled `Decide: <topic>` using the decision
template and discuss it there. Once agreed, add a record file (and an index
entry) in the PR that implements it, keeping the existing ID scheme.

## Reporting bugs

Open a bug issue with the version or commit, how you run m3m0s (Docker or
binary), steps to reproduce, expected vs actual behavior, and logs with all
secrets removed. For security problems, do **not** open a public issue; see
[SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
project's [MIT License](LICENSE).

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please)
(`release-please-config.json`). Each component has its own version, tag and
changelog, derived from the conventional commits that touch its directory:

| Component | Path | Tag | Published to |
|---|---|---|---|
| m3m0s (server, CLI, hooks) | `/` | `vX.Y.Z` | GitHub release (binaries), ghcr image |
| pi extension | `integrations/pi` | `pi-vX.Y.Z` | npm `m3m0s-pi` |
| opencode plugin | `integrations/opencode` | `opencode-vX.Y.Z` | npm `m3m0s-opencode` |
| Claude Code plugin | `integrations/claude-code` | `claude-code-vX.Y.Z` | marketplace in this repo (`.claude-plugin/marketplace.json`) |
| Hermes plugin | `integrations/hermes` | `hermes-vX.Y.Z` | `hermes plugins install` from this repo |

Merge the open "release" PR to cut a release. npm publishing uses trusted
publishing (OIDC, no stored token) and `npm stage publish`: after the
workflow runs, approve the staged version on npmjs.com (2FA).

### First npm publish of a package (one time)

npm cannot configure trusted publishing for a package that does not exist yet,
so the first version is published by hand:

```sh
cd integrations/pi            # or integrations/opencode
npm login --auth-type=web
npm publish --access public
npm trust github m3m0s-pi \
  --repo rafaelvieiras/m3m0s --file release-please.yml --allow-stage-publish
```

The trust configuration must see a successful publish from CI within 2 days
or it expires. Requires npm 11.15 or later and 2FA on the account.

## Website

The site (`https://m3m0s.rafaelvieiras.com`) lives in `site/` (Astro). The home
page is in `site/src/pages/index.astro` and the guide is Markdown in
`site/src/content/guide/`. Open Graph images are generated at build time.

```sh
cd site && npm ci && npm run dev      # local preview
npm run build                          # static output in site/dist
```

Deploy (Cloudflare Workers static assets, custom domain in `wrangler.jsonc`):

```sh
cd site && npm run build
CLOUDFLARE_ACCOUNT_ID=<account id> npx wrangler deploy
```

Brand assets (icon, wordmark and banner, light and dark) are in `docs/brand/`.
