package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/chat"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// chatToolRounds limits handleChat's tool-calling loop: the 1st call may come
// back with an add_memory to execute, the 2nd is always the final
// natural-language answer (with the tool result already injected as a
// role=tool message). 2 rounds is enough for a single tool (D23); we never
// let the LLM chain tool calls indefinitely.
const chatToolRounds = 2

// chatRequestTimeout covers up to chatToolRounds provider calls within a
// single POST /api/v1/chat HTTP request; more generous than chat.Client's
// internal 30s timeout (which is per call), since here there may be 2 calls
// in sequence.
const chatRequestTimeout = 45 * time.Second

const addMemoryToolName = "add_memory"

// addMemoryTool is the only tool exposed to the chat LLM (D23): "save
// something to memory" via function-calling, instead of requiring a dedicated
// command from the user. "Asking" uses no tool at all: the context of
// relevant memories is already injected into the system prompt via RAG
// (buildChatMessages).
var addMemoryTool = chat.ToolDef{
	Type: "function",
	Function: chat.ToolFunction{
		Name:        addMemoryToolName,
		Description: "Saves a new memory in m3m0s. Use it only when the user explicitly asks to remember, save or store some piece of information, never in response to a question.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"content": {
					"type": "string",
					"description": "The text of the memory to save. It must make sense on its own (self-contained), without depending on the rest of the conversation. Write it in the same language the user used."
				},
				"domain": {
					"type": "string",
					"description": "Domain in the format category:instance (e.g. codigo:m3m0s, pessoal:financas, cliente:acme; the category names are kept in Portuguese to match existing stored data). If the user does not specify one, use the current domain suggested in this conversation's context."
				},
				"tags": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Optional tags to make future search easier."
				}
			},
			"required": ["content", "domain"]
		}`),
	},
}

// handleChat serves POST /api/v1/chat (D23, docs/decisions/): the WebUI's
// floating chat. Two capabilities, both in the same call: (1) RAG: every
// question automatically searches memory (s.store.Search) and injects the
// results into the system prompt, without requiring any tool; (2)
// tool-calling: the LLM may call add_memory when the user explicitly asks to
// save something, executed here against s.store.Add.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat_disabled", "WebUI chat not configured (M3M0S_CHAT_API_KEY/M3M0S_EXTRACT_API_KEY missing)")
		return
	}

	var req apitypes.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "empty_messages", "messages must not be empty")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), chatRequestTimeout)
	defer cancel()

	contextEntries := s.searchChatContext(ctx, req)
	messages := buildChatMessages(req, contextEntries)
	toolResults := make([]apitypes.ChatToolCallResult, 0)

	for round := 0; round < chatToolRounds; round++ {
		reply, err := s.chat.Complete(ctx, messages, []chat.ToolDef{addMemoryTool})
		if err != nil {
			writeError(w, http.StatusBadGateway, "chat_upstream_failed", err.Error())
			return
		}
		if len(reply.ToolCalls) == 0 {
			writeJSON(w, http.StatusOK, apitypes.ChatResponse{
				Reply:           reply.Content,
				ToolCalls:       toolResults,
				ContextEntryIDs: entryIDs(contextEntries),
			})
			return
		}

		messages = append(messages, reply)
		for _, tc := range reply.ToolCalls {
			result, toolMessageContent := s.executeChatTool(ctx, tc, req.Domain)
			toolResults = append(toolResults, result)
			messages = append(messages, chat.Message{
				Role:       "tool",
				Content:    toolMessageContent,
				ToolCallID: tc.ID,
			})
		}
	}

	// Exhausted chatToolRounds without a final text answer (the last round
	// only had a tool call again); return the tool results already executed
	// instead of leaving the request with no response at all.
	writeJSON(w, http.StatusOK, apitypes.ChatResponse{
		Reply:           "Memory processed.",
		ToolCalls:       toolResults,
		ContextEntryIDs: entryIDs(contextEntries),
	})
}

// searchChatContext runs best-effort RAG: it searches memory for the user's
// last message and returns the results to become context in the system
// prompt. A search failure is logged and treated as "no context"; it never
// brings down the chat response (same spirit as the DiskUsageBytes fallback
// in handleStats).
func (s *Server) searchChatContext(ctx context.Context, req apitypes.ChatRequest) []schema.Entry {
	query := lastUserMessage(req.Messages)
	if query == "" {
		return nil
	}
	results, err := s.store.Search(ctx, query, 5, store.Filter{Domain: req.Domain}, time.Now())
	if err != nil {
		log.Printf("m3m0s: chat context search failed (best-effort, continuing without context): %v", err)
		return nil
	}
	return results
}

func lastUserMessage(messages []apitypes.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// buildChatMessages builds the full history for the provider: 1 system
// message (assistant role + context of memories retrieved via RAG + suggested
// domain) followed by the conversation history coming from the request.
func buildChatMessages(req apitypes.ChatRequest, contextEntries []schema.Entry) []chat.Message {
	out := make([]chat.Message, 0, len(req.Messages)+1)
	out = append(out, chat.Message{Role: "system", Content: chatSystemPrompt(contextEntries, req.Domain)})
	for _, m := range req.Messages {
		out = append(out, chat.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

func chatSystemPrompt(entries []schema.Entry, domain string) string {
	var b strings.Builder
	b.WriteString("You are the memory assistant of m3m0s, a semantic memory service. " +
		"Reply in the same language as the user's message. Use the relevant memories below (if any) to answer questions about what has already been recorded, " +
		"and say clearly when you do not know, instead of making things up. " +
		"You have access to the add_memory tool: use it only when the user explicitly asks you to remember, save or store something, never in response to an ordinary question.")

	if len(entries) == 0 {
		b.WriteString("\n\nNo relevant memory was found for this question.")
	} else {
		b.WriteString("\n\n" + apitypes.BuildContextBlock(entries))
	}

	if domain != "" {
		fmt.Fprintf(&b, "\n\nCurrent WebUI domain: %s. Use it as the default for new memories when the user does not specify another.", domain)
	}

	return b.String()
}

// executeChatTool executes 1 ToolCall against the store and returns (summary
// for ChatResponse.ToolCalls, content of the role=tool message that feeds the
// LLM back). It never propagates an error to the caller: a malformed tool
// call or an Add failure becomes a textual error message that the LLM itself
// reads and can explain to the user in the final answer.
func (s *Server) executeChatTool(ctx context.Context, tc chat.ToolCall, defaultDomain string) (apitypes.ChatToolCallResult, string) {
	if tc.Function.Name != addMemoryToolName {
		msg := fmt.Sprintf("unknown tool: %s", tc.Function.Name)
		return apitypes.ChatToolCallResult{Name: tc.Function.Name, Arguments: tc.Function.Arguments, ResultSummary: msg}, msg
	}

	var args struct {
		Content string   `json:"content"`
		Domain  string   `json:"domain"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		msg := fmt.Sprintf("invalid arguments: %v", err)
		return apitypes.ChatToolCallResult{Name: tc.Function.Name, Arguments: tc.Function.Arguments, ResultSummary: msg}, msg
	}
	if args.Domain == "" {
		args.Domain = defaultDomain
	}
	if strings.TrimSpace(args.Content) == "" || strings.TrimSpace(args.Domain) == "" {
		msg := "content and domain are required to save a memory"
		return apitypes.ChatToolCallResult{Name: tc.Function.Name, Arguments: tc.Function.Arguments, ResultSummary: msg}, msg
	}

	out, err := s.store.Add(ctx, store.AddInput{
		Content: args.Content,
		Domain:  args.Domain,
		Source:  "webui-chat",
		Tags:    args.Tags,
	}, time.Now())
	if err != nil {
		msg := fmt.Sprintf("failed to save memory: %v", err)
		return apitypes.ChatToolCallResult{Name: tc.Function.Name, Arguments: tc.Function.Arguments, ResultSummary: msg}, msg
	}

	ids := make([]string, 0, len(out.Entries))
	for _, e := range out.Entries {
		ids = append(ids, e.Entry.ID)
	}
	msg := fmt.Sprintf("memory saved successfully (id: %s)", strings.Join(ids, ", "))
	return apitypes.ChatToolCallResult{Name: tc.Function.Name, Arguments: tc.Function.Arguments, ResultSummary: msg}, msg
}

func entryIDs(entries []schema.Entry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}
