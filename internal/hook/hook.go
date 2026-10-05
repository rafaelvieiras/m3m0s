package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// ClaudeCodeSource is the source used in every POST /api/v1/memories fired
// by this hook (D14).
const ClaudeCodeSource = "claude-code"

// replayPerHook bounds how many spooled payloads one hook run retries, so a
// large backlog never stalls the host agent.
const replayPerHook = 5

// minStopChars is the minimum amount of new transcript text (runes) that makes
// a Stop event worth a capture; smaller turns accumulate until a later Stop,
// PreCompact or SessionEnd.
const minStopChars = 4000

func newTextChars(messages []TranscriptMessage) int {
	n := 0
	for _, m := range messages {
		n += len([]rune(m.Text))
	}
	return n
}

// Payload is the body Claude Code sends via stdin to the
// PreCompact/SessionEnd/SessionStart/Stop hooks; only the fields used here.
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

	// Stop fires after every assistant turn: capturing each one would run the
	// fact extractor per turn. Wait until enough new text accumulated; the
	// checkpoint is left untouched so the next event sees the same lines.
	if payload.HookEventName == "Stop" && newTextChars(messages) < minStopChars {
		return
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

	chunks := BuildDigests(messages)
	if len(chunks) == 0 {
		return
	}

	domain := deps.Domain(payload.Cwd)
	now := time.Now()
	baseURL, apiKey, ok := ResolveCredentials(deps.Getenv, deps.HomeDir)
	var c *client.Client
	if ok {
		c = deps.NewClient(baseURL, apiKey)
	} else {
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: missing M3M0S_URL/M3M0S_API_KEY credentials (environment or ~/.m3m0s/hook.env), capture spooled\n")
	}

	// Replay older spooled payloads first (chronological order), then send
	// this run's chunks; whatever the server does not accept is spooled, so
	// the checkpoint advancing above never loses data.
	down := !ok
	if ok {
		down = Replay(ctx, deps.HomeDir, c, replayPerHook, now).Unreachable
	}
	for i, chunk := range chunks {
		req := apitypes.AddRequest{Content: chunk, Domain: domain, Source: ClaudeCodeSource}
		var sendErr error
		if down {
			sendErr = errors.New("server unavailable")
		} else if _, sendErr = c.Add(ctx, req); sendErr != nil && isTransport(sendErr) {
			down = true
		}
		if sendErr == nil {
			continue
		}
		if _, err := Enqueue(deps.HomeDir, req, sendErr, now.Add(time.Duration(i))); err != nil {
			fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: capture lost, spool write failed: %v\n", err)
			continue
		}
		fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: POST /api/v1/memories failed, chunk %d/%d spooled: %v\n", i+1, len(chunks), sendErr)
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
