package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

func TestProvenanceOverHTTP(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	call := func(method, path, body string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, httpSrv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp, raw
	}

	resp, raw := call("POST", "/api/v1/memories", `{"content":"alpha fact about deploys","domain":"codigo:t","source":"opencode","model":"gpt-5","project":"m3m0s"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("add: %d %s", resp.StatusCode, raw)
	}
	var added apitypes.AddResponse
	_ = json.Unmarshal(raw, &added)
	e := added.Entries[0].Entry
	if e.ModelValue() != "gpt-5" || e.ProjectValue() != "m3m0s" {
		t.Fatalf("add response lost provenance: %+v", e)
	}

	for _, body := range []string{
		`{"content":"x","domain":"codigo:t","project":"Not A Slug"}`,
		`{"content":"x","domain":"codigo:t","model":"two words"}`,
	} {
		if resp, raw := call("POST", "/api/v1/memories", body); resp.StatusCode != 400 || !strings.Contains(string(raw), "invalid_") {
			t.Errorf("invalid provenance %s: %d %s", body, resp.StatusCode, raw)
		}
	}

	// Filters on search and recent.
	call("POST", "/api/v1/memories", `{"content":"beta fact about deploys","domain":"codigo:t","source":"claude-code"}`)
	_, raw = call("POST", "/api/v1/search", `{"query":"fact about deploys","mode":"keyword","project":"m3m0s"}`)
	var sr apitypes.SearchResponse
	_ = json.Unmarshal(raw, &sr)
	if len(sr.Results) != 1 || sr.Results[0].ID != e.ID {
		t.Errorf("search project filter: %+v", sr.Results)
	}
	_, raw = call("GET", "/api/v1/recent?model=gpt-5", "")
	var rr apitypes.RecentResponse
	_ = json.Unmarshal(raw, &rr)
	if len(rr.Results) != 1 || rr.Results[0].ID != e.ID {
		t.Errorf("recent model filter: %+v", rr.Results)
	}

	// PATCH: set, then clear with null.
	resp, raw = call("PATCH", "/api/v1/memories/"+e.ID, `{"project":"other","model":null}`)
	var up apitypes.UpdateResponse
	_ = json.Unmarshal(raw, &up)
	if resp.StatusCode != 200 || up.ProjectValue() != "other" || up.Model != nil {
		t.Fatalf("patch: %d %s", resp.StatusCode, raw)
	}
	resp, raw = call("PATCH", "/api/v1/memories/"+e.ID, `{"project":null}`)
	_ = json.Unmarshal(raw, &up)
	if resp.StatusCode != 200 || up.Project != nil {
		t.Fatalf("patch clearing project: %d %s", resp.StatusCode, raw)
	}
}
