// Package httpapi implements the HTTP surface of m3m0s serve, as per
// docs/api-contract.md: POST /api/v1/memories, GET /api/v1/memories/pending/:id
// (D26, asynchronous Add), POST /api/v1/search, GET /api/v1/recent,
// GET /api/v1/stats, GET /api/v1/memories/:id, PATCH /api/v1/memories/:id,
// DELETE /api/v1/memories(/:id), GET /api/v1/jobs, POST /api/v1/jobs/:name/run,
// PATCH /api/v1/jobs/:name (D27), GET /api/v1/preview/injection (D29),
// GET /health (unprefixed, on purpose), and the embedded WebUI served at "/".
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/chat"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// EmbeddingModel identifies the model used, exposed in GET /stats.
const EmbeddingModel = "text-embedding-3-small"

// Backend identifies the storage backend, exposed in GET /stats; SQLite
// since D28 (docs/decisions/), replacing chromem-go.
const Backend = "sqlite + OpenAI embeddings"

// Server builds the HTTP mux on top of a MemoryStore and an API key Store.
type Server struct {
	store       *store.MemoryStore
	keys        *auth.Store
	webUIAssets fs.FS
	chat        *chat.Client // nil disables POST /api/v1/chat (D23)
	startTime   time.Time    // used by GET /stats (telemetry, uptimeSeconds)

	// addSyncTimeout and pendingAdds implement the asynchronous Add (D26,
	// docs/decisions/): addSyncTimeout <= 0 preserves the usual synchronous
	// behavior (escape hatch), any value > 0 is how long handleAdd waits
	// before degrading to 202 + poll on GET /memories/pending/:id
	// (pendingadd.go).
	addSyncTimeout time.Duration
	pendingAdds    *pendingAddTracker

	// jobsSched/db back GET/POST/PATCH /api/v1/jobs* (D27,
	// docs/decisions/). A nil jobsSched disables the 3 routes (same pattern
	// as a nil chat disabling POST /api/v1/chat), useful for tests that do
	// not care about jobs. A nil db disables persistence of
	// PATCH /api/v1/jobs/:name (the in-memory change via
	// Scheduler.SetInterval still works, it just does not survive a
	// restart). Since D28 it is the same *sqlstore.DB the MemoryStore uses,
	// no longer a separate jobs-config.json path.
	jobsSched *jobs.Scheduler
	db        *sqlstore.DB
}

// New creates the Server. webUIAssets is the dist/ filesystem (already
// "stripped" via fs.Sub by the caller, see cmd/m3m0s/serve.go) used to serve
// the embedded WebUI at "/"; it may be nil (no handler is registered at "/",
// useful for tests that do not care about static files,
// docs/api-contract.md). chatClient is optional (nil disables the WebUI chat,
// D23; M3M0S_CHAT_API_KEY/M3M0S_EXTRACT_API_KEY missing in
// cmd/m3m0s/serve.go). addSyncTimeout controls the asynchronous Add (D26);
// <= 0 preserves the usual 100% synchronous behavior. jobsSched/db are
// optional (D27); see the comment on the Server struct above.
func New(memStore *store.MemoryStore, keyStore *auth.Store, webUIAssets fs.FS, chatClient *chat.Client, addSyncTimeout time.Duration, jobsSched *jobs.Scheduler, db *sqlstore.DB) *Server {
	return &Server{
		store:          memStore,
		keys:           keyStore,
		webUIAssets:    webUIAssets,
		chat:           chatClient,
		startTime:      time.Now(),
		addSyncTimeout: addSyncTimeout,
		pendingAdds:    newPendingAddTracker(),
		jobsSched:      jobsSched,
		db:             db,
	}
}

