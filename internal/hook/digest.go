package hook

import (
	"fmt"
	"strings"
)

// maxDigestChars is the size of one digest chunk (D14/Phase 8, plan item
// 4); it keeps one giant compaction/session from becoming an absurd payload
// for the fact extractor (Phase 4/D12). Digests above the limit are no longer
// truncated (that dropped data silently): they are split into several chunks
// at message boundaries, each one sent as its own memory.
const maxDigestChars = 40000

// BuildDigests concatenates the messages extracted from the transcript into
// readable text — each part prefixed with "[user]"/"[assistant]", one blank
// line between messages — and splits the result into chunks of at most
// maxDigestChars runes, in chronological order. Chunks break between
// messages; a single message larger than the limit is split by runes, each
// piece keeping its role prefix. Returns nil for no messages.
func BuildDigests(messages []TranscriptMessage) []string {
	var chunks []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, string(cur))
			cur = nil
		}
	}
	for _, m := range messages {
		prefix := fmt.Sprintf("[%s]\n", m.Role)
		body := []rune(strings.TrimSpace(m.Text))
		sep := 0
		if len(cur) > 0 {
			sep = 2
		}
		if len(cur)+sep+len([]rune(prefix))+len(body) <= maxDigestChars {
			if sep > 0 {
				cur = append(cur, '\n', '\n')
			}
			cur = append(cur, []rune(prefix)...)
			cur = append(cur, body...)
			continue
		}
		flush()
		room := maxDigestChars - len([]rune(prefix))
		for len(body) > room {
			chunks = append(chunks, prefix+string(body[:room]))
			body = body[room:]
		}
		cur = append([]rune(prefix), body...)
	}
	flush()
	return chunks
}
