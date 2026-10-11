package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// The model that deletes is the configured one (D39); a request may only
// preview another model, and only as a dry-run.
func TestForgetSweepModelSelection(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	call := func(body string) (int, apitypes.ForgetSweepResponse, string) {
		code, out := doReq(t, ts.URL, key, "POST", "/api/v1/memories/forget-sweep", body)
		var resp apitypes.ForgetSweepResponse
		_ = json.Unmarshal([]byte(out), &resp)
		return code, resp, out
	}

	if code, resp, out := call(""); code != http.StatusOK || resp.Model != "legacy" {
		t.Errorf("default = %d %s", code, out)
	}
	if code, resp, out := call(`{"dryRun":true,"model":"per-kind"}`); code != http.StatusOK || resp.Model != "per-kind" || !resp.DryRun {
		t.Errorf("dry-run preview = %d %s", code, out)
	}
	if code, _, out := call(`{"dryRun":true,"model":"turbo"}`); code != http.StatusBadRequest || !contains(out, "invalid_model") {
		t.Errorf("unknown model = %d %s", code, out)
	}
	if code, _, out := call(`{"model":"per-kind"}`); code != http.StatusBadRequest || !contains(out, "model_requires_dry_run") {
		t.Errorf("real sweep with another model = %d %s", code, out)
	}
	// Naming the configured model on a real sweep is harmless.
	if code, resp, out := call(`{"model":"legacy"}`); code != http.StatusOK || resp.Model != "legacy" || resp.DryRun {
		t.Errorf("real sweep with the configured model = %d %s", code, out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
