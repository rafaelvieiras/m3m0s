// Package mcpserver implements the `m3m0s mcp` bridge described in
// docs/api-contract.md: a stateless process per host session (pi,
// Claude Code, OpenCode), which speaks MCP over stdio on one side and HTTP
// against `m3m0s serve` on the other (via internal/client). It keeps no
// state of its own — serve does.
//
// Protocol: MCP version 2024-11-05, stdio transport — one JSON-RPC 2.0
// message per line, delimited by \n, on both stdin and stdout. This is not
// the LSP Content-Length framing; do not confuse the two.
// Implemented by hand (no external MCP SDK) because the surface needed here
// is small: initialize, notifications/initialized, tools/list and
// tools/call over the docs/api-contract.md operations that are first-class
// MCP tools (add/search/recent/stats/update/get — memory_delete stays
// HTTP-only by design, D11).
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/project"
)

const protocolVersion = "2024-11-05"

const (
	errParseError     = -32700
	errMethodNotFound = -32601
	errInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// isNotification: a JSON-RPC message without "id" is a notification — it
// never receives a response, not even an error one (JSON-RPC 2.0 spec).
func (r rpcRequest) isNotification() bool {
	return r.ID == nil
}

// toolCallParams is the standard shape of "params" in tools/call.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// Run executes the read/dispatch loop until EOF on `in`, writing each
// response as a JSON line to `out`. I/O errors interrupt the loop;
// protocol/tool errors become normal JSON-RPC responses.
func Run(ctx context.Context, in io.Reader, out io.Writer) error {
	c := client.NewFromEnv()
	// The MCP server runs in the session's working directory, so it can
	// resolve the session project once (D34).
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	sessionProject = project.Resolve(os.Getenv, home, cwd)

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := handleLine(ctx, c, line, out); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// sessionProject is the project of the session this MCP server runs in ("" =
// none). memory_search/memory_recent default to it: global memory plus the
// project's, or only global memory when there is none, so a session never
// recalls another project's memories unless it asks explicitly.
var sessionProject string

// defaultScope fills project/scope for a recall that specified neither.
func defaultScope(project, scope string) (string, string) {
	if project != "" || scope != "" {
		return project, scope
	}
	if sessionProject != "" {
		return sessionProject, ""
	}
	return "", "global"
}

func handleLine(ctx context.Context, c *client.Client, line []byte, out io.Writer) error {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return writeResponse(out, rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: errParseError, Message: "parse error: " + err.Error()},
		})
	}

	switch req.Method {
	case "initialize":
		return respondResult(out, req, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "m3m0s", "version": "0.1.0"},
		})
	case "notifications/initialized":
		// Notification: never responds.
		return nil
	case "tools/list":
		return respondResult(out, req, map[string]any{"tools": toolDefs})
	case "tools/call":
		return handleToolCall(ctx, c, req, out)
	default:
		if req.isNotification() {
			return nil
		}
		return respondError(out, req, errMethodNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func handleToolCall(ctx context.Context, c *client.Client, req rpcRequest, out io.Writer) error {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		if req.isNotification() {
			return nil
		}
		return respondError(out, req, errInvalidParams, "invalid params: "+err.Error())
	}

	result := dispatchTool(ctx, c, params.Name, params.Arguments)
	return respondResult(out, req, result)
}

