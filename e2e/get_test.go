//go:build e2e

// E2E tests for GET /memories/:id (memory_get, a later elaboration after
// Phase 5/D13) — starting a real `m3m0s serve`, exactly like the rest of
// e2e/http_test.go/update_test.go.
package e2e

import (
	"net/http"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

func TestEndToEndGet(t *testing.T) {
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

	get := func(id, apiKey string) (*http.Response, []byte) {
		return doJSON(t, http.MethodGet, srv.URL+"/api/v1/memories/"+id, apiKey, nil)
	}

	t.Run("401 without key", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "content for auth test", Domain: "code:test-e2e-get-auth"})
		resp, _ := get(added.Entries[0].Entry.ID, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("404 for nonexistent id", func(t *testing.T) {
		resp, data := get("mem_really_does_not_exist", srv.APIKey)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "not_found" {
			t.Errorf("expected code not_found, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("200 returns the complete MemoryEntry, identical to add", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{
			Content: "content for reading via memory_get", Domain: "code:test-e2e-get-ok",
			Source: "manual", Agent: "my-agent", Tags: []string{"foo", "bar"}, Metadata: map[string]string{"k": "v"},
		})
		want := added.Entries[0].Entry

		resp, data := get(want.ID, srv.APIKey)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get: status %d, body %s", resp.StatusCode, data)
		}
		var got apitypes.GetResponse
		mustUnmarshal(t, data, &got)

		if got.ID != want.ID {
			t.Errorf("unexpected ID: %q", got.ID)
		}
		if got.Content != want.Content {
			t.Errorf("unexpected Content: %q", got.Content)
		}
		if got.Domain != want.Domain {
			t.Errorf("unexpected Domain: %q", got.Domain)
		}
		if got.DomainCategory != want.DomainCategory {
			t.Errorf("unexpected DomainCategory: %q", got.DomainCategory)
		}
		if got.Source != want.Source {
			t.Errorf("unexpected Source: %q", got.Source)
		}
		if got.AgentValue() != want.AgentValue() {
			t.Errorf("unexpected Agent: %v", got.Agent)
		}
		if len(got.Tags) != len(want.Tags) {
			t.Errorf("unexpected Tags: %v", got.Tags)
		}
		if got.Metadata["k"] != "v" {
			t.Errorf("unexpected Metadata: %v", got.Metadata)
		}
		if !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Errorf("UpdatedAt should not change on a GET: before=%v after=%v", want.UpdatedAt, got.UpdatedAt)
		}
	})

	t.Run("get after update reflects the latest value", func(t *testing.T) {
		added := addOne(apitypes.AddRequest{Content: "original content", Domain: "code:test-e2e-get-after-update"})
		id := added.Entries[0].Entry.ID

		resp, data := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/memories/"+id, srv.APIKey, map[string]any{
			"content": "content edited before the get",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update: status %d, body %s", resp.StatusCode, data)
		}

		resp, data = get(id, srv.APIKey)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get: status %d, body %s", resp.StatusCode, data)
		}
		var got apitypes.GetResponse
		mustUnmarshal(t, data, &got)
		if got.Content != "content edited before the get" {
			t.Errorf("get did not reflect the update: %q", got.Content)
		}
	})
}
