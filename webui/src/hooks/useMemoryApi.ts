// TanStack Query hooks for all operations against `m3m0s serve`
// (docs/api-contract.md). The single data access point for components — no
// raw fetch outside here / src/lib/api.ts.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as api from "@/lib/api";
import type { AddFormValues, Entry, UpdatePayload } from "@/lib/schemas";

// ── stats ──

/** includeDocs: also count the built-in documentation memories ("selfdocs"). */
export function useStats(includeDocs = false) {
  return useQuery({
    queryKey: ["stats", { includeDocs }],
    queryFn: () => api.getStats({ excludeSource: includeDocs ? undefined : "selfdocs" }),
  });
}

// ── search (POST /search) ──
//
// Semantically an idempotent read (same query = same result modulo index
// changes), so it fits as a useQuery even though it is a POST on the wire —
// avoids reinventing the cache/retry that TanStack Query already solves. Not
// named `useSearch` on purpose — it would collide with TanStack Router's own
// search-params hook used in the route pages.
export function useSearchMemories(params: api.SearchParams | null) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["search", params],
    queryFn: async () => {
      const res = await api.search(params!);
      seedEntryCache(qc, res.results);
      return res;
    },
    enabled: !!params && params.query.trim().length > 0,
  });
}

// ── recent (GET /recent) ──

export function useRecent(params: api.RecentParams) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["recent", params],
    queryFn: async () => {
      const res = await api.recent(params);
      seedEntryCache(qc, res.results);
      return res;
    },
  });
}

// ── add (POST /memories) ──

export function useAddMemory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: AddFormValues) => api.addMemory(payload),
    onSuccess: (res) => {
      seedEntryCache(
        qc,
        res.entries.map((e) => e.entry),
      );
      qc.invalidateQueries({ queryKey: ["recent"] });
      qc.invalidateQueries({ queryKey: ["stats"] });
    },
  });
}

// ── update (PATCH /memories/:id) ──

export function useUpdateMemory(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: UpdatePayload) => api.updateMemory(id, payload),
    onSuccess: (entry) => {
      qc.setQueryData(["entry", id], entry);
      qc.invalidateQueries({ queryKey: ["recent"] });
      qc.invalidateQueries({ queryKey: ["search"] });
    },
  });
}

// ── delete (DELETE /memories/:id) ──

export function useDeleteMemory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteMemory(id),
    onSuccess: (_data, id) => {
      qc.removeQueries({ queryKey: ["entry", id] });
      invalidateMemoryLists(qc);
    },
  });
}

// ── trash (D53): restore, list, delete forever, empty ──

function invalidateMemoryLists(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["recent"] });
  qc.invalidateQueries({ queryKey: ["search"] });
  qc.invalidateQueries({ queryKey: ["stats"] });
  qc.invalidateQueries({ queryKey: ["trash"] });
}

export function useRestoreMemory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.restoreMemory(id),
    onSuccess: (entry) => {
      qc.setQueryData(["entry", entry.id], entry);
      invalidateMemoryLists(qc);
    },
  });
}

export function useTrash() {
  return useQuery({ queryKey: ["trash"], queryFn: api.listTrash });
}

export function usePurgeTrashed() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.purgeTrashed(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["trash"] });
      qc.invalidateQueries({ queryKey: ["stats"] });
    },
  });
}

export function useEmptyTrash() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.emptyTrash(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["trash"] });
      qc.invalidateQueries({ queryKey: ["stats"] });
    },
  });
}

// ── single entry detail (GET /memories/:id) ──
//
// memory_get (later elaboration of Phase 5/D13, docs/api-contract.md) closes
// the earlier limitation: the detail/edit screen could only show an entry
// that had already appeared in a search/recent/add list in this browser
// session (TanStack Query cache seeded by seedEntryCache below). Now it is a
// regular useQuery against api.getEntry — `initialData` from the existing
// cache (if any) avoids a loading flash when the entry was already seen in
// this session, but there is always a fallback to the real fetch, so a
// direct link/F5/bookmark "cold" works too.
export function useEntry(id: string) {
  const qc = useQueryClient();
  return useQuery<Entry>({
    queryKey: ["entry", id],
    queryFn: () => api.getEntry(id),
    initialData: () => qc.getQueryData<Entry>(["entry", id]),
  });
}

// ── WebUI floating chat (D23, POST /chat) ──
//
// When the response carries a non-empty toolCalls, the LLM saved a new
// memory via the add_memory tool (internal/httpapi/chat.go) — invalidates
// the same caches that useAddMemory invalidates, to reflect the change in
// the rest of the UI without a manual reload.
export function useChatMessage() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: api.ChatRequestPayload) => api.sendChatMessage(payload),
    onSuccess: (res) => {
      if (res.toolCalls.length > 0) {
        qc.invalidateQueries({ queryKey: ["recent"] });
        qc.invalidateQueries({ queryKey: ["search"] });
        qc.invalidateQueries({ queryKey: ["stats"] });
      }
    },
  });
}

// ── context injection preview (D29, GET /preview/injection) ──
//
// enabled: !!params.mode — guards against firing before the page picks an
// initial mode (should not happen in practice, since PreviewPage always
// initializes with a default, but avoids a call with an empty mode if the
// component mounts in a transitional state).
export function usePreviewInjection(params: api.PreviewInjectionParams) {
  return useQuery({
    queryKey: ["preview-injection", params],
    queryFn: () => api.previewInjection(params),
    enabled: !!params.mode,
  });
}

function seedEntryCache(qc: ReturnType<typeof useQueryClient>, entries: Entry[]) {
  for (const entry of entries) {
    qc.setQueryData(["entry", entry.id], entry);
  }
}
