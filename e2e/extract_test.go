//go:build e2e

// E2E tests for LLM fact extraction (Phase 4, D12): they start real
// `m3m0s serve`/`m3m0s watch`, with a local OpenAI-compatible chat
// completions fake (the same contract OpenRouter speaks) in place of the
// real OpenRouter — same principle as the embeddings fake in
// embed_test.go, 100% offline and deterministic.
package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

type fakeChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type fakeChatRequest struct {
	Model    string            `json:"model"`
	Messages []fakeChatMessage `json:"messages"`
}

type fakeChatResponse struct {
	Choices []struct {
		Message fakeChatMessage `json:"message"`
	} `json:"choices"`
}

// newFakeExtractServer starts a local HTTP server that speaks the same
// OpenAI-compatible chat completions contract OpenRouter uses
// (POST /chat/completions), delegating the response decision to
// responder — it receives the content of the "user" message (which includes
// the domain and the original text, see internal/extract.Extract) and
// returns (HTTP status, assistant text body).
func newFakeExtractServer(t *testing.T, responder func(userContent string) (int, string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req fakeChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var userContent string
		for _, m := range req.Messages {
			if m.Role == "user" {
				userContent = m.Content
			}
		}
		status, content := responder(userContent)
		if status >= 400 {
			http.Error(w, content, status)
			return
		}
		resp := fakeChatResponse{}
		resp.Choices = []struct {
			Message fakeChatMessage `json:"message"`
		}{{Message: fakeChatMessage{Role: "assistant", Content: content}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// startServeWithExtraction starts `m3m0s serve` with fact extraction
// enabled, pointed at the chat completions fake at extractURL.
func startServeWithExtraction(t *testing.T, embedURL, extractURL string) *serveProc {
	t.Helper()
	return startServe(t, embedURL, "-extract-key", "fake-openrouter-key-not-validated", "-extract-base", extractURL)
}

func TestEndToEndExtractionEnabledGeneratesMultipleEntries(t *testing.T) {
	embed := newFakeEmbedServer(t)
	extract := newFakeExtractServer(t, func(_ string) (int, string) {
		return http.StatusOK, `["The maintainer prefers Docker for deployment.", "M3m0s uses chromem-go for vector search."]`
	})
	srv := startServeWithExtraction(t, embed.URL, extract.URL)

	resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
		Content: "Any raw text, the fake extractor ignores it and always returns the same 2 facts.",
		Domain:  "code:test-e2e-extract-on",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, body %s", resp.StatusCode, data)
	}
	var added apitypes.AddResponse
	mustUnmarshal(t, data, &added)

	if !added.Extracted {
		t.Error("expected extracted=true with extraction enabled")
	}
	if len(added.Entries) != 2 {
		t.Fatalf("expected 2 entries (2 extracted facts), got %d: %#v", len(added.Entries), added.Entries)
	}
	if added.Entries[0].Entry.Content != "The maintainer prefers Docker for deployment." {
		t.Errorf("unexpected fact 1: %q", added.Entries[0].Entry.Content)
	}
	if added.Entries[1].Entry.Content != "M3m0s uses chromem-go for vector search." {
		t.Errorf("unexpected fact 2: %q", added.Entries[1].Entry.Content)
	}
	for _, e := range added.Entries {
		if e.Entry.Domain != "code:test-e2e-extract-on" {
			t.Errorf("domain not propagated to the extracted fact: %q", e.Entry.Domain)
		}
	}
}

// TestEndToEndExtractionSourceContentAppearsOnlyWhenExtracted checks D13,
// item 3/docs/api-contract.md: each extracted fact carries
// metadata.source_content = the original text that generated it, and the
// entry does NOT carry that field when extraction is disabled (the entry's
// own content is already the original in that case).
func TestEndToEndExtractionSourceContentAppearsOnlyWhenExtracted(t *testing.T) {
	embed := newFakeEmbedServer(t)
	extract := newFakeExtractServer(t, func(_ string) (int, string) {
		return http.StatusOK, `["First atomic fact.", "Second atomic fact."]`
	})
	srv := startServeWithExtraction(t, embed.URL, extract.URL)

	original := "Original raw text that the fake extractor ignores and always turns into the same 2 facts."
	resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
		Content: original,
		Domain:  "code:test-e2e-source-content-on",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, body %s", resp.StatusCode, data)
	}
	var added apitypes.AddResponse
	mustUnmarshal(t, data, &added)
	if !added.Extracted {
		t.Fatal("setup: expected extracted=true")
	}
	for _, e := range added.Entries {
		if e.Entry.Metadata["source_content"] != original {
			t.Errorf("expected metadata.source_content=%q, got %q", original, e.Entry.Metadata["source_content"])
		}
	}
}

func TestEndToEndExtractionDisabledSourceContentAbsent(t *testing.T) {
	embed := newFakeEmbedServer(t)
	// No -extract-key: extraction disabled.
	srv := startServe(t, embed.URL)

	resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
		Content: "raw content without extraction enabled",
		Domain:  "code:test-e2e-source-content-off",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, body %s", resp.StatusCode, data)
	}
	var added apitypes.AddResponse
	mustUnmarshal(t, data, &added)
	if added.Extracted {
		t.Fatal("setup: expected extracted=false")
	}
	if _, ok := added.Entries[0].Entry.Metadata["source_content"]; ok {
		t.Errorf("did not expect metadata.source_content without extraction, got %q", added.Entries[0].Entry.Metadata["source_content"])
	}
}

func TestEndToEndExtractionDisabledBehavesLikeBeforePhase4(t *testing.T) {
	embed := newFakeEmbedServer(t)
	// No -extract-key: extraction disabled, same toggle pattern that
	// -openai-base already uses for embeddings.
	srv := startServe(t, embed.URL)

	resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
		Content: "raw content without extraction enabled on the server",
		Domain:  "code:test-e2e-extract-off",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, body %s", resp.StatusCode, data)
	}
	var added apitypes.AddResponse
	mustUnmarshal(t, data, &added)

	if added.Extracted {
		t.Error("expected extracted=false without M3M0S_EXTRACT_API_KEY configured")
	}
	if len(added.Entries) != 1 || added.Entries[0].Entry.Content != "raw content without extraction enabled on the server" {
		t.Fatalf("expected 1 entry with the original content intact, got %#v", added.Entries)
	}
}

