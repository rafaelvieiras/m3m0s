package hook

import (
	"io"
	"time"
)

// TimestampWriter prefixes every Write (the hook writes one log line per
// call) with an RFC 3339 UTC timestamp, so failures in the hook log can be
// correlated with outages. `m3m0s doctor` parses this prefix.
type TimestampWriter struct {
	W   io.Writer
	Now func() time.Time // time.Now when nil
}

func (t TimestampWriter) Write(p []byte) (int, error) {
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	line := append([]byte(now().UTC().Format(time.RFC3339)+" "), p...)
	if _, err := t.W.Write(line); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ParseLogTime extracts the timestamp prefix written by TimestampWriter.
func ParseLogTime(line string) (time.Time, bool) {
	if len(line) < len(time.RFC3339) {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, line[:len("2006-01-02T15:04:05Z")])
	return ts, err == nil
}
