// Package apitypes defines the JSON bodies shared between the HTTP server
// (internal/httpapi) and the clients that talk to it (CLI, m3m0s mcp,
// m3m0s watch), as specified in docs/api-contract.md. It avoids duplicating
// the structs on both sides of the same connection.
package apitypes

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// AddRequest is the body of POST /memories.
type AddRequest struct {
	Content string `json:"content"`
	Domain  string `json:"domain"`
	Source  string `json:"source,omitempty"`
	Agent   string `json:"agent,omitempty"`
	// Model is the LLM that produced the content (provenance, optional).
	Model string `json:"model,omitempty"`
	// Project scopes the memory to a project; omitted = global.
	Project  string            `json:"project,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	// IDPrefix is optional (the server defaults to "mem" when absent) — an
	// additive Phase 2 extension (D11): m3m0s watch asks for "chunk" so the
	// IDs make it clear the memory came from a reindexed file chunk. No
	// existing caller needs to change; the field is ignored when omitted.
	IDPrefix string `json:"idPrefix,omitempty"`
	// Capture marks an automatic capture path (the Claude Code hook). Only
	// such requests go through the ingestion boilerplate filter (#53);
	// explicit writes leave it false and are never filtered.
	Capture bool `json:"capture,omitempty"`
}

// AddedEntry is 1 item of AddResponse.Entries — 1 stored memory, plus whether
// it was treated as a duplicate (docs/api-contract.md, memory_add).
type AddedEntry struct {
	Entry   schema.Entry `json:"entry"`
	Deduped bool         `json:"deduped"`
}

// AddResponse is the 200 response of POST /memories. Phase 4 (D12, item 2,
// breaking, no compatibility shim): the shape changed from singular
// ({entry, deduped}) to plural, because 1 `add` can now produce N entries
// when fact extraction (D12) is enabled on the server — each extracted
// atomic fact becomes 1 item of Entries. Extracted indicates whether the
// extraction pipeline actually ran; when false, Entries always has exactly 1
// item (identical to pre-Phase-4 behavior).
//
// Status is additive (D26, docs/decisions/ — async Add): always "completed"
// when this struct is the final response of an Add, whether through the
// direct synchronous path (200) or after being resolved by polling
// GET /memories/pending/:id. omitempty preserves compatibility with any
// existing client that decodes this shape without knowing the field.
type AddResponse struct {
	Entries   []AddedEntry `json:"entries"`
	Extracted bool         `json:"extracted"`
	Status    string       `json:"status,omitempty"`
	// Filtered is how many segments or facts the ingestion boilerplate filter
	// dropped (#53); Entries may then be empty.
	Filtered int `json:"filtered,omitempty"`
}

// AddPendingResponse is the 202 Accepted response of POST /memories when the
// Add pipeline (which may involve LLM fact extraction, D12/D18/D19, taking
// several seconds) does not finish within -add-sync-timeout/
// M3M0S_ADD_SYNC_TIMEOUT (D26, docs/decisions/). Status is always "pending"
// here — the final value (completed/failed) only appears in
// PendingAddResponse, by polling GET /memories/pending/:id.
type AddPendingResponse struct {
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
}

// PendingAddResponse is the 200 response of GET /memories/pending/:id (D26) —
// queried by internal/client.Client.Add itself in transparent polling (the
// caller of Add never sees the 202 nor makes this call manually, except for
// direct use of the HTTP/MCP API outside the Go client). Result is only
// filled when Status is "completed"; Error only when Status is "failed". An
// ID never seen by the server (or already expired from the retention TTL,
// see internal/httpapi/pendingadd.go) answers 404, not this shape.
type PendingAddResponse struct {
	OperationID string       `json:"operationId"`
	Status      string       `json:"status"`
	Result      *AddResponse `json:"result,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// SearchRequest is the body of POST /search.
type SearchRequest struct {
	Query          string `json:"query"`
	Limit          int    `json:"limit,omitempty"`
	Domain         string `json:"domain,omitempty"`
	DomainCategory string `json:"domainCategory,omitempty"`
	Source         string `json:"source,omitempty"`
	Agent          string `json:"agent,omitempty"`
	Model          string `json:"model,omitempty"`
	Project        string `json:"project,omitempty"`
	// Scope is "global" or "project-only"; with a project and no scope the
	// recall is global + that project (D34).
	Scope string   `json:"scope,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	// Kind keeps only memories of that nature (D38); "other" also matches
	// memories that were never classified.
	Kind string `json:"kind,omitempty"`
	// Mode selects the ranking: "hybrid" (default: vector + keyword fused),
	// "vector" or "keyword".
	Mode string `json:"mode,omitempty"`
	// MaxTokens caps the estimated tokens of the returned results (content
	// only, 0 = no cap); lower-ranked results are dropped first.
	MaxTokens int `json:"maxTokens,omitempty"`
}

// SearchResponse is the 200 response of POST /search.
type SearchResponse struct {
	Results      []schema.Entry `json:"results"`
	ContextBlock string         `json:"contextBlock"`
	// EstimatedTokens is the estimated token cost of ContextBlock.
	EstimatedTokens int `json:"estimatedTokens"`
}

// RecentParams are the query params of GET /recent.
type RecentParams struct {
	Limit          int
	Domain         string
	DomainCategory string
	Source         string
	Agent          string
	Model          string
	Project        string
	Scope          string
	Kind           string
}

// RecentResponse is the 200 response of GET /recent.
type RecentResponse struct {
	Results []schema.Entry `json:"results"`
}

// StatsResponse is the 200 response of GET /stats.
type StatsResponse struct {
	Count                 int            `json:"count"`
	CountByDomainCategory map[string]int `json:"countByDomainCategory"`
	// CountByProject lists only projects; global entries are the remainder.
	CountByProject map[string]int `json:"countByProject,omitempty"`
	Backend        string         `json:"backend"`
	Model          string         `json:"model"`
	Extraction     ExtractionInfo `json:"extraction"`
	Telemetry      TelemetryInfo  `json:"telemetry"`
}

// TelemetryInfo is the `telemetry` field of GET /stats — operational metrics
// of the process/database, complementary to Count/CountByDomainCategory
// (which describe the content of the memories, not the service footprint):
//
//   - ApiKeyCount: how many API keys are registered (internal/auth.Store.Count).
//   - MemoryUsageMB: memory of the Go process (runtime.MemStats.Sys — total
//     obtained from the OS, not just the live heap).
//   - DatabaseSizeMB: sum of the files in dataDir (own index + chromem-go
//     persistence).
//   - UptimeSeconds: how long this `m3m0s serve` process has been up.
//   - ExtractionTokensUsed: tokens (prompt+completion) reported by the
//     extraction provider (Phase 4/D12) since the process started — 0 if
//     extraction is off. Does not include embedding tokens, which chromem-go
//     does not expose to the caller.
//   - TokensSavedByCompaction: estimate (characters/token heuristic, not
//     exact tokenization) of how much fact extraction has shrunk the stored
//     content over time — persisted, survives restarts.
//   - SoftDeletedCount: how many entries are soft-deleted (D22, closes A9) —
//     hidden from search/recent/get, awaiting hard-delete by a future
//     ForgetSweep (or Restore before that).
type TelemetryInfo struct {
	APIKeyCount             int     `json:"apiKeyCount"`
	MemoryUsageMB           float64 `json:"memoryUsageMB"`
	DatabaseSizeMB          float64 `json:"databaseSizeMB"`
	UptimeSeconds           float64 `json:"uptimeSeconds"`
	ExtractionTokensUsed    int64   `json:"extractionTokensUsed"`
	TokensSavedByCompaction int64   `json:"tokensSavedByCompaction"`
	SoftDeletedCount        int     `json:"softDeletedCount"`
}

// ExtractionInfo is the `extraction` field of GET /stats (Phase 5, D13) — it
// mirrors whether fact extraction (Phase 4/D12) is enabled on the server
// (M3M0S_EXTRACT_API_KEY/-extract-key present) and, if so, which
// M3M0S_EXTRACT_MODEL is configured. Model is nil (never an empty string)
// when Enabled is false — the consumer should not need to check both fields
// to know whether extraction is off.
type ExtractionInfo struct {
	Enabled bool    `json:"enabled"`
	Model   *string `json:"model"`
}

// UpdateRequest is the body of PATCH /memories/:id (HTTP) and, together with
// "id", the input of the MCP tool memory_update (D13, docs/api-contract.md).
// The 6 editable fields use json.RawMessage — not concrete types with a
// simple pointer (*string/*[]string/*map[string]string) — because that is
// the only way to tell "key absent from the body" from "key present with a
// null/empty value": decoding straight into a simple pointer leaves nil in
// both cases, and the contract requires telling them apart (a PATCH without
// "tags" keeps the current tags, but "tags": [] clears them all).
// json.RawMessage preserves this distinction (absent field => len 0; field
// present with "null" => 4 literal bytes) and, via `omitempty`, makes the
// reverse path work for free for whoever builds the body (the CLI): just set
// RawMessage for the fields whose flag was actually passed, leaving the
// others out of the JSON sent.
type UpdateRequest struct {
	Content  json.RawMessage `json:"content,omitempty"`
	Domain   json.RawMessage `json:"domain,omitempty"`
	Source   json.RawMessage `json:"source,omitempty"`
	Agent    json.RawMessage `json:"agent,omitempty"`
	Model    json.RawMessage `json:"model,omitempty"`
	Project  json.RawMessage `json:"project,omitempty"`
	Tags     json.RawMessage `json:"tags,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	// Kind corrects the nature of the fact (D38): a string from the closed
	// enum; it also records kind_source=manual, which backfills never overwrite.
	Kind json.RawMessage `json:"kind,omitempty"`
}

// HasAnyField reports whether at least one of the 8 editable fields is
// present in the body — used by the HTTP layer to decide on the
// 400 no_fields_to_update error (docs/api-contract.md).
func (r UpdateRequest) HasAnyField() bool {
	return len(r.Content) > 0 || len(r.Domain) > 0 || len(r.Source) > 0 ||
		len(r.Agent) > 0 || len(r.Model) > 0 || len(r.Project) > 0 || len(r.Tags) > 0 || len(r.Metadata) > 0 || len(r.Kind) > 0
}

// UpdateResponse is the 200 response of PATCH /memories/:id: the complete
// updated MemoryEntry, not enveloped — memory_update always acts on exactly
// 1 entry, and never produces N like memory_add with extraction enabled
// (docs/api-contract.md).
type UpdateResponse = schema.Entry

// GetResponse is the 200 response of GET /memories/:id (memory_get, a
// later elaboration after Phase 5/D13, docs/api-contract.md): the complete
// MemoryEntry, not enveloped — same shape as UpdateResponse, since
// memory_get also always acts on exactly 1 entry.
type GetResponse = schema.Entry

// ForgetSweepRequest is the body of POST /memories/forget-sweep (D22,
// docs/decisions/ — closes A9). dryRun=true only reports which entries would
// qualify for soft/hard-delete in this call, without changing anything;
// omitted/false actually executes. An empty body is valid (equivalent to
// {"dryRun": false}).
type ForgetSweepRequest struct {
	DryRun bool `json:"dryRun,omitempty"`
}

// ForgetSweepResponse is the 200 response of POST /memories/forget-sweep.
// *Candidates always reflects which entries qualify in this call (dry-run or
// not); SoftDeleted/HardDeleted are only filled in a real call
// (dryRun=false) — null in every dry-run response.
type ForgetSweepResponse struct {
	DryRun               bool     `json:"dryRun"`
	Evaluated            int      `json:"evaluated"`
	SoftDeleteCandidates []string `json:"softDeleteCandidates"`
	HardDeleteCandidates []string `json:"hardDeleteCandidates"`
	SoftDeleted          []string `json:"softDeleted"`
	HardDeleted          []string `json:"hardDeleted"`
}

// RestoreResponse is the 200 response of POST /memories/:id/restore (D22):
// the complete, already restored MemoryEntry, same shape as
// UpdateResponse/GetResponse.
type RestoreResponse = schema.Entry

// ChatMessage is 1 message of the POST /chat history (D23, docs/decisions/)
// — role "user" or "assistant" (the system prompt is built by the server and
// never comes from the caller).
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the body of POST /chat. Domain is optional — when present,
// it scopes the context search (RAG) by domain and becomes the suggested
// default for the add_memory tool when the user does not specify another.
type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
	Domain   string        `json:"domain,omitempty"`
}

