package mcpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// maxHTTPBody caps one POST /mcp body.
const maxHTTPBody = 4 << 20

// HTTPOptions configures the native Streamable HTTP endpoint (D54).
type HTTPOptions struct {
	// ReadOnly hides and rejects the write tools (D45) for every connection.
	// A client can also ask for it on its own URL with ?read_only=1; it can
	// only narrow the server setting, never widen it.
	ReadOnly bool
	// AllowedOrigins lists extra browser Origins accepted besides the
	// request's own origin (DNS-rebinding protection). "*" accepts any.
	AllowedOrigins []string
}

// NewHTTPHandler returns the MCP Streamable HTTP endpoint (spec 2025-03-26 and
// later): JSON-RPC over POST, answered with application/json, no SSE stream
// (GET answers 405) and no sessions (stateless; Mcp-Session-Id is neither
// issued nor required). It reuses Handler, so the tools, their dispatch and
// the read-only rules are the ones of the stdio bridge.
//
// api is the server's own API handler: tools run against it in-process, with
// the caller's Bearer token, so they go through the exact auth and validation
// of /api/v1. The caller must authenticate the request before this handler
// (the endpoint itself checks nothing but Origin).
func NewHTTPHandler(api http.Handler, opts HTTPOptions) http.Handler {
	return &httpHandler{api: api, opts: opts}
}

type httpHandler struct {
	api  http.Handler
	opts HTTPOptions
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.originAllowed(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		// No server-initiated SSE stream and no sessions to delete.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed: this endpoint only accepts POST", http.StatusMethodNotAllowed)
		return
	}
	if v := r.Header.Get("Mcp-Protocol-Version"); v != "" && !supportedVersion(v) {
		http.Error(w, "unsupported Mcp-Protocol-Version: "+v, http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPBody))
	if err != nil {
		http.Error(w, "cannot read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	body = bytes.TrimSpace(body)

	readOnly := h.opts.ReadOnly
	if q := r.URL.Query().Get("read_only"); q == "1" || strings.EqualFold(q, "true") {
		readOnly = true
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	c := client.NewWithHTTPClient("http://m3m0s.invalid", token, &http.Client{
		Transport: handlerTransport{h.api},
		Timeout:   30 * time.Second,
	})
	mh := NewHandler(c, readOnly, "")

	if len(body) > 0 && body[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
			writeRPC(w, http.StatusBadRequest, invalidRequest(rpcRequest{}))
			return
		}
		var out []*rpcResponse
		for _, m := range batch {
			if resp := mh.HandleMessage(r.Context(), m); resp != nil {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	resp := mh.HandleMessage(r.Context(), body)
	switch {
	case resp == nil:
		w.WriteHeader(http.StatusAccepted)
	case resp.Error != nil && (resp.Error.Code == errParseError || resp.Error.Code == errInvalidRequest):
		writeRPC(w, http.StatusBadRequest, resp)
	default:
		writeRPC(w, http.StatusOK, resp)
	}
}

func writeRPC(w http.ResponseWriter, status int, resp *rpcResponse) { writeJSON(w, status, resp) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func supportedVersion(v string) bool {
	for _, s := range SupportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

// originAllowed implements the spec's DNS-rebinding guard: a request without
// an Origin header (every non-browser MCP client) passes; one with an Origin
// must match the Host it was sent to, or be listed in AllowedOrigins.
func (h *httpHandler) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, a := range h.opts.AllowedOrigins {
		if a == "*" || strings.EqualFold(strings.TrimRight(a, "/"), origin) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// handlerTransport is an http.RoundTripper that serves requests with an
// http.Handler in the same process, no socket involved.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}
