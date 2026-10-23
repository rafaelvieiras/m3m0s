// Thin HTTP client for `m3m0s serve`, faithful to docs/api-contract.md.
// Central interceptor: injects "Authorization: Bearer <key>" into every call
// (read from localStorage via lib/auth.ts) and logs out automatically on 401.
// Every TanStack Query hook (src/hooks) goes through here — no raw
// `useEffect`+`fetch` in components.
import { z } from "zod";
import { clearApiKey, getApiKey } from "./auth";
import { hasKey, translate } from "@/i18n";
import {
  addResponseSchema,
  authStatusSchema,
  meSchema,
  resetInfoSchema,
  chatResponseSchema,
  embeddingConfigSchema,
  hygieneCancelSchema,
  hygieneResponseSchema,
  hygieneStartSchema,
  insightsOverviewSchema,
  entrySchema,
  errorBodySchema,
  jobRunResponseSchema,
  jobStatusSchema,
  jobsListResponseSchema,
  previewInjectionResponseSchema,
  reembedCancelSchema,
  reembedEstimateSchema,
  reembedProgressSchema,
  reembedStartSchema,
  recentResponseSchema,
  searchResponseSchema,
  statsResponseSchema,
  updateResponseSchema,
  type AddFormValues,
  type AddResponse,
  type AuthStatus,
  type Me,
  type ResetInfo,
  type ChatMessagePayload,
  type ChatResponse,
  type EmbeddingConfig,
  type EmbeddingConfigPayload,
  type HygieneResponse,
  type InsightsOverview,
  type Entry,
  type JobRunResponse,
  type JobStatus,
  type JobsListResponse,
  type PreviewInjectionMode,
  type PreviewInjectionResponse,
  type ReembedEstimate,
  type ReembedEstimatePayload,
  type ReembedProgress,
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

// userMessage turns an API error into text for the person using the UI:
// known codes are translated, anything else falls back to the server's own
// message with its first letter capitalized.
function userMessage(code: string, serverMessage: string): string {
  const key = `error.${code}`;
  if (hasKey(key)) return translate(key);
  const trimmed = serverMessage.trim();
  if (!trimmed || code === "unknown_error") return translate("error.unknown");
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}

export class ApiError extends Error {
  code: string;
  status: number;
  /** The raw message from the server, kept for logs and debugging. */
  serverMessage: string;

  constructor(status: number, code: string, message: string) {
    super(userMessage(code, message));
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.serverMessage = message;
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
  // Required by the server for state-changing requests authenticated by the
  // session cookie (a cross-site form cannot set it).
  headers.set("X-M3m0s-Csrf", "1");
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

// ── accounts (D35) ──
//
// The /auth/* endpoints are public or session-based; a 401 from them (wrong
// password, expired link) is an answer, not a logged-out session, so they go
// through authRequest, which never redirects to /login.
async function authRequest<T>(path: string, init: RequestInit, schema: z.ZodType<T> | null): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  headers.set("X-M3m0s-Csrf", "1");
  const apiKey = getApiKey();
  if (apiKey) headers.set("Authorization", `Bearer ${apiKey}`);
  const res = await fetch(`${BASE_URL}${API_PREFIX}${path}`, { ...init, headers });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    const parsed = errorBodySchema.safeParse(body);
    if (parsed.success) {
      throw new ApiError(res.status, parsed.data.error.code, parsed.data.error.message);
    }
    throw new ApiError(res.status, "unknown_error", `HTTP ${res.status}`);
  }
  if (res.status === 204 || !schema) return undefined as T;
  const json = await res.json();
  const parsed = schema.safeParse(json);
  if (!parsed.success) throw new Error(`Unexpected response from ${path}: ${parsed.error.message}`);
  return parsed.data;
}

export function getAuthStatus(): Promise<AuthStatus> {
  return authRequest("/auth/status", { method: "GET" }, authStatusSchema);
}

// getMe returns who the caller is, or null when not signed in.
export async function getMe(): Promise<Me | null> {
  try {
    return await authRequest("/auth/me", { method: "GET" }, meSchema);
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null;
    throw e;
  }
}

export function login(username: string, password: string): Promise<void> {
  return authRequest("/auth/login", { method: "POST", body: JSON.stringify({ username, password }) }, null);
}

export function logout(): Promise<void> {
  return authRequest("/auth/logout", { method: "POST" }, null);
}

// Onboarding needs no API key (D52); it is refused once any account exists.
export function onboard(username: string, password: string, confirm: string): Promise<void> {
  return authRequest("/auth/onboard", { method: "POST", body: JSON.stringify({ username, password, confirm }) }, null);
}

export function getResetInfo(token: string): Promise<ResetInfo> {
  return authRequest(`/auth/reset?token=${encodeURIComponent(token)}`, { method: "GET" }, resetInfoSchema);
}

export function resetPassword(token: string, password: string, confirm: string): Promise<void> {
  return authRequest("/auth/reset", { method: "POST", body: JSON.stringify({ token, password, confirm }) }, null);
}

export function getStats(opts: { excludeSource?: string } = {}): Promise<StatsResponse> {
  const qs = opts.excludeSource ? `?excludeSource=${encodeURIComponent(opts.excludeSource)}` : "";
  return request(`/stats${qs}`, { method: "GET" }, statsResponseSchema);
}

export interface SearchParams {
  query: string;
  limit?: number;
  domain?: string;
  domainCategory?: string;
  source?: string;
  excludeSource?: string;
  agent?: string;
  model?: string;
  project?: string;
  scope?: "global" | "project-only";
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
  excludeSource?: string;
  agent?: string;
  model?: string;
  project?: string;
  scope?: "global" | "project-only";
}

export function recent(params: RecentParams): Promise<RecentResponse> {
  const qs = new URLSearchParams();
  if (params.limit) qs.set("limit", String(params.limit));
  if (params.domain) qs.set("domain", params.domain);
  if (params.domainCategory) qs.set("domainCategory", params.domainCategory);
  if (params.source) qs.set("source", params.source);
  if (params.excludeSource) qs.set("excludeSource", params.excludeSource);
  if (params.agent) qs.set("agent", params.agent);
  if (params.model) qs.set("model", params.model);
  if (params.project) qs.set("project", params.project);
  if (params.scope) qs.set("scope", params.scope);
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

// ── Embedding configuration (D36) ──

export function getEmbeddingConfig(): Promise<EmbeddingConfig> {
  return request("/embedding-config", { method: "GET" }, embeddingConfigSchema);
}

export function putEmbeddingConfig(payload: EmbeddingConfigPayload): Promise<EmbeddingConfig> {
  return request("/embedding-config", { method: "PUT", body: JSON.stringify(payload) }, embeddingConfigSchema);
}

export function estimateReembed(payload: ReembedEstimatePayload): Promise<ReembedEstimate> {
  return request(
    "/embedding-config/reembed/estimate",
    { method: "POST", body: JSON.stringify(payload) },
    reembedEstimateSchema,
  );
}

export function getReembedStatus(): Promise<ReembedProgress> {
  return request("/embedding-config/reembed", { method: "GET" }, reembedProgressSchema);
}

export function startReembed(): Promise<void> {
  return request("/embedding-config/reembed", { method: "POST" }, reembedStartSchema).then(() => undefined);
}

export function cancelReembed(): Promise<void> {
  return request("/embedding-config/reembed", { method: "DELETE" }, reembedCancelSchema).then(() => undefined);
}

// ── Memory health · /insights (D37) ──

/** The built-in docs ("selfdocs") are left out of the user's numbers. */
export function getInsightsOverview(): Promise<InsightsOverview> {
  return request("/insights/overview?excludeSource=selfdocs", { method: "GET" }, insightsOverviewSchema);
}

export function getHygiene(): Promise<HygieneResponse> {
  return request("/insights/hygiene", { method: "GET" }, hygieneResponseSchema);
}

export function startHygiene(): Promise<void> {
  return request("/insights/hygiene/run", { method: "POST" }, hygieneStartSchema).then(() => undefined);
}

export function cancelHygiene(): Promise<void> {
  return request("/insights/hygiene/cancel", { method: "POST" }, hygieneCancelSchema).then(() => undefined);
}
