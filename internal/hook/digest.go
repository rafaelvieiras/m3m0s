package hook

import (
	"fmt"
	"strings"
)

// maxDigestChars is the defensive size limit for the digest (D14/Phase 8,
// plan item 4); it keeps one giant compaction/session from becoming an
// absurd payload for the fact extractor (Phase 4/D12). It truncates keeping
// the END (not the start, as in the first version): validated in production
// on 2026-07-16 that the first hook run in a long session processes the
// WHOLE transcript accumulated from line 0 up to the checkpoint (not just
// the last compaction "window"). Keeping the start in that case discards
// exactly the most recent work (what the user wants to track) and preserves
// only old, already-resolved conversation. The end tends to be more
// actionable both in that case and in the common case (one normal
// compaction).
const maxDigestChars = 40000

// BuildDigest concatenates the messages extracted from the transcript into a
// single readable text: each chunk prefixed with "[user]"/"[assistant]", one
// blank line between messages. truncated reports whether the result exceeded
// maxDigestChars and had to be cut.
func BuildDigest(messages []TranscriptMessage) (digest string, truncated bool) {
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "[%s]\n%s", m.Role, strings.TrimSpace(m.Text))
	}
	digest = b.String()

	runes := []rune(digest)
	if len(runes) > maxDigestChars {
		digest = string(runes[len(runes)-maxDigestChars:])
		truncated = true
	}
	return digest, truncated
}
