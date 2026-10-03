package chunker

import (
	"strings"
	"testing"
)

func TestSplitNoHeadersFallsBackToSingleChunk(t *testing.T) {
	content := strings.Repeat("a line with no markdown header in it.\n", 20)
	chunks := Split(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Heading != "" || chunks[0].Content != content {
		t.Errorf("the single chunk should be the whole content with no heading: %#v", chunks[0])
	}
}

func TestSplitShortContentWithHeadersFallsBackToSingleChunk(t *testing.T) {
	content := "# Title\n\nShort."
	chunks := Split(content)
	if len(chunks) != 1 {
		t.Fatalf("short content should fall back to 1 chunk, got %d", len(chunks))
	}
}

func TestSplitMultipleH2Sections(t *testing.T) {
	var b strings.Builder
	b.WriteString("## First section\n")
	b.WriteString(strings.Repeat("content of the first section, quite detailed. ", 10) + "\n\n")
	b.WriteString("## Second section\n")
	b.WriteString(strings.Repeat("content of the second section, also detailed. ", 10) + "\n")
	content := b.String()

	chunks := Split(content)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0].Heading != "First section" {
		t.Errorf("Heading[0] = %q, want %q", chunks[0].Heading, "First section")
	}
	if chunks[1].Heading != "Second section" {
		t.Errorf("Heading[1] = %q, want %q", chunks[1].Heading, "Second section")
	}
	if !strings.Contains(chunks[0].Content, "first section") || strings.Contains(chunks[0].Content, "second section") {
		t.Errorf("chunk 0 should contain only the first section's content: %q", chunks[0].Content)
	}
}

func TestSplitPreambleBecomesOwnChunk(t *testing.T) {
	var b strings.Builder
	b.WriteString(strings.Repeat("preamble before any header, with plenty of text. ", 6) + "\n\n")
	b.WriteString("## Only section\n")
	b.WriteString(strings.Repeat("section content. ", 10) + "\n")
	content := b.String()

	chunks := Split(content)
	if len(chunks) != 2 {
		t.Fatalf("expected preamble + 1 section = 2 chunks, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0].Heading != "" {
		t.Errorf("preamble chunk should have an empty Heading, got %q", chunks[0].Heading)
	}
	if !strings.Contains(chunks[0].Content, "preamble") {
		t.Errorf("preamble chunk does not contain the expected text: %q", chunks[0].Content)
	}
	if chunks[1].Heading != "Only section" {
		t.Errorf("Heading[1] = %q, want %q", chunks[1].Heading, "Only section")
	}
}

func TestSplitH3AndDeeperStayInsideParentChunk(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Parent section\n")
	b.WriteString(strings.Repeat("parent section text. ", 10) + "\n")
	b.WriteString("### Subsection\n")
	b.WriteString(strings.Repeat("subsection text, which should not become its own chunk. ", 5) + "\n")
	b.WriteString("#### Sub-subsection\n")
	b.WriteString(strings.Repeat("even deeper, also inside the same chunk. ", 5) + "\n")
	b.WriteString("## Another parent section\n")
	b.WriteString(strings.Repeat("text of the other parent section. ", 10) + "\n")
	content := b.String()

	chunks := Split(content)
	if len(chunks) != 2 {
		t.Fatalf("### and #### headers should not create their own chunk; expected 2 chunks, got %d: %#v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "Subsection") || !strings.Contains(chunks[0].Content, "Sub-subsection") {
		t.Errorf("chunk 0 should contain the ### and #### subsections inside it: %q", chunks[0].Content)
	}
	if chunks[1].Heading != "Another parent section" {
		t.Errorf("Heading[1] = %q, want %q", chunks[1].Heading, "Another parent section")
	}
}

func TestSplitEmptyContent(t *testing.T) {
	if chunks := Split(""); chunks != nil {
		t.Errorf("Split(\"\") should return nil, got %#v", chunks)
	}
	if chunks := Split("   \n\n  "); chunks != nil {
		t.Errorf("Split of whitespace-only content should return nil, got %#v", chunks)
	}
}
