package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

const secretKey = "sk-test-SECRET-1234567890"

// fakeEmbeddingServer mimics the /embedding-config routes. It returns 409 when
// the model changes without confirm_reembed, and records every PUT body.
type fakeEmbeddingServer struct {
	mu       sync.Mutex
	cfg      apitypes.EmbeddingConfigResponse
	puts     []apitypes.EmbeddingConfigRequest
	rawPuts  []string
	progress []apitypes.ReembedProgress
	polls    int
	started  bool
	canceled bool
}

func newFake() *fakeEmbeddingServer {
	return &fakeEmbeddingServer{cfg: apitypes.EmbeddingConfigResponse{
		Enabled: true, Model: "text-embedding-3-small", Source: "environment", HasAPIKey: true,
		KeySource: "environment", Active: true, VectorDims: 1536,
		Vectors: apitypes.EmbeddingVectors{Total: 100, Current: 100},
	}}
}

func writeJSONT(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrT(w http.ResponseWriter, status int, code, msg string) {
	var b apitypes.ErrorBody
	b.ErrorInfo.Code, b.ErrorInfo.Message = code, msg
	writeJSONT(w, status, b)
}

func (f *fakeEmbeddingServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/embedding-config", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodGet {
			writeJSONT(w, 200, f.cfg)
			return
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		f.rawPuts = append(f.rawPuts, buf.String())
		var req apitypes.EmbeddingConfigRequest
		_ = json.Unmarshal(buf.Bytes(), &req)
		f.puts = append(f.puts, req)
		if req.Model == "bad-model" {
			writeErrT(w, 422, "embedding_config_invalid", "provider rejected model bad-model")
			return
		}
		if req.Model != f.cfg.Model && !req.ConfirmReembed {
			f.cfg.Vectors = apitypes.EmbeddingVectors{Total: 100, Current: 0, Stale: 100}
			writeErrT(w, 409, "reembed_required", "changing model leaves 100 stored vectors to re-embed; confirm to proceed")
			return
		}
		started := req.ConfirmReembed && req.Model != f.cfg.Model
		f.cfg.Model, f.cfg.Enabled, f.cfg.Dimensions, f.cfg.BaseURL = req.Model, req.Enabled, req.Dimensions, req.BaseURL
		out := f.cfg
		if req.APIKey != nil {
			out.HasAPIKey, out.KeySource = *req.APIKey != "", "database"
		}
		out.ReembedStarted = started
		writeJSONT(w, 200, out)
	})
	mux.HandleFunc("/api/v1/embedding-config/reembed/estimate", func(w http.ResponseWriter, r *http.Request) {
		cost, price := 0.002, 0.02
		writeJSONT(w, 200, apitypes.ReembedEstimateResponse{
			Model: "m", Pending: 100, Characters: 400000, EstimatedTokens: 100000, Approximate: true,
			PricePerMTokUSD: &price, EstimatedCostUSD: &cost, PricesAsOf: "2026-10-08",
		})
	})
	mux.HandleFunc("/api/v1/embedding-config/reembed", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			f.started = true
			writeJSONT(w, 202, apitypes.ReembedStartResponse{Started: true})
		case http.MethodDelete:
			f.canceled = true
			writeJSONT(w, 200, apitypes.ReembedCancelResponse{Cancelled: true})
		default:
			i := f.polls
			if i >= len(f.progress) {
				i = len(f.progress) - 1
			}
			f.polls++
			writeJSONT(w, 200, f.progress[i])
		}
	})
	return mux
}

type result struct {
	out, errOut string
	err         error
}

func runCmd(t *testing.T, f *fakeEmbeddingServer, stdin string, interactive bool, env map[string]string, args ...string) result {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	e := &embeddingEnv{
		c: client.New(srv.URL, "api-key"), in: strings.NewReader(stdin), out: &out, errOut: &errOut,
		getenv: func(k string) string { return env[k] }, interactive: interactive, pollEvery: time.Millisecond,
	}
	err := embeddingMain(context.Background(), e, args)
	return result{out.String(), errOut.String(), err}
}

