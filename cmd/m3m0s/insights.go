package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

const insightsUsage = `usage: m3m0s insights show [--json]
  show   memory health: totals, what forgetting would do next, and the last hygiene analysis
         (start the analysis from the WebUI "Insights" page or with POST /api/v1/insights/hygiene/run)`

// runInsights is `m3m0s insights` (D37): a read-only summary of the memory
// health dashboard.
func runInsights(ctx context.Context, args []string) error {
	return insightsMain(ctx, client.NewFromEnv(), os.Stdout, os.Stderr, args)
}

func insightsMain(ctx context.Context, c *client.Client, out, errOut io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "show" {
		return errors.New(insightsUsage)
	}
	fs := flag.NewFlagSet("insights show", flag.ContinueOnError)
	fs.SetOutput(errOut)
	jsonOut := fs.Bool("json", false, "print the raw responses as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ov, err := c.GetInsightsOverview(ctx)
	if err != nil {
		return err
	}
	hy, err := c.GetHygiene(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		b, err := json.MarshalIndent(struct {
			Overview apitypes.InsightsOverviewResponse `json:"overview"`
			Hygiene  apitypes.HygieneResponse          `json:"hygiene"`
		}{ov, hy}, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(b))
		return err
	}
	printInsights(out, ov, hy)
	return nil
}

func printInsights(w io.Writer, ov apitypes.InsightsOverviewResponse, hy apitypes.HygieneResponse) {
	t, f := ov.Totals, ov.Forgetting
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("memories:      %d active, %d soft-deleted (%d stored)\n", t.Active, t.SoftDeleted, t.Total)
	p("never read:    %d (%.1f%% of active)\n", t.NeverAccessed, t.NeverAccessedPct)
	p("valid_until:   %d set, %d already past; %d pinned\n", t.WithValidUntil, t.Expired, t.Pinned)
	p("kind:          %d extracted facts, %d raw\n", t.Extracted, t.Raw)
	if len(ov.ByKind) > 0 {
		p("nature:        ")
		for i, k := range ov.ByKind {
			if i > 0 {
				p(", ")
			}
			p("%s %d", k.Key, k.Count)
		}
		p("\n")
	}
	p("text length:   avg %.0f, median %d, p90 %d chars\n", ov.Text.AvgChars, ov.Text.MedianChars, ov.Text.P90Chars)
	p("forgetting:    %d cold now (of %d eligible); %d soft-deleted waiting, %d past the %d-day grace\n",
		f.ColdNow, f.Eligible, f.SoftDeleted, f.HardDeleteDue, f.GraceDays)
	if len(ov.TopDomains) > 0 {
		p("top domains:  ")
		for i, d := range ov.TopDomains[:min(5, len(ov.TopDomains))] {
			if i > 0 {
				p(", ")
			}
			p("%s (%d)", d.Domain, d.Count)
		}
		p("\n")
	}

	r := hy.Report
	switch {
	case hy.Status.Running:
		p("hygiene:       running (%s)\n", hy.Status.Phase)
	case r == nil:
		p("hygiene:       never run\n")
	}
	if r == nil {
		return
	}
	p("hygiene:       analysed %s (took %s, %d scanned)", r.ComputedAt.Local().Format("2006-01-02 15:04"),
		(time.Duration(r.DurationMS) * time.Millisecond).Round(time.Second), r.Scanned)
	if r.Partial {
		p(" [partial: %s]", r.PartialReason)
	}
	p("\n")
	p("  identical:  %d groups, %d redundant copies\n", r.Exact.Groups, r.Exact.Redundant)
	if r.Near.Available {
		for _, th := range r.Near.Thresholds {
			p("  cosine>=%.2f: %d pairs, %d clusters (%d same domain, %d across domains)\n",
				th.Threshold, th.Pairs, th.Clusters, th.SameDomain, th.CrossDomain)
		}
	} else {
		p("  near-duplicates unavailable: %s\n", r.Near.Reason)
	}
	p("  noise (%s): %d conservative candidates (%d eligible), %d for review\n",
		r.Noise.Category, r.Noise.ConservativeUnion, r.Noise.ConservativeEligible, r.Noise.ReviewUnion)
}
