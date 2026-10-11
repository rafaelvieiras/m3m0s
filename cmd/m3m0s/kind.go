package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

const kindUsage = `usage: m3m0s kind <sample|report|backfill> [flags]  (fact nature, D38)
  sample [--n 200] [--seed 1] [--out sample.json] [--blind]
         export a sample of facts, spread over domains and ages, to label by hand: fill the
         "label" field of each item with one of: decision, rule, preference, gotcha, procedure,
         state, noise, other. --blind writes the server's tags to <out>.predictions.json instead
         of the sample, so they do not bias the labelling.
  report --file sample.json [--predictions sample.predictions.json] [--stored-source llm] [--json]
         agreement of the stored tag and of the heuristic with your labels (per class, and
         durable-vs-transient). Works offline on the file.
  retention --file sample.json [--predictions ...] [--horizon 30] [--kinds stored|label] [--json]
         offline: how many facts of your labelled sample the legacy and the per-kind retention
         scores (D39) would forget within the horizon, and how many of those are durable. Parameters
         come from M3M0S_RETENTION_* (defaults otherwise). Needs no server.
  backfill estimate | start [--yes] | status | cancel
         tag the facts stored before D38 (rate limited, resumable). "start" shows the estimate
         first and asks for confirmation (--yes to skip the prompt)`

// runKind is `m3m0s kind`: the tools around the fact nature tag (D38).
func runKind(ctx context.Context, args []string) error {
	interactive := false
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		null, nerr := os.Stat(os.DevNull)
		interactive = nerr != nil || !os.SameFile(fi, null)
	}
	return kindMain(ctx, client.NewFromEnv(), os.Stdin, os.Stdout, os.Stderr, interactive, args)
}

