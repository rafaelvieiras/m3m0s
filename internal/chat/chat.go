// Package chat implements the chat completions client with tool-calling
// (function calling) used by the WebUI's floating memory assistant (D23 in
// docs/decisions/). It uses the same OpenAI-compatible OpenRouter API
// already used in internal/extract, with two deliberate differences: the
// response is natural-language text (never forced JSON, unlike Extract/D19)
// and may include tool_calls, which the caller (internal/httpapi) executes
// and feeds back in a following call. This package only talks to the
// provider; it does not decide what to do with a tool_call.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultBaseURL/DefaultModel mirror the defaults of internal/extract,
// duplicated on purpose (it does not import internal/extract) to keep the two
// packages decoupled, the same pattern as internal/watch/match.go, written by
// hand instead of reusing a dependency on another internal package.
const (
	DefaultBaseURL = "https://openrouter.ai/api/v1"
	DefaultModel   = "deepseek/deepseek-v4-flash"
)

// Message is the OpenAI-compatible message format (roles: system, user,
// assistant, tool), used both for what we send and for what the provider
// returns. ToolCalls is only filled in an assistant response that decided to
// call a tool; ToolCallID is only used in a role=tool message returning the
// execution result to the model.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a function call the model requested. Arguments comes as a raw
// JSON string (OpenAI-compatible contract), not decoded here: whoever
// executes the tool decides the expected shape of the arguments.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef declares a tool available for the model to call. Parameters is a
// raw JSON Schema (json.RawMessage) so no separate schema package is needed
// just for this; whoever builds the ToolDef already writes the schema as a
// JSON literal.
type ToolDef struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Client calls an LLM via OpenAI-compatible chat completions, with
// tool-calling support, for the WebUI chat (D23).
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client

	// promptTokens/completionTokens follow the same pattern as
	// internal/extract.Extractor: a process-level accumulator, exposed via
	// TokensUsed, not persisted.
	promptTokens     int64
	completionTokens int64
}

// New creates a Client. Empty baseURL/model fall back to the defaults above,
// the same firstNonEmpty pattern already used in cmd/m3m0s/serve.go.
func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		// 30s: more generous than the 20s timeout of internal/extract; a
		// natural-language chat response (potentially with 1 tool call in
		// the middle) tends to generate more output tokens than an Extract.
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Model returns the configured model; same purpose as
// internal/extract.Extractor.Model, exposed for future telemetry.
func (c *Client) Model() string {
	return c.model
}

// TokensUsed returns the accumulated total of prompt_tokens/completion_tokens
// reported by the provider since this Client was created.
func (c *Client) TokensUsed() (prompt, completion int64) {
	return atomic.LoadInt64(&c.promptTokens), atomic.LoadInt64(&c.completionTokens)
}

func (c *Client) recordUsage(u chatUsage) {
	if u.PromptTokens > 0 {
		atomic.AddInt64(&c.promptTokens, int64(u.PromptTokens))
	}
	if u.CompletionTokens > 0 {
		atomic.AddInt64(&c.completionTokens, int64(u.CompletionTokens))
	}
}

type chatRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	Tools      []ToolDef        `json:"tools,omitempty"`
	ToolChoice string           `json:"tool_choice,omitempty"`
	Reasoning  *reasoningConfig `json:"reasoning,omitempty"`
}

// reasoningConfig={"enabled":false}: same rationale as D20 in
// internal/extract. It turns off chain-of-thought reasoning so latency does
// not blow up with a "reasoning" model despite its name (e.g. DefaultModel).
type reasoningConfig struct {
	Enabled bool `json:"enabled"`
}

type chatChoice struct {
	Message Message `json:"message"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

// Complete sends messages (already including the system prompt, built by the
// caller) and tools (may be empty, meaning no function-calling) to the
// configured model, and returns the assistant reply message (Content and/or
// ToolCalls). It does not run the tool-calling loop here; that is the
// caller's responsibility (internal/httpapi.handleChat), which decides how to
// execute each ToolCall and builds the next call with the result.
//
// Every error (network, HTTP >=400, unparseable JSON, response without
// choices) is returned as an error.
func (c *Client) Complete(ctx context.Context, messages []Message, tools []ToolDef) (Message, error) {
	req := chatRequest{
		Model:     c.model,
		Messages:  messages,
		Reasoning: &reasoningConfig{Enabled: false},
	}
	if len(tools) > 0 {
		req.Tools = tools
		req.ToolChoice = "auto"
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return Message{}, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return Message{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Message{}, fmt.Errorf("chat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return Message{}, fmt.Errorf("chat responded %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return Message{}, fmt.Errorf("unmarshal chat response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return Message{}, errors.New("chat response has no choices")
	}
	c.recordUsage(chatResp.Usage)

	return chatResp.Choices[0].Message, nil
}
