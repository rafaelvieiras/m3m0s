// Context injection preview (D29, docs/api-contract.md): GET
// /api/v1/preview/injection. For a real domain already existing in the
// database, it shows exactly the text each injection mechanism would produce
// today (apitypes.PreviewInjectionMode). It is a step prior to the decision
// to implement new hooks for other tools (pi, OpenCode), not a new injection
// feature in itself.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/hook"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// mcpContentBlock/mcpToolResult mirror 1:1 the unexported types of
// internal/mcpserver (contentBlock/toolCallResult), duplicated here instead
// of imported because mcpserver is client-side code (it talks to `serve` via
// internal/client), while the preview runs inside `serve` itself with direct
// access to the store; importing mcpserver from httpapi would invert that
// dependency needlessly. The shape is the public contract of the MCP
// protocol (tool_result content blocks), not an internal detail of the
// package.
type mcpContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContentBlock `json:"content"`
}

// hookSessionStartOutput mirrors the unexported sessionStartOutput shape of
// internal/hook/sessionstart.go for the same reason as above; it is the
// documented Claude Code contract for a SessionStart hook, not an internal
// detail of the hook package.
type hookSessionStartOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// previewManualLimit is how many memories go into the "manual" mode; the same
// number as SessionStartRecentLimit (D17) for reading consistency across the
// 3 modes, but without the InjectableEntries filter: manual mode has no
// coupled extraction pipeline, it is just "paste this into a prompt".
const previewManualLimit = hook.SessionStartRecentLimit

// previewMCPRecentLimit mirrors the documented default of the MCP tool
// memory_recent (internal/mcpserver, toolDefs): 10, when the client does not
// pass an explicit "limit".
const previewMCPRecentLimit = 10

func (s *Server) handlePreviewInjection(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	domain := q.Get("domain")
	mode := apitypes.PreviewInjectionMode(q.Get("mode"))
	if mode == "" {
		mode = apitypes.PreviewInjectionModeHook
	}

	filter := store.Filter{Domain: domain}
	now := time.Now()

	switch mode {
	case apitypes.PreviewInjectionModeHook:
		results := s.store.Recent(hook.SessionStartFetchLimit, filter, now)
		extracted := hook.InjectableEntries(results)

		var out hookSessionStartOutput
		out.HookSpecificOutput.HookEventName = "SessionStart"
		out.HookSpecificOutput.AdditionalContext = apitypes.BuildContextBlock(extracted)
		raw, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "preview_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, apitypes.PreviewInjectionResponse{
			Mode:   mode,
			Domain: domain,
			Mechanism: "Claude Code SessionStart hook (D14/D17): runs automatically " +
				"before every new session or /clear, synchronous, 5s timeout. It is the only " +
				"automatic injection mechanism that exists today; no other tool " +
				"has an equivalent hook yet.",
			EntriesFetched: len(results),
			EntriesUsed:    len(extracted),
			Raw:            string(raw),
		})

	case apitypes.PreviewInjectionModeMCP:
		results := s.store.Recent(previewMCPRecentLimit, filter, now)
		entriesJSON, err := json.Marshal(results)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "preview_failed", err.Error())
			return
		}
		toolResult := mcpToolResult{Content: []mcpContentBlock{{Type: "text", Text: string(entriesJSON)}}}
		raw, err := json.MarshalIndent(toolResult, "", "  ")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "preview_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, apitypes.PreviewInjectionResponse{
			Mode:   mode,
			Domain: domain,
			Mechanism: "Exact result of the MCP tool memory_recent (internal/mcpserver). " +
				"No hook at all: a generic MCP client (pi, OpenCode) only sees this if the " +
				"model itself decides to call the tool during the conversation; nothing is injected " +
				"automatically at the start of the session.",
			EntriesFetched: len(results),
			EntriesUsed:    len(results),
			Raw:            string(raw),
		})

	case apitypes.PreviewInjectionModeManual:
		results := s.store.Recent(previewManualLimit, filter, now)
		block := apitypes.BuildContextBlock(results)

		writeJSON(w, http.StatusOK, apitypes.PreviewInjectionResponse{
			Mode:   mode,
			Domain: domain,
			Mechanism: "Raw text (apitypes.BuildContextBlock), with no hook or MCP envelope; " +
				"what one would paste by hand into the system prompt of a tool with no hook " +
				"or MCP integration at all (scripts, cron, a new agent not plugged in yet).",
			EntriesFetched: len(results),
			EntriesUsed:    len(results),
			Raw:            block,
		})

	default:
		writeError(w, http.StatusBadRequest, "invalid_mode", "invalid mode: "+string(mode)+" (expected hook, mcp or manual)")
	}
}
