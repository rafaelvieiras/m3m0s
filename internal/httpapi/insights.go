// Memory health ("Insights", D37): GET /insights/overview (live, cheap),
// GET /insights/hygiene (last analysis + job status), POST /insights/hygiene/run
// and POST /insights/hygiene/cancel. Read-only over the memories; responses
// carry no embeddings and only truncated, secret-free text examples.
package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

func (s *Server) handleInsightsOverview(w http.ResponseWriter, r *http.Request) {
	ov := s.store.InsightsOverviewExcluding(time.Now(), r.URL.Query().Get("excludeSource"))
	ov.Ingest = s.ingest.overview(r.Context())
	writeJSON(w, http.StatusOK, ov)
}

func (s *Server) handleInsightsHygiene(w http.ResponseWriter, r *http.Request) {
	rep, err := s.store.HygieneReport(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insights_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.HygieneResponse{Status: s.store.HygieneStatus(), Report: rep})
}

func (s *Server) handleInsightsHygieneRun(w http.ResponseWriter, r *http.Request) {
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "job scheduler unavailable")
		return
	}
	switch err := s.jobsSched.TriggerNow(store.InsightsJobName); {
	case errors.Is(err, jobs.ErrJobAlreadyRunning):
		writeError(w, http.StatusConflict, "job_already_running", err.Error())
	case errors.Is(err, jobs.ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "insights_start_failed", err.Error())
	default:
		writeJSON(w, http.StatusAccepted, apitypes.HygieneStartResponse{Started: true})
	}
}

func (s *Server) handleInsightsHygieneCancel(w http.ResponseWriter, r *http.Request) {
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "job scheduler unavailable")
		return
	}
	switch err := s.jobsSched.Cancel(store.InsightsJobName); {
	case errors.Is(err, jobs.ErrJobNotRunning):
		writeError(w, http.StatusConflict, "insights_not_running", err.Error())
	case errors.Is(err, jobs.ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "insights_cancel_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, apitypes.HygieneCancelResponse{Cancelled: true})
	}
}
