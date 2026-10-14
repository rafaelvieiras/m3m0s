package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/store"
)

func putEmbeddingConfig(t *testing.T, base, key, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/embedding-config", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestEmbeddingConfigEndpoints(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	// Unauthenticated callers are refused.
	if resp, err := http.Get(httpSrv.URL + "/api/v1/embedding-config"); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %v, %v", resp, err)
	}

	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/embedding-config", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET = %v, %v", resp, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `"source":"environment"`) || !strings.Contains(string(body), `"has_api_key":true`) {
		t.Errorf("unexpected GET body: %s", body)
	}

	// The key set through PUT is stored but never echoed back.
	code, body2 := putEmbeddingConfig(t, httpSrv.URL, rawKey, `{"enabled":true,"model":"text-embedding-3-small","dimensions":4,"api_key":"sk-super-secret"}`)
	if code != http.StatusOK || strings.Contains(body2, "sk-super-secret") || !strings.Contains(body2, `"key_source":"database"`) {
		t.Fatalf("PUT = %d %s", code, body2)
	}
	req, _ = http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/embedding-config", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Contains(string(body), "sk-super-secret") {
		t.Errorf("GET leaked the API key: %s", body)
	}

	// The provider returns 4 dimensions: asking for 2 is rejected.
	if code, body := putEmbeddingConfig(t, httpSrv.URL, rawKey, `{"enabled":true,"model":"text-embedding-3-small","dimensions":2}`); code != http.StatusUnprocessableEntity {
		t.Errorf("mismatched dimensions = %d %s, want 422", code, body)
	}

	// With stored vectors, a model change needs confirm_reembed.
	if _, err := srv.store.Add(context.Background(), store.AddInput{Content: "a stored note", Domain: "personal:test"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	change := `{"enabled":true,"model":"other-model","dimensions":4}`
	if code, body := putEmbeddingConfig(t, httpSrv.URL, rawKey, change); code != http.StatusConflict || !strings.Contains(body, "reembed_required") {
		t.Errorf("unconfirmed change = %d %s, want 409", code, body)
	}
	if code, body := putEmbeddingConfig(t, httpSrv.URL, rawKey, strings.Replace(change, "}", `,"confirm_reembed":true}`, 1)); code != http.StatusOK || !strings.Contains(body, `"stale":1`) {
		t.Errorf("confirmed change = %d %s, want 200 with 1 stale vector", code, body)
	}
}