// ChatToolCallResult summarizes 1 tool call the LLM triggered while serving
// POST /chat — today only the add_memory tool exists (D23).
type ChatToolCallResult struct {
	Name          string `json:"name"`
	Arguments     string `json:"arguments"`
	ResultSummary string `json:"resultSummary"`
}

// ChatResponse is the 200 response of POST /chat. ToolCalls is empty (not
// nil) when the LLM answered without triggering any tool. ContextEntryIDs
// lists the IDs of the memories retrieved via RAG and injected into the
// prompt of this call (may be empty if the search found nothing or failed —
// best-effort, never breaks the response).
type ChatResponse struct {
	Reply           string               `json:"reply"`
	ToolCalls       []ChatToolCallResult `json:"toolCalls"`
	ContextEntryIDs []string             `json:"contextEntryIds"`
}

// DeleteByFilePathResponse is the 200 response of
// DELETE /memories?domain=X&filePath=Y (D11/docs/api-contract.md, memory_delete
// section). Delete by ID has no success body (204).
type DeleteByFilePathResponse struct {
	DeletedCount int `json:"deletedCount"`
}

// ── Jobs (D27, docs/decisions/) — visibility/control of internal/jobs.Scheduler ──

// JobRun is 1 execution recorded in a job's history — an item of
// JobStatus.History (most recent first).
type JobRun struct {
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Success    bool      `json:"success"`
	Summary    string    `json:"summary,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// JobStatus is the snapshot of 1 job registered in the Scheduler, an item of
// JobsListResponse.Jobs (GET /api/v1/jobs). Last* is empty/nil when the job
// has never run in this process instance (History is also empty then).
type JobStatus struct {
	Name            string     `json:"name"`
	IntervalSeconds int64      `json:"intervalSeconds"`
	Running         bool       `json:"running"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
	LastDurationMs  int64      `json:"lastDurationMs,omitempty"`
	LastSuccess     *bool      `json:"lastSuccess,omitempty"`
	LastSummary     string     `json:"lastSummary,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	NextRunAt       time.Time  `json:"nextRunAt"`
	// Progress is present only for jobs that report it (re-embedding, D36).
	Progress *JobProgress `json:"progress,omitempty"`
	// History is always non-nil (may be empty), most recent first, capped at
	// the last ~20 executions (internal/jobs.historyLimit).
	History []JobRun `json:"history"`
}

// JobProgress is the live progress of a long job.
type JobProgress struct {
	Done       int `json:"done"`
	Total      int `json:"total"`
	Errors     int `json:"errors"`
	ETASeconds int `json:"etaSeconds"` // 0 = unknown
}

// JobsListResponse is the 200 response of GET /api/v1/jobs.
type JobsListResponse struct {
	Jobs []JobStatus `json:"jobs"`
}

// JobRunResponse is the 200 response of POST /api/v1/jobs/:name/run.
type JobRunResponse struct {
	Started bool   `json:"started"`
	Message string `json:"message,omitempty"`
}

// JobIntervalRequest is the body of PATCH /api/v1/jobs/:name.
// IntervalSeconds must be positive (400 invalid_interval otherwise).
type JobIntervalRequest struct {
	IntervalSeconds int64 `json:"intervalSeconds"`
}

// ErrorBody is the error format common to the 3 surfaces (docs/api-contract.md).
// The field is called ErrorInfo in Go (it cannot be "Error", which would
// collide with the method below), but serializes as "error" in JSON.
type ErrorBody struct {
	ErrorInfo struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e ErrorBody) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorInfo.Code, e.ErrorInfo.Message)
}

// MetadataKeySourceContent is the metadata key that signals that an entry
// actually went through the fact extraction pipeline (Phase 4/D12) — it
// carries the original raw text the fact was extracted from. It mirrors
// (same literal value, "source_content") the unexported constant
// metadataKeySourceContent in internal/store/store.go: the two sides do not
// share the constant directly so as not to create an apitypes→store
// dependency (which does not exist today and should not — apitypes is the
// HTTP contract, store is a server implementation). Used by the
// SessionStart hook (D17) to filter out of the injected digest any entry
// without extraction — e.g. the raw-content fallback when extraction times
// out (D12 item 5): a whole, uncurated compaction digest is noise (or worse,
// redundant with the summary Claude Code itself just generated) when
// injected as "relevant context" at the start of a session.
const MetadataKeySourceContent = "source_content"

// BuildContextBlock builds the markdown ready for prompt injection, in the
// format documented in docs/api-contract.md. When the entry carries
// metadata.file_path (every chunk generated by m3m0s watch, D11), the main
// line gets a " — <file_path>" suffix and, if metadata.heading is also
// present, " § <heading>" — the chunk's provenance in search. Entries
// brought in by the 1-hop related_to expansion (Phase 7, D15 — non-nil
// RelatedVia) do not take part in the normal numbering: they appear marked
// as "↳ related to <id>" right after the result that brought them in.
func BuildContextBlock(entries []schema.Entry) string {
	return buildContextBlock(entries)
}

// contextSnippetChars is how much of an entry's content BuildContextBlock
// renders; EntryTokens uses the same cut so the budget matches the output.
const contextSnippetChars = 240

// contextEntryOverhead is the estimated token cost of the numbering, domain,
// source and date line around each rendered snippet.
const contextEntryOverhead = 12

// EstimateTokens is a cheap token estimate (about 4 characters per token),
// good enough to enforce a budget without a tokenizer dependency.
func EstimateTokens(s string) int { return (len([]rune(s)) + 3) / 4 }

// EntryTokens estimates the tokens one entry adds to a context block.
func EntryTokens(e schema.Entry) int {
	n := len([]rune(e.Content))
	if n > contextSnippetChars {
		n = contextSnippetChars + 3
	}
	return (n+3)/4 + contextEntryOverhead
}

// FitTokenBudget keeps the longest ranked prefix of entries whose estimated
// context cost fits maxTokens (0 or negative = no cap). Lower-ranked entries
// are dropped first, so the budget never reorders results.
func FitTokenBudget(entries []schema.Entry, maxTokens int) []schema.Entry {
	if maxTokens <= 0 {
		return entries
	}
	used := 0
	for i, e := range entries {
		used += EntryTokens(e)
		if used > maxTokens {
			return entries[:i]
		}
	}
	return entries
}

func buildContextBlock(entries []schema.Entry) string {
	if len(entries) == 0 {
		return "## Relevant memories\n\n(no relevant memories found)"
	}

	const maxContentChars = contextSnippetChars
	var b strings.Builder
	b.WriteString("## Relevant memories\n\n")
	num := 0
	for _, e := range entries {
		snippet := e.Content
		runes := []rune(snippet)
		if len(runes) > maxContentChars {
			snippet = string(runes[:maxContentChars]) + "..."
		}
		if e.RelatedVia != nil {
			fmt.Fprintf(&b, "   ↳ related to %s%s\n   %s\n\n", *e.RelatedVia, provenanceSuffix(e), snippet)
			continue
		}
		num++
		// scorePrefix is empty when Score is nil (GET /recent, D17 — ordered
		// by CreatedAt, not by similarity): "[0.00]" would suggest zero
		// relevance instead of "not scored", which is misleading.
		scorePrefix := ""
		if e.Score != nil {
			scorePrefix = fmt.Sprintf("[%.2f] ", *e.Score)
		}
		fmt.Fprintf(&b, "%d. %s%s (%s, %s)%s\n   %s\n\n",
			num, scorePrefix, e.Domain, e.Source, e.CreatedAt.Format("2006-01-02"), provenanceSuffix(e), snippet)
	}
	return strings.TrimRight(b.String(), "\n")
}

// provenanceSuffix builds the optional " — <file_path> § <heading>". Empty
// when the entry has no metadata.file_path (memories that did not come from
// m3m0s watch).
func provenanceSuffix(e schema.Entry) string {
	filePath := e.Metadata["file_path"]
	if filePath == "" {
		return ""
	}
	suffix := " — " + filePath
	if heading := e.Metadata["heading"]; heading != "" {
		suffix += " § " + heading
	}
	return suffix
}

// PreviewInjectionMode enumerates the 3 real context injection mechanisms
// that exist today in m3m0s (D29) — the preview never invents a mechanism
// that no tool actually uses:
//   - "hook": the Claude Code SessionStart hook (D14/D17,
//     internal/hook.RunSessionStart) — the only automatic mechanism that
//     exists today, synchronous, runs before any new session.
//   - "mcp": the exact result the MCP tool memory_recent returns
//     (internal/mcpserver, dispatchTool) — the case of any generic MCP
//     client (pi, D10; OpenCode) that has no hook of its own yet and would
//     need to call the tool manually.
//   - "manual": raw apitypes.BuildContextBlock, with no envelope — the text
//     one would paste by hand into a system prompt for a tool with neither
//     hook nor MCP (scripts, cron).
type PreviewInjectionMode string

const (
	PreviewInjectionModeHook   PreviewInjectionMode = "hook"
	PreviewInjectionModeMCP    PreviewInjectionMode = "mcp"
	PreviewInjectionModeManual PreviewInjectionMode = "manual"
)

// PreviewInjectionResponse is the response of GET /api/v1/preview/injection
// (D29, docs/api-contract.md) — it shows in the WebUI, before any new
// implementation, exactly what would be injected today for a real domain,
// in one of the 3 PreviewInjectionMode mechanisms.
type PreviewInjectionResponse struct {
	Mode           PreviewInjectionMode `json:"mode"`
	Domain         string               `json:"domain"`
	Mechanism      string               `json:"mechanism"`
	EntriesFetched int                  `json:"entriesFetched"`
	EntriesUsed    int                  `json:"entriesUsed"`
	// EstimatedTokens is the estimated token cost of what this mechanism
	// injects; TokenBudget is the cap applied (0 = none).
	EstimatedTokens int    `json:"estimatedTokens"`
	TokenBudget     int    `json:"tokenBudget"`
	Raw             string `json:"raw"`
}

// ── accounts (D35) ──

// AuthStatusResponse is the 200 body of GET /auth/status (no auth).
type AuthStatusResponse struct {
	NeedsOnboarding bool `json:"needsOnboarding"`
}

// AccountUser is an account as returned by the API (never a password or hash).
type AccountUser struct {
	Username    string     `json:"username"`
	HasPassword bool       `json:"hasPassword"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// OnboardRequest is the body of POST /auth/onboard.
type OnboardRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Confirm  string `json:"confirm"`
}

