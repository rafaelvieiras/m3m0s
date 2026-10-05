package hook

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

func TestEnqueueIsIdempotentForSamePayload(t *testing.T) {
	home := t.TempDir()
	req := apitypes.AddRequest{Content: "hello", Domain: "code:x"}
	now := time.Now()
	a, err := Enqueue(home, req, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Enqueue(home, req, nil, now.Add(time.Second))
	if a != b {
		t.Errorf("same payload spooled twice: %s vs %s", a, b)
	}
	if got := Stats(home).Pending; got != 1 {
		t.Errorf("pending = %d, want 1", got)
	}
}

func TestReplaySendsOldestFirstAndDeletes(t *testing.T) {
	home := t.TempDir()
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()
	now := time.Now()
	for i, c := range []string{"one", "two", "three"} {
		if _, err := Enqueue(home, apitypes.AddRequest{Content: c, Domain: "code:x"}, nil, now.Add(time.Duration(i))); err != nil {
			t.Fatal(err)
		}
	}
	res := Replay(context.Background(), home, client.New(srv.URL, "k"), 0, now)
	if res.Sent != 3 || Stats(home).Pending != 0 {
		t.Fatalf("res = %+v, pending = %d", res, Stats(home).Pending)
	}
	if fake.requests[0].Content != "one" || fake.requests[2].Content != "three" {
		t.Errorf("replay order wrong: %+v", fake.requests)
	}
}

func TestReplayStopsOnTransportErrorKeepingFiles(t *testing.T) {
	home := t.TempDir()
	srv := (&fakeServe{}).server()
	url := srv.URL
	srv.Close() // server down
	now := time.Now()
	_, _ = Enqueue(home, apitypes.AddRequest{Content: "a", Domain: "code:x"}, nil, now)
	_, _ = Enqueue(home, apitypes.AddRequest{Content: "b", Domain: "code:x"}, nil, now.Add(1))
	res := Replay(context.Background(), home, client.New(url, "k"), 0, now)
	if !res.Unreachable || res.Sent != 0 || Stats(home).Pending != 2 {
		t.Errorf("res = %+v, pending = %d", res, Stats(home).Pending)
	}
}

func TestReplayDropsExpired(t *testing.T) {
	home := t.TempDir()
	srv := (&fakeServe{}).server()
	defer srv.Close()
	old := time.Now().Add(-spoolRetention - time.Hour)
	_, _ = Enqueue(home, apitypes.AddRequest{Content: "stale", Domain: "code:x"}, nil, old)
	res := Replay(context.Background(), home, client.New(srv.URL, "k"), 0, time.Now())
	if res.Expired != 1 || Stats(home).Pending != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestReplayQuarantinesCorruptFile(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(SpoolDir(home), 0o700)
	_ = os.WriteFile(filepath.Join(SpoolDir(home), "00000000000000000001-bad.json"), []byte("{nope"), 0o600)
	srv := (&fakeServe{}).server()
	defer srv.Close()
	res := Replay(context.Background(), home, client.New(srv.URL, "k"), 0, time.Now())
	if res.Dead != 1 || Stats(home).Dead != 1 {
		t.Errorf("res = %+v, stats = %+v", res, Stats(home))
	}
}

// End to end: server down at capture → payload spooled; server back → the
// next capture hook replays it, and the data reaches the server.
func TestHookSpoolsWhenServerDownAndReplaysLater(t *testing.T) {
	home := t.TempDir()
	tp := filepath.Join(t.TempDir(), "t.jsonl")
	writeTranscript(t, tp, `{"type":"user","message":{"role":"user","content":"remember the blue door"}}`)

	down := (&fakeServe{}).server()
	deps, stderr := testDeps(t, home, down)
	down.Close()
	Run(context.Background(), bytes.NewReader(payloadJSON(t, "s1", tp, "/w/proj")), deps)
	if Stats(home).Pending != 1 {
		t.Fatalf("expected 1 spooled payload; stderr: %s", stderr)
	}

	fake := &fakeServe{}
	up := fake.server()
	defer up.Close()
	deps2, _ := testDeps(t, home, up)
	tp2 := filepath.Join(t.TempDir(), "t2.jsonl")
	writeTranscript(t, tp2, `{"type":"user","message":{"role":"user","content":"second session"}}`)
	Run(context.Background(), bytes.NewReader(payloadJSON(t, "s2", tp2, "/w/proj")), deps2)

	if Stats(home).Pending != 0 || fake.callCount() != 2 {
		t.Fatalf("pending = %d, calls = %d", Stats(home).Pending, fake.callCount())
	}
	if !strings.Contains(fake.requests[0].Content, "blue door") {
		t.Errorf("spooled payload should be replayed first: %q", fake.requests[0].Content)
	}
}

func TestTimestampWriterPrefixesLines(t *testing.T) {
	var b bytes.Buffer
	w := TimestampWriter{W: &b, Now: func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }}
	_, _ = w.Write([]byte("failed\n"))
	if b.String() != "2026-10-05T12:00:00Z failed\n" {
		t.Errorf("got %q", b.String())
	}
	if ts, ok := ParseLogTime(b.String()); !ok || ts.Year() != 2026 {
		t.Errorf("ParseLogTime failed: %v %v", ts, ok)
	}
}
