//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// mcpProc is a real `m3m0s mcp` running as a subprocess, spoken to over
// stdin/stdout with line-delimited JSON-RPC 2.0 (MCP spec 2024-11-05 —
// see internal/mcpserver/mcp.go).
type mcpProc struct {
	cmd     *exec.Cmd
	stdin   *bufio.Writer
	scanner *bufio.Scanner
	stderr  *bytes.Buffer
}

func startMCP(t *testing.T, env []string) *mcpProc {
	t.Helper()

	cmd := exec.Command(binPath, "mcp")
	cmd.Env = env

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start m3m0s mcp: %v", err)
	}

	p := &mcpProc{
		cmd:     cmd,
		stdin:   bufio.NewWriter(stdinPipe),
		scanner: bufio.NewScanner(stdoutPipe),
		stderr:  &stderrBuf,
	}
	p.scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	t.Cleanup(func() {
		_ = stdinPipe.Close()
		_ = cmd.Wait()
	})

	return p
}

func (p *mcpProc) send(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal mcp message: %v", err)
	}
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		t.Fatalf("write to the mcp stdin: %v", err)
	}
	if err := p.stdin.Flush(); err != nil {
		t.Fatalf("flush the mcp stdin: %v", err)
	}
}

func (p *mcpProc) readLine(t *testing.T) map[string]any {
	t.Helper()

	type result struct {
		line string
		ok   bool
	}
	done := make(chan result, 1)
	go func() {
		ok := p.scanner.Scan()
		done <- result{line: p.scanner.Text(), ok: ok}
	}()

	select {
	case r := <-done:
		if !r.ok {
			t.Fatalf("unexpected EOF/error reading from mcp (err=%v) — stderr: %s", p.scanner.Err(), p.stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(r.line), &m); err != nil {
			t.Fatalf("mcp line is not valid JSON (%v): %q", err, r.line)
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for the mcp response — stderr so far: %s", p.stderr.String())
	}
	panic("unreachable")
}

var mcpIDRe = regexp.MustCompile(`mem_[0-9A-Za-z]+`)

func TestEndToEndMCP(t *testing.T) {
	embed := newFakeEmbedServer(t)
	srv := startServe(t, embed.URL)
	env := append(os.Environ(), "M3M0S_URL="+srv.URL, "M3M0S_API_KEY="+srv.APIKey)

	mcp := startMCP(t, env)

	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	resp := mcp.readLine(t)
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("initialize without result: %v", resp)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocolVersion: %v", result["protocolVersion"])
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "m3m0s" {
		t.Errorf("unexpected serverInfo.name: %v", serverInfo["name"])
	}

	// notification, without "id" — must never generate a response of its
	// own. The proof is the id==2 below: if the server had written anything
	// for this notification, that check would fail because the next line
	// read would not have id 2.
	mcp.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
	})
	resp = mcp.readLine(t)
	if id, _ := resp["id"].(float64); id != 2 {
		t.Fatalf("expected a response with id=2 (nothing should answer the previous notification), got: %v", resp)
	}
	result, _ = resp["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("tools/list came back empty")
	}
	names := map[string]bool{}
	for _, raw := range tools {
		tm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tm["name"].(string)
		names[name] = true
		if tm["inputSchema"] == nil {
			t.Errorf("tool %q without inputSchema", name)
		}
	}
	for _, want := range []string{"memory_add", "memory_search", "memory_recent", "memory_stats", "memory_update", "memory_get"} {
		if !names[want] {
			t.Errorf("tool %q missing from tools/list", want)
		}
	}

	const domain = "codigo:test-e2e-mcp"
	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name": "memory_add",
			"arguments": map[string]any{
				"content": "Memory created via MCP e2e about full integration tests",
				"domain":  domain,
			},
		},
	})
	resp = mcp.readLine(t)
	result, _ = resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call memory_add without result: %v", resp)
	}
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("memory_add returned isError=true: %v", result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("memory_add without content in the response")
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	id := mcpIDRe.FindString(text)
	if id == "" {
		t.Fatalf("did not find a mem_... id in the memory_add response text: %q", text)
	}

	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{
			"name": "memory_search",
			"arguments": map[string]any{
				"query":  "Memory created via MCP about integration tests",
				"domain": domain,
			},
		},
	})
	resp = mcp.readLine(t)
	result, _ = resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("memory_search returned isError=true: %v", result)
	}
	content, _ = result["content"].([]any)
	text, _ = content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, domain) {
		t.Errorf("memory_search text (expected to be the contextBlock) without the domain %q: %q", domain, text)
	}

	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{
			"name":      "memory_get",
			"arguments": map[string]any{"id": id},
		},
	})
	resp = mcp.readLine(t)
	result, _ = resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("memory_get returned isError=true: %v", result)
	}
	content, _ = result["content"].([]any)
	text, _ = content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, id) {
		t.Errorf("memory_get text without the id %q: %q", id, text)
	}

	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 6, "method": "tools/call",
		"params": map[string]any{
			"name":      "memory_get",
			"arguments": map[string]any{"id": "mem_really_does_not_exist"},
		},
	})
	resp = mcp.readLine(t)
	result, _ = resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true for memory_get of a nonexistent id, got: %v", result)
	}

	mcp.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{
			"name":      "tool_that_does_not_exist",
			"arguments": map[string]any{},
		},
	})
	resp = mcp.readLine(t)
	result, _ = resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("unknown tool should return a tool result (isError), not %v", resp)
	}
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true for an unknown tool, got: %v", result)
	}
	if resp["error"] != nil {
		t.Fatalf("unknown tool should not become a JSON-RPC transport error: %v", resp["error"])
	}
}
