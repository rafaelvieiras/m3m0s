// Thin HTTP client for `m3m0s serve`, faithful to docs/api-contract.md.
// Central interceptor: injects "Authorization: Bearer <key>" into every call
// (read from localStorage via lib/auth.ts) and logs out automatically on 401.
// Every TanStack Query hook (src/hooks) goes through here — no raw
// `useEffect`+`fetch` in components.
import { z } from "zod";
import { clearApiKey, getApiKey } from "./auth";
import {
  addResponseSchema,
  chatResponseSchema,
  entrySchema,
  errorBodySchema,
  jobRunResponseSchema,
  jobStatusSchema,
  jobsListResponseSchema,
  previewInjectionResponseSchema,
  recentResponseSchema,
  searchResponseSchema,
  statsResponseSchema,
  updateResponseSchema,
  type AddFormValues,
  type AddResponse,
  type ChatMessagePayload,
  type ChatResponse,
  type Entry,
  type JobRunResponse,
  type JobStatus,
  type JobsListResponse,
  type PreviewInjectionMode,
  type PreviewInjectionResponse,
  type RecentResponse,
  type SearchFormValues,
  type SearchResponse,
  type StatsResponse,
  type UpdatePayload,
} from "./schemas";

// Empty default = same-origin: in production `m3m0s serve` itself serves the
// WebUI (docs/api-contract.md, section "/api/v1 prefix and embedded WebUI"),
// so a relative path already lands in the right place. VITE_M3M0S_URL only
// exists as an override to point at a remote `serve` outside the common case
// — in dev, the Vite proxy (vite.config.ts) covers the local case without
// needing this variable.
const BASE_URL = (import.meta.env.VITE_M3M0S_URL ?? "").replace(/\/$/, "");
const API_PREFIX = "/api/v1";

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// request performs the call, injects auth, and validates the response body
// against the expected Zod schema before returning — never trusts raw JSON
// from the network.
async function request<T>(
  path: string,
  init: RequestInit,
  schema: z.ZodType<T>,
): Promise<T> {
  const apiKey = getApiKey();
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (apiKey) headers.set("Authorization", `Bearer ${apiKey}`);

  const res = await fetch(`${BASE_URL}${API_PREFIX}${path}`, { ...init, headers });

  if (!res.ok) {
    if (res.status === 401) {
      // Invalid/expired session — log out and force a reload to the login
      // screen (avoids a partially authenticated React state lingering in
      // memory).
      clearApiKey();
      if (typeof window !== "undefined" && window.location.pathname !== "/login") {
        window.location.assign("/login");
      }
    }
    const body = await res.json().catch(() => null);
    const parsedError = errorBodySchema.safeParse(body);
    if (parsedError.success) {
      throw new ApiError(res.status, parsedError.data.error.code, parsedError.data.error.message);
    }
    throw new ApiError(res.status, "unknown_error", `HTTP ${res.status}`);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const json = await res.json();
  const parsed = schema.safeParse(json);
  if (!parsed.success) {
    throw new Error(`Unexpected response from ${path}: ${parsed.error.message}`);
  }
  return parsed.data;
}

// API key validation on the login screen (D13, item 2): there is no "who am
// I" endpoint, so we use GET /stats as a real authentication probe — if the
// key is invalid, the server returns 401 before anything else
// (docs/api-contract.md, section Auth).
export async function validateApiKey(key: string): Promise<boolean> {
  const res = await fetch(`${BASE_URL}${API_PREFIX}/stats`, {
    headers: { Authorization: `Bearer ${key}` },
  });
  return res.ok;
}

export function getStats(): Promise<StatsResponse> {
  return request("/stats", { method: "GET" }, statsResponseSchema);
}

export interface SearchParams {
  query: string;
  limit?: number;
  domain?: string;
  domainCategory?: string;
  source?: string;
  agent?: string;
  model?: string;
  project?: string;
  tags?: string[];
}

export function search(params: SearchParams): Promise<SearchResponse> {
  return request(
    "/search",
    { method: "POST", body: JSON.stringify(params) },
    searchResponseSchema,
  );
}

export interface RecentParams {
  limit?: number;
  domain?: string;
  domainCategory?: string;
  source?: string;
  agent?: string;
  model?: string;
  project?: string;
}

export function recent(params: RecentParams): Promise<RecentResponse> {
  const qs = new URLSearchParams();
  if (params.limit) qs.set("limit", String(params.limit));
  if (params.domain) qs.set("domain", params.domain);
  if (params.domainCategory) qs.set("domainCategory", params.domainCategory);
  if (params.source) qs.set("source", params.source);
  if (params.agent) qs.set("agent", params.agent);
  if (params.model) qs.set("model", params.model);
  if (params.project) qs.set("project", params.project);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return request(`/recent${suffix}`, { method: "GET" }, recentResponseSchema);
}

export function addMemory(payload: AddFormValues): Promise<AddResponse> {
  return request(
    "/memories",
    { method: "POST", body: JSON.stringify(payload) },
    addResponseSchema,
  );
}

// updateMemory builds the body by key presence: keys with an `undefined`
// value in the input no longer appear in the object (the form only populates
// what the user touched), and JSON.stringify automatically omits any that
// remain `undefined` — exactly reproducing the PATCH semantics of
// docs/api-contract.md (field absent from the body = leave untouched).
export function updateMemory(id: string, payload: UpdatePayload): Promise<Entry> {
  return request(
    `/memories/${encodeURIComponent(id)}`,
    { method: "PATCH", body: JSON.stringify(payload) },
    updateResponseSchema,
  );
}

export function deleteMemory(id: string): Promise<void> {
  return request(`/memories/${encodeURIComponent(id)}`, { method: "DELETE" }, z.void());
}

// getEntry calls GET /memories/:id (memory_get, later elaboration of Phase
// 5/D13 — docs/api-contract.md) — allows opening the detail/edit screen
// "cold" (direct link, F5, bookmark), closing the limitation that existed
// while the only source of an entry was the TanStack Query cache seeded by
// search/recent/add (see useMemoryApi.ts).
export function getEntry(id: string): Promise<Entry> {
  return request(`/memories/${encodeURIComponent(id)}`, { method: "GET" }, entrySchema);
}

// ── WebUI floating chat (D23) ──

export interface ChatRequestPayload {
  messages: ChatMessagePayload[];
  domain?: string;
}

export function sendChatMessage(payload: ChatRequestPayload): Promise<ChatResponse> {
  return request("/chat", { method: "POST", body: JSON.stringify(payload) }, chatResponseSchema);
}

// ── in-process jobs (D27) ──

export function listJobs(): Promise<JobsListResponse> {
  return request("/jobs", { method: "GET" }, jobsListResponseSchema);
}

export function runJobNow(name: string): Promise<JobRunResponse> {
  return request(`/jobs/${encodeURIComponent(name)}/run`, { method: "POST" }, jobRunResponseSchema);
}

export function setJobInterval(name: string, seconds: number): Promise<JobStatus> {
  return request(
    `/jobs/${encodeURIComponent(name)}`,
    { method: "PATCH", body: JSON.stringify({ intervalSeconds: seconds }) },
    jobStatusSchema,
  );
}

// ── Context injection preview (D29) ──

export interface PreviewInjectionParams {
  mode: PreviewInjectionMode;
  domain?: string;
}

export function previewInjection(params: PreviewInjectionParams): Promise<PreviewInjectionResponse> {
  const qs = new URLSearchParams();
  qs.set("mode", params.mode);
  if (params.domain) qs.set("domain", params.domain);
  return request(`/preview/injection?${qs.toString()}`, { method: "GET" }, previewInjectionResponseSchema);
}

export type { SearchFormValues };