// Handler returns the http.Handler ready to be plugged into an http.Server.
// Every API operation lives under /api/v1, except /health, deliberately left
// at the root for infra/orchestrator probes that expect a fixed path without
// an embedded version (docs/api-contract.md, section "Prefixo /api/v1 e WebUI
// embutida").
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/memories", s.withAuth(s.handleAdd))
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", s.withAuth(s.handlePendingAdd))
	mux.HandleFunc("POST /api/v1/search", s.withAuth(s.handleSearch))
	mux.HandleFunc("GET /api/v1/recent", s.withAuth(s.handleRecent))
	mux.HandleFunc("GET /api/v1/preview/injection", s.withAuth(s.handlePreviewInjection))
	mux.HandleFunc("GET /api/v1/stats", s.withAuth(s.handleStats))
	mux.HandleFunc("GET /api/v1/memories/{id}", s.withAuth(s.handleGet))
	mux.HandleFunc("PATCH /api/v1/memories/{id}", s.withAuth(s.handleUpdate))
	mux.HandleFunc("DELETE /api/v1/memories/{id}", s.withAuth(s.handleDeleteByID))
	mux.HandleFunc("DELETE /api/v1/memories", s.withAuth(s.handleDeleteByFilePath))
	mux.HandleFunc("POST /api/v1/memories/forget-sweep", s.withAuth(s.handleForgetSweep))
	mux.HandleFunc("POST /api/v1/memories/{id}/restore", s.withAuth(s.handleRestore))
	mux.HandleFunc("POST /api/v1/chat", s.withAuth(s.handleChat))
	mux.HandleFunc("GET /api/v1/jobs", s.withAuth(s.handleJobsList))
	mux.HandleFunc("POST /api/v1/jobs/{name}/run", s.withAuth(s.handleJobRun))
	mux.HandleFunc("PATCH /api/v1/jobs/{name}", s.withAuth(s.handleJobSetInterval))
	if s.webUIAssets != nil {
		// No withAuth: the static assets (HTML/JS/CSS) are public; auth
		// happens client-side via Authorization: Bearer against /api/v1/*
		// (docs/api-contract.md).
		mux.Handle("/", spaHandler(s.webUIAssets))
	}
	return withCORS(mux)
}

// spaHandler serves asset files directly when the requested path exists, and
// falls back to index.html in any other case; this is needed for the TanStack
// Router client-side routes to work on refresh/direct link (e.g. GET
// /entries/mem_abc123 does not exist as a file, but must return the SPA's
// index.html so the router mounts on the client and resolves the route). Since
// this handler is registered at "/" and Go's ServeMux gives precedence to more
// specific patterns, no /api/v1/* or /health route ever reaches here.
func spaHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if f, err := assets.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Does not reuse fileServer with a URL.Path rewritten to "/index.html":
		// http.FileServer treats any path ending in "/index.html" as a
		// canonicalization request and answers 301 to "./" (the same logic
		// that avoids duplicate URLs /foo/index.html vs /foo/), which would
		// break exactly the SPA fallback this handler exists to provide.
		// It serves the content directly via ServeContent, which has no such
		// special case.
		serveIndexHTML(w, r, assets)
	})
}

func serveIndexHTML(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	f, err := assets.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	var modTime time.Time
	if stat, err := f.Stat(); err == nil {
		modTime = stat.ModTime()
	}

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		// Does not trust the concrete type returned by fs.Sub/embed.FS beyond
		// the fs.File interface; if it does not implement Seek, read
		// everything into memory (index.html is always small) and serve from
		// a bytes.Reader.
		data, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "error reading embedded index.html", http.StatusInternalServerError)
			return
		}
		rs = bytes.NewReader(data)
	}
	http.ServeContent(w, r, "index.html", modTime, rs)
}

// withCORS allows cross-origin access for the WebUI (D13), which runs in a
// process and origin separate from m3m0s serve (Vite in dev, a static host in
// production). Safe with `Access-Control-Allow-Origin: *` because auth is an
// explicit Bearer token in the header, never a cookie; there is no ambient
// state for CSRF to exploit, so allowing any origin does not widen the attack
// surface.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth requires a valid "Authorization: Bearer <api_key>", as per the
// Auth section of docs/api-contract.md. /health is deliberately left out.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := auth.BearerToken(r.Header.Get("Authorization"))
		if !ok || !s.keys.Verify(token) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
			return
		}
		next(w, r)
	}
}