func dispatchTool(ctx context.Context, c *client.Client, name string, args json.RawMessage) toolCallResult {
	switch name {
	case "memory_add":
		var in apitypes.AddRequest
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		resp, err := c.Add(ctx, in)
		if err != nil {
			return errorResult(err)
		}
		text := fmt.Sprintf("%d memory(ies) stored (extraction: %v)\n%s", len(resp.Entries), resp.Extracted, toJSON(resp))
		return textResult(text)

	case "memory_search":
		var in apitypes.SearchRequest
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		in.Project, in.Scope = defaultScope(in.Project, in.Scope)
		resp, err := c.Search(ctx, in)
		if err != nil {
			return errorResult(err)
		}
		return textResult(resp.ContextBlock)

	case "memory_recent":
		var in apitypes.RecentParams
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		in.Project, in.Scope = defaultScope(in.Project, in.Scope)
		resp, err := c.Recent(ctx, in)
		if err != nil {
			return errorResult(err)
		}
		return textResult(toJSON(resp.Results))

	case "memory_stats":
		resp, err := c.Stats(ctx)
		if err != nil {
			return errorResult(err)
		}
		return textResult(toJSON(resp))

	case "memory_update":
		var in updateToolArgs
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		if strings.TrimSpace(in.ID) == "" {
			return errorResult(fmt.Errorf("id is required"))
		}
		resp, err := c.Update(ctx, in.ID, in.UpdateRequest)
		if err != nil {
			return errorResult(err)
		}
		return textResult(toJSON(resp))

	case "memory_get":
		var in getToolArgs
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		if strings.TrimSpace(in.ID) == "" {
			return errorResult(fmt.Errorf("id is required"))
		}
		resp, err := c.Get(ctx, in.ID)
		if err != nil {
			return errorResult(err)
		}
		return textResult(toJSON(resp))

	case "memory_forget_sweep":
		var in apitypes.ForgetSweepRequest
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		resp, err := c.ForgetSweep(ctx, in)
		if err != nil {
			return errorResult(err)
		}
		return textResult(toJSON(resp))

	case "memory_restore":
		var in getToolArgs
		if err := unmarshalArgs(args, &in); err != nil {
			return errorResult(err)
		}
		if strings.TrimSpace(in.ID) == "" {
			return errorResult(fmt.Errorf("id is required"))
		}
		resp, err := c.Restore(ctx, in.ID)
		if err != nil {
			return errorResult(err)
		}
		return textResult(toJSON(resp))

	default:
		return errorResult(fmt.Errorf("unknown tool: %s", name))
	}
}

// updateToolArgs is the input of the memory_update tool: the same JSON body
// as PATCH /memories/:id (apitypes.UpdateRequest, embedded to inherit its
// json.RawMessage fields at the same level) plus "id" — input schema =
// PATCH body + required id, as specified in docs/api-contract.md.
type updateToolArgs struct {
	ID string `json:"id"`
	apitypes.UpdateRequest
}

// getToolArgs is the input of the memory_get tool: just "id" (a later
// elaboration after Phase 5/D13, docs/api-contract.md, memory_get section).
type getToolArgs struct {
	ID string `json:"id"`
}

func unmarshalArgs(args json.RawMessage, out any) error {
	if len(args) == 0 {
		return nil
	}
	return json.Unmarshal(args, out)
}

func textResult(text string) toolCallResult {
	return toolCallResult{Content: []contentBlock{{Type: "text", Text: text}}}
}

// errorResult is never a JSON-RPC transport error — it is always a tool
// result with isError:true, so as not to break the MCP host's tool loop
// (Claude Code/OpenCode/pi) on a business failure (e.g. 401 from serve,
// invalid domain).
func errorResult(err error) toolCallResult {
	return toolCallResult{
		Content: []contentBlock{{Type: "text", Text: "error: " + err.Error()}},
		IsError: true,
	}
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	return string(b)
}

func respondResult(out io.Writer, req rpcRequest, result any) error {
	if req.isNotification() {
		return nil
	}
	return writeResponse(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func respondError(out io.Writer, req rpcRequest, code int, message string) error {
	if req.isNotification() {
		return nil
	}
	return writeResponse(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: message}})
}

func writeResponse(out io.Writer, resp rpcResponse) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

