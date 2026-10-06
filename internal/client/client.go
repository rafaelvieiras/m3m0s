// Package client is the HTTP client shared by every "client" mode of
// m3m0s: the CLI, the `m3m0s mcp` bridge and `m3m0s watch` — following the
// topology described in docs/api-contract.md, they all talk to the central
// `m3m0s serve` over HTTP, never to chromem-go directly.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// DefaultURL is the same port as the previous memory-server — see
// docs/api-contract.md, Topology section.
const DefaultURL = "http://localhost:19234"

// apiPrefix precedes every API route except /health — docs/api-contract.md,
// "/api/v1 prefix and embedded WebUI" section. M3M0S_URL/baseURL point only
// to host:port; this client builds the rest of the path.
const apiPrefix = "/api/v1"

// Client talks to an m3m0s serve.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates an explicit Client.
func New(baseURL, apiKey string) *Client {
	return NewWithTimeout(baseURL, apiKey, 30*time.Second)
}

// NewWithTimeout creates a Client with a custom HTTP timeout — used by
// `m3m0s watch` (D24, docs/decisions/), which does synchronous Add with fact
// extraction on (D12) in batches during the initial scan and may legitimately
// take longer than the 30s designed for interactive CLI/MCP use (D17).
func NewWithTimeout(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
	}
}

// NewFromEnv reads M3M0S_URL (default DefaultURL) and M3M0S_API_KEY, as
// specified in docs/api-contract.md.
func NewFromEnv() *Client {
	return NewFromEnvWithTimeout(30 * time.Second)
}

// NewFromEnvWithTimeout is NewFromEnv with a custom HTTP timeout.
func NewFromEnvWithTimeout(timeout time.Duration) *Client {
	baseURL := os.Getenv("M3M0S_URL")
	if baseURL == "" {
		baseURL = DefaultURL
	}
	return NewWithTimeout(baseURL, os.Getenv("M3M0S_API_KEY"), timeout)
}