// ── /memories ──

// handleAdd implements the asynchronous Add with a short timeout (D26,
// docs/decisions/), meant to support a future feature of automatic memory
// ingestion as the user keeps sending messages, where blocking the caller for
// seconds on every Add (fact extraction via LLM, D12/D18/D19) would be
// unacceptable.
//
// Flow: 1) validate the request as usual; 2) fire store.Add in its own
// goroutine with context.Background(), NEVER r.Context() (the comment below
// explains why); 3) wait up to s.addSyncTimeout (or indefinitely if <= 0, the
// escape hatch that preserves the usual synchronous behavior) for that
// goroutine; 4a) if it finished in time, answer 200 with the usual
// AddResponse (+ the additive Status field); 4b) if not, answer 202 with an
// OperationID pollable at GET /memories/pending/:id; the goroutine keeps
// running and completing the result in the tracker, for whoever queries it
// later.
func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req apitypes.AddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content_required", "content is required")
		return
	}
	if err := schema.ValidateDomain(req.Domain); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_domain", err.Error())
		return
	}

	op := s.pendingAdds.start()
	go func() {
		// context.Background(), not r.Context(): if we used the request
		// context, extraction would be cancelled the instant this handler
		// answers (200 or 202) and the caller's HTTP connection closes,
		// which is exactly the case the asynchronous path exists to support.
		// An in-flight Add must survive the request that fired it.
		out, err := s.store.Add(context.Background(), store.AddInput{
			Content:  req.Content,
			Domain:   req.Domain,
			Source:   req.Source,
			Agent:    req.Agent,
			Tags:     req.Tags,
			Metadata: req.Metadata,
			IDPrefix: req.IDPrefix,
		}, time.Now())
		if err != nil {
			op.fail(err)
		} else {
			op.complete(buildAddResponse(out, "completed"))
		}
		s.pendingAdds.finish(op.id)
	}()

	if s.addSyncTimeout > 0 {
		select {
		case <-op.done:
		case <-time.After(s.addSyncTimeout):
			writeJSON(w, http.StatusAccepted, apitypes.AddPendingResponse{
				OperationID: op.id,
				Status:      string(pendingStatusPending),
			})
			return
		}
	} else {
		<-op.done
	}

	status, result, errMsg := op.snapshot()
	if status == pendingStatusFailed {
		writeError(w, http.StatusInternalServerError, "add_failed", errMsg)
		return
	}
	writeJSON(w, http.StatusOK, *result)
}

// buildAddResponse translates store.AddOutput into the public shape of
// apitypes.AddResponse, including the additive Status field (D26).
func buildAddResponse(out store.AddOutput, status string) apitypes.AddResponse {
	entries := make([]apitypes.AddedEntry, 0, len(out.Entries))
	for _, e := range out.Entries {
		entries = append(entries, apitypes.AddedEntry{Entry: e.Entry, Deduped: e.Deduped})
	}
	return apitypes.AddResponse{Entries: entries, Extracted: out.Extracted, Status: status}
}

