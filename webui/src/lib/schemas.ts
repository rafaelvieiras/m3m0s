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
  // Nature of the fact (D38): always present on current servers; "other" for
  // memories that were never classified. kindSource: llm | heuristic | manual.
  kind: z.string().optional(),
  kindSource: z.string().optional(),
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
  // Memories per source; "selfdocs" is the built-in documentation. Absent on older servers.
  countBySource: z.record(z.string(), z.number()).optional(),
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
  // One of the factkind values; setting it records "manual" as the source.
  kind?: string;
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

// ── Memory health · /insights (D37) ──
//
// Go encodes an empty slice it never allocated as null, so every list is
// accepted as null and read as [].

const list = <T extends z.ZodType>(item: T) =>
  z
    .array(item)
    .nullish()
    .transform((v) => v ?? []);

const countItemSchema = z.object({ key: z.string(), count: z.number() });
export type CountItem = z.infer<typeof countItemSchema>;

const domainStatSchema = z.object({
  domain: z.string(),
  count: z.number(),
  never_accessed: z.number(),
  expired: z.number(),
});

const shadowKindSchema = z.object({
  kind: z.string(),
  eligible: z.number(),
  protected: z.number(),
  legacy: list(z.number()),
  per_kind: list(z.number()),
});
export type ShadowKind = z.infer<typeof shadowKindSchema>;

// Shadow-mode comparison of the two retention models (D39).
export const retentionShadowSchema = z.object({
  active: z.string(),
  horizons_days: list(z.number()),
  legacy: list(z.number()),
  per_kind: list(z.number()),
  eligible: z.number(),
  protected: z.number(),
  by_kind: list(shadowKindSchema),
  half_life_days: z.record(z.string(), z.number()).nullish().transform((v) => v ?? {}),
  expired_cap_enabled: z.boolean(),
  expired_cap_days: z.number(),
});
export type RetentionShadow = z.infer<typeof retentionShadowSchema>;

export const forgettingStatsSchema = z.object({
  eligible: z.number(),
  exempt: z.number(),
  cold_now: z.number(),
  soft_deleted: z.number(),
  hard_delete_due: z.number(),
  threshold: z.number(),
  age_half_life_days: z.number(),
  grace_days: z.number(),
  retention_histogram: list(countItemSchema),
  // Absent on servers that predate per-kind retention (D39).
  model: z.string().optional(),
  shadow: retentionShadowSchema.optional(),
});
export type ForgettingStats = z.infer<typeof forgettingStatsSchema>;

const filterRuleStatsSchema = z.object({
  id: z.string(),
  scope: z.string(),
  description: z.string().optional(),
  action: z.string(),
  seen: z.number(),
  dropped: z.number(),
  tagged: z.number(),
});
export type FilterRuleStats = z.infer<typeof filterRuleStatsSchema>;

export const filterOverviewSchema = z.object({
  mode: z.string(),
  rules_version: z.number(),
  rules: list(filterRuleStatsSchema),
});
export type FilterOverview = z.infer<typeof filterOverviewSchema>;

export const insightsOverviewSchema = z.object({
  generated_at: z.iso.datetime({ offset: true }),
  totals: z.object({
    total: z.number(),
    active: z.number(),
    soft_deleted: z.number(),
    with_valid_until: z.number(),
    expired: z.number(),
    pinned: z.number(),
    extracted: z.number(),
    raw: z.number(),
    never_accessed: z.number(),
    never_accessed_pct: z.number(),
  }),
  text: z.object({ avg_chars: z.number(), median_chars: z.number(), p90_chars: z.number(), max_chars: z.number() }),
  soft_deleted_by_day: list(countItemSchema),
  by_category: list(countItemSchema),
  by_source: list(countItemSchema),
  by_age: list(countItemSchema),
  // Nature distribution (D38); absent on servers that predate it.
  by_kind: list(countItemSchema).optional(),
  by_kind_source: list(countItemSchema).optional(),
  domain_count: z.number(),
  top_domains: list(domainStatSchema),
  vectors_indexed: z.number(),
  forgetting: forgettingStatsSchema,
  // Absent on servers that predate the ingestion filter.
  filter: filterOverviewSchema.optional(),
});
export type InsightsOverview = z.infer<typeof insightsOverviewSchema>;

const exampleEntrySchema = z.object({
  id: z.string(),
  domain: z.string(),
  source: z.string(),
  text: z.string().optional(),
});
export type ExampleEntry = z.infer<typeof exampleEntrySchema>;

const nearThresholdSchema = z.object({
  threshold: z.number(),
  pairs: z.number(),
  clusters: z.number(),
  redundant: z.number(),
  largest_cluster: z.number(),
  same_domain: z.number(),
  cross_domain: z.number(),
  cross_source: z.number(),
  same_capture: z.number(),
  numbers_differ: z.number(),
});
export type NearThreshold = z.infer<typeof nearThresholdSchema>;

const noiseCriterionSchema = z.object({
  id: z.string(),
  name: z.string(),
  tier: z.string(),
  description: z.string(),
  matches: z.number(),
  never_accessed: z.number(),
  examples: list(exampleEntrySchema),
});
export type NoiseCriterion = z.infer<typeof noiseCriterionSchema>;

export const hygieneReportSchema = z.object({
  version: z.number(),
  computed_at: z.iso.datetime({ offset: true }),
  duration_ms: z.number(),
  partial: z.boolean(),
  partial_reason: z.string().optional(),
  scanned: z.number(),
  exact_duplicates: z.object({
    groups: z.number(),
    entries: z.number(),
    redundant: z.number(),
    same_domain_groups: z.number(),
    cross_domain_groups: z.number(),
    cross_source_groups: z.number(),
    examples: list(
      z.object({ size: z.number(), domains: z.number(), text: z.string(), members: list(exampleEntrySchema) }),
    ),
  }),
  near_duplicates: z.object({
    available: z.boolean(),
    reason: z.string().optional(),
    method: z.string().optional(),
    indexed: z.number().optional(),
    tables: z.number().optional(),
    bits: z.number().optional(),
    oversized_buckets: z.number().optional(),
    pairs_truncated: z.boolean().optional(),
    thresholds: list(nearThresholdSchema),
    cross_source: list(countItemSchema),
    bands: list(
      z.object({
        label: z.string(),
        examples: list(z.object({ similarity: z.number(), a: exampleEntrySchema, b: exampleEntrySchema })),
      }),
    ),
  }),
  noise: z.object({
    category: z.string(),
    scanned: z.number(),
    criteria: list(noiseCriterionSchema),
    conservative_union: z.number(),
    conservative_eligible: z.number(),
    review_union: z.number(),
  }),
});
export type HygieneReport = z.infer<typeof hygieneReportSchema>;

export const hygieneStatusSchema = z.object({
  running: z.boolean(),
  phase: z.string().optional(),
  done: z.number(),
  total: z.number(),
  started_at: z.iso.datetime({ offset: true }).nullish(),
  finished_at: z.iso.datetime({ offset: true }).nullish(),
  last_error: z.string().optional(),
});
export type HygieneStatus = z.infer<typeof hygieneStatusSchema>;

export const hygieneResponseSchema = z.object({
  status: hygieneStatusSchema,
  report: hygieneReportSchema.nullable(),
});
export type HygieneResponse = z.infer<typeof hygieneResponseSchema>;

export const hygieneStartSchema = z.object({ started: z.boolean() });
export const hygieneCancelSchema = z.object({ cancelled: z.boolean() });
