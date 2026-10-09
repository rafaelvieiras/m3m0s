package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

func TestInsightsEndpointsEndToEnd(t *testing.T) {
	const apiSecret = "sk-http-secret-999"
	memStore, err := store.New(t.TempDir(), apiSecret, oneHotEmbedServer(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	keyStore, _ := auth.LoadStore(memStore.DB())
	rawKey, _, _ := keyStore.Create("test")
	sched := jobs.NewScheduler(time.Hour)
	sched.Register(jobs.Job{
		Name: store.InsightsJobName,
		Run:  func(ctx context.Context) (string, error) { return memStore.RunHygieneAnalysis(ctx) },
	})
	httpSrv := httptest.NewServer(New(memStore, keyStore, nil, nil, 0, sched, memStore.DB()).Handler())
	t.Cleanup(httpSrv.Close)
	base := httpSrv.URL

	endpoints := []struct{ method, path string }{
		{"GET", "/api/v1/insights/overview"}, {"GET", "/api/v1/insights/hygiene"},
		{"POST", "/api/v1/insights/hygiene/run"}, {"POST", "/api/v1/insights/hygiene/cancel"},
	}
	for _, tc := range endpoints {
		if code, _ := doReq(t, base, "", tc.method, tc.path, ""); code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", tc.method, tc.path, code)
		}
	}

	for _, text := range []string{"a", "bb", "bb ", "ccc token: hunter2hunter2"} {
		body, _ := json.Marshal(map[string]string{"content": text, "domain": "codigo:test"})
		if code, out := doReq(t, base, rawKey, "POST", "/api/v1/memories", string(body)); code/100 != 2 {
			t.Fatalf("add %q = %d %s", text, code, out)
		}
	}

	// Overview.
	code, body := doReq(t, base, rawKey, "GET", "/api/v1/insights/overview", "")
	var ov apitypes.InsightsOverviewResponse
	if code != 200 || json.Unmarshal([]byte(body), &ov) != nil {
		t.Fatalf("overview = %d %s", code, body)
	}
	if ov.Totals.Active != 3 && ov.Totals.Active != 4 { // "bb" and "bb " may be merged by dedup
		t.Errorf("active = %d: %s", ov.Totals.Active, body)
	}
	if ov.VectorsIndexed == 0 || ov.Forgetting.Threshold == 0 {
		t.Errorf("overview = %+v", ov)
	}

	// Never analysed yet: no report, idle status; nothing to cancel.
	code, body = doReq(t, base, rawKey, "GET", "/api/v1/insights/hygiene", "")
	if code != 200 || !strings.Contains(body, `"report":null`) || !strings.Contains(body, `"running":false`) {
		t.Fatalf("hygiene before run = %d %s", code, body)
	}
	if code, _ = doReq(t, base, rawKey, "POST", "/api/v1/insights/hygiene/cancel", ""); code != http.StatusConflict {
		t.Errorf("cancel with nothing running = %d, want 409", code)
	}

	if code, body = doReq(t, base, rawKey, "POST", "/api/v1/insights/hygiene/run", ""); code != http.StatusAccepted {
		t.Fatalf("run = %d %s", code, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, body = doReq(t, base, rawKey, "GET", "/api/v1/insights/hygiene", "")
		if code == 200 && !strings.Contains(body, `"report":null`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("analysis never produced a report: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var h apitypes.HygieneResponse
	if err := json.Unmarshal([]byte(body), &h); err != nil || h.Report == nil {
		t.Fatalf("decode: %v %s", err, body)
	}
	if h.Report.Scanned != ov.Totals.Active || !h.Report.Near.Available || h.Report.ComputedAt.IsZero() {
		t.Errorf("report = %+v", h.Report)
	}
	for _, banned := range []string{`"embedding`, apiSecret, "hunter2hunter2"} {
		if strings.Contains(body, banned) || strings.Contains(doReqBody(t, base, rawKey, "/api/v1/insights/overview"), banned) {
			t.Errorf("response contains %q", banned)
		}
	}
}

func doReqBody(t *testing.T, base, key, path string) string {
	t.Helper()
	_, body := doReq(t, base, key, "GET", path, "")
	return body
}