func kindMain(ctx context.Context, c *client.Client, in io.Reader, out, errOut io.Writer, interactive bool, args []string) error {
	if len(args) == 0 {
		return errors.New(kindUsage)
	}
	var err error
	switch args[0] {
	case "sample":
		err = kindSample(ctx, c, out, errOut, args[1:])
	case "report":
		err = kindReport(out, errOut, args[1:])
	case "retention":
		err = kindRetention(out, errOut, args[1:])
	case "backfill":
		err = kindBackfill(ctx, c, in, out, errOut, interactive, args[1:])
	default:
		return errors.New(kindUsage)
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func kindSample(ctx context.Context, c *client.Client, out, errOut io.Writer, args []string) error {
	fs := flag.NewFlagSet("kind sample", flag.ContinueOnError)
	fs.SetOutput(errOut)
	n := fs.Int("n", 200, "number of facts to sample")
	seed := fs.Int64("seed", 1, "sampling seed (same seed, same data: same sample)")
	outPath := fs.String("out", "kind-sample.json", "file to write")
	blind := fs.Bool("blind", false, "write the server's tags to a separate predictions file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := c.GetKindSample(ctx, *n, *seed)
	if err != nil {
		return err
	}
	items := resp.Items
	if *blind {
		preds := map[string]map[string]string{}
		for i := range items {
			preds[items[i].ID] = map[string]string{"kind": items[i].Kind, "kindSource": items[i].KindSource}
			items[i].Kind, items[i].KindSource = "", ""
		}
		if err := writeJSONFile(strings.TrimSuffix(*outPath, filepath.Ext(*outPath))+".predictions.json", preds); err != nil {
			return err
		}
	}
	if err := writeJSONFile(*outPath, items); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "wrote %d facts to %s; fill \"label\" on each, then run: m3m0s kind report --file %s\n", len(items), *outPath, *outPath)
	return nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func kindReport(out, errOut io.Writer, args []string) error {
	fs := flag.NewFlagSet("kind report", flag.ContinueOnError)
	fs.SetOutput(errOut)
	file := fs.String("file", "", "labelled sample written by `kind sample` (required)")
	predFile := fs.String("predictions", "", "predictions file written by `kind sample --blind`")
	storedSource := fs.String("stored-source", "", "evaluate the stored tag only on facts it came from this source (llm, heuristic, manual)")
	jsonOut := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	items, err := loadSample(*file, *predFile)
	if err != nil {
		return err
	}
	rep := factkind.Evaluate(items, *storedSource)
	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	_, err = fmt.Fprint(out, rep.String())
	return err
}

// loadSample reads a labelled sample and, when given, merges the predictions
// file written by `kind sample --blind` into it.
func loadSample(file, predFile string) ([]factkind.SampleItem, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var items []factkind.SampleItem
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	if predFile != "" {
		pb, err := os.ReadFile(predFile)
		if err != nil {
			return nil, err
		}
		preds := map[string]map[string]string{}
		if err := json.Unmarshal(pb, &preds); err != nil {
			return nil, fmt.Errorf("parse %s: %w", predFile, err)
		}
		for i := range items {
			if p, ok := preds[items[i].ID]; ok {
				items[i].Kind, items[i].KindSource = p["kind"], p["kindSource"]
			}
		}
	}
	return items, nil
}

// kindRetention is `m3m0s kind retention`: the offline acceptance check of the
// per-kind retention model (D39, issue #52) against a hand-labelled sample.
func kindRetention(out, errOut io.Writer, args []string) error {
	fs := flag.NewFlagSet("kind retention", flag.ContinueOnError)
	fs.SetOutput(errOut)
	file := fs.String("file", "", "labelled sample written by `kind sample` (required)")
	predFile := fs.String("predictions", "", "predictions file written by `kind sample --blind`")
	horizon := fs.Int("horizon", 30, "days ahead to evaluate (the acceptance horizon is 30)")
	kinds := fs.String("kinds", store.SampleKindsStored, "kind given to each fact: stored (the sample's tag) or label (your label, as a perfect classifier)")
	jsonOut := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	if *kinds != store.SampleKindsStored && *kinds != store.SampleKindsLabel {
		return fmt.Errorf("--kinds must be %q or %q", store.SampleKindsStored, store.SampleKindsLabel)
	}
	if *horizon < 0 {
		return errors.New("--horizon must not be negative")
	}
	params, err := store.RetentionParamsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	items, err := loadSample(*file, *predFile)
	if err != nil {
		return err
	}
	rep := store.EvaluateRetentionSample(items, params, store.RetentionSampleOptions{HorizonDays: *horizon, Now: time.Now(), Kinds: *kinds})
	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	_, err = fmt.Fprint(out, rep.String())
	return err
}

func kindBackfill(ctx context.Context, c *client.Client, in io.Reader, out, errOut io.Writer, interactive bool, args []string) error {
	if len(args) == 0 {
		return errors.New(kindUsage)
	}
	sub := args[0]
	fs := flag.NewFlagSet("kind backfill "+sub, flag.ContinueOnError)
	fs.SetOutput(errOut)
	yes := fs.Bool("yes", false, "start without the confirmation prompt (start only)")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pj := func(v any) error {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	switch sub {
	case "estimate":
		est, err := c.EstimateKindBackfill(ctx)
		if err != nil {
			return err
		}
		if *jsonOut {
			return pj(est)
		}
		printKindEstimate(out, est)
		return nil
	case "start":
		est, err := c.EstimateKindBackfill(ctx)
		if err != nil {
			return err
		}
		printKindEstimate(out, est)
		if est.Pending == 0 {
			_, _ = fmt.Fprintln(out, "nothing to do")
			return nil
		}
		if est.Mode == "llm" && !*yes {
			if !interactive {
				return errors.New("the backfill spends LLM tokens and must be confirmed: repeat with --yes")
			}
			_, _ = fmt.Fprint(errOut, "Start the backfill? [y/N] ")
			line, _ := bufio.NewReader(in).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				return errors.New("aborted: nothing was started")
			}
		}
		if _, err := c.StartKindBackfill(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, "backfill started (check: m3m0s kind backfill status; stop: m3m0s kind backfill cancel). Manual tags are never overwritten; a new start resumes what is pending.")
		return nil
	case "status":
		p, err := c.KindBackfillStatus(ctx)
		if err != nil {
			return err
		}
		if *jsonOut {
			return pj(p)
		}
		state := "idle"
		if p.Running {
			state = "running"
		}
		_, _ = fmt.Fprintf(out, "%s (%s): %d/%d tagged, %d errors, %d skipped", state, p.Mode, p.Done, p.Total, p.Errors, p.Skipped)
		if p.Running && p.ETASeconds > 0 {
			_, _ = fmt.Fprintf(out, ", ~%ds left", p.ETASeconds)
		}
		if p.LastError != "" {
			_, _ = fmt.Fprintf(out, " (last error: %s)", p.LastError)
		}
		_, _ = fmt.Fprintln(out)
		return nil
	case "cancel":
		if _, err := c.CancelKindBackfill(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, "cancelled; tags already written are kept, and a new start continues with what is pending")
		return nil
	}
	return errors.New(kindUsage)
}

func printKindEstimate(w io.Writer, e apitypes.KindBackfillEstimateResponse) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("kind backfill (%s", e.Mode)
	if e.Model != "" {
		p(", model %s", e.Model)
	}
	p("):\n  facts to tag:  %d\n", e.Pending)
	if e.Mode != "llm" {
		p("  no extractor configured: the free heuristic is used (it will leave most facts as \"other\")\n")
		return
	}
	p("  requests:      %d (~%ds at the configured rate)\n", e.Requests, e.EstimatedSecond)
	p("  tokens:        ~%d in, ~%d out (characters / 4, an approximation)\n", e.InputTokens, e.OutputTokens)
	if e.EstimatedCost != nil {
		p("  cost:          ~US$ %.4f\n", *e.EstimatedCost)
	} else {
		p("  cost:          unknown (set M3M0S_EXTRACT_PRICE_IN_PER_MTOK and M3M0S_EXTRACT_PRICE_OUT_PER_MTOK on the server to get one)\n")
	}
}
