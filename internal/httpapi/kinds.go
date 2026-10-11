// Fact nature ("kind", D38): GET /kinds/sample (labelling sample) and the
// backfill of facts stored before the tag existed — POST /kinds/backfill/estimate,
// GET|POST|DELETE /kinds/backfill. Starting an LLM-backed backfill needs an
// explicit confirmation, like the re-embedding (D36).
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// maxKindSample bounds GET /kinds/sample so one call cannot dump the database.
const maxKindSample = 1000

func (s *Server) handleKindSample(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, seed := 200, int64(1)
	if v := q.Get("n"); v != "" {
		x, err := strconv.Atoi(v)
		if err != nil || x <= 0 || x > maxKindSample {
			writeError(w, http.StatusBadRequest, "invalid_request", "n must be between 1 and "+strconv.Itoa(maxKindSample))
			return
		}
		n = x
	}
	if v := q.Get("seed"); v != "" {
		x, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "seed must be an integer")
			return
		}
		seed = x
	}
	writeJSON(w, http.StatusOK, apitypes.KindSampleResponse{Items: s.store.KindSample(n, seed, time.Now())})
}

func (s *Server) handleKindBackfillEstimate(w http.ResponseWriter, r *http.Request) {
	e := s.store.EstimateKindBackfill()
	writeJSON(w, http.StatusOK, apitypes.KindBackfillEstimateResponse{
		Mode: e.Mode, Model: e.Model, Pending: e.Pending, Requests: e.Requests,
		InputTokens: e.InputTokens, OutputTokens: e.OutputTokens, Approximate: true,
		PriceInPerMTok: e.PriceInPerMTok, PriceOutPerMTok: e.PriceOutPerMTok,
		EstimatedCost: e.CostUSD, EstimatedSecond: e.Seconds,
	})
}

func toKindBackfillProgress(p store.KindBackfillProgress) apitypes.KindBackfillProgress {
	out := apitypes.KindBackfillProgress{
		Running: p.Running, Mode: p.Mode, Total: p.Total, Done: p.Done, Errors: p.Errors, Skipped: p.Skipped,
		ETASeconds: int(p.ETA / time.Second), LastError: p.LastError,
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

func (s *Server) handleKindBackfillStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toKindBackfillProgress(s.store.KindBackfillStatus()))
}

func (s *Server) handleKindBackfillStart(w http.ResponseWriter, r *http.Request) {
	var req apitypes.KindBackfillStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "job scheduler unavailable")
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusConflict, "backfill_confirmation_required",
			"the kind backfill spends extraction-provider tokens: call POST /kinds/backfill/estimate, then repeat with {\"confirm\": true}")
		return
	}
	s.store.ConfirmKindBackfill()
	switch err := s.jobsSched.TriggerNow(store.KindBackfillJobName); {
	case errors.Is(err, jobs.ErrJobAlreadyRunning):
		s.store.RevokeKindBackfillConfirmation()
		writeError(w, http.StatusConflict, "job_already_running", err.Error())
	case errors.Is(err, jobs.ErrJobNotFound):
		s.store.RevokeKindBackfillConfirmation()
		writeError(w, http.StatusNotFound, "job_not_found", err.Error())
	case err != nil:
		s.store.RevokeKindBackfillConfirmation()
		writeError(w, http.StatusInternalServerError, "kind_backfill_start_failed", err.Error())
	default:
		writeJSON(w, http.StatusAccepted, apitypes.KindBackfillStartResponse{Started: true})
	}
}

func (s *Server) handleKindBackfillCancel(w http.ResponseWriter, r *http.Request) {
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "job scheduler unavailable")
		return
	}
	switch err := s.jobsSched.Cancel(store.KindBackfillJobName); {
	case errors.Is(err, jobs.ErrJobNotRunning):
		writeError(w, http.StatusConflict, "kind_backfill_not_running", err.Error())
	case errors.Is(err, jobs.ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "kind_backfill_cancel_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, apitypes.KindBackfillCancelResponse{Cancelled: true})
	}
}