// LoginRequest is the body of POST /auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// MeResponse is the 200 body of GET /auth/me: who the caller is. Via is
// "session" (Username set) or "apikey".
type MeResponse struct {
	Via      string `json:"via"`
	Username string `json:"username,omitempty"`
}

// ResetInfoResponse is the 200 body of GET /auth/reset?token=...
type ResetInfoResponse struct {
	Username string `json:"username"`
	// Kind is "reset" (existing account) or "invite" (new account).
	Kind string `json:"kind"`
}

// ResetPasswordRequest is the body of POST /auth/reset.
type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
	Confirm  string `json:"confirm"`
}

// CreateUserRequest is the body of POST /users (API key only).
type CreateUserRequest struct {
	Username string `json:"username"`
}

// ResetLinkResponse is returned when a link is issued (create user / reset):
// open Path on the server to set the password.
type ResetLinkResponse struct {
	User      AccountUser `json:"user"`
	Kind      string      `json:"kind"`
	Token     string      `json:"token"`
	Path      string      `json:"path"`
	ExpiresAt time.Time   `json:"expiresAt"`
}

// UsersResponse is the 200 body of GET /users.
type UsersResponse struct {
	Users []AccountUser `json:"users"`
}

// EmbeddingConfigResponse is the body of GET/PUT /embedding-config (#39,
// D36). The API key is write-only: only HasAPIKey/KeySource say whether one
// is set.
type EmbeddingConfigResponse struct {
	Enabled    bool   `json:"enabled"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	VectorDims int    `json:"vector_dims"`
	// Source is where the active configuration comes from: "database" (saved
	// by an admin) or "environment" (the server's env defaults).
	Source    string `json:"source"`
	HasAPIKey bool   `json:"has_api_key"`
	// KeySource is "database", "environment" or "none".
	KeySource string `json:"key_source"`
	// Active reports whether vector search is running (enabled and a key).
	Active  bool             `json:"active"`
	Vectors EmbeddingVectors `json:"vectors"`
	// Reembed is the state of the re-embedding job; ReembedStarted is true on
	// a PUT that confirmed a re-embedding and started it.
	Reembed        ReembedProgress `json:"reembed"`
	ReembedStarted bool            `json:"reembed_started,omitempty"`
	UpdatedAt      *time.Time      `json:"updated_at,omitempty"`
}

// EmbeddingVectors counts the stored vectors by relation to the active
// configuration; Stale ones come from another model or dimension and wait
// for re-embedding.
type EmbeddingVectors struct {
	Total   int `json:"total"`
	Current int `json:"current"`
	Stale   int `json:"stale"`
}

// EmbeddingConfigRequest is the body of PUT /embedding-config. APIKey omitted
// keeps the saved key; an empty string clears it (the environment key then
// applies). ConfirmReembed must be true when the change leaves stored vectors
// from another model/dimensions behind.
type EmbeddingConfigRequest struct {
	Enabled        bool    `json:"enabled"`
	BaseURL        string  `json:"base_url"`
	APIKey         *string `json:"api_key,omitempty"`
	Model          string  `json:"model"`
	Dimensions     int     `json:"dimensions"`
	ConfirmReembed bool    `json:"confirm_reembed"`
}

// ReembedProgress is the state of the re-embedding job (#39 phase 2, D36).
type ReembedProgress struct {
	Running    bool       `json:"running"`
	Model      string     `json:"model,omitempty"`
	Dimensions int        `json:"dimensions,omitempty"`
	Total      int        `json:"total"`
	Done       int        `json:"done"` // rows written by this job
	Errors     int        `json:"errors"`
	Skipped    int        `json:"skipped"`     // rows counted in total that left the pending set without this job (deleted, edited, re-embedded by a normal write)
	ETASeconds int        `json:"eta_seconds"` // 0 = unknown
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

// ReembedEstimateRequest optionally describes a configuration that is not
// active yet; omitted fields estimate the active one. The API key is not
// part of it.
type ReembedEstimateRequest struct {
	Model      string `json:"model,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
}

// ReembedEstimateResponse is the body of POST /embedding-config/reembed/estimate.
// Tokens are approximate (characters / 4); cost is null when the model has no
// known list price or the endpoint is not the public OpenAI one.
type ReembedEstimateResponse struct {
	Model            string   `json:"model"`
	Dimensions       int      `json:"dimensions"`
	Pending          int      `json:"pending"`
	Characters       int64    `json:"characters"`
	EstimatedTokens  int64    `json:"estimated_tokens"`
	Approximate      bool     `json:"approximate"`
	PricePerMTokUSD  *float64 `json:"price_per_million_tokens_usd"`
	EstimatedCostUSD *float64 `json:"estimated_cost_usd"`
	PricesAsOf       string   `json:"prices_as_of"`
}

// ReembedStartResponse is the body of POST /embedding-config/reembed.
type ReembedStartResponse struct {
	Started bool `json:"started"`
}

// ReembedCancelResponse is the body of DELETE /embedding-config/reembed.
type ReembedCancelResponse struct {
	Cancelled bool `json:"cancelled"`
}
