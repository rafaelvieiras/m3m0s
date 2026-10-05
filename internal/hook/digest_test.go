package hook

import (
	"strings"
	"testing"
)

func TestBuildDigestsFormatsPrefixesAndBlankLineBetween(t *testing.T) {
	chunks := BuildDigests([]TranscriptMessage{
		{Role: "user", Text: "first message"},
		{Role: "assistant", Text: "second message"},
	})
	want := "[user]\nfirst message\n\n[assistant]\nsecond message"
	if len(chunks) != 1 || chunks[0] != want {
		t.Errorf("chunks = %q, want [%q]", chunks, want)
	}
}

func TestBuildDigestsSplitsAtMessageBoundaries(t *testing.T) {
	half := strings.Repeat("a", maxDigestChars/2+10)
	chunks := BuildDigests([]TranscriptMessage{
		{Role: "user", Text: half},
		{Role: "assistant", Text: half},
		{Role: "user", Text: "last"},
	})
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if !strings.HasPrefix(chunks[1], "[assistant]\n") || !strings.HasSuffix(chunks[1], "last") {
		t.Errorf("chunks should break between messages and keep order")
	}
}

func TestBuildDigestsSplitsOversizedMessageWithoutLoss(t *testing.T) {
	text := strings.Repeat("x", maxDigestChars*2+500)
	chunks := BuildDigests([]TranscriptMessage{{Role: "user", Text: text}})
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	total := 0
	for _, c := range chunks {
		if len([]rune(c)) > maxDigestChars {
			t.Errorf("chunk has %d runes, above the limit", len([]rune(c)))
		}
		if !strings.HasPrefix(c, "[user]\n") {
			t.Errorf("each piece should keep the role prefix")
		}
		total += strings.Count(c, "x")
	}
	if total != len(text) {
		t.Errorf("captured %d of %d characters", total, len(text))
	}
}

func TestBuildDigestsEmpty(t *testing.T) {
	if got := BuildDigests(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
