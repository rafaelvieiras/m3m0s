package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/mcpserver"
)

// mcpPost sends one body to POST /mcp and returns status and raw response.
func mcpPost(t *testing.T, base, path, key, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func mcpRPC(t *testing.T, base, key string, id int, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	status, raw := mcpPost(t, base, "/mcp", key, string(body), nil)
	if status != http.StatusOK {
		t.Fatalf("%s: status %d, body %s", method, status, raw)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("%s: bad json %q: %v", method, raw, err)
	}
	return out
}

func toolNames(t *testing.T, resp map[string]any) map[string]bool {
	t.Helper()
	res, _ := resp["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	names := map[string]bool{}
	for _, x := range tools {
		names[x.(map[string]any)["name"].(string)] = true
	}
	return names
}

func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	res, _ := resp["result"].(map[string]any)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in %v", resp)
	}
	isErr, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isErr
}

func TestMCPHTTPInitializeListAndCalls(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	init := mcpRPC(t, ts.URL, key, 1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "0"},
	})
	res := init["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v, want echo of 2025-03-26", res["protocolVersion"])
	}
	if res["serverInfo"].(map[string]any)["name"] != "m3m0s" {
		t.Errorf("serverInfo = %v", res["serverInfo"])
	}

	// Unknown client version falls back to the server default.
	fb := mcpRPC(t, ts.URL, key, 2, "initialize", map[string]any{"protocolVersion": "1999-01-01"})
	if fb["result"].(map[string]any)["protocolVersion"] == "1999-01-01" {
		t.Errorf("unsupported version must not be echoed")
	}

	// notifications/initialized: 202 Accepted, empty body.
	status, raw := mcpPost(t, ts.URL, "/mcp", key, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if status != http.StatusAccepted || raw != "" {
		t.Errorf("notification: status %d body %q, want 202 and empty", status, raw)
	}

	if ping := mcpRPC(t, ts.URL, key, 3, "ping", nil); ping["error"] != nil {
		t.Errorf("ping: %v", ping)
	}

	names := toolNames(t, mcpRPC(t, ts.URL, key, 4, "tools/list", nil))
	for _, n := range []string{"memory_add", "memory_search", "memory_recent", "memory_stats", "memory_update", "memory_get", "memory_forget_sweep", "memory_restore"} {
		if !names[n] {
			t.Errorf("tools/list missing %s", n)
		}
	}

	// tools/call add then search, through the real API underneath.
	add := mcpRPC(t, ts.URL, key, 5, "tools/call", map[string]any{
		"name": "memory_add",
		"arguments": map[string]any{
			"content": "The deploy uses blue green releases", "domain": "code:demo", "source": "test",
		},
	})
	if text, isErr := toolText(t, add); isErr || !strings.Contains(text, "stored") {
		t.Fatalf("memory_add: isErr=%v text=%s", isErr, text)
	}
	search := mcpRPC(t, ts.URL, key, 6, "tools/call", map[string]any{
		"name":      "memory_search",
		"arguments": map[string]any{"query": "blue green releases", "mode": "keyword", "scope": "global"},
	})
	if text, isErr := toolText(t, search); isErr || !strings.Contains(text, "blue green") {
		t.Fatalf("memory_search: isErr=%v text=%s", isErr, text)
	}
}

func TestMCPHTTPAuthFailure(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	if status, _ := mcpPost(t, ts.URL, "/mcp", "", body, nil); status != http.StatusUnauthorized {
		t.Errorf("no key: status %d, want 401", status)
	}
	if status, _ := mcpPost(t, ts.URL, "/mcp", "wrong-key", body, nil); status != http.StatusUnauthorized {
		t.Errorf("bad key: status %d, want 401", status)
	}
	if status, _ := mcpPost(t, ts.URL, "/mcp", key, body, nil); status != http.StatusOK {
		t.Errorf("valid key: status %d, want 200", status)
	}
}

