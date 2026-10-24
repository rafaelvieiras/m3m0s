# D53 — Reversible manual delete: trash and undo (builds on D22)

**Status:** Accepted.

**Context:** the WebUI told people that deleting a memory "cannot be undone", while the soft delete that [D22](D22-memory-layers-automatic-forgetting.md) built for the forget-sweep already existed. A misclick on "Delete" lost data for good. Issue #106 asked for a trash with restore and an undo toast, with permanent delete only from the trash.

**Decision:**

- `DELETE /api/v1/memories/:id` is now a **soft delete**: it sets `metadata.deleted_at`, the same mechanism as the sweep. The existing `forget-sweep` hard-deletes the entry after `SoftDeleteGraceDays = 30`, and `POST /memories/:id/restore` brings it back until then. No new state or job.
- `?purge=true` skips the trash and hard-deletes at once, for content that must not linger (a pasted secret, personal data).
- The response changed from `204` to `200` with a small JSON body (`id`, `purged`, `deletedAt`, `purgeAt`) so a client can tell a trashed entry from a purged one. Callers that only check for a 2xx status keep working.
- `DELETE /api/v1/memories?domain=X&filePath=Y` (used by `m3m0s watch` for reindexing and cascade purge) stays a **hard delete**. Those chunks are regenerated from the file, so a trash would only fill with duplicates.
- New endpoints: `GET /memories/trash` (id, snippet, kind, domain, `deletedAt`, `purgeAt`, newest first), `DELETE /memories/trash/:id` (hard-deletes one entry, and only if it is in the trash, so a stale screen cannot remove a restored memory) and `DELETE /memories/trash` (empties the trash). They use the same auth and CSRF rules as the other handlers.
- Trashed entries stay invisible to `Get`/`Search`/`Recent` as in D22. They are also left out of the counts in `GET /stats` (`count`, `countBySource`, `countByProject`), which would otherwise not drop after a delete; `telemetry.softDeletedCount` still reports them.
- CLI: `m3m0s delete <id> [--purge]` is new (there was no manual-delete verb before; the API contract had left it out because the only caller was `watch`). Restoring is the existing `m3m0s restore <id>`. There is still no `memory_delete` MCP tool.
- WebUI: deleting shows an Undo toast that calls restore, and an Advanced-mode Trash page lists trashed entries with Restore, Delete forever and Empty trash, each destructive action behind a confirm dialog.

A manual soft delete also writes `metadata.deleted_by = "user"` (removed by `Restore`). The opt-in per-kind retention model ([D39](D39-per-kind-retention.md)) spares from hard-delete the entries it would not have soft-deleted itself; without the marker a manually deleted entry could sit in the trash forever, so the marker exempts it from that sparing.

**Consequences:** a manual delete no longer frees space or removes sensitive text until the sweep runs, which is why `?purge=true` and Delete forever exist. The sweep's interval (default 12h) means the real removal can happen shortly after the `purgeAt` date shown. Pinned or non-extracted entries can now be soft-deleted by hand.