// toolDefs mirrors 1:1 the docs/api-contract.md operations that are
// first-class MCP tools (except memory_delete, which stays HTTP-only by
// design, D11 — minimal scope for the watch cascading purge, not a general
// feature).
//
// memory_get: first-class on all 3 surfaces like the rest (a later
// elaboration after Phase 5/D13) — a pure read of 1 memory by id, the same
// pattern as memory_update without the write part.
var toolDefs = []map[string]any{
	{
		"name":        "memory_add",
		"description": "Stores a semantic memory in M3m0s. Pipeline: secret redaction, similarity dedup within the domain, insert or update.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content":  map[string]any{"type": "string", "description": "Memory text."},
				"domain":   map[string]any{"type": "string", "description": "Format <category>:<instance>, e.g. codigo:m3m0s."},
				"source":   map[string]any{"type": "string", "description": "Originating tool, e.g. claude-code, opencode, manual."},
				"agent":    map[string]any{"type": "string", "description": "Optional named agent identity, e.g. my-agent."},
				"model":    map[string]any{"type": "string", "description": "Optional LLM that produced the content, e.g. claude-sonnet-5-5."},
				"project":  map[string]any{"type": "string", "description": "Optional project slug (lowercase letters, digits, . _ -); omit for global memory."},
				"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"metadata": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
			"required": []string{"content", "domain"},
		},
	},
	{
		"name":        "memory_search",
		"description": "Hybrid (semantic + keyword) search over M3m0s memories. The response text comes ready for prompt injection (contextBlock); maxTokens caps its size.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":          map[string]any{"type": "string"},
				"limit":          map[string]any{"type": "integer", "default": 5},
				"domain":         map[string]any{"type": "string"},
				"domainCategory": map[string]any{"type": "string"},
				"source":         map[string]any{"type": "string"},
				"agent":          map[string]any{"type": "string"},
				"model":          map[string]any{"type": "string"},
				"project":        map[string]any{"type": "string", "description": "recall global memory plus this project's; defaults to the session project when one is configured"},
				"scope":          map[string]any{"type": "string", "enum": []string{"global", "project-only"}, "description": "global = only memories without a project; project-only = only the given project"},
				"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"kind":           map[string]any{"type": "string", "enum": []string{"decision", "rule", "preference", "gotcha", "procedure", "state", "noise", "other"}, "description": "only memories of this nature; other also matches unclassified ones"},
				"mode":           map[string]any{"type": "string", "enum": []string{"hybrid", "vector", "keyword"}, "default": "hybrid"},
				"maxTokens":      map[string]any{"type": "integer", "description": "cap on the estimated tokens of the response (0 = no cap)"},
			},
			"required": []string{"query"},
		},
	},
	{
		"name":        "memory_recent",
		"description": "Lists the most recent memories, without similarity search — only exact filters and ordering by date.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":          map[string]any{"type": "integer", "default": 10},
				"domain":         map[string]any{"type": "string"},
				"domainCategory": map[string]any{"type": "string"},
				"source":         map[string]any{"type": "string"},
				"agent":          map[string]any{"type": "string"},
				"model":          map[string]any{"type": "string"},
				"project":        map[string]any{"type": "string"},
				"scope":          map[string]any{"type": "string", "enum": []string{"global", "project-only"}},
				"kind":           map[string]any{"type": "string", "enum": []string{"decision", "rule", "preference", "gotcha", "procedure", "state", "noise", "other"}, "description": "only memories of this nature; other also matches unclassified ones"},
			},
		},
	},
	{
		"name":        "memory_stats",
		"description": "Global counts and counts by domain_category of the memories stored in M3m0s.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		"name":        "memory_update",
		"description": "Partially edits an existing memory by id. Only the fields sent change; the rest of the entry stays the same. No fact extraction or dedup — a direct, deliberate overwrite.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "ID of the memory to edit, required."},
				"content":  map[string]any{"type": "string", "description": "New content, replaces the current one. Re-embeds."},
				"domain":   map[string]any{"type": "string", "description": "New domain, same format as memory_add. Recomputes domainCategory."},
				"source":   map[string]any{"type": "string", "description": "New source."},
				"agent":    map[string]any{"type": []string{"string", "null"}, "description": "New agent; null removes the agent."},
				"model":    map[string]any{"type": []string{"string", "null"}, "description": "New model; null clears it."},
				"project":  map[string]any{"type": []string{"string", "null"}, "description": "New project; null makes the memory global."},
				"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Replaces the entire set of tags; [] clears."},
				"metadata": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Replaces the entire metadata; {} clears."},
				"kind":     map[string]any{"type": "string", "enum": []string{"decision", "rule", "preference", "gotcha", "procedure", "state", "noise", "other"}, "description": "Corrects the nature of the fact; recorded as a manual tag that no backfill overwrites."},
			},
			"required": []string{"id"},
		},
	},
	{
		"name":        "memory_get",
		"description": "Fetches 1 memory by id. Records an access reinforcement (D22); does not change updatedAt. Returns 404/error if the id does not exist or is soft-deleted.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "ID of the memory to fetch, required."},
			},
			"required": []string{"id"},
		},
	},
	{
		"name":        "memory_forget_sweep",
		"description": "Memory layers / automatic forgetting (D22): evaluates cold extracted facts (low retention score) and, if dryRun=false, soft-deletes the candidates and hard-deletes soft-deletes already past the grace window. dryRun=true only reports candidates, without changing anything.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dryRun": map[string]any{"type": "boolean", "default": false, "description": "true = only reports candidates, changes nothing."},
			},
		},
	},
	{
		"name":        "memory_restore",
		"description": "Reverts the soft-delete of a memory by id (D22), as long as it has not yet been hard-deleted by a forget-sweep. Returns 404/error if the id does not exist or is not soft-deleted.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "ID of the memory to restore, required."},
			},
			"required": []string{"id"},
		},
	},
}
