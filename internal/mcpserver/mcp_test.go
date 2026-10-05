package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

const testAPIKey = "m3m0s_test-key"

// fakeServe simulates an m3m0s serve minimal enough to exercise the
// tools/call dispatch without depending on real embeddings/network.
func fakeServe(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	requireAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+testAPIKey {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		var req apitypes.AddRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := apitypes.AddResponse{
			Entries: []apitypes.AddedEntry{{
				Entry: schema.Entry{
					ID:             "mem_test0001",
					Content:        req.Content,
					Domain:         req.Domain,
					DomainCategory: schema.DomainCategory(req.Domain),
					Source:         req.Source,
					Tags:           req.Tags,
					Metadata:       req.Metadata,
					CreatedAt:      now,
					UpdatedAt:      now,
				},
				Deduped: false,
			}},
			Extracted: false,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("POST /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		resp := apitypes.SearchResponse{
			Results:      []schema.Entry{{ID: "mem_test0002", Content: "search result", Domain: "codigo:m3m0s", Source: "manual", Tags: []string{}, Metadata: map[string]string{}, CreatedAt: now, UpdatedAt: now}},
			ContextBlock: "## Relevant memories\n\n1. [0.90] codigo:m3m0s (manual, 2026-07-15)\n   search result",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("GET /api/v1/recent", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		resp := apitypes.RecentResponse{
			Results: []schema.Entry{{ID: "mem_test0003", Content: "recent memory", Domain: "codigo:m3m0s", Source: "manual", Tags: []string{}, Metadata: map[string]string{}, CreatedAt: now, UpdatedAt: now}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		extractModel := "fake-extract-model"
		resp := apitypes.StatsResponse{
			Count: 3, CountByDomainCategory: map[string]int{"codigo": 3}, Backend: "fake", Model: "fake-model",
			Extraction: apitypes.ExtractionInfo{Enabled: true, Model: &extractModel},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("PATCH /api/v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		id := r.PathValue("id")
		var req apitypes.UpdateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		entry := schema.Entry{
			ID: id, Content: "updated content", Domain: "codigo:m3m0s", DomainCategory: "codigo",
			Source: "manual", Tags: []string{}, Metadata: map[string]string{}, CreatedAt: now, UpdatedAt: now,
		}
		_ = json.NewEncoder(w).Encode(entry)
	})

	mux.HandleFunc("GET /api/v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		id := r.PathValue("id")
		if id == "mem_does_not_exist" {
			w.WriteHeader(http.StatusNotFound)
			var body apitypes.ErrorBody
			body.ErrorInfo.Code = "not_found"
			body.ErrorInfo.Message = "memory not found: " + id
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		entry := schema.Entry{
			ID: id, Content: "content read via memory_get", Domain: "codigo:m3m0s", DomainCategory: "codigo",
			Source: "manual", Tags: []string{}, Metadata: map[string]string{}, CreatedAt: now, UpdatedAt: now,
		}
		_ = json.NewEncoder(w).Encode(entry)
	})

	return httptest.NewServer(mux)
}

// rpcCall builds a JSON-RPC request line.
func rpcCall(t *testing.T, id int, method string, params any) string {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(b)
}

func runLines(t *testing.T, lines []string) []map[string]any {
	t.Helper()
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	if err := Run(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var responses []map[string]any
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("response is not valid JSON: %s: %v", line, err)
		}
		responses = append(responses, m)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}
	return responses
}

func TestInitializeAndNotification(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	lines := []string{
		rpcCall(t, 1, "initialize", map[string]any{}),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}
	responses := runLines(t, lines)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response (notification does not respond), got %d: %v", len(responses), responses)
	}
	result, ok := responses[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize response without result: %v", responses[0])
	}
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v, want %v", result["protocolVersion"], protocolVersion)
	}
}

func TestToolsList(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	responses := runLines(t, []string{rpcCall(t, 1, "tools/list", nil)})
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	result := responses[0]["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 8 {
		t.Fatalf("expected 8 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"memory_add", "memory_search", "memory_recent", "memory_stats", "memory_update", "memory_get", "memory_forget_sweep", "memory_restore"} {
		if !names[want] {
			t.Errorf("tool %q not found in tools/list", want)
		}
	}
}

func TestToolsCallMemoryAdd(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_add",
		"arguments": map[string]any{"content": "test content", "domain": "codigo:m3m0s"},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("memory_add should not fail: %v", result)
	}
	content := result["content"].([]any)[0].(map[string]any)
	text := content["text"].(string)
	if !strings.Contains(text, "mem_test0001") {
		t.Errorf("memory_add text does not contain the id returned by the fake serve: %s", text)
	}
}

func TestToolsCallMemorySearchReturnsContextBlock(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_search",
		"arguments": map[string]any{"query": "something"},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "## Relevant memories") {
		t.Errorf("memory_search should return the contextBlock as the main text, got: %s", text)
	}
}

func TestToolsCallMemoryRecentAndStats(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	recentParams := map[string]any{"name": "memory_recent", "arguments": map[string]any{"limit": 5}}
	statsParams := map[string]any{"name": "memory_stats", "arguments": map[string]any{}}

	responses := runLines(t, []string{
		rpcCall(t, 1, "tools/call", recentParams),
		rpcCall(t, 2, "tools/call", statsParams),
	})
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}

	recentText := responses[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(recentText, "mem_test0003") {
		t.Errorf("memory_recent does not contain the expected id: %s", recentText)
	}

	statsText := responses[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(statsText, "fake-model") {
		t.Errorf("memory_stats does not contain the expected model: %s", statsText)
	}
}

func TestToolsCallMemoryUpdate(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_update",
		"arguments": map[string]any{"id": "mem_test0001", "content": "new content"},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("memory_update should not fail: %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "mem_test0001") {
		t.Errorf("memory_update text does not contain the id: %s", text)
	}
}

func TestToolsCallMemoryUpdateWithoutIDIsToolError(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_update",
		"arguments": map[string]any{"content": "no id"},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true when id is missing, got: %v", result)
	}
}

func TestToolsCallMemoryGet(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_get",
		"arguments": map[string]any{"id": "mem_test0001"},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("memory_get should not fail: %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "mem_test0001") {
		t.Errorf("memory_get text does not contain the id: %s", text)
	}
}

func TestToolsCallMemoryGetNotFoundIsToolError(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_get",
		"arguments": map[string]any{"id": "mem_does_not_exist"},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true for a nonexistent id, got: %v", result)
	}
}

func TestToolsCallMemoryGetWithoutIDIsToolError(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{
		"name":      "memory_get",
		"arguments": map[string]any{},
	}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result := responses[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true when id is missing, got: %v", result)
	}
}

func TestToolsCallUnknownToolIsToolErrorNotTransportError(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	params := map[string]any{"name": "memory_delete", "arguments": map[string]any{}}
	responses := runLines(t, []string{rpcCall(t, 1, "tools/call", params)})
	result, ok := responses[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result (tool error), not a JSON-RPC transport error: %v", responses[0])
	}
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true for an unknown tool, got: %v", result)
	}
}

func TestUnknownMethodIsJSONRPCError(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	responses := runLines(t, []string{rpcCall(t, 1, "method/nonexistent", nil)})
	errObj, ok := responses[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error field for an unknown method: %v", responses[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != errMethodNotFound {
		t.Errorf("code = %v, want %d", errObj["code"], errMethodNotFound)
	}
}

func TestMalformedJSONLineReturnsParseError(t *testing.T) {
	srv := fakeServe(t)
	defer srv.Close()
	t.Setenv("M3M0S_URL", srv.URL)
	t.Setenv("M3M0S_API_KEY", testAPIKey)

	responses := runLines(t, []string{`{"jsonrpc":"2.0","id":1,"method":`})
	errObj, ok := responses[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error field for a malformed line: %v", responses[0])
	}
	if code, _ := errObj["code"].(float64); int(code) != errParseError {
		t.Errorf("code = %v, want %d", errObj["code"], errParseError)
	}
}