// buildRequest builds the *http.Request shared by do() and doRaw() —
// extracted in D26 (docs/decisions/) only so Add does not duplicate the
// URL/headers/body assembly logic that already existed in do().
func (c *Client) buildRequest(ctx context.Context, method, path string, query url.Values, body any) (*http.Request, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	req, err := c.buildRequest(ctx, method, path, query, body)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("m3m0s serve unreachable at %s: %w", c.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		var errBody apitypes.ErrorBody
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.ErrorInfo.Code != "" {
			return errBody
		}
		return fmt.Errorf("m3m0s serve responded %d", resp.StatusCode)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// doRaw is the variant used only by Add (D26, docs/decisions/): it returns
// the raw HTTP status code and the already-read body, without decoding into
// any fixed shape — Add needs the status to decide WHETHER to decode as
// AddResponse (200) or AddPendingResponse (202), a distinction do()
// deliberately does not expose (no other method of this client needs it).
func (c *Client) doRaw(ctx context.Context, method, path string, query url.Values, body any) (status int, respBody []byte, err error) {
	req, err := c.buildRequest(ctx, method, path, query, body)
	if err != nil {
		return 0, nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("m3m0s serve unreachable at %s: %w", c.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errBody apitypes.ErrorBody
		if json.Unmarshal(data, &errBody) == nil && errBody.ErrorInfo.Code != "" {
			return resp.StatusCode, nil, errBody
		}
		return resp.StatusCode, nil, fmt.Errorf("m3m0s serve responded %d", resp.StatusCode)
	}
	return resp.StatusCode, data, nil
}

// addPollInterval is the interval between polls of GET /memories/pending/:id
// when Add degrades to the server's asynchronous path (202, D26) — short
// enough not to noticeably delay whoever is waiting for the result, without
// hammering the server with requests.
const addPollInterval = 200 * time.Millisecond

// Add calls POST /api/v1/memories. It polls transparently when the server
// degrades to the asynchronous path (D26, docs/decisions/) — the public
// signature does not change: the CLI, `m3m0s watch`, `m3m0s mcp` and
// internal/selfindex keep calling exactly as before, without knowing whether
// the result came directly (200) or after N poll calls (202 →
// GET /memories/pending/:id in a loop).
func (c *Client) Add(ctx context.Context, req apitypes.AddRequest) (apitypes.AddResponse, error) {
	status, body, err := c.doRaw(ctx, http.MethodPost, apiPrefix+"/memories", nil, req)
	if err != nil {
		return apitypes.AddResponse{}, err
	}
	if status != http.StatusAccepted {
		var out apitypes.AddResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return apitypes.AddResponse{}, fmt.Errorf("decode response: %w", err)
		}
		return out, nil
	}

	var pending apitypes.AddPendingResponse
	if err := json.Unmarshal(body, &pending); err != nil {
		return apitypes.AddResponse{}, fmt.Errorf("decode pending response: %w", err)
	}
	return c.pollAdd(ctx, pending.OperationID)
}

// pollAdd queries GET /memories/pending/:id in a loop until the server
// reports status != "pending" (D26). It respects ctx — cancellation/timeout
// of the caller interrupts the loop immediately — but imposes no total-time
// ceiling of its own beyond that: each individual HTTP call (each poll GET)
// already expires by the c.http.Client timeout (30s CLI/MCP, 120s
// `m3m0s watch`, see NewWithTimeout/NewFromEnvWithTimeout), so an Add that
// never finishes on the server makes the client fail with a transport
// timeout on the next call, instead of hanging in an infinite loop.
func (c *Client) pollAdd(ctx context.Context, operationID string) (apitypes.AddResponse, error) {
	path := apiPrefix + "/memories/pending/" + url.PathEscape(operationID)
	for {
		var pending apitypes.PendingAddResponse
		if err := c.do(ctx, http.MethodGet, path, nil, nil, &pending); err != nil {
			return apitypes.AddResponse{}, err
		}
		switch pending.Status {
		case "completed":
			if pending.Result == nil {
				return apitypes.AddResponse{}, fmt.Errorf("m3m0s serve: operation %s completed without a result", operationID)
			}
			return *pending.Result, nil
		case "failed":
			return apitypes.AddResponse{}, fmt.Errorf("m3m0s serve: async add failed: %s", pending.Error)
		}

		select {
		case <-ctx.Done():
			return apitypes.AddResponse{}, ctx.Err()
		case <-time.After(addPollInterval):
		}
	}
}

// Search calls POST /api/v1/search.
func (c *Client) Search(ctx context.Context, req apitypes.SearchRequest) (apitypes.SearchResponse, error) {
	var out apitypes.SearchResponse
	err := c.do(ctx, http.MethodPost, apiPrefix+"/search", nil, req, &out)
	return out, err
}

// Recent calls GET /api/v1/recent.
func (c *Client) Recent(ctx context.Context, p apitypes.RecentParams) (apitypes.RecentResponse, error) {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Domain != "" {
		q.Set("domain", p.Domain)
	}
	if p.DomainCategory != "" {
		q.Set("domainCategory", p.DomainCategory)
	}
	if p.Source != "" {
		q.Set("source", p.Source)
	}
	if p.Agent != "" {
		q.Set("agent", p.Agent)
	}
	if p.Model != "" {
		q.Set("model", p.Model)
	}
	if p.Project != "" {
		q.Set("project", p.Project)
	}
	if p.Scope != "" {
		q.Set("scope", p.Scope)
	}

	var out apitypes.RecentResponse
	err := c.do(ctx, http.MethodGet, apiPrefix+"/recent", q, nil, &out)
	return out, err
}

// Stats calls GET /api/v1/stats.
func (c *Client) Stats(ctx context.Context) (apitypes.StatsResponse, error) {
	var out apitypes.StatsResponse
	err := c.do(ctx, http.MethodGet, apiPrefix+"/stats", nil, nil, &out)
	return out, err
}

// Update calls PATCH /api/v1/memories/:id (D13/docs/api-contract.md,
// memory_update section) and returns the complete updated MemoryEntry.
func (c *Client) Update(ctx context.Context, id string, req apitypes.UpdateRequest) (apitypes.UpdateResponse, error) {
	var out apitypes.UpdateResponse
	err := c.do(ctx, http.MethodPatch, apiPrefix+"/memories/"+url.PathEscape(id), nil, req, &out)
	return out, err
}

// Get calls GET /api/v1/memories/:id (a later elaboration after Phase 5/D13,
// docs/api-contract.md, memory_get section) and returns the complete
// MemoryEntry.
func (c *Client) Get(ctx context.Context, id string) (apitypes.GetResponse, error) {
	var out apitypes.GetResponse
	err := c.do(ctx, http.MethodGet, apiPrefix+"/memories/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

// ForgetSweep calls POST /api/v1/memories/forget-sweep (D22, docs/decisions/
// — closes A9): automatic decay/forgetting of cold extracted facts.
func (c *Client) ForgetSweep(ctx context.Context, req apitypes.ForgetSweepRequest) (apitypes.ForgetSweepResponse, error) {
	var out apitypes.ForgetSweepResponse
	err := c.do(ctx, http.MethodPost, apiPrefix+"/memories/forget-sweep", nil, req, &out)
	return out, err
}

// Restore calls POST /api/v1/memories/:id/restore (D22) — reverts a
// soft-delete within the grace window.
func (c *Client) Restore(ctx context.Context, id string) (apitypes.RestoreResponse, error) {
	var out apitypes.RestoreResponse
	err := c.do(ctx, http.MethodPost, apiPrefix+"/memories/"+url.PathEscape(id)+"/restore", nil, nil, &out)
	return out, err
}

// Health calls GET /health.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil, nil)
}

// DeleteByID calls DELETE /api/v1/memories/:id (D11, minimal scope for the
// cascading purge of m3m0s watch). It returns found=false (no error) if the
// ID does not exist — a 404 from serve is not treated as a transport failure
// here, because "already gone" is a valid result for whoever calls delete.
func (c *Client) DeleteByID(ctx context.Context, id string) (found bool, err error) {
	err = c.do(ctx, http.MethodDelete, apiPrefix+"/memories/"+url.PathEscape(id), nil, nil, nil)
	if err != nil {
		var errBody apitypes.ErrorBody
		if errors.As(err, &errBody) && errBody.ErrorInfo.Code == "not_found" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// DeleteByFilePath calls DELETE /api/v1/memories?domain=X&filePath=Y.
func (c *Client) DeleteByFilePath(ctx context.Context, domain, filePath string) (int, error) {
	q := url.Values{"domain": {domain}, "filePath": {filePath}}
	var out apitypes.DeleteByFilePathResponse
	err := c.do(ctx, http.MethodDelete, apiPrefix+"/memories", q, nil, &out)
	return out.DeletedCount, err
}
