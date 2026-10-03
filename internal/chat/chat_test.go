package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFakeChatServer starts a local HTTP server that speaks the same
// OpenAI-compatible chat completions contract OpenRouter uses; same pattern
// as internal/extract/extract_test.go, a local version so this package does
// not depend on test exports from another package.
func newFakeChatServer(t *testing.T, responder func(req chatRequest) (int, chatResponse)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		status, resp := responder(req)
		if status >= 400 {
			http.Error(w, "simulated error", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCompleteSimpleReplyWithoutToolCall(t *testing.T) {
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		return http.StatusOK, chatResponse{
			Choices: []chatChoice{{Message: Message{Role: "assistant", Content: "hello, how are you?"}}},
		}
	})
	c := New("fake-key", srv.URL, "fake-model")

	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "hello, how are you?" {
		t.Fatalf("content = %q, want %q", msg.Content, "hello, how are you?")
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %#v", msg.ToolCalls)
	}
}

func TestCompleteParsesToolCall(t *testing.T) {
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		return http.StatusOK, chatResponse{
			Choices: []chatChoice{{Message: Message{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: ToolCallFunction{
						Name:      "add_memory",
						Arguments: `{"content":"remember this","domain":"personal:test"}`,
					},
				}},
			}}},
		}
	})
	c := New("fake-key", srv.URL, "fake-model")
	tools := []ToolDef{{Type: "function", Function: ToolFunction{Name: "add_memory", Parameters: json.RawMessage(`{"type":"object"}`)}}}

	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "remember this"}}, tools)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != "add_memory" {
		t.Fatalf("tool call name = %q, want add_memory", msg.ToolCalls[0].Function.Name)
	}
}

func TestCompleteSendsToolsAndAutoChoiceOnlyWhenToolsPresent(t *testing.T) {
	var gotTools []ToolDef
	var gotChoice string
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		gotTools = req.Tools
		gotChoice = req.ToolChoice
		return http.StatusOK, chatResponse{Choices: []chatChoice{{Message: Message{Role: "assistant", Content: "ok"}}}}
	})
	c := New("fake-key", srv.URL, "fake-model")

	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotTools != nil || gotChoice != "" {
		t.Fatalf("without tools expected tools/tool_choice absent, got tools=%#v tool_choice=%q", gotTools, gotChoice)
	}

	tools := []ToolDef{{Type: "function", Function: ToolFunction{Name: "add_memory"}}}
	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(gotTools) != 1 || gotChoice != "auto" {
		t.Fatalf("with tools expected tool_choice=auto and 1 tool, got tools=%#v tool_choice=%q", gotTools, gotChoice)
	}
}

func TestCompleteDisablesReasoning(t *testing.T) {
	var gotReasoning *reasoningConfig
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		gotReasoning = req.Reasoning
		return http.StatusOK, chatResponse{Choices: []chatChoice{{Message: Message{Role: "assistant", Content: "ok"}}}}
	})
	c := New("fake-key", srv.URL, "fake-model")

	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotReasoning == nil || gotReasoning.Enabled {
		t.Fatalf("expected reasoning={enabled:false}, got %#v", gotReasoning)
	}
}

func TestCompleteErrorsOnHTTPFailure(t *testing.T) {
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		return http.StatusInternalServerError, chatResponse{}
	})
	c := New("fake-key", srv.URL, "fake-model")

	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("expected an error on HTTP failure")
	}
}

func TestCompleteErrorsOnEmptyChoices(t *testing.T) {
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		return http.StatusOK, chatResponse{Choices: nil}
	})
	c := New("fake-key", srv.URL, "fake-model")

	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("expected an error on a response without choices")
	}
}

func TestTokensUsedAccumulatesAcrossCalls(t *testing.T) {
	srv := newFakeChatServer(t, func(req chatRequest) (int, chatResponse) {
		return http.StatusOK, chatResponse{
			Choices: []chatChoice{{Message: Message{Role: "assistant", Content: "ok"}}},
			Usage:   chatUsage{PromptTokens: 10, CompletionTokens: 5},
		}
	})
	c := New("fake-key", srv.URL, "fake-model")

	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	prompt, completion := c.TokensUsed()
	if prompt != 20 || completion != 10 {
		t.Fatalf("TokensUsed = (%d, %d), want (20, 10)", prompt, completion)
	}
}

func TestNewFallsBackToDefaults(t *testing.T) {
	c := New("key", "", "")
	if c.baseURL != DefaultBaseURL {
		t.Errorf("expected default baseURL %q, got %q", DefaultBaseURL, c.baseURL)
	}
	if c.model != DefaultModel {
		t.Errorf("expected default model %q, got %q", DefaultModel, c.model)
	}
}