func TestEndToEndExtractionFailureFallsBackToRawEntry(t *testing.T) {
	embed := newFakeEmbedServer(t)
	extract := newFakeExtractServer(t, func(_ string) (int, string) {
		return http.StatusInternalServerError, "openrouter is down"
	})
	srv := startServeWithExtraction(t, embed.URL, extract.URL)

	resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
		Content: "original content that must survive the extractor failure",
		Domain:  "code:test-e2e-extract-fallback",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add should not fail when only extraction fails: status %d, body %s", resp.StatusCode, data)
	}
	var added apitypes.AddResponse
	mustUnmarshal(t, data, &added)

	if added.Extracted {
		t.Error("expected extracted=false in the fallback")
	}
	if len(added.Entries) != 1 || added.Entries[0].Entry.Content != "original content that must survive the extractor failure" {
		t.Fatalf("expected fallback to the original content as 1 entry, got %#v", added.Entries)
	}
}

func TestEndToEndWatchWithExtractionTracksAllFactsForCascadePurge(t *testing.T) {
	embed := newFakeEmbedServer(t)
	extract := newFakeExtractServer(t, func(_ string) (int, string) {
		return http.StatusOK, `["first fact of the chunk watched by watch", "second fact of the chunk watched by watch"]`
	})
	srv := startServeWithExtraction(t, embed.URL, extract.URL)

	root := t.TempDir()
	const domain = "code:test-e2e-watch-extract"
	mustWriteFile(t, filepath.Join(root, "AGENTS.md"), "# Single header\n\nany paragraph that becomes 2 facts.\n")

	startWatch(t, srv, root, domain)

	waitUntil(t, 15*time.Second, func() bool {
		out := searchDomain(t, srv, "first fact of the chunk watched", domain)
		return len(out.Results) >= 1
	})

	out := searchDomain(t, srv, "fact of the chunk watched by watch", domain)
	if len(out.Results) < 2 {
		t.Fatalf("expected the 2 facts extracted from the single chunk to be indexed, got %d: %#v", len(out.Results), out.Results)
	}

	if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("remove AGENTS.md: %v", err)
	}

	waitUntil(t, 15*time.Second, func() bool {
		out := searchDomain(t, srv, "fact of the chunk watched by watch", domain)
		for _, r := range out.Results {
			if r.Metadata["file_path"] == "AGENTS.md" {
				return false
			}
		}
		return true
	})

	// Checks that the cascading purge really removed both facts, not just
	// the first — if ChunkIDs had lost an ID along the way, that fact
	// would be left orphaned in the index.
	final := searchDomain(t, srv, "fact of the chunk watched by watch", domain)
	for _, r := range final.Results {
		if strings.Contains(r.Content, "fact of the chunk watched by watch") {
			t.Errorf("orphan fact surviving the cascading purge: %#v", r)
		}
	}
}
