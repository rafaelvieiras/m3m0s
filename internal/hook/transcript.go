// Package hook implements `m3m0s hook claude-code` (Phase 8, D14): the
// subcommand that serves as a native Claude Code hook (PreCompact/
// SessionEnd) for automatic memory capture at the end of a session or
// compaction. cmd/m3m0s only does the wiring (real stdin, real env, real
// home dir); all testable logic lives here.
//
// Accepted risk (documented in D14): the Claude Code transcript JSONL format
// is not a stable public API; the official docs warn it may change between
// versions. That is why all parsing here is deliberately tolerant: a
// malformed line or unexpected field is ignored and never aborts processing
// of the remaining lines.
package hook

import (
	"encoding/json"
	"strings"
)

// TranscriptMessage is one message with its text already extracted: only
// what remains after discarding tool_use/tool_result/thinking blocks (D14).
type TranscriptMessage struct {
	Role string // "user" or "assistant" (or the line "type", if message.role is empty)
	Text string
}

// transcriptLine is the (unstable, see package comment) format of one JSONL
// line: only the fields used here, everything else is ignored.
type transcriptLine struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// transcriptMessageBody is the "message" field of a "user"/"assistant" line.
type transcriptMessageBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentBlock is one element of the "content" array when it is not a plain
// string. Only the "text" field matters here; blocks of any other "type"
// (tool_use/tool_result/thinking/etc.) are ignored entirely.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ParseLine extracts the text of one transcript JSONL line. It returns
// ok=false (no error; parsing never fails the program) for: invalid JSON;
// "type" other than "user"/"assistant" (e.g. "summary", system lines);
// missing or malformed "message"; or no real text extracted (empty content,
// or only non-"text" blocks).
func ParseLine(raw []byte) (TranscriptMessage, bool) {
	var line transcriptLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return TranscriptMessage{}, false
	}
	if line.Type != "user" && line.Type != "assistant" {
		return TranscriptMessage{}, false
	}
	if len(line.Message) == 0 {
		return TranscriptMessage{}, false
	}

	var msg transcriptMessageBody
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		return TranscriptMessage{}, false
	}

	role := msg.Role
	if role == "" {
		role = line.Type
	}

	text := extractContentText(msg.Content)
	if strings.TrimSpace(text) == "" {
		return TranscriptMessage{}, false
	}
	return TranscriptMessage{Role: role, Text: text}, true
}

// extractContentText handles the 2 possible "content" formats: a plain
// string, or an array of typed blocks. It only concatenates blocks with
// "type": "text" (separated by a blank line); everything else
// (tool_use/tool_result/thinking/unknown future types) is discarded.
func extractContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// readLinesFromBytes returns data as a slice of lines (without the "\n"),
// preserving each line's index; that index is what the checkpoint (D14,
// item 3) uses to know which lines were already processed.
func readLinesFromBytes(data []byte) []string {
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
