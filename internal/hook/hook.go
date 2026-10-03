package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// ClaudeCodeSource is the source used in every POST /api/v1/memories fired
// by this hook (D14).
const ClaudeCodeSource = "claude-code"

// Payload is the body Claude Code sends via stdin to the
// PreCompact/SessionEnd/SessionStart hooks; only the fields used here.
// trigger/reason/custom_instructions are deliberately ignored (D14, item 2:
// PreCompact and SessionEnd are handled identically). Source only exists in
// SessionStart ("startup"/"resume"/"clear"/"compact"), see D17.
type Payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source"`
}

// DomainFunc derives the domain from the cwd received in the payload.
// cmd/m3m0s injects the same sanitization rule `m3m0s watch` already uses
// (D11), to keep domain inference identical across the two automatic
// capture mechanisms (D14, item 4).
type DomainFunc func(cwd string) string

// Deps groups Run's injectable dependencies, so the whole pipeline can be
// tested without touching real stdin/filesystem/network.
type Deps struct {
	Stdout    io.Writer // where Run writes the hookSpecificOutput JSON (D17, SessionStart); os.Stdout in production
	Stderr    io.Writer
	HomeDir   string                                      // ~ already resolved
	Getenv    func(string) string                         // os.Getenv in production
	Domain    DomainFunc                                  // derives the domain from payload.Cwd
	NewClient func(baseURL, apiKey string) *client.Client // client.New in production; injectable to point at an httptest.Server in tests
}

// Run dispatches `m3m0s hook claude-code` according to payload.HookEventName:
// on SessionStart, it delegates to the context read/injection path (D17,
// RunSessionStart); on any other event (PreCompact/SessionEnd), it follows
// the original capture path (D14) implemented below. It never returns an
// error: any failure at any step is logged to deps.Stderr and treated as a
// silent return. The hook must never block the user's Claude Code session,
// in either direction (D14 item 1, D17 item 6).
func Run(ctx context.Context, stdin io.Reader, deps Deps) {
	payload, err := readPayload(stdin)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: invalid payload: %v\n", err)
		return
	}

	if payload.HookEventName == "SessionStart" {
		RunSessionStart(ctx, payload, deps)
		return
	}

	if payload.TranscriptPath == "" {
		return
	}

	data, err := os.ReadFile(payload.TranscriptPath)
	if err != nil {
		// transcript_path missing/inaccessible: exit silently (mechanism
		// item 1: never fail the hook because of this).
		return
	}
	lines := readLinesFromBytes(data)
	if len(lines) == 0 {
		return
	}

	cpPath, err := CheckpointPath(deps.HomeDir, payload.SessionID)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: resolving checkpoint: %v\n", err)
		return
	}
	cp, err := LoadCheckpoint(cpPath)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: corrupt checkpoint, reprocessing from the start: %v\n", err)
		cp = Checkpoint{}
	}

	start := cp.LastLine
	if start < 0 || start > len(lines) {
		start = 0
	}

	var messages []TranscriptMessage
	for _, raw := range lines[start:] {
		if msg, ok := ParseLine([]byte(raw)); ok {
			messages = append(messages, msg)
		}
	}

	// The checkpoint advances to the current total of lines even when they
	// hold no useful text (mechanism item 3): what matters is never
	// reprocessing the same raw line again (PreCompact followed by
	// SessionEnd in the same session must not duplicate the capture).
	if err := SaveCheckpoint(cpPath, Checkpoint{LastLine: len(lines)}); err != nil {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: writing checkpoint: %v\n", err)
	}

	if len(messages) == 0 {
		return
	}

	digest, truncated := BuildDigest(messages)
	if strings.TrimSpace(digest) == "" {
		return
	}
	if truncated {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: digest truncated to %d characters (session %s)\n", maxDigestChars, payload.SessionID)
	}

	baseURL, apiKey, ok := ResolveCredentials(deps.Getenv, deps.HomeDir)
	if !ok {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: missing M3M0S_URL/M3M0S_API_KEY credentials (environment or ~/.m3m0s/hook.env), capture skipped\n")
		return
	}

	c := deps.NewClient(baseURL, apiKey)
	domain := deps.Domain(payload.Cwd)
	if _, err := c.Add(ctx, apitypes.AddRequest{
		Content: digest,
		Domain:  domain,
		Source:  ClaudeCodeSource,
	}); err != nil {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: POST /api/v1/memories failed: %v\n", err)
	}
}

func readPayload(r io.Reader) (Payload, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Payload{}, err
	}
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return Payload{}, err
	}
	return p, nil
}
