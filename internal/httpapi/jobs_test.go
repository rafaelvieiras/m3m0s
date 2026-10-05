package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// newTestServerWithJobs builds a Server with a real Scheduler registered (only
// the "manual" job, controlled by the test itself via a release channel, not
// by the Scheduler tick) and the MemoryStore's own *sqlstore.DB (D28), to
// exercise GET/POST/PATCH /api/v1/jobs (D27).
func newTestServerWithJobs(t *testing.T) (*Server, string, *jobs.Scheduler, *sqlstore.DB) {
	t.Helper()
	embed := fakeEmbedServer(t)
	memStore, err := store.New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	keyStore, err := auth.LoadStore(memStore.DB())
	if err != nil {
		t.Fatalf("auth.LoadStore: %v", err)
	}
	rawKey, _, err := keyStore.Create("test")
	if err != nil {
		t.Fatalf("keyStore.Create: %v", err)
	}

	sched := jobs.NewScheduler(time.Hour) // huge tick; only TriggerNow fires in these tests

	srv := New(memStore, keyStore, nil, nil, 0, sched, memStore.DB())
	return srv, rawKey, sched, memStore.DB()
}

func doJobsRequest(t *testing.T, httpSrv *httptest.Server, rawKey, method, path string, body []byte) *http.Response {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, httpSrv.URL+path, reqBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestHandleJobsListEmptyWithoutScheduler(t *testing.T) {
	srv, rawKey := newTestServer(t) // no scheduler (nil); same pattern as nil chat
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodGet, "/api/v1/jobs", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out apitypes.JobsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Jobs) != 0 {
		t.Fatalf("Jobs = %v, want empty without scheduler", out.Jobs)
	}
}

func TestHandleJobsListReflectsRegisteredJobs(t *testing.T) {
	srv, rawKey, sched, _ := newTestServerWithJobs(t)
	sched.Register(jobs.Job{
		Name:     "manual",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			return "3 processed", nil
		},
	})

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodGet, "/api/v1/jobs", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out apitypes.JobsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Jobs) != 1 {
		t.Fatalf("Jobs = %d, want 1", len(out.Jobs))
	}
	if out.Jobs[0].Name != "manual" {
		t.Fatalf("Jobs[0].Name = %q, want manual", out.Jobs[0].Name)
	}
	if out.Jobs[0].IntervalSeconds != int64((time.Hour).Seconds()) {
		t.Fatalf("IntervalSeconds = %d, want %d", out.Jobs[0].IntervalSeconds, int64((time.Hour).Seconds()))
	}
	if out.Jobs[0].LastRunAt != nil {
		t.Fatalf("LastRunAt should be nil before any run")
	}
}

func TestHandleJobRunTriggersAndReflectsInList(t *testing.T) {
	srv, rawKey, sched, _ := newTestServerWithJobs(t)
	done := make(chan struct{})
	sched.Register(jobs.Job{
		Name:     "manual",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			defer close(done)
			return "ran via API", nil
		},
	})

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodPost, "/api/v1/jobs/manual/run", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var runOut apitypes.JobRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&runOut); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !runOut.Started {
		t.Fatalf("Started = false, want true")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("job did not run within 1s after POST /run")
	}

	// Gives the scheduler time to record the history (the job goroutine is
	// still in Scheduler.run's defer right after signaling `done`).
	var status apitypes.JobStatus
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		listResp := doJobsRequest(t, httpSrv, rawKey, http.MethodGet, "/api/v1/jobs", nil)
		var out apitypes.JobsListResponse
		if err := json.NewDecoder(listResp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.Jobs) == 1 && out.Jobs[0].LastRunAt != nil {
			status = out.Jobs[0]
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if status.LastSummary != "ran via API" {
		t.Fatalf("LastSummary = %q, want %q", status.LastSummary, "ran via API")
	}
	if status.LastSuccess == nil || !*status.LastSuccess {
		t.Fatalf("LastSuccess = %v, want true", status.LastSuccess)
	}
	if len(status.History) != 1 {
		t.Fatalf("History = %d, want 1", len(status.History))
	}
}

func TestHandleJobRunUnknownJob(t *testing.T) {
	srv, rawKey, _, _ := newTestServerWithJobs(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodPost, "/api/v1/jobs/does-not-exist/run", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var out apitypes.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ErrorInfo.Code != "job_not_found" {
		t.Fatalf("error.code = %q, want job_not_found", out.ErrorInfo.Code)
	}
}

func TestHandleJobRunAlreadyRunning(t *testing.T) {
	srv, rawKey, sched, _ := newTestServerWithJobs(t)
	release := make(chan struct{})
	sched.Register(jobs.Job{
		Name:     "busy",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			<-release
			return "", nil
		},
	})

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	first := doJobsRequest(t, httpSrv, rawKey, http.MethodPost, "/api/v1/jobs/busy/run", nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("1st POST /run status = %d, want 200", first.StatusCode)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		running := false
		for _, st := range sched.Status() {
			if st.Name == "busy" && st.Running {
				running = true
			}
		}
		if running {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	second := doJobsRequest(t, httpSrv, rawKey, http.MethodPost, "/api/v1/jobs/busy/run", nil)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("2nd POST /run status = %d, want 409", second.StatusCode)
	}
	var out apitypes.ErrorBody
	if err := json.NewDecoder(second.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ErrorInfo.Code != "job_already_running" {
		t.Fatalf("error.code = %q, want job_already_running", out.ErrorInfo.Code)
	}

	close(release)
}

func TestHandleJobSetIntervalUpdatesAndPersists(t *testing.T) {
	srv, rawKey, sched, db := newTestServerWithJobs(t)
	sched.Register(jobs.Job{
		Name:     "adjustable",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			return "", nil
		},
	})

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	body, _ := json.Marshal(apitypes.JobIntervalRequest{IntervalSeconds: 42})
	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodPatch, "/api/v1/jobs/adjustable", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out apitypes.JobStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.IntervalSeconds != 42 {
		t.Fatalf("IntervalSeconds = %d, want 42", out.IntervalSeconds)
	}

	// In-memory effect, via the Scheduler itself.
	for _, st := range sched.Status() {
		if st.Name == "adjustable" && st.Interval != 42*time.Second {
			t.Fatalf("Scheduler.Status Interval = %s, want 42s", st.Interval)
		}
	}

	// Effect persisted in the job_intervals table (D28).
	saved, err := jobs.LoadIntervals(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadIntervals: %v", err)
	}
	if saved["adjustable"] != 42*time.Second {
		t.Fatalf("job_intervals[adjustable] = %s, want 42s", saved["adjustable"])
	}
}

func TestHandleJobSetIntervalInvalid(t *testing.T) {
	srv, rawKey, sched, _ := newTestServerWithJobs(t)
	sched.Register(jobs.Job{
		Name:     "adjustable",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			return "", nil
		},
	})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	body, _ := json.Marshal(apitypes.JobIntervalRequest{IntervalSeconds: 0})
	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodPatch, "/api/v1/jobs/adjustable", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var out apitypes.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ErrorInfo.Code != "invalid_interval" {
		t.Fatalf("error.code = %q, want invalid_interval", out.ErrorInfo.Code)
	}
}

func TestHandleJobSetIntervalUnknownJob(t *testing.T) {
	srv, rawKey, _, _ := newTestServerWithJobs(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	body, _ := json.Marshal(apitypes.JobIntervalRequest{IntervalSeconds: 60})
	resp := doJobsRequest(t, httpSrv, rawKey, http.MethodPatch, "/api/v1/jobs/does-not-exist", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
