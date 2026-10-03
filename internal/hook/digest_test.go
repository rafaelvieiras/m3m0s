package hook

import (
	"strings"
	"testing"
)

func TestBuildDigestFormatsPrefixesAndBlankLineBetween(t *testing.T) {
	messages := []TranscriptMessage{
		{Role: "user", Text: "first message"},
		{Role: "assistant", Text: "second message"},
	}
	digest, truncated := BuildDigest(messages)
	if truncated {
		t.Error("should not truncate a small digest")
	}
	want := "[user]\nfirst message\n\n[assistant]\nsecond message"
	if digest != want {
		t.Errorf("digest = %q, want %q", digest, want)
	}
}

func TestBuildDigestTruncatesOversizedDigestKeepingEnd(t *testing.T) {
	messages := []TranscriptMessage{
		{Role: "user", Text: strings.Repeat("x", maxDigestChars+5000)},
		{Role: "assistant", Text: "most recent message"},
	}

	digest, truncated := BuildDigest(messages)
	if !truncated {
		t.Fatal("expected truncated=true for a digest above the limit")
	}
	if len([]rune(digest)) != maxDigestChars {
		t.Errorf("len(digest) = %d, want %d", len([]rune(digest)), maxDigestChars)
	}
	if !strings.HasSuffix(digest, "most recent message") {
		t.Errorf("truncation should keep the end (most recent) of the digest, got: %q", digest[len(digest)-40:])
	}
	if strings.HasPrefix(digest, "[user]\nxxx") {
		t.Errorf("truncation should not keep the old start of the digest")
	}
}

func TestBuildDigestEmptyMessagesYieldsEmptyDigest(t *testing.T) {
	digest, truncated := BuildDigest(nil)
	if digest != "" {
		t.Errorf("digest = %q, want empty", digest)
	}
	if truncated {
		t.Error("an empty digest should never be reported as truncated")
	}
}
