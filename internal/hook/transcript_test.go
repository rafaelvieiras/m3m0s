package hook

import (
	"strings"
	"testing"
)

func TestParseLineContentAsSimpleString(t *testing.T) {
	line := []byte(`{"type":"user","message":{"role":"user","content":"hello, this is a test"}}`)
	msg, ok := ParseLine(line)
	if !ok {
		t.Fatal("expected ok=true for plain string content")
	}
	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if msg.Text != "hello, this is a test" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello, this is a test")
	}
}

func TestParseLineContentAsMixedBlocksExtractsOnlyText(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[
		{"type":"text","text":"first text block"},
		{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}},
		{"type":"tool_result","tool_use_id":"t1","content":"command output"},
		{"type":"thinking","thinking":"internal reasoning, should never leak"},
		{"type":"text","text":"second text block"}
	]}}`)
	msg, ok := ParseLine(line)
	if !ok {
		t.Fatal("expected ok=true for content with mixed blocks")
	}
	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want %q", msg.Role, "assistant")
	}
	want := "first text block\n\nsecond text block"
	if msg.Text != want {
		t.Errorf("Text = %q, want %q", msg.Text, want)
	}
	for _, banned := range []string{"tool_use", "Bash", "command output", "internal reasoning"} {
		if strings.Contains(msg.Text, banned) {
			t.Errorf("Text should not contain %q (leaked from a non-text block): %q", banned, msg.Text)
		}
	}
}

func TestParseLineOnlyNonTextBlocksYieldsNoText(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[
		{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}},
		{"type":"thinking","thinking":"thinking only, no real text"}
	]}}`)
	_, ok := ParseLine(line)
	if ok {
		t.Fatal("expected ok=false when there is no type=text block")
	}
}

func TestParseLineIgnoresUnknownLineTypes(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"type":"summary","summary":"session summary"}`),
		[]byte(`{"type":"system","message":{"role":"system","content":"system line"}}`),
		[]byte(`{"leafUuid":"abc"}`), // no "type" at all
	}
	for _, c := range cases {
		if _, ok := ParseLine(c); ok {
			t.Errorf("expected ok=false for a line without type user/assistant: %s", c)
		}
	}
}

func TestParseLineMalformedJSONIsSkippedNotFatal(t *testing.T) {
	cases := [][]byte{
		[]byte(`this is not JSON`),
		[]byte(`{"type":"user","message":`), // truncated JSON
		[]byte(``),
		[]byte(`{"type":"user"}`),                           // no message
		[]byte(`{"type":"user","message":{"role":"user"}}`), // no content
		[]byte(`{"type":"user","message":{"content":123}}`), // content is neither string nor array
	}
	for _, c := range cases {
		if _, ok := ParseLine(c); ok {
			t.Errorf("expected ok=false for malformed line: %s", c)
		}
	}
}

func TestParseLineRoleFallsBackToLineType(t *testing.T) {
	line := []byte(`{"type":"user","message":{"content":"no explicit role in message"}}`)
	msg, ok := ParseLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if msg.Role != "user" {
		t.Errorf("Role = %q, want fallback to the line type %q", msg.Role, "user")
	}
}

func TestReadLinesFromBytesEmptyFile(t *testing.T) {
	if lines := readLinesFromBytes([]byte("")); lines != nil {
		t.Errorf("expected nil for empty file, got %v", lines)
	}
	if lines := readLinesFromBytes([]byte("\n")); lines != nil {
		t.Errorf("expected nil for newline-only file, got %v", lines)
	}
}

func TestReadLinesFromBytesPreservesLineCount(t *testing.T) {
	lines := readLinesFromBytes([]byte("line1\nline2\nline3\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("unexpected lines: %v", lines)
	}
}