// handlePendingAdd implements GET /api/v1/memories/pending/:id (D26), a poll
// for the result of an Add that degraded to the asynchronous path (202). 200
// whenever the ID exists (regardless of the status being pending/completed/
// failed); 404 if the ID never existed or has already expired from the
// retention TTL (pendingAddTTL, pendingadd.go).
func (s *Server) handlePendingAdd(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	op, ok := s.pendingAdds.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "operation not found or expired: "+id)
		return
	}

	status, result, errMsg := op.snapshot()
	resp := apitypes.PendingAddResponse{OperationID: id, Status: string(status)}
	switch status {
	case pendingStatusCompleted:
		resp.Result = result
	case pendingStatusFailed:
		resp.Error = errMsg
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── /search ──

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req apitypes.SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query_required", "query is required")
		return
	}

	filter := store.Filter{
		Domain:         req.Domain,
		DomainCategory: req.DomainCategory,
		Source:         req.Source,
		Agent:          req.Agent,
		Tags:           req.Tags,
	}
	results, err := s.store.Search(r.Context(), req.Query, req.Limit, filter, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, apitypes.SearchResponse{
		Results:      results,
		ContextBlock: apitypes.BuildContextBlock(results),
	})
}

// ── /recent ──

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 10
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	filter := store.Filter{
		Domain:         q.Get("domain"),
		DomainCategory: q.Get("domainCategory"),
		Source:         q.Get("source"),
		Agent:          q.Get("agent"),
	}

	results := s.store.Recent(limit, filter, time.Now())
	writeJSON(w, http.StatusOK, apitypes.RecentResponse{Results: results})
}

// ── /stats ──

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	count, byCategory := s.store.Stats()
	enabled, model := s.store.ExtractionInfo()
	var modelPtr *string
	if enabled {
		modelPtr = &model
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// DiskUsageBytes walks the data directory on every call; if it fails
	// (e.g. permission, dataDir removed at runtime), it does not take down the
	// rest of /stats: it just reports 0 MB and logs, the same pattern already
	// used for classification/extraction failures in internal/store.
	dbBytes, err := s.store.DiskUsageBytes()
	if err != nil {
		log.Printf("m3m0s: failed to compute on-disk database size: %v", err)
	}

	writeJSON(w, http.StatusOK, apitypes.StatsResponse{
		Count:                 count,
		CountByDomainCategory: byCategory,
		Backend:               Backend,
		Model:                 EmbeddingModel,
		Extraction: apitypes.ExtractionInfo{
			Enabled: enabled,
			Model:   modelPtr,
		},
		Telemetry: apitypes.TelemetryInfo{
			APIKeyCount:             s.keys.Count(),
			MemoryUsageMB:           bytesToMB(mem.Sys),
			DatabaseSizeMB:          bytesToMB(uint64(dbBytes)),
			UptimeSeconds:           time.Since(s.startTime).Seconds(),
			ExtractionTokensUsed:    s.store.ExtractionTokensUsed(),
			TokensSavedByCompaction: s.store.TokensSavedByCompaction(),
			SoftDeletedCount:        s.store.SoftDeletedCount(),
		},
	})
}

// bytesToMB converts bytes to MB (base 1024, not 1000) rounded to 2 decimal
// places; enough for GET /stats telemetry, without spurious floating-point
// decimals in the JSON.
func bytesToMB(b uint64) float64 {
	return math.Round(float64(b)/(1024*1024)*100) / 100
}

