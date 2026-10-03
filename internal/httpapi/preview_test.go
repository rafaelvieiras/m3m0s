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
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

const (
	previewExtractedContent = "curated fact extracted from the original digest"
	previewRawContent       = "raw content that skipped extraction"
)

// previewVectorEmbedServer returns fixed orthogonal vectors for the 2 texts
// used in seedPreviewEntries; same rationale as vectorEmbedServer in
// internal/store/related_test.go (not directly reusable, it is a _test.go
// helper from another package): with the generic fakeEmbedServer from
// server_test.go (a constant vector for any input) the two entries would
// have cosine similarity 1.0 and dedup (D12, store.DedupSimilarityThreshold)
// would merge them into one, breaking the test's premise of "2 distinct
// entries in the same domain".
func previewVectorEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	vectors := map[string][]float32{
		previewExtractedContent: {1, 0, 0, 0},
		previewRawContent:       {0, 1, 0, 0},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		vec, ok := vectors[req.Input]
		if !ok {
			vec = []float32{0, 0, 1, 0}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newPreviewTestServer is newTestServer (server_test.go) swapping the
// constant fake embed for the previewVectorEmbedServer above; needed only by
// the tests in this file, which need 2 entries that do not dedup against each
// other.
func newPreviewTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	embed := previewVectorEmbedServer(t)
	memStore, err := store.New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	keyStore, err := auth.LoadStore(memStore.DB())
	if err != nil {
		t.Fatalf("auth.LoadStore: %v", err)
	}
	rawKey, _, err := keyStore.Create("test")
	if err != nil {
		t.Fatalf("keyStore.Create: %v", err)
	}
	return New(memStore, keyStore, nil, nil, 0, nil, nil), rawKey
}

// seedPreviewEntries writes 1 "extracted" entry (with metadata.source_content,
// injectable by hook mode) and 1 raw entry (no extraction, D12 item 5) in the
// test domain; it covers the filter that only the "hook" mode applies (D17).
func seedPreviewEntries(t *testing.T, srv *Server, domain string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()

	if _, err := srv.store.Add(ctx, store.AddInput{
		Content: previewExtractedContent,
		Domain:  domain,
		Source:  "claude-code",
		Metadata: map[string]string{
			apitypes.MetadataKeySourceContent: "original digest from which the fact was extracted",
		},
	}, now); err != nil {
		t.Fatalf("Add (extracted): %v", err)
	}

	if _, err := srv.store.Add(ctx, store.AddInput{
		Content: previewRawContent,
		Domain:  domain,
		Source:  "claude-code",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("Add (raw): %v", err)
	}
}

func TestHandlePreviewInjectionHookModeFiltersRawEntries(t *testing.T) {
	srv, rawKey := newPreviewTestServer(t)
	const domain = "code:preview-test"
	seedPreviewEntries(t, srv, domain)

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/preview/injection?domain="+domain+"&mode=hook", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out apitypes.PreviewInjectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if out.Mode != apitypes.PreviewInjectionModeHook {
		t.Errorf("Mode = %q, want hook", out.Mode)
	}
	if out.EntriesFetched != 2 {
		t.Errorf("EntriesFetched = %d, want 2", out.EntriesFetched)
	}
	if out.EntriesUsed != 1 {
		t.Errorf("EntriesUsed = %d, want 1 (only the extracted entry passes the filter)", out.EntriesUsed)
	}
	if !strings.Contains(out.Raw, "hookSpecificOutput") {
		t.Errorf("Raw does not contain the expected hook envelope: %q", out.Raw)
	}
	if !strings.Contains(out.Raw, previewExtractedContent) {
		t.Errorf("Raw does not contain the extracted entry: %q", out.Raw)
	}
	if strings.Contains(out.Raw, previewRawContent) {
		t.Errorf("Raw should not contain the raw entry (no source_content): %q", out.Raw)
	}
}

func TestHandlePreviewInjectionMCPModeReturnsToolResultEnvelope(t *testing.T) {
	srv, rawKey := newPreviewTestServer(t)
	const domain = "code:preview-test"
	seedPreviewEntries(t, srv, domain)

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/preview/injection?domain="+domain+"&mode=mcp", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	var out apitypes.PreviewInjectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if out.EntriesUsed != 2 {
		t.Errorf("EntriesUsed = %d, want 2 (mcp/memory_recent does not filter by extraction)", out.EntriesUsed)
	}
	if !strings.Contains(out.Raw, `"content"`) || !strings.Contains(out.Raw, previewRawContent) {
		t.Errorf("Raw does not contain the expected tool result envelope: %q", out.Raw)
	}
}

func TestHandlePreviewInjectionManualModeReturnsRawBlock(t *testing.T) {
	srv, rawKey := newPreviewTestServer(t)
	const domain = "code:preview-test"
	seedPreviewEntries(t, srv, domain)

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/preview/injection?domain="+domain+"&mode=manual", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	var out apitypes.PreviewInjectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasPrefix(out.Raw, "## Relevant memories") {
		t.Errorf("Raw should be the raw BuildContextBlock, got: %q", out.Raw)
	}
	if strings.Contains(out.Raw, "hookSpecificOutput") || strings.Contains(out.Raw, `"content"`) {
		t.Errorf("Raw should have no envelope from any other mode: %q", out.Raw)
	}
}

func TestHandlePreviewInjectionRejectsInvalidMode(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/preview/injection?mode=nonsense", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
