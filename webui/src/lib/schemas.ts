// Zod schemas mirroring docs/api-contract.md — used both to validate forms
// (integrated with TanStack Form) and to parse/validate the HTTP responses of
// `m3m0s serve` before trusting them.
import { z } from "zod";

// `^[a-z0-9_]+:[a-z0-9._-]+$` — docs/memory-schema.md / internal/schema.ValidateDomain.
export const domainRegex = /^[a-z0-9_]+:[a-z0-9._-]+$/;

export const domainSchema = z
  .string()
  .min(1, "domain is required")
  .regex(domainRegex, "invalid format — expected category:instance (e.g. code:m3m0s)");

// metadata_json (docs/memory-schema.md) only supports string values — it
// does not nest objects/arrays, same limitation as chromem-go's
// map[string]string.
export const metadataSchema = z.record(z.string(), z.string());

export const tagsSchema = z.array(z.string().min(1));

// MemoryEntry (docs/api-contract.md, "Common format").
export const entrySchema = z.object({
  id: z.string(),
  content: z.string(),
  domain: z.string(),
  domainCategory: z.string(),
  source: z.string(),
  agent: z.string().nullable(),
  // Provenance (F2): null/absent = unknown model, global entry.
  model: z.string().nullable().optional(),
  project: z.string().nullable().optional(),
  tags: z.array(z.string()),
  metadata: metadataSchema,
  score: z.number().optional(),
  createdAt: z.iso.datetime({ offset: true }),
  updatedAt: z.iso.datetime({ offset: true }),
});
export type Entry = z.infer<typeof entrySchema>;

// ── memory_add · POST /memories ──

export const addFormSchema = z.object({
  content: z.string().min(1, "content is required"),
  domain: domainSchema,
  source: z.string().optional(),
  agent: z.string().optional(),
  model: z.string().optional(),
  project: z.string().optional(),
  tags: tagsSchema,
  metadata: metadataSchema,
});
export type AddFormValues = z.infer<typeof addFormSchema>;

export const addedEntrySchema = z.object({
  entry: entrySchema,
  deduped: z.boolean(),
});

export const addResponseSchema = z.object({
  entries: z.array(addedEntrySchema),
  extracted: z.boolean(),
});
export type AddResponse = z.infer<typeof addResponseSchema>;

// ── memory_search · POST /search ──

export const searchFormSchema = z.object({
  query: z.string().min(1, "query is required"),
  limit: z.number().int().positive().optional(),
  domain: z.string().optional(),
  domainCategory: z.string().optional(),
  source: z.string().optional(),
  agent: z.string().optional(),
  model: z.string().optional(),
  project: z.string().optional(),
  scope: z.enum(["global", "project-only"]).optional(),
  tags: z.array(z.string()).optional(),
});
export type SearchFormValues = z.infer<typeof searchFormSchema>;

export const searchResponseSchema = z.object({
  results: z.array(entrySchema),
  contextBlock: z.string(),
});
export type SearchResponse = z.infer<typeof searchResponseSchema>;

// ── memory_recent · GET /recent ──

export const recentResponseSchema = z.object({
  results: z.array(entrySchema),
});
export type RecentResponse = z.infer<typeof recentResponseSchema>;

// ── memory_stats · GET /stats ──

export const extractionInfoSchema = z.object({
  enabled: z.boolean(),
  model: z.string().nullable(),
});

export const telemetryInfoSchema = z.object({
  apiKeyCount: z.number(),
  memoryUsageMB: z.number(),
  databaseSizeMB: z.number(),
  uptimeSeconds: z.number(),
  extractionTokensUsed: z.number(),
  tokensSavedByCompaction: z.number(),
});

export const statsResponseSchema = z.object({
  count: z.number(),
  countByDomainCategory: z.record(z.string(), z.number()),
  backend: z.string(),
  model: z.string(),
  extraction: extractionInfoSchema,
  telemetry: telemetryInfoSchema,
});
export type StatsResponse = z.infer<typeof statsResponseSchema>;

// ── memory_update · PATCH /memories/:id ──
//
// UpdateResponse is the full Entry (docs/api-contract.md) — reuses
// entrySchema. The request payload has NO parse schema (it is built by hand
// in lib/api.ts, see the comment there about key-presence semantics via
// `undefined` vs. an explicit value).
export const updateResponseSchema = entrySchema;

// PATCH /memories/:id payload — absent fields (undefined) are omitted from
// the JSON sent (JSON.stringify already does this natively), present fields
// replace the whole value (docs/api-contract.md, memory_update). `agent`
// accepts an explicit null to clear it.
export interface UpdatePayload {
  content?: string;
  domain?: string;
  source?: string;
  agent?: string | null;
  model?: string | null;
  project?: string | null;
  tags?: string[];
  metadata?: Record<string, string>;
}

// ── WebUI floating chat · POST /chat (D23) ──

export const chatMessageSchema = z.object({
  role: z.enum(["user", "assistant"]),
  content: z.string(),
});
export type ChatMessagePayload = z.infer<typeof chatMessageSchema>;

export const chatToolCallResultSchema = z.object({
  name: z.string(),
  arguments: z.string(),
  resultSummary: z.string(),
});

export const chatResponseSchema = z.object({
  reply: z.string(),
  toolCalls: z.array(chatToolCallResultSchema),
  contextEntryIds: z.array(z.string()),
});
export type ChatResponse = z.infer<typeof chatResponseSchema>;

