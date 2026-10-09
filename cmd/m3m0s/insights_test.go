package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

func TestInsightsShow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/insights/overview", func(w http.ResponseWriter, r *http.Request) {
		writeJSONT(w, 200, apitypes.InsightsOverviewResponse{
			Totals:     apitypes.InsightsTotals{Total: 12, Active: 10, SoftDeleted: 2, NeverAccessed: 9, NeverAccessedPct: 90},
			Forgetting: apitypes.ForgettingStats{Eligible: 8, ColdNow: 3, SoftDeleted: 2, GraceDays: 30},
			TopDomains: []apitypes.DomainStat{{Domain: "codigo:x", Count: 10}},
		})
	})
	mux.HandleFunc("/api/v1/insights/hygiene", func(w http.ResponseWriter, r *http.Request) {
		writeJSONT(w, 200, apitypes.HygieneResponse{Report: &apitypes.HygieneReport{
			ComputedAt: time.Now(), Scanned: 10,
			Exact: apitypes.ExactDupReport{Groups: 1, Redundant: 1},
			Near:  apitypes.NearDupReport{Available: true, Thresholds: []apitypes.NearDupThreshold{{Threshold: 0.98, Pairs: 2}}},
			Noise: apitypes.NoiseReport{Category: "codigo", ConservativeUnion: 4},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.New(srv.URL, "k")

	var out, errOut bytes.Buffer
	if err := insightsMain(context.Background(), c, &out, &errOut, []string{"show"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"10 active, 2 soft-deleted", "3 cold now", "identical:  1 groups", "cosine>=0.98: 2 pairs", "4 conservative"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := insightsMain(context.Background(), c, &out, &errOut, []string{"show", "--json"}); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil || parsed["overview"] == nil || parsed["hygiene"] == nil {
		t.Errorf("--json output = %s (%v)", out.String(), err)
	}

	if err := insightsMain(context.Background(), c, &out, &errOut, nil); err == nil {
		t.Error("no subcommand must print usage as an error")
	}
}
