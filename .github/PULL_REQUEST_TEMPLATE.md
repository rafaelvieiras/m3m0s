## Summary

<!-- What does this change and why? -->

## Linked issue

Closes #

## Checklist

- [ ] `gofmt -l .` is clean, `go vet ./...` and `go test ./...` pass
- [ ] WebUI builds (`cd webui && npm ci && npm run build`) if it was touched
- [ ] Docs updated (README, `docs/`, `CHANGELOG.md` under `[Unreleased]`)
- [ ] English only (code, comments, strings, docs, commit messages)
- [ ] No personal/infrastructure data and no secrets, keys or real memory data
- [ ] Decision record added under `docs/decisions/` if this is architectural
