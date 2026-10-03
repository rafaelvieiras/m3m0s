package extract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeChatServer starts a local HTTP server that speaks the same
// OpenAI-compatible chat completions contract OpenRouter uses: POST
// /chat/completions receiving {model, messages}, answering
// {"choices":[{"message":{"content":"..."}}]}. responder receives the
// "user" message content and returns (status, assistant reply text).
func newFakeChatServer(t *testing.T, responder func(userContent string) (int, string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
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
		resp := chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: content}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestExtractParsesCleanJSONArray(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, `["fact one", "fact two"]`
	})
	e := New("fake-key", srv.URL, "fake-model")

	facts, err := e.Extract(context.Background(), "any content", "code:x")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts) != 2 || facts[0] != "fact one" || facts[1] != "fact two" {
		t.Fatalf("unexpected facts: %#v", facts)
	}
}

func TestExtractParsesJSONWithSurroundingText(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, "Here are the facts:\n```json\n[\"fact\"]\n```\nHope that helps!"
	})
	e := New("fake-key", srv.URL, "fake-model")

	facts, err := e.Extract(context.Background(), "x", "code:x")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts) != 1 || facts[0] != "fact" {
		t.Fatalf("unexpected facts: %#v", facts)
	}
}

func TestExtractParsesJSONObjectWithFactsKey(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, `{"facts": ["fact one", "fact two"]}`
	})
	e := New("fake-key", srv.URL, "fake-model")

	facts, err := e.Extract(context.Background(), "any content", "code:x")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts) != 2 || facts[0] != "fact one" || facts[1] != "fact two" {
		t.Fatalf("unexpected facts: %#v", facts)
	}
}

func TestExtractRequestsJSONObjectResponseFormat(t *testing.T) {
	var gotFormat *responseFormat
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotFormat = req.ResponseFormat
		resp := chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: `{"facts": ["fact"]}`}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if gotFormat == nil || gotFormat.Type != "json_object" {
		t.Fatalf("expected response_format={type: json_object} in the Extract call, got %#v", gotFormat)
	}
}

func TestClassifyDoesNotForceResponseFormat(t *testing.T) {
	var gotFormat *responseFormat
	sawFormat := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotFormat = req.ResponseFormat
		sawFormat = true
		resp := chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "related"}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Classify(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !sawFormat {
		t.Fatal("handler was never called")
	}
	if gotFormat != nil {
		t.Fatalf("Classify should not force response_format (Extract only), got %#v", gotFormat)
	}
}

func TestExtractDisablesReasoning(t *testing.T) {
	var gotReasoning *reasoningConfig
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotReasoning = req.Reasoning
		resp := chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: `{"facts": ["fact"]}`}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if gotReasoning == nil || gotReasoning.Enabled {
		t.Fatalf("expected reasoning={enabled: false} in the Extract call, got %#v", gotReasoning)
	}
}

func TestClassifyDisablesReasoning(t *testing.T) {
	var gotReasoning *reasoningConfig
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotReasoning = req.Reasoning
		resp := chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "related"}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Classify(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if gotReasoning == nil || gotReasoning.Enabled {
		t.Fatalf("expected reasoning={enabled: false} in the Classify call, got %#v", gotReasoning)
	}
}

func TestExtractErrorsOnHTTPFailure(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusInternalServerError, "internal error"
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:x"); err == nil {
		t.Fatal("expected an error on HTTP failure")
	}
}

func TestExtractErrorsOnEmptyArray(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, `[]`
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:x"); err == nil {
		t.Fatal("expected an error on empty array")
	}
}

func TestExtractErrorsOnInvalidJSON(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, "this is not JSON at all"
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:x"); err == nil {
		t.Fatal("expected an error on response without a JSON array")
	}
}

func TestExtractErrorsOnAllWhitespaceFacts(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, `["   ", ""]`
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:x"); err == nil {
		t.Fatal("expected an error when all facts are empty/whitespace")
	}
}

func TestExtractSendsDomainAsContext(t *testing.T) {
	var sawDomain bool
	srv := newFakeChatServer(t, func(userContent string) (int, string) {
		if strings.Contains(userContent, "code:my-project") {
			sawDomain = true
		}
		return http.StatusOK, `["fact"]`
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Extract(context.Background(), "x", "code:my-project"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !sawDomain {
		t.Fatal("expected the domain in the prompt sent to the model")
	}
}

func TestNewFallsBackToDefaults(t *testing.T) {
	e := New("key", "", "")
	if e.baseURL != DefaultBaseURL {
		t.Errorf("expected default baseURL %q, got %q", DefaultBaseURL, e.baseURL)
	}
	if e.model != DefaultModel {
		t.Errorf("expected default model %q, got %q", DefaultModel, e.model)
	}
}

func TestClassifyLowercasesAndTrimsResponse(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusOK, "  Contradicts  \n"
	})
	e := New("fake-key", srv.URL, "fake-model")

	got, err := e.Classify(context.Background(), "uses Python 3.12", "uses Python 3.10")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got != "contradicts" {
		t.Errorf("relation = %q, want %q", got, "contradicts")
	}
}

func TestClassifySendsBothFactsInPrompt(t *testing.T) {
	var sawBoth bool
	srv := newFakeChatServer(t, func(userContent string) (int, string) {
		if strings.Contains(userContent, "new fact here") && strings.Contains(userContent, "existing fact here") {
			sawBoth = true
		}
		return http.StatusOK, "related"
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Classify(context.Background(), "new fact here", "existing fact here"); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !sawBoth {
		t.Fatal("expected both facts in the prompt sent to the model")
	}
}

func TestTokensUsedAccumulatesAcrossCalls(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: `["fact"]`}}},
			Usage:   chatUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := New("fake-key", srv.URL, "fake-model")

	if prompt, completion := e.TokensUsed(); prompt != 0 || completion != 0 {
		t.Fatalf("TokensUsed before any call = (%d, %d), want (0, 0)", prompt, completion)
	}

	if _, err := e.Extract(context.Background(), "x", "code:x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := e.Extract(context.Background(), "x", "code:x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	prompt, completion := e.TokensUsed()
	if prompt != 20 || completion != 10 {
		t.Fatalf("TokensUsed after 2 calls = (%d, %d), want (20, 10)", prompt, completion)
	}
}

func TestClassifyErrorsOnHTTPFailure(t *testing.T) {
	srv := newFakeChatServer(t, func(_ string) (int, string) {
		return http.StatusInternalServerError, "internal error"
	})
	e := New("fake-key", srv.URL, "fake-model")

	if _, err := e.Classify(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected an error on HTTP failure")
	}
}
