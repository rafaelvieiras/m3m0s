package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/factkind"
)

func TestKindSampleBlindAndReport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/kinds/sample", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("n") != "3" || r.URL.Query().Get("seed") != "9" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		writeJSONT(w, 200, apitypes.KindSampleResponse{Items: []factkind.SampleItem{
			{ID: "a", Domain: "code:x", Content: "Always run the linter before pushing.", Kind: "rule", KindSource: "llm"},
			{ID: "b", Domain: "code:x", Content: "Status: waiting for review.", Kind: "state", KindSource: "llm"},
			{ID: "c", Domain: "code:y", Content: "Plain words.", Kind: "other"},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.New(srv.URL, "k")
	dir := t.TempDir()
	sample := filepath.Join(dir, "s.json")

	var out, errOut bytes.Buffer
	if err := kindMain(context.Background(), c, nil, &out, &errOut, false, []string{"sample", "--n", "3", "--seed", "9", "--out", sample, "--blind"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sample)
	if strings.Contains(string(raw), `"kind": "rule"`) {
		t.Fatalf("a blind sample must not carry the server's tags:\n%s", raw)
	}
	pred := filepath.Join(dir, "s.predictions.json")
	if _, err := os.Stat(pred); err != nil {
		t.Fatalf("predictions file missing: %v", err)
	}

	// label by hand: fill the "label" fields
	var items []factkind.SampleItem
	_ = json.Unmarshal(raw, &items)
	labels := map[string]string{"a": "rule", "b": "noise", "c": "gotcha"}
	for i := range items {
		items[i].Label = labels[items[i].ID]
	}
	b, _ := json.Marshal(items)
	_ = os.WriteFile(sample, b, 0o600)

	out.Reset()
	if err := kindMain(context.Background(), c, nil, &out, &errOut, false, []string{"report", "--file", sample, "--predictions", pred, "--stored-source", "llm"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"labelled: 3", "stored tag: exact agreement 1/2", "heuristic tag", "durable-vs-transient"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := kindMain(context.Background(), c, nil, &out, &errOut, false, []string{"report", "--file", sample, "--json"}); err != nil {
		t.Fatal(err)
	}
	var rep factkind.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || rep.Labelled != 3 {
		t.Errorf("json report: %v %s", err, out.String())
	}
	if err := kindMain(context.Background(), c, nil, &out, &errOut, false, []string{"report"}); err == nil {
		t.Error("report without --file must fail")
	}
}

func TestKindBackfillCommands(t *testing.T) {
	var started atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/kinds/backfill/estimate", func(w http.ResponseWriter, r *http.Request) {
		writeJSONT(w, 200, apitypes.KindBackfillEstimateResponse{Mode: "llm", Model: "m", Pending: 40, Requests: 2, InputTokens: 900, OutputTokens: 200, Approximate: true})
	})
	mux.HandleFunc("POST /api/v1/kinds/backfill", func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.KindBackfillStartRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Confirm {
			t.Error("the CLI must send confirm=true")
		}
		started.Add(1)
		writeJSONT(w, 202, apitypes.KindBackfillStartResponse{Started: true})
	})
	mux.HandleFunc("GET /api/v1/kinds/backfill", func(w http.ResponseWriter, r *http.Request) {
		writeJSONT(w, 200, apitypes.KindBackfillProgress{Running: true, Mode: "llm", Total: 40, Done: 10})
	})
	mux.HandleFunc("DELETE /api/v1/kinds/backfill", func(w http.ResponseWriter, r *http.Request) {
		writeJSONT(w, 200, apitypes.KindBackfillCancelResponse{Cancelled: true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.New(srv.URL, "k")
	run := func(in string, interactive bool, args ...string) (string, error) {
		var out, errOut bytes.Buffer
		err := kindMain(context.Background(), c, strings.NewReader(in), &out, &errOut, interactive, append([]string{"backfill"}, args...))
		return out.String(), err
	}

	out, err := run("", false, "estimate")
	if err != nil || !strings.Contains(out, "facts to tag:  40") || !strings.Contains(out, "cost:          unknown") {
		t.Fatalf("estimate: %v\n%s", err, out)
	}

	// Not interactive and no --yes: refuses, nothing started.
	if _, err := run("", false, "start"); err == nil || started.Load() != 0 {
		t.Fatalf("unconfirmed start: err=%v started=%d", err, started.Load())
	}
	// Interactive "n" aborts; "y" starts.
	if _, err := run("n\n", true, "start"); err == nil || started.Load() != 0 {
		t.Fatalf("declined start: err=%v started=%d", err, started.Load())
	}
	if _, err := run("y\n", true, "start"); err != nil || started.Load() != 1 {
		t.Fatalf("confirmed start: err=%v started=%d", err, started.Load())
	}
	if _, err := run("", false, "start", "--yes"); err != nil || started.Load() != 2 {
		t.Fatalf("--yes start: err=%v started=%d", err, started.Load())
	}

	out, err = run("", false, "status")
	if err != nil || !strings.Contains(out, "running (llm): 10/40 tagged") {
		t.Fatalf("status: %v\n%s", err, out)
	}
	out, err = run("", false, "cancel")
	if err != nil || !strings.Contains(out, "cancelled") {
		t.Fatalf("cancel: %v\n%s", err, out)
	}
	if _, err := run("", false, "bogus"); err == nil {
		t.Error("unknown subcommand must fail")
	}
}

func TestKindUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := kindMain(context.Background(), client.New("http://127.0.0.1:1", "k"), nil, &out, &errOut, false, nil); err == nil || !strings.Contains(err.Error(), "usage: m3m0s kind") {
		t.Errorf("err = %v", err)
	}
}
