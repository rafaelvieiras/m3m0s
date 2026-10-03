//go:build e2e

// E2E tests for PATCH /memories/:id (memory_update, Phase 5, D13) and the
// extraction field in GET /stats — starting a real `m3m0s serve`, exactly
// like the rest of e2e/http_test.go.
package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

func TestEndToEndUpdate(t *testing.T) {
	embed := newFakeEmbedServer(t)
	srv := startServe(t, embed.URL)

	addOne := func(req apitypes.AddRequest) apitypes.AddResponse {
		resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("add: status %d, body %s", resp.StatusCode, data)
		}
		var out apitypes.AddResponse
		mustUnmarshal(t, data, &out)
		return out
	}

	patch := func(id, apiKey string, body map[string]any) (*http.Response, []byte) {
		return doJSON(t, http.MethodPatch, srv.URL+"/api/v1/memories/"+id, apiKey, body)
	}

	t.Run("401 without key", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "content for auth test", Domain: "codigo:test-e2e-update-auth"})
		resp, _ := patch(added.Entries[0].Entry.ID, "", map[string]any{"content": "new"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("404 for nonexistent id", func(t *testing.T) {
		resp, data := patch("mem_really_does_not_exist", srv.APIKey, map[string]any{"content": "new"})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "not_found" {
			t.Errorf("expected code not_found, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("400 no_fields_to_update for empty body", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "original content", Domain: "codigo:test-e2e-update-empty"})
		resp, data := patch(added.Entries[0].Entry.ID, srv.APIKey, map[string]any{})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "no_fields_to_update" {
			t.Errorf("expected code no_fields_to_update, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("400 invalid_domain", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "original content", Domain: "codigo:test-e2e-update-domain"})
		resp, data := patch(added.Entries[0].Entry.ID, srv.APIKey, map[string]any{"domain": "DomainSemFormato"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "invalid_domain" {
			t.Errorf("expected code invalid_domain, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("400 content_required for empty content", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "original content", Domain: "codigo:test-e2e-update-content-req"})
		resp, data := patch(added.Entries[0].Entry.ID, srv.APIKey, map[string]any{"content": "   "})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "content_required" {
			t.Errorf("expected code content_required, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("400 source_required for empty source", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "original content", Domain: "codigo:test-e2e-update-source-req", Source: "manual"})
		resp, data := patch(added.Entries[0].Entry.ID, srv.APIKey, map[string]any{"source": ""})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "source_required" {
			t.Errorf("expected code source_required, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("partial edit: only domain changes, rest preserved", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{
			Content: "content that must not change", Domain: "codigo:test-e2e-update-partial",
			Source: "manual", Agent: "my-agent", Tags: []string{"foo"}, Metadata: map[string]string{"k": "v"},
		})
		id := added.Entries[0].Entry.ID

		resp, data := patch(id, srv.APIKey, map[string]any{"domain": "escrita:test-e2e-update-partial-new"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update: status %d, body %s", resp.StatusCode, data)
		}
		var updated apitypes.UpdateResponse
		mustUnmarshal(t, data, &updated)

		if updated.Domain != "escrita:test-e2e-update-partial-new" {
			t.Errorf("domain did not change: %q", updated.Domain)
		}
		if updated.DomainCategory != "escrita" {
			t.Errorf("domainCategory not recomputed: %q", updated.DomainCategory)
		}
		if updated.Content != "content that must not change" {
			t.Errorf("content changed without being sent: %q", updated.Content)
		}
		if updated.AgentValue() != "my-agent" {
			t.Errorf("agent changed without being sent: %v", updated.Agent)
		}
		if len(updated.Tags) != 1 || updated.Tags[0] != "foo" {
			t.Errorf("tags changed without being sent: %v", updated.Tags)
		}
		if updated.Metadata["k"] != "v" {
			t.Errorf("metadata changed without being sent: %v", updated.Metadata)
		}
		if updated.ID != id {
			t.Error("id should not change")
		}
	})

	t.Run("clear tags/metadata/agent", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{
			Content: "content to clear fields", Domain: "codigo:test-e2e-update-clear",
			Agent: "my-agent", Tags: []string{"foo", "bar"}, Metadata: map[string]string{"k": "v"},
		})
		id := added.Entries[0].Entry.ID

		resp, data := patch(id, srv.APIKey, map[string]any{
			"tags":     []string{},
			"metadata": map[string]any{},
			"agent":    nil,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update: status %d, body %s", resp.StatusCode, data)
		}
		var updated apitypes.UpdateResponse
		mustUnmarshal(t, data, &updated)

		if len(updated.Tags) != 0 {
			t.Errorf("expected empty tags, got %v", updated.Tags)
		}
		if len(updated.Metadata) != 0 {
			t.Errorf("expected empty metadata, got %v", updated.Metadata)
		}
		if updated.Agent != nil {
			t.Errorf("expected cleared agent (nil), got %v", *updated.Agent)
		}
	})

	t.Run("reembed: searching by the new content finds the entry", func(t *testing.T) {
		domain := "codigo:test-e2e-update-reembed"
		added := addOne(apitypes.AddRequest{Content: "original content before the edit", Domain: domain})
		id := added.Entries[0].Entry.ID

		resp, data := patch(id, srv.APIKey, map[string]any{"content": "completely new phrase after reembed"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update: status %d, body %s", resp.StatusCode, data)
		}

		searchResp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/search", srv.APIKey, apitypes.SearchRequest{
			Query: "completely new phrase after reembed", Domain: domain,
		})
		if searchResp.StatusCode != http.StatusOK {
			t.Fatalf("search: status %d, body %s", searchResp.StatusCode, data)
		}
		var searched apitypes.SearchResponse
		mustUnmarshal(t, data, &searched)
		var found bool
		for _, r := range searched.Results {
			if r.ID == id {
				found = true
				if !strings.Contains(r.Content, "completely new phrase") {
					t.Errorf("indexed content does not reflect the update: %q", r.Content)
				}
			}
		}
		if !found {
			t.Error("expected to find the edited entry searching by the new content (did reembed not happen?)")
		}
	})

	t.Run("update runs neither extraction nor dedup", func(t *testing.T) {
		extract := newFakeExtractServer(t, func(_ string) (int, string) {
			return http.StatusOK, `["should not be generated by update"]`
		})
		srvExtract := startServeWithExtraction(t, embed.URL, extract.URL)

		addResp, data := doJSON(t, http.MethodPost, srvExtract.URL+"/api/v1/memories", srvExtract.APIKey, apitypes.AddRequest{
			Content: "original content", Domain: "codigo:test-e2e-update-no-extract",
		})
		if addResp.StatusCode != http.StatusOK {
			t.Fatalf("add: status %d, body %s", addResp.StatusCode, data)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, data, &added)
		id := added.Entries[0].Entry.ID

		resp, data := doJSON(t, http.MethodPatch, srvExtract.URL+"/api/v1/memories/"+id, srvExtract.APIKey, map[string]any{
			"content": "content completely rewritten by update",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update: status %d, body %s", resp.StatusCode, data)
		}
		var updated apitypes.UpdateResponse
		mustUnmarshal(t, data, &updated)
		if updated.Content != "content completely rewritten by update" {
			t.Errorf("expected the raw content sent (without extraction), got %q", updated.Content)
		}
		if updated.ID != id {
			t.Errorf("update should not change the id: expected %s, got %s", id, updated.ID)
		}

		// Count only this test's domain: the background self-indexing of the
		// embedded docs (D25) also writes to the "codigo" category, so the
		// global category count is racy.
		recentResp, data := doJSON(t, http.MethodGet, srvExtract.URL+"/api/v1/recent?domain=codigo:test-e2e-update-no-extract", srvExtract.APIKey, nil)
		if recentResp.StatusCode != http.StatusOK {
			t.Fatalf("recent: status %d, body %s", recentResp.StatusCode, data)
		}
		var recent apitypes.RecentResponse
		mustUnmarshal(t, data, &recent)
		if len(recent.Results) != 1 {
			t.Errorf("update should not have generated extra entries: %d entries in the domain", len(recent.Results))
		}
	})
}

func TestEndToEndStatsExtractionField(t *testing.T) {
	t.Run("disabled: enabled=false, model=null", func(t *testing.T) {
		embed := newFakeEmbedServer(t)
		srv := startServe(t, embed.URL)

		resp, data := doJSON(t, http.MethodGet, srv.URL+"/api/v1/stats", srv.APIKey, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stats: status %d, body %s", resp.StatusCode, data)
		}
		var decoded map[string]any
		mustUnmarshal(t, data, &decoded)
		extraction, ok := decoded["extraction"].(map[string]any)
		if !ok {
			t.Fatalf("expected \"extraction\" field in the body, got: %s", data)
		}
		if extraction["enabled"] != false {
			t.Errorf("expected enabled=false, got %v", extraction["enabled"])
		}
		if v, ok := extraction["model"]; !ok || v != nil {
			t.Errorf("expected model:null, got %v (key present=%v)", v, ok)
		}

		var stats apitypes.StatsResponse
		mustUnmarshal(t, data, &stats)
		if stats.Extraction.Enabled {
			t.Error("Extraction.Enabled should be false")
		}
		if stats.Extraction.Model != nil {
			t.Errorf("Extraction.Model should be nil, got %q", *stats.Extraction.Model)
		}
	})

	t.Run("enabled: enabled=true, configured model", func(t *testing.T) {
		embed := newFakeEmbedServer(t)
		extract := newFakeExtractServer(t, func(_ string) (int, string) {
			return http.StatusOK, `["fact irrelevant for this test"]`
		})
		srv := startServe(t, embed.URL, "-extract-key", "fake-openrouter-key-not-validated", "-extract-base", extract.URL, "-extract-model", "e2e-test-model")

		resp, data := doJSON(t, http.MethodGet, srv.URL+"/api/v1/stats", srv.APIKey, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stats: status %d, body %s", resp.StatusCode, data)
		}
		var stats apitypes.StatsResponse
		mustUnmarshal(t, data, &stats)
		if !stats.Extraction.Enabled {
			t.Fatal("expected Extraction.Enabled=true with M3M0S_EXTRACT_API_KEY set")
		}
		if stats.Extraction.Model == nil || *stats.Extraction.Model != "e2e-test-model" {
			t.Errorf("expected Extraction.Model=%q, got %v", "e2e-test-model", stats.Extraction.Model)
		}
	})
}