func TestMCPHTTPReadOnly(t *testing.T) {
	srv, key := newTestServer(t)
	srv.SetMCPOptions(mcpserver.HTTPOptions{ReadOnly: true})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	names := toolNames(t, mcpRPC(t, ts.URL, key, 1, "tools/list", nil))
	for _, n := range []string{"memory_add", "memory_update", "memory_forget_sweep", "memory_restore"} {
		if names[n] {
			t.Errorf("read-only tools/list still has %s", n)
		}
	}
	for _, n := range []string{"memory_search", "memory_get", "memory_recent", "memory_stats"} {
		if !names[n] {
			t.Errorf("read-only tools/list lost %s", n)
		}
	}
	add := mcpRPC(t, ts.URL, key, 2, "tools/call", map[string]any{
		"name":      "memory_add",
		"arguments": map[string]any{"content": "x", "domain": "code:demo"},
	})
	if text, isErr := toolText(t, add); !isErr || !strings.Contains(text, "read-only") {
		t.Errorf("memory_add must be rejected in read-only mode: isErr=%v text=%s", isErr, text)
	}
}

func TestMCPHTTPReadOnlyQueryNarrowsOnly(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	_, raw := mcpPost(t, ts.URL, "/mcp?read_only=1", key, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	if strings.Contains(raw, `"memory_add"`) {
		t.Errorf("?read_only=1 must hide write tools: %s", raw)
	}
	_, raw = mcpPost(t, ts.URL, "/mcp", key, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	if !strings.Contains(raw, `"memory_add"`) {
		t.Errorf("default must expose write tools")
	}
}

func TestMCPHTTPInvalidJSONRPC(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	cases := map[string]struct {
		body string
		code int
	}{
		"malformed json":  {`{not json`, -32700},
		"wrong version":   {`{"jsonrpc":"1.0","id":1,"method":"ping"}`, -32600},
		"missing method":  {`{"jsonrpc":"2.0","id":1}`, -32600},
		"unknown method":  {`{"jsonrpc":"2.0","id":1,"method":"nope/nothing"}`, -32601},
		"bad call params": {`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"x"}`, -32602},
	}
	for name, tc := range cases {
		status, raw := mcpPost(t, ts.URL, "/mcp", key, tc.body, nil)
		var out struct {
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Errorf("%s: bad json %q", name, raw)
			continue
		}
		if out.Error.Code != tc.code {
			t.Errorf("%s: code %d, want %d (status %d, body %s)", name, out.Error.Code, tc.code, status, raw)
		}
		if tc.code != -32601 && tc.code != -32602 && status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, status)
		}
	}

	if status, _ := mcpPost(t, ts.URL, "/mcp", key, `[]`, nil); status != http.StatusBadRequest {
		t.Errorf("empty batch: status %d, want 400", status)
	}
	_, raw := mcpPost(t, ts.URL, "/mcp", key, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"}]`, nil)
	var batch []map[string]any
	if err := json.Unmarshal([]byte(raw), &batch); err != nil || len(batch) != 1 {
		t.Errorf("batch response = %q, want 1 element", raw)
	}
}

func TestMCPHTTPMethodAndHeaders(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(m, ts.URL+"/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
			t.Errorf("%s: status %d allow %q, want 405 POST", m, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if status, _ := mcpPost(t, ts.URL, "/mcp", key, body, map[string]string{"Mcp-Protocol-Version": "1999-01-01"}); status != http.StatusBadRequest {
		t.Errorf("unsupported protocol header: status %d, want 400", status)
	}
	if status, _ := mcpPost(t, ts.URL, "/mcp", key, body, map[string]string{"Mcp-Protocol-Version": "2025-06-18", "Mcp-Session-Id": "ignored"}); status != http.StatusOK {
		t.Errorf("supported protocol header: status %d, want 200", status)
	}
}

func TestMCPHTTPOriginValidation(t *testing.T) {
	srv, key := newTestServer(t)
	srv.SetMCPOptions(mcpserver.HTTPOptions{AllowedOrigins: []string{"https://chat.example.com"}})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	cases := []struct {
		origin string
		want   int
	}{
		{"", http.StatusOK}, // non-browser client
		{"http://evil.example.net", http.StatusForbidden}, // rebinding / foreign page
		{"https://chat.example.com", http.StatusOK},       // configured
		{ts.URL, http.StatusOK},                           // same origin
	}
	for _, tc := range cases {
		hdr := map[string]string{}
		if tc.origin != "" {
			hdr["Origin"] = tc.origin
		}
		if status, _ := mcpPost(t, ts.URL, "/mcp", key, body, hdr); status != tc.want {
			t.Errorf("origin %q: status %d, want %d", tc.origin, status, tc.want)
		}
	}
}