// ── In-process jobs · GET/POST/PATCH /jobs (D27) ──

export const jobRunSchema = z.object({
  startedAt: z.iso.datetime({ offset: true }),
  durationMs: z.number(),
  success: z.boolean(),
  summary: z.string().optional(),
  error: z.string().optional(),
});
export type JobRun = z.infer<typeof jobRunSchema>;

export const jobStatusSchema = z.object({
  name: z.string(),
  intervalSeconds: z.number(),
  running: z.boolean(),
  lastRunAt: z.iso.datetime({ offset: true }).nullable().optional(),
  lastDurationMs: z.number().optional(),
  lastSuccess: z.boolean().nullable().optional(),
  lastSummary: z.string().optional(),
  lastError: z.string().optional(),
  nextRunAt: z.iso.datetime({ offset: true }),
  history: z.array(jobRunSchema),
});
export type JobStatus = z.infer<typeof jobStatusSchema>;

export const jobsListResponseSchema = z.object({
  jobs: z.array(jobStatusSchema),
});
export type JobsListResponse = z.infer<typeof jobsListResponseSchema>;

export const jobRunResponseSchema = z.object({
  started: z.boolean(),
  message: z.string().optional(),
});
export type JobRunResponse = z.infer<typeof jobRunResponseSchema>;

// ── Context injection preview · GET /preview/injection (D29) ──

export const previewInjectionModeSchema = z.enum(["hook", "mcp", "manual"]);
export type PreviewInjectionMode = z.infer<typeof previewInjectionModeSchema>;

export const previewInjectionResponseSchema = z.object({
  mode: previewInjectionModeSchema,
  domain: z.string(),
  mechanism: z.string(),
  entriesFetched: z.number(),
  entriesUsed: z.number(),
  raw: z.string(),
});
export type PreviewInjectionResponse = z.infer<typeof previewInjectionResponseSchema>;

// ── Errors (common format across the 3 surfaces) ──

export const errorBodySchema = z.object({
  error: z.object({
    code: z.string(),
    message: z.string(),
  }),
});
export type ErrorBody = z.infer<typeof errorBodySchema>;


// ── accounts (D35) · /auth/* ──

export const authStatusSchema = z.object({ needsOnboarding: z.boolean() });
export type AuthStatus = z.infer<typeof authStatusSchema>;

export const meSchema = z.object({
  via: z.enum(["session", "apikey"]),
  username: z.string().optional(),
});
export type Me = z.infer<typeof meSchema>;

export const resetInfoSchema = z.object({
  username: z.string(),
  kind: z.enum(["reset", "invite"]),
});
export type ResetInfo = z.infer<typeof resetInfoSchema>;

// Mirrors internal/auth: length-only policy.
export const MIN_PASSWORD_LENGTH = 10;
export const MAX_PASSWORD_LENGTH = 128;
export const usernameRegex = /^[a-z0-9][a-z0-9._-]{2,31}$/;

// ── Embedding configuration · /embedding-config (D36) ──
//
// The API key is write-only: responses carry only has_api_key/key_source.

export const reembedProgressSchema = z.object({
  running: z.boolean(),
  model: z.string().optional(),
  dimensions: z.number().optional(),
  total: z.number(),
  done: z.number(),
  errors: z.number(),
  skipped: z.number().default(0),
  eta_seconds: z.number(),
  started_at: z.iso.datetime({ offset: true }).nullable().optional(),
  finished_at: z.iso.datetime({ offset: true }).nullable().optional(),
  last_error: z.string().optional(),
});
export type ReembedProgress = z.infer<typeof reembedProgressSchema>;

export const embeddingConfigSchema = z.object({
  enabled: z.boolean(),
  base_url: z.string(),
  model: z.string(),
  dimensions: z.number(),
  vector_dims: z.number(),
  source: z.string(),
  has_api_key: z.boolean(),
  key_source: z.string(),
  active: z.boolean(),
  vectors: z.object({ total: z.number(), current: z.number(), stale: z.number() }),
  reembed: reembedProgressSchema,
  reembed_started: z.boolean().optional(),
  updated_at: z.iso.datetime({ offset: true }).nullable().optional(),
});
export type EmbeddingConfig = z.infer<typeof embeddingConfigSchema>;

export const reembedEstimateSchema = z.object({
  model: z.string(),
  dimensions: z.number(),
  pending: z.number(),
  characters: z.number(),
  estimated_tokens: z.number(),
  approximate: z.boolean(),
  price_per_million_tokens_usd: z.number().nullable(),
  estimated_cost_usd: z.number().nullable(),
  prices_as_of: z.string(),
});
export type ReembedEstimate = z.infer<typeof reembedEstimateSchema>;

export const reembedStartSchema = z.object({ started: z.boolean() });
export const reembedCancelSchema = z.object({ cancelled: z.boolean() });

// PUT body. `api_key` is omitted to keep the saved key, "" clears it.
export interface EmbeddingConfigPayload {
  enabled: boolean;
  base_url: string;
  model: string;
  dimensions: number;
  api_key?: string;
  confirm_reembed?: boolean;
}

export interface ReembedEstimatePayload {
  model?: string;
  base_url?: string;
  dimensions?: number;
}