// ── GET /memories/:id (elaboration after Phase 5/D13, docs/api-contract.md, memory_get section) ──

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	entry, err := s.store.Get(id, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "memory not found: "+id)
			return
		}
		writeError(w, http.StatusInternalServerError, "get_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// ── PATCH /memories/:id (D13/docs/api-contract.md, memory_update section) ──

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req apitypes.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if !req.HasAnyField() {
		writeError(w, http.StatusBadRequest, "no_fields_to_update", "body has no recognized field (content/domain/source/agent/tags/metadata)")
		return
	}

	patch, err := decodeUpdatePatch(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	entry, err := s.store.Update(r.Context(), id, patch, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "memory not found: "+id)
			return
		}
		var verr *store.ValidationError
		if errors.As(err, &verr) {
			writeError(w, http.StatusBadRequest, verr.Code, verr.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// decodeUpdatePatch translates the semi-decoded body (json.RawMessage per
// field, see apitypes.UpdateRequest) into the plain-pointer store.UpdatePatch
// that internal/store.Update expects; this is where "present" actually
// becomes "apply" (even if zero-value) and "absent" becomes nil.
// Content/Domain/Source are never "clearable" (only replaceable; non-empty
// validation is left to store.Update); Agent accepts an explicit null (clears
// the agent); Tags/Metadata accept [] / {} (or null) to clear.
func decodeUpdatePatch(req apitypes.UpdateRequest) (store.UpdatePatch, error) {
	var patch store.UpdatePatch

	if len(req.Content) > 0 {
		var v string
		if err := json.Unmarshal(req.Content, &v); err != nil {
			return store.UpdatePatch{}, errors.New("invalid content: " + err.Error())
		}
		patch.Content = &v
	}

	if len(req.Domain) > 0 {
		var v string
		if err := json.Unmarshal(req.Domain, &v); err != nil {
			return store.UpdatePatch{}, errors.New("invalid domain: " + err.Error())
		}
		patch.Domain = &v
	}

	if len(req.Source) > 0 {
		var v string
		if err := json.Unmarshal(req.Source, &v); err != nil {
			return store.UpdatePatch{}, errors.New("invalid source: " + err.Error())
		}
		patch.Source = &v
	}

	if len(req.Agent) > 0 {
		var v *string
		if err := json.Unmarshal(req.Agent, &v); err != nil {
			return store.UpdatePatch{}, errors.New("invalid agent: " + err.Error())
		}
		cleared := ""
		if v != nil {
			cleared = *v
		}
		patch.Agent = &cleared
	}

	if len(req.Tags) > 0 {
		var v []string
		if err := json.Unmarshal(req.Tags, &v); err != nil {
			return store.UpdatePatch{}, errors.New("invalid tags: " + err.Error())
		}
		patch.Tags = &v
	}

	if len(req.Metadata) > 0 {
		var v map[string]string
		if err := json.Unmarshal(req.Metadata, &v); err != nil {
			return store.UpdatePatch{}, errors.New("invalid metadata: " + err.Error())
		}
		patch.Metadata = &v
	}

	return patch, nil
}

// ── DELETE /memories (D11, minimal scope for the watch cascade purge) ──

func (s *Server) handleDeleteByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	found, err := s.store.Delete(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "memory not found: "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteByFilePath(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	filePath := r.URL.Query().Get("filePath")
	if domain == "" || filePath == "" {
		writeError(w, http.StatusBadRequest, "domain_and_filepath_required", "domain and filePath are required")
		return
	}

	count, err := s.store.DeleteByFilePath(r.Context(), domain, filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.DeleteByFilePathResponse{DeletedCount: count})
}

// ── /memories/forget-sweep and /memories/:id/restore (D22, docs/decisions/, closes A9) ──

func (s *Server) handleForgetSweep(w http.ResponseWriter, r *http.Request) {
	// Optional body: {"dryRun": true} or empty (equivalent to dryRun=false).
	// Unlike handleAdd/handleSearch, which require a body, this endpoint has
	// no required field, so a POST without a body is valid.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	var req apitypes.ForgetSweepRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}

	result, err := s.store.ForgetSweep(r.Context(), req.DryRun, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "forget_sweep_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, apitypes.ForgetSweepResponse{
		DryRun:               result.DryRun,
		Evaluated:            result.Evaluated,
		SoftDeleteCandidates: result.SoftDeleteCandidates,
		HardDeleteCandidates: result.HardDeleteCandidates,
		SoftDeleted:          result.SoftDeleted,
		HardDeleted:          result.HardDeleted,
	})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	entry, err := s.store.Restore(r.Context(), id, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "memory not found or not soft-deleted: "+id)
			return
		}
		writeError(w, http.StatusInternalServerError, "restore_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// ── /health ──

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── helpers ──

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body apitypes.ErrorBody
	body.ErrorInfo.Code = code
	body.ErrorInfo.Message = message
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
