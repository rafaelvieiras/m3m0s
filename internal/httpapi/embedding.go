// Runtime embedding configuration (#39, D36): GET/PUT /api/v1/embedding-config.
// The API key is write-only and never appears in a response.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

func toEmbeddingResponse(st store.EmbeddingStatus) apitypes.EmbeddingConfigResponse {
	out := apitypes.EmbeddingConfigResponse{
		Enabled: st.Enabled, BaseURL: st.BaseURL, Model: st.Model, Dimensions: st.Dimensions,
		VectorDims: st.VectorDims, Source: st.Source, HasAPIKey: st.HasAPIKey, KeySource: st.KeySource,
		Active:  st.Active,
		Vectors: apitypes.EmbeddingVectors{Total: st.Total, Current: st.Current, Stale: st.Stale},
	}
	out.Reembed = toReembedProgress(st.Reembed)
	if !st.UpdatedAt.IsZero() {
		t := st.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func toReembedProgress(p store.ReembedProgress) apitypes.ReembedProgress {
	out := apitypes.ReembedProgress{
		Running: p.Running, Model: p.Model, Dimensions: p.Dims, Total: p.Total, Done: p.Done, Skipped: p.Skipped,
		Errors: p.Errors, ETASeconds: int(p.ETA / time.Second), LastError: p.LastError,
	}
	if !p.StartedAt.IsZero() {
		t := p.StartedAt
		out.StartedAt = &t
	}
	if !p.FinishedAt.IsZero() {
		t := p.FinishedAt
		out.FinishedAt = &t
	}
	return out
}

func (s *Server) handleEmbeddingConfigGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.EmbeddingStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "embedding_config_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toEmbeddingResponse(st))
}

func (s *Server) handleEmbeddingConfigPut(w http.ResponseWriter, r *http.Request) {
	var req apitypes.EmbeddingConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	st, err := s.store.SetEmbeddingSettings(r.Context(), store.EmbeddingUpdate{
		EmbeddingSettings: store.EmbeddingSettings{Enabled: req.Enabled, BaseURL: req.BaseURL, Model: req.Model, Dimensions: req.Dimensions},
		APIKey:            req.APIKey,
	}, req.ConfirmReembed)
	var reembed *store.ReembedRequiredError
	switch {
	case errors.As(err, &reembed):
		writeError(w, http.StatusConflict, "reembed_required", reembed.Error())
	case errors.Is(err, store.ErrEmbeddingInvalid):
		writeError(w, http.StatusUnprocessableEntity, "embedding_config_invalid", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "embedding_config_failed", err.Error())
	default:
		out := toEmbeddingResponse(st)
		// A confirmed change that left vectors behind starts the re-embedding
		// right away; an already running job notices the new space by itself.
		if req.ConfirmReembed && st.Active && st.Stale > 0 && s.jobsSched != nil {
			if err := s.jobsSched.TriggerNow(store.ReembedJobName); err == nil {
				out.ReembedStarted = true
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// POST /embedding-config/reembed/estimate: size and approximate cost of the
// run a (proposed) configuration would need. Changes nothing.
func (s *Server) handleReembedEstimate(w http.ResponseWriter, r *http.Request) {
	var req apitypes.ReembedEstimateRequest
	var proposed *store.EmbeddingSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	} else if err == nil && (req.Model != "" || req.Dimensions != 0 || req.BaseURL != "") {
		cur, serr := s.store.EmbeddingStatus(r.Context())
		if serr != nil {
			writeError(w, http.StatusInternalServerError, "embedding_config_failed", serr.Error())
			return
		}
		p := cur.EmbeddingSettings // omitted fields keep the active value
		if req.Model != "" {
			p.Model = req.Model
		}
		if req.BaseURL != "" {
			p.BaseURL = req.BaseURL
		}
		p.Dimensions = req.Dimensions
		proposed = &p
	}
	est, err := s.store.EstimateReembed(r.Context(), proposed)
	switch {
	case errors.Is(err, store.ErrEmbeddingInvalid):
		writeError(w, http.StatusUnprocessableEntity, "embedding_config_invalid", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "reembed_estimate_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, apitypes.ReembedEstimateResponse{
			Model: est.Model, Dimensions: est.Dims, Pending: est.Pending, Characters: est.Chars,
			EstimatedTokens: est.Tokens, Approximate: true, PricePerMTokUSD: est.PricePerMTok,
			EstimatedCostUSD: est.CostUSD, PricesAsOf: est.PricesAsOf,
		})
	}
}

// GET /embedding-config/reembed: progress of the current or last run.
func (s *Server) handleReembedStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toReembedProgress(s.store.ReembedStatus()))
}

// POST /embedding-config/reembed: start the job (explicit confirmation).
func (s *Server) handleReembedStart(w http.ResponseWriter, r *http.Request) {
	if !s.store.HasEmbeddings() {
		writeError(w, http.StatusUnprocessableEntity, "embedding_inactive", "embeddings are disabled or have no API key")
		return
	}
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "job scheduler unavailable")
		return
	}
	switch err := s.jobsSched.TriggerNow(store.ReembedJobName); {
	case errors.Is(err, jobs.ErrJobAlreadyRunning):
		writeError(w, http.StatusConflict, "job_already_running", err.Error())
	case errors.Is(err, jobs.ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "reembed_start_failed", err.Error())
	default:
		writeJSON(w, http.StatusAccepted, apitypes.ReembedStartResponse{Started: true})
	}
}

// DELETE /embedding-config/reembed: cancel the running job. Nothing is lost:
// the next start continues with the rows still pending.
func (s *Server) handleReembedCancel(w http.ResponseWriter, r *http.Request) {
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "job scheduler unavailable")
		return
	}
	switch err := s.jobsSched.Cancel(store.ReembedJobName); {
	case errors.Is(err, jobs.ErrJobNotRunning):
		writeError(w, http.StatusConflict, "reembed_not_running", err.Error())
	case errors.Is(err, jobs.ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "reembed_cancel_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, apitypes.ReembedCancelResponse{Cancelled: true})
	}
}