func TestShowHasNoSecretAndShowsCounts(t *testing.T) {
	r := runCmd(t, newFake(), "", false, nil, "show")
	if r.err != nil {
		t.Fatal(r.err)
	}
	for _, want := range []string{"text-embedding-3-small", "set (from environment)", "100 total", "everything is up to date"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	j := runCmd(t, newFake(), "", false, nil, "show", "--json")
	var cfg apitypes.EmbeddingConfigResponse
	if err := json.Unmarshal([]byte(j.out), &cfg); err != nil || cfg.Model == "" {
		t.Fatalf("bad json %v: %s", err, j.out)
	}
}

func TestSetKeyFromStdinNeverPrinted(t *testing.T) {
	f := newFake()
	r := runCmd(t, f, secretKey+"\n", false, nil, "set", "--api-key-stdin", "--dimensions", "512")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if got := f.puts[0].APIKey; got == nil || *got != secretKey {
		t.Fatalf("key not sent: %v", got)
	}
	if f.puts[0].Dimensions != 512 || f.puts[0].Model != "text-embedding-3-small" {
		t.Errorf("unexpected merge: %+v", f.puts[0])
	}
	if strings.Contains(r.out+r.errOut, secretKey) {
		t.Fatal("key leaked into output")
	}
}

func TestSetKeyFromEnvAndNoKeyKeepsSaved(t *testing.T) {
	f := newFake()
	r := runCmd(t, f, "", false, map[string]string{"EMB_KEY": secretKey}, "set", "--api-key-env", "EMB_KEY")
	if r.err != nil || f.puts[0].APIKey == nil || *f.puts[0].APIKey != secretKey {
		t.Fatalf("env key not sent: %v %v", r.err, f.puts)
	}
	if strings.Contains(r.out+r.errOut, secretKey) {
		t.Fatal("key leaked")
	}
	f2 := newFake()
	if r := runCmd(t, f2, "", false, nil, "set", "--disable"); r.err != nil {
		t.Fatal(r.err)
	}
	if f2.puts[0].APIKey != nil || strings.Contains(f2.rawPuts[0], "api_key") {
		t.Errorf("api_key must be omitted to keep the saved one: %s", f2.rawPuts[0])
	}
	if f2.puts[0].Enabled {
		t.Error("--disable ignored")
	}
}

func TestSetRejectsKeyAsArgument(t *testing.T) {
	f := newFake()
	for _, args := range [][]string{
		{"set", secretKey},
		{"set", "--api-key", secretKey},
	} {
		r := runCmd(t, f, "", false, nil, args...)
		if r.err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
	if len(f.puts) != 0 {
		t.Error("a PUT was sent")
	}
}

func TestSetPositionalArgErrorDoesNotEchoKey(t *testing.T) {
	r := runCmd(t, newFake(), "", false, nil, "set", secretKey)
	if r.err == nil || strings.Contains(r.err.Error(), secretKey) {
		t.Fatalf("error must exist and not echo the value: %v", r.err)
	}
}

func TestSetConflictNonInteractiveNeedsConfirmation(t *testing.T) {
	f := newFake()
	r := runCmd(t, f, "", false, nil, "set", "--model", "text-embedding-3-large")
	if r.err == nil || !strings.Contains(r.err.Error(), "--confirm-reembed") {
		t.Fatalf("want confirmation error, got %v", r.err)
	}
	if !strings.Contains(r.errOut, "100 of 100") || !strings.Contains(r.errOut, "100000") || !strings.Contains(r.errOut, "approximation") {
		t.Errorf("counts/estimate missing:\n%s", r.errOut)
	}
	if len(f.puts) != 1 {
		t.Errorf("expected a single PUT, got %d", len(f.puts))
	}
}

func TestSetConflictConfirmFlagRetries(t *testing.T) {
	f := newFake()
	r := runCmd(t, f, "", false, nil, "set", "--model", "text-embedding-3-large", "--confirm-reembed")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !f.puts[len(f.puts)-1].ConfirmReembed || !strings.Contains(r.out, "re-embedding started") {
		t.Errorf("not confirmed/started: %+v\n%s", f.puts, r.out)
	}
}

func TestSetConflictInteractivePrompt(t *testing.T) {
	f := newFake()
	r := runCmd(t, f, "n\n", true, nil, "set", "--model", "text-embedding-3-large")
	if r.err == nil || !strings.Contains(r.err.Error(), "aborted") {
		t.Fatalf("want abort, got %v", r.err)
	}
	f = newFake()
	r = runCmd(t, f, "y\n", true, nil, "set", "--model", "text-embedding-3-large")
	if r.err != nil || !f.puts[len(f.puts)-1].ConfirmReembed {
		t.Fatalf("yes should confirm: %v %+v", r.err, f.puts)
	}
}

func TestSetProviderRejection422(t *testing.T) {
	r := runCmd(t, newFake(), "", false, nil, "set", "--model", "bad-model")
	if r.err == nil || !strings.Contains(r.err.Error(), "provider rejected model") {
		t.Fatalf("422 message not surfaced: %v", r.err)
	}
}

func TestSetKeyFlagConflictsAndEmpty(t *testing.T) {
	if r := runCmd(t, newFake(), secretKey, false, map[string]string{"X": "y"}, "set", "--api-key-stdin", "--api-key-env", "X"); r.err == nil {
		t.Error("both key sources accepted")
	}
	if r := runCmd(t, newFake(), "", false, nil, "set", "--api-key-env", "UNSET"); r.err == nil {
		t.Error("empty env accepted")
	}
	f := newFake()
	if r := runCmd(t, f, "", false, nil, "set", "--clear-api-key"); r.err != nil || f.puts[0].APIKey == nil || *f.puts[0].APIKey != "" {
		t.Errorf("clear key: %v %+v", r.err, f.puts)
	}
}

func TestEstimateAndReembed(t *testing.T) {
	f := newFake()
	r := runCmd(t, f, "", false, nil, "estimate", "--model", "text-embedding-3-large", "--dimensions", "1024")
	if r.err != nil || !strings.Contains(r.out, "~US$ 0.0020") {
		t.Fatalf("%v\n%s", r.err, r.out)
	}
	if r := runCmd(t, f, "", false, nil, "reembed", "cancel"); r.err != nil || !f.canceled {
		t.Fatalf("cancel: %v", r.err)
	}
}

func TestReembedStartWatchFollowsToEnd(t *testing.T) {
	f := newFake()
	fin := time.Now()
	f.progress = []apitypes.ReembedProgress{
		{Running: true, Total: 100, Done: 10, ETASeconds: 90},
		{Running: true, Total: 100, Done: 60, ETASeconds: 30},
		{Running: false, Total: 100, Done: 100, FinishedAt: &fin},
	}
	r := runCmd(t, f, "", false, nil, "reembed", "start", "--watch")
	if r.err != nil || !f.started {
		t.Fatalf("%v started=%v", r.err, f.started)
	}
	for _, want := range []string{"10/100", "60/100", "finished, 100/100", "ETA 1m30s"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q:\n%s", want, r.out)
		}
	}
}

func TestReembedStatusLastErrorFails(t *testing.T) {
	f := newFake()
	fin := time.Now()
	f.progress = []apitypes.ReembedProgress{{Total: 5, Done: 2, Errors: 1, FinishedAt: &fin, LastError: "provider 401"}}
	r := runCmd(t, f, "", false, nil, "reembed", "status", "--watch")
	if r.err == nil || !strings.Contains(r.err.Error(), "provider 401") {
		t.Fatalf("want stop error, got %v", r.err)
	}
}

func TestUnknownCommands(t *testing.T) {
	if r := runCmd(t, newFake(), "", false, nil); r.err == nil {
		t.Error("no args accepted")
	}
	if r := runCmd(t, newFake(), "", false, nil, "bogus"); r.err == nil {
		t.Error("bogus accepted")
	}
}
