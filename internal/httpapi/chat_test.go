package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/chat"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// The wire* types below mirror the OpenAI-compatible contract that
// internal/chat.Client speaks; a local test version so this package does not
// depend on test exports from another package (same pattern as
// fakeEmbedServer in server_test.go).
type wireToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function wireToolCallFunction `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireChatRequest struct {
	Messages []wireMessage `json:"messages"`
}

type wireChatChoice struct {
	Message wireMessage `json:"message"`
}

type wireChatResponse struct {
	Choices []wireChatChoice `json:"choices"`
}

func newFakeChatUpstream(t *testing.T, responder func(req wireChatRequest) wireChatResponse) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req wireChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := responder(req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestServerWithChat(t *testing.T, chatUpstreamURL string) (*Server, string) {
	t.Helper()
	embed := fakeEmbedServer(t)
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
	chatClient := chat.New("fake-key", chatUpstreamURL, "fake-model")
	return New(memStore, keyStore, nil, chatClient, 0, nil, nil), rawKey
}

func doChatRequest(t *testing.T, httpSrv *httptest.Server, rawKey string, body apitypes.ChatRequest) (*http.Response, apitypes.ChatResponse) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/chat", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	var out apitypes.ChatResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp, out
}

// TestHandleChatDisabledReturns503 checks that POST /api/v1/chat degrades
// gracefully (503, not 500/panic) when the server has no chat.Client
// configured; the same degradation pattern as fact extraction (D12).
func TestHandleChatDisabledReturns503(t *testing.T) {
	srv, rawKey := newTestServer(t) // chat nil by default
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp, _ := doChatRequest(t, httpSrv, rawKey, apitypes.ChatRequest{
		Messages: []apitypes.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestHandleChatEmptyMessagesReturns400 checks body validation.
func TestHandleChatEmptyMessagesReturns400(t *testing.T) {
	upstream := newFakeChatUpstream(t, func(req wireChatRequest) wireChatResponse {
		t.Fatal("upstream should not be called with empty messages")
		return wireChatResponse{}
	})
	srv, rawKey := newTestServerWithChat(t, upstream.URL)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp, _ := doChatRequest(t, httpSrv, rawKey, apitypes.ChatRequest{Messages: nil})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestHandleChatRAGOnlyRepliesWithoutToolCall covers the question flow:
// with no tool call, the upstream response becomes Reply directly, with an
// empty (non-nil) ToolCalls.
func TestHandleChatRAGOnlyRepliesWithoutToolCall(t *testing.T) {
	upstream := newFakeChatUpstream(t, func(req wireChatRequest) wireChatResponse {
		return wireChatResponse{Choices: []wireChatChoice{{Message: wireMessage{
			Role: "assistant", Content: "I found nothing about that in memory.",
		}}}}
	})
	srv, rawKey := newTestServerWithChat(t, upstream.URL)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp, out := doChatRequest(t, httpSrv, rawKey, apitypes.ChatRequest{
		Messages: []apitypes.ChatMessage{{Role: "user", Content: "what do I know about cats?"}},
		Domain:   "personal:test",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if out.Reply != "I found nothing about that in memory." {
		t.Fatalf("reply = %q", out.Reply)
	}
	if out.ToolCalls == nil || len(out.ToolCalls) != 0 {
		t.Fatalf("toolCalls = %#v, want empty non-nil", out.ToolCalls)
	}
}

// TestHandleChatExecutesAddMemoryToolCall covers the "save something" flow:
// the upstream's 1st response brings an add_memory tool_call, the handler
// executes it against the real store, feeds the result back and makes a 2nd
// call that returns the final text answer.
func TestHandleChatExecutesAddMemoryToolCall(t *testing.T) {
	calls := 0
	upstream := newFakeChatUpstream(t, func(req wireChatRequest) wireChatResponse {
		calls++
		if calls == 1 {
			return wireChatResponse{Choices: []wireChatChoice{{Message: wireMessage{
				Role: "assistant",
				ToolCalls: []wireToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: wireToolCallFunction{
						Name:      "add_memory",
						Arguments: `{"content":"I like drip coffee","domain":"personal:test-chat"}`,
					},
				}},
			}}}}
		}
		// 2nd call: confirms that the role=tool message with the execution
		// result was really injected into the history before the final
		// answer.
		sawToolResult := false
		for _, m := range req.Messages {
			if m.Role == "tool" && m.ToolCallID == "call_1" {
				sawToolResult = true
			}
		}
		if !sawToolResult {
			t.Error("expected role=tool message with tool_call_id=call_1 in the 2nd call")
		}
		return wireChatResponse{Choices: []wireChatChoice{{Message: wireMessage{
			Role: "assistant", Content: "Saved that for you.",
		}}}}
	})
	srv, rawKey := newTestServerWithChat(t, upstream.URL)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp, out := doChatRequest(t, httpSrv, rawKey, apitypes.ChatRequest{
		Messages: []apitypes.ChatMessage{{Role: "user", Content: "remember that I like drip coffee"}},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if out.Reply != "Saved that for you." {
		t.Fatalf("reply = %q", out.Reply)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "add_memory" {
		t.Fatalf("toolCalls = %#v", out.ToolCalls)
	}
	if calls != 2 {
		t.Fatalf("upstream was called %d time(s), want 2", calls)
	}

	saved := srv.store.Recent(10, store.Filter{Domain: "personal:test-chat"}, time.Now())
	if len(saved) != 1 {
		t.Fatalf("expected 1 memory saved in personal:test-chat, got %d", len(saved))
	}
	if saved[0].Source != "webui-chat" {
		t.Fatalf("source = %q, want webui-chat", saved[0].Source)
	}
}

// TestHandleChatUpstreamFailureReturns502 checks that a provider failure
// becomes 502, not 500/panic.
func TestHandleChatUpstreamFailureReturns502(t *testing.T) {
	upstream := newFakeChatUpstream(t, func(req wireChatRequest) wireChatResponse {
		panic("should never be called; the test drops the connection beforehand")
	})
	upstream.Close() // valid URL, but nobody listening: forces a network error

	srv, rawKey := newTestServerWithChat(t, upstream.URL)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp, _ := doChatRequest(t, httpSrv, rawKey, apitypes.ChatRequest{
		Messages: []apitypes.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}
