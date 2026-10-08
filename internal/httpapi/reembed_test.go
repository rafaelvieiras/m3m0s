package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// oneHotEmbedServer answers single and array inputs with orthogonal vectors
// (position = text length mod dims), honoring "dimensions" (default 4).
func oneHotEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input      json.RawMessage `json:"input"`
			Dimensions int             `json:"dimensions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var inputs []string
		if json.Unmarshal(req.Input, &inputs) != nil {
			var one string
			_ = json.Unmarshal(req.Input, &one)
			inputs = []string{one}
		}
		dims := 4
		if req.Dimensions > 0 {
			dims = req.Dimensions
		}
		data := make([]map[string]any, len(inputs))
		for i, in := range inputs {
			v := make([]float32, dims)
			v[len(in)%dims] = 1
			data[i] = map[string]any{"index": i, "embedding": v}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func doReq(t *testing.T, base, key, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, bytes.NewBufferString(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestReembedEndpointsEndToEnd(t *testing.T) {
	const secret = "sk-http-secret-999"
	memStore, err := store.New(t.TempDir(), secret, oneHotEmbedServer(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	memStore.SetReembedOptions(store.ReembedOptions{RatePerSec: 10000, BackoffBase: time.Millisecond})
	keyStore, _ := auth.LoadStore(memStore.DB())
	rawKey, _, _ := keyStore.Create("test")
	sched := jobs.NewScheduler(time.Hour)
	sched.Register(jobs.Job{
		Name: store.ReembedJobName,
		Progress: func() *jobs.Progress {
			p := memStore.ReembedStatus()
			if p.StartedAt.IsZero() {
				return nil
			}
			return &jobs.Progress{Done: p.Done, Total: p.Total, Errors: p.Errors, ETA: p.ETA}
		},
		Run: func(ctx context.Context) (string, error) { return memStore.RunReembed(ctx) },
	})
	httpSrv := httptest.NewServer(New(memStore, keyStore, nil, nil, 0, sched, memStore.DB()).Handler())
	t.Cleanup(httpSrv.Close)
	base := httpSrv.URL

	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/embedding-config/reembed/estimate"}, {"GET", "/api/v1/embedding-config/reembed"},
		{"POST", "/api/v1/embedding-config/reembed"}, {"DELETE", "/api/v1/embedding-config/reembed"},
	} {
		if code, _ := doReq(t, base, "", tc.method, tc.path, ""); code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", tc.method, tc.path, code)
		}
	}

	for _, text := range []string{"a", "bb"} {
		if code, body := doReq(t, base, rawKey, "POST", "/api/v1/memories", `{"content":"`+text+`","domain":"pessoal:test"}`); code/100 != 2 {
			t.Fatalf("add %q = %d %s", text, code, body)
		}
	}

	// Nothing to cancel yet.
	if code, _ := doReq(t, base, rawKey, "DELETE", "/api/v1/embedding-config/reembed", ""); code != http.StatusConflict {
		t.Errorf("cancel with nothing running = %d, want 409", code)
	}

	// The estimate for a proposed change, before confirming it.
	code, body := doReq(t, base, rawKey, "POST", "/api/v1/embedding-config/reembed/estimate", `{"dimensions":2}`)
	var est map[string]any
	_ = json.Unmarshal([]byte(body), &est)
	if code != 200 || est["pending"] != float64(2) || est["approximate"] != true || est["estimated_cost_usd"] != nil || est["estimated_tokens"] != float64(1) {
		t.Fatalf("estimate = %d %s", code, body)
	}
	// With no body it describes the active configuration: nothing pending.
	if code, body = doReq(t, base, rawKey, "POST", "/api/v1/embedding-config/reembed/estimate", ""); code != 200 || !strings.Contains(body, `"pending":0`) {
		t.Fatalf("active estimate = %d %s", code, body)
	}

	if code, _ = doReq(t, base, rawKey, "PUT", "/api/v1/embedding-config", `{"enabled":true,"model":"text-embedding-3-small","dimensions":2}`); code != http.StatusConflict {
		t.Fatalf("unconfirmed PUT = %d, want 409", code)
	}
	// Confirming starts the job.
	code, body = doReq(t, base, rawKey, "PUT", "/api/v1/embedding-config", `{"enabled":true,"model":"text-embedding-3-small","dimensions":2,"confirm_reembed":true}`)
	if code != 200 || !strings.Contains(body, `"reembed_started":true`) {
		t.Fatalf("confirmed PUT = %d %s", code, body)
	}
	var all []string
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body = doReq(t, base, rawKey, "GET", "/api/v1/embedding-config/reembed", "")
		all = append(all, body)
		if strings.Contains(body, `"finished_at"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never finished: %s", body)
		}
		time.Sleep(5 * time.Millisecond)
	}
	var pr struct {
		Running             bool
		Total, Done, Errors int
	}
	_ = json.Unmarshal([]byte(body), &pr)
	if pr.Running || pr.Done != 2 || pr.Total != 2 || pr.Errors != 0 {
		t.Errorf("final progress = %s", body)
	}
	_, cfg := doReq(t, base, rawKey, "GET", "/api/v1/embedding-config", "")
	_, jobsBody := doReq(t, base, rawKey, "GET", "/api/v1/jobs", "")
	all = append(all, cfg, jobsBody)
	if !strings.Contains(cfg, `"stale":0`) || !strings.Contains(jobsBody, `"progress":{"done":2,"total":2`) {
		t.Errorf("config = %s\njobs = %s", cfg, jobsBody)
	}
	for _, b := range all {
		if strings.Contains(b, secret) {
			t.Errorf("a response leaked the API key: %s", b)
		}
	}

	// Starting without a usable provider is refused.
	if code, _ = doReq(t, base, rawKey, "PUT", "/api/v1/embedding-config", `{"enabled":false,"model":"text-embedding-3-small","dimensions":2}`); code != 200 {
		t.Fatalf("disable = %d", code)
	}
	if code, _ = doReq(t, base, rawKey, "POST", "/api/v1/embedding-config/reembed", ""); code != http.StatusUnprocessableEntity {
		t.Errorf("start while disabled = %d, want 422", code)
	}
}
