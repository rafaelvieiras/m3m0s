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

func TestCaptureFlagControlsBoilerplateFilterOverHTTP(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	call := func(method, path, body string) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, httpSrv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
		}
		return raw
	}
	const digest = "[user]\\nThis session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\\n\\nSummary: synthetic."

	var captured apitypes.AddResponse
	_ = json.Unmarshal(call("POST", "/api/v1/memories", `{"content":"`+digest+`","domain":"code:t","source":"claude-code","capture":true}`), &captured)
	if len(captured.Entries) != 0 || captured.Filtered != 1 {
		t.Fatalf("captured boilerplate was stored: %+v", captured)
	}

	var explicit apitypes.AddResponse
	_ = json.Unmarshal(call("POST", "/api/v1/memories", `{"content":"`+digest+`","domain":"code:t","source":"manual"}`), &explicit)
	if len(explicit.Entries) != 1 || explicit.Filtered != 0 {
		t.Fatalf("explicit write was filtered: %+v", explicit)
	}

	var ov apitypes.InsightsOverviewResponse
	_ = json.Unmarshal(call("GET", "/api/v1/insights/overview", ""), &ov)
	var dropped int64
	for _, r := range ov.Filter.Rules {
		dropped += r.Dropped
	}
	if len(ov.Filter.Rules) == 0 || dropped != 1 {
		t.Errorf("overview filter section: %+v", ov.Filter)
	}
}
