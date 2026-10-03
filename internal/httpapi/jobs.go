// Visibility/control of the in-process jobs (internal/jobs, D16), D27,
// docs/decisions/. GET /api/v1/jobs lists all registered jobs with history;
// POST /api/v1/jobs/:name/run fires one outside the normal cycle; PATCH
// /api/v1/jobs/:name changes the interval at runtime and persists it
// (internal/jobs/persist.go) to survive a restart.
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
)

// ── GET /jobs ──

func (s *Server) handleJobsList(w http.ResponseWriter, r *http.Request) {
	out := apitypes.JobsListResponse{Jobs: []apitypes.JobStatus{}}
	if s.jobsSched != nil {
		for _, st := range s.jobsSched.Status() {
			out.Jobs = append(out.Jobs, toAPIJobStatus(st))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── POST /jobs/:name/run ──

func (s *Server) handleJobRun(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "unknown job: "+name)
		return
	}

	if err := s.jobsSched.TriggerNow(name); err != nil {
		switch {
		case errors.Is(err, jobs.ErrJobNotFound):
			writeError(w, http.StatusNotFound, "job_not_found", err.Error())
		case errors.Is(err, jobs.ErrJobAlreadyRunning):
			writeError(w, http.StatusConflict, "job_already_running", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "job_run_failed", err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, apitypes.JobRunResponse{Started: true})
}

// ── PATCH /jobs/:name ──

func (s *Server) handleJobSetInterval(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req apitypes.JobIntervalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.IntervalSeconds <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_interval", "intervalSeconds must be positive")
		return
	}
	if s.jobsSched == nil {
		writeError(w, http.StatusNotFound, "job_not_found", "unknown job: "+name)
		return
	}

	interval := time.Duration(req.IntervalSeconds) * time.Second
	if err := s.jobsSched.SetInterval(name, interval); err != nil {
		if errors.Is(err, jobs.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "job_not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "job_set_interval_failed", err.Error())
		return
	}

	if s.db != nil {
		if err := jobs.SaveInterval(r.Context(), s.db, name, interval); err != nil {
			// Best-effort: the change already took effect in memory
			// (SetInterval above), it just does not survive a restart; this
			// does not turn the 200 response into a failure, but it is logged
			// for investigation.
			log.Printf("m3m0s: failed to persist interval of job %q: %v", name, err)
		}
	}

	for _, st := range s.jobsSched.Status() {
		if st.Name == name {
			writeJSON(w, http.StatusOK, toAPIJobStatus(st))
			return
		}
	}
	// Unreachable in practice (SetInterval validated that the name exists a
	// few microseconds ago), but it avoids a handler without a response if
	// the job is removed between the two calls.
	writeJSON(w, http.StatusOK, apitypes.JobRunResponse{Started: false})
}

// toAPIJobStatus translates the internal snapshot (internal/jobs.JobStatus,
// Go-native types such as time.Duration) into the wire shape
// (internal/apitypes, whole seconds); D27.
func toAPIJobStatus(st jobs.JobStatus) apitypes.JobStatus {
	out := apitypes.JobStatus{
		Name:            st.Name,
		IntervalSeconds: int64(st.Interval / time.Second),
		Running:         st.Running,
		NextRunAt:       st.NextRun,
		History:         make([]apitypes.JobRun, 0, len(st.History)),
	}
	if st.LastRun != nil {
		startedAt := st.LastRun.StartedAt
		success := st.LastRun.Success
		out.LastRunAt = &startedAt
		out.LastDurationMs = st.LastRun.Duration.Milliseconds()
		out.LastSuccess = &success
		out.LastSummary = st.LastRun.Summary
		out.LastError = st.LastRun.Err
	}
	for _, run := range st.History {
		out.History = append(out.History, apitypes.JobRun{
			StartedAt:  run.StartedAt,
			DurationMs: run.Duration.Milliseconds(),
			Success:    run.Success,
			Summary:    run.Summary,
			Error:      run.Err,
		})
	}
	return out
}
