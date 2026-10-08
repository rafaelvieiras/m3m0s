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
	"os/signal"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// embeddingEnv carries what the `embedding` subcommands need from the process,
// so tests can drive them without touching real stdin/stdout/environment.
type embeddingEnv struct {
	c           *client.Client
	in          io.Reader
	out, errOut io.Writer
	getenv      func(string) string
	interactive bool // stdin is a terminal: a confirmation prompt can be answered
	pollEvery   time.Duration
}

// runEmbedding is `m3m0s embedding`: the runtime embedding configuration
// (D36) and the re-embedding job.
//
//	m3m0s embedding show
//	m3m0s embedding set [--model M] [--base-url U] [--dimensions N] [--enable|--disable]
//	                    [--api-key-stdin | --api-key-env VAR | --clear-api-key]
//	                    [--confirm-reembed|--yes]
//	m3m0s embedding estimate [--model M] [--base-url U] [--dimensions N]
//	m3m0s embedding reembed start|status|cancel [--watch]
//
// The API key is never accepted as an argument (it would leak into shell
// history and `ps`) and is never printed.
func runEmbedding(ctx context.Context, args []string) error {
	interactive := false
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		// /dev/null is a character device too, but nobody can answer there.
		null, nerr := os.Stat(os.DevNull)
		interactive = nerr != nil || !os.SameFile(fi, null)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	return embeddingMain(ctx, &embeddingEnv{
		c: client.NewFromEnv(), in: os.Stdin, out: os.Stdout, errOut: os.Stderr,
		getenv: os.Getenv, interactive: interactive, pollEvery: 2 * time.Second,
	}, args)
}

const embeddingUsage = `usage: m3m0s embedding <show|set|estimate|reembed> [flags]
  show                     current configuration, vector counts and re-embedding state
  set [flags]              change the configuration (see "m3m0s embedding set -h")
  estimate [flags]         approximate size/cost of re-embedding for a proposed configuration
  reembed start|status|cancel [--watch]`

func embeddingMain(ctx context.Context, e *embeddingEnv, args []string) error {
	err := embeddingDispatch(ctx, e, args)
	if errors.Is(err, flag.ErrHelp) {
		return nil // usage was already printed
	}
	return err
}

func embeddingDispatch(ctx context.Context, e *embeddingEnv, args []string) error {
	if len(args) == 0 {
		return errors.New(embeddingUsage)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "show":
		return e.show(ctx, rest)
	case "set":
		return e.set(ctx, rest)
	case "estimate":
		return e.estimate(ctx, rest)
	case "reembed":
		return e.reembed(ctx, rest)
	default:
		return fmt.Errorf("unknown embedding command %q\n%s", cmd, embeddingUsage)
	}
}

func (e *embeddingEnv) flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("embedding "+name, flag.ContinueOnError)
	fs.SetOutput(e.errOut)
	return fs
}

func (e *embeddingEnv) printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(e.out, string(b))
	return err
}

func (e *embeddingEnv) show(ctx context.Context, args []string) error {
	fs := e.flagSet("show")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := e.c.GetEmbeddingConfig(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return e.printJSON(cfg)
	}
	e.printConfig(cfg)
	return nil
}

func (e *embeddingEnv) printConfig(cfg apitypes.EmbeddingConfigResponse) {
	w := e.out
	fmt.Fprintf(w, "enabled:      %v (vector search %s)\n", cfg.Enabled, map[bool]string{true: "active", false: "inactive"}[cfg.Active])
	base := cfg.BaseURL
	if base == "" {
		base = "(default / environment)"
	}
	fmt.Fprintf(w, "base URL:     %s\n", base)
	fmt.Fprintf(w, "model:        %s\n", cfg.Model)
	fmt.Fprintf(w, "dimensions:   %s (vector size reported by the provider: %s)\n", dimsLabel(cfg.Dimensions), dimsLabel(cfg.VectorDims))
	key := "not set"
	if cfg.HasAPIKey {
		key = "set (from " + cfg.KeySource + ")"
	}
	fmt.Fprintf(w, "API key:      %s\n", key)
	fmt.Fprintf(w, "configured:   %s\n", cfg.Source)
	fmt.Fprintf(w, "vectors:      %d total, %d current, %d pending re-embedding\n", cfg.Vectors.Total, cfg.Vectors.Current, cfg.Vectors.Stale)
	if cfg.Vectors.Stale == 0 && cfg.Vectors.Total > 0 {
		fmt.Fprintln(w, "              everything is up to date")
	}
	if cfg.Reembed.Running || cfg.Reembed.StartedAt != nil {
		fmt.Fprintf(w, "re-embedding: %s\n", progressLine(cfg.Reembed))
	}
}

func dimsLabel(n int) string {
	if n <= 0 {
		return "provider default"
	}
	return fmt.Sprint(n)
}

func progressLine(p apitypes.ReembedProgress) string {
	state := "idle"
	if p.Running {
		state = "running"
	} else if p.FinishedAt != nil {
		state = "finished"
	}
	s := fmt.Sprintf("%s, %d/%d done, %d errors", state, p.Done, p.Total, p.Errors)
	if p.Running && p.ETASeconds > 0 {
		s += ", ETA " + (time.Duration(p.ETASeconds) * time.Second).String()
	}
	if p.LastError != "" {
		s += " (last error: " + p.LastError + ")"
	}
	return s
}

func (e *embeddingEnv) printEstimate(est apitypes.ReembedEstimateResponse) {
	w := e.out
	fmt.Fprintf(w, "re-embedding with %s (%s):\n", est.Model, dimsLabel(est.Dimensions))
	fmt.Fprintf(w, "  rows to process:  %d\n", est.Pending)
	fmt.Fprintf(w, "  characters:       %d\n", est.Characters)
	fmt.Fprintf(w, "  tokens:           ~%d (characters / 4, an approximation)\n", est.EstimatedTokens)
	if est.EstimatedCostUSD != nil && est.PricePerMTokUSD != nil {
		fmt.Fprintf(w, "  cost:             ~US$ %.4f at US$ %.2f per 1M tokens (list price as of %s)\n", *est.EstimatedCostUSD, *est.PricePerMTokUSD, est.PricesAsOf)
	} else {
		fmt.Fprintln(w, "  cost:             unknown (no known list price for this model/endpoint)")
	}
}

func (e *embeddingEnv) estimate(ctx context.Context, args []string) error {
	fs := e.flagSet("estimate")
	model := fs.String("model", "", "proposed model (default: the active one)")
	baseURL := fs.String("base-url", "", "proposed endpoint (default: the active one)")
	dims := fs.Int("dimensions", 0, "proposed dimensions (0 = model default)")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	est, err := e.c.EstimateReembed(ctx, apitypes.ReembedEstimateRequest{Model: *model, BaseURL: *baseURL, Dimensions: *dims})
	if err != nil {
		return err
	}
	if *jsonOut {
		return e.printJSON(est)
	}
	e.printEstimate(est)
	return nil
}

// readKey returns the key from stdin or the named environment variable. The
// value is trimmed of surrounding whitespace and never echoed.
func (e *embeddingEnv) readKey(fromStdin bool, envVar string) (*string, error) {
	switch {
	case fromStdin && envVar != "":
		return nil, errors.New("use only one of --api-key-stdin and --api-key-env")
	case fromStdin:
		b, err := io.ReadAll(io.LimitReader(e.in, 64<<10))
		if err != nil {
			return nil, fmt.Errorf("read API key from stdin: %w", err)
		}
		k := strings.TrimSpace(string(b))
		if k == "" {
			return nil, errors.New("--api-key-stdin: stdin is empty")
		}
		return &k, nil
	case envVar != "":
		k := strings.TrimSpace(e.getenv(envVar))
		if k == "" {
			return nil, fmt.Errorf("--api-key-env: environment variable %s is empty or unset", envVar)
		}
		return &k, nil
	}
	return nil, nil
}

func (e *embeddingEnv) set(ctx context.Context, args []string) error {
	fs := e.flagSet("set")
	model := fs.String("model", "", "embedding model")
	baseURL := fs.String("base-url", "", "OpenAI-compatible embeddings endpoint (empty keeps the current one)")
	dims := fs.Int("dimensions", -1, "requested vector size (0 = model default; omit to keep)")
	enable := fs.Bool("enable", false, "enable vector search")
	disable := fs.Bool("disable", false, "disable vector search (keyword-only)")
	keyStdin := fs.Bool("api-key-stdin", false, "read the new API key from stdin")
	keyEnv := fs.String("api-key-env", "", "read the new API key from this environment variable")
	clearKey := fs.Bool("clear-api-key", false, "remove the saved key (the server environment key then applies)")
	confirm := fs.Bool("confirm-reembed", false, "accept re-embedding the stored vectors if the change requires it")
	yes := fs.Bool("yes", false, "same as --confirm-reembed, and skip the prompt")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		// Never echo the value: a positional argument here may well be a key.
		return errors.New("unexpected positional argument (the API key is never taken from the command line; use --api-key-stdin or --api-key-env)")
	}
	if *enable && *disable {
		return errors.New("use only one of --enable and --disable")
	}
	if *clearKey && (*keyStdin || *keyEnv != "") {
		return errors.New("--clear-api-key cannot be combined with a new key")
	}
	key, err := e.readKey(*keyStdin, *keyEnv)
	if err != nil {
		return err
	}

	cur, err := e.c.GetEmbeddingConfig(ctx)
	if err != nil {
		return err
	}
	req := apitypes.EmbeddingConfigRequest{
		Enabled: cur.Enabled, BaseURL: cur.BaseURL, Model: cur.Model, Dimensions: cur.Dimensions, APIKey: key,
	}
	if *model != "" {
		req.Model = *model
	}
	if *baseURL != "" {
		req.BaseURL = *baseURL
	}
	if *dims >= 0 {
		req.Dimensions = *dims
	}
	if *enable {
		req.Enabled = true
	}
	if *disable {
		req.Enabled = false
	}
	if *clearKey {
		empty := ""
		req.APIKey = &empty
	}
	req.ConfirmReembed = *confirm || *yes

	resp, err := e.c.PutEmbeddingConfig(ctx, req)
	var apiErr apitypes.ErrorBody
	if errors.As(err, &apiErr) && apiErr.ErrorInfo.Code == "reembed_required" {
		resp, err = e.confirmAndRetry(ctx, req, *yes, *keyStdin)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return e.printJSON(resp)
	}
	fmt.Fprintln(e.out, "configuration saved")
	e.printConfig(resp)
	if resp.ReembedStarted {
		fmt.Fprintln(e.out, "re-embedding started; follow it with: m3m0s embedding reembed status --watch")
	}
	return nil
}

// confirmAndRetry handles the 409: show how many vectors are stale and the
// approximate cost, then ask (or, without a terminal, refuse and say how to
// confirm).
func (e *embeddingEnv) confirmAndRetry(ctx context.Context, req apitypes.EmbeddingConfigRequest, yes, stdinUsed bool) (apitypes.EmbeddingConfigResponse, error) {
	var zero apitypes.EmbeddingConfigResponse
	cur, err := e.c.GetEmbeddingConfig(ctx)
	if err != nil {
		return zero, err
	}
	est, err := e.c.EstimateReembed(ctx, apitypes.ReembedEstimateRequest{Model: req.Model, BaseURL: req.BaseURL, Dimensions: req.Dimensions})
	if err != nil {
		return zero, err
	}
	fmt.Fprintf(e.errOut, "This change makes the stored vectors unusable until they are re-embedded (%d of %d memories would need a new vector).\n", est.Pending, cur.Vectors.Total)
	saved := e.out
	e.out = e.errOut
	e.printEstimate(est)
	e.out = saved
	if !yes {
		if !e.interactive || stdinUsed {
			return zero, errors.New("re-embedding must be confirmed: repeat with --confirm-reembed (or --yes)")
		}
		fmt.Fprint(e.errOut, "Apply the change and start re-embedding? [y/N] ")
		line, _ := bufio.NewReader(e.in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return zero, errors.New("aborted: nothing was changed")
		}
	}
	req.ConfirmReembed = true
	return e.c.PutEmbeddingConfig(ctx, req)
}

func (e *embeddingEnv) reembed(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: m3m0s embedding reembed <start|status|cancel> [--watch] [--json]")
	}
	sub, rest := args[0], args[1:]
	fs := e.flagSet("reembed " + sub)
	watch := fs.Bool("watch", false, "follow the progress until the run ends")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	switch sub {
	case "start":
		if _, err := e.c.StartReembed(ctx); err != nil {
			return err
		}
		fmt.Fprintln(e.out, "re-embedding started (cancel any time with: m3m0s embedding reembed cancel)")
		if *watch {
			return e.watchProgress(ctx, *jsonOut)
		}
		return nil
	case "status":
		if *watch {
			return e.watchProgress(ctx, *jsonOut)
		}
		p, err := e.c.ReembedStatus(ctx)
		if err != nil {
			return err
		}
		if *jsonOut {
			return e.printJSON(p)
		}
		fmt.Fprintln(e.out, progressLine(p))
		return nil
	case "cancel":
		if _, err := e.c.CancelReembed(ctx); err != nil {
			return err
		}
		fmt.Fprintln(e.out, "cancelled; vectors already written are kept, and a new start continues from what is pending")
		return nil
	default:
		return fmt.Errorf("unknown reembed command %q (expected start, status or cancel)", sub)
	}
}

// watchProgress polls until the run ends. A short grace period tolerates the
// moment between the start request and the job reporting itself as running.
func (e *embeddingEnv) watchProgress(ctx context.Context, jsonOut bool) error {
	idle := 0
	for {
		p, err := e.c.ReembedStatus(ctx)
		if err != nil {
			return err
		}
		if jsonOut {
			if err := e.printJSON(p); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(e.out, progressLine(p))
		}
		if p.Running {
			idle = 0
		} else if idle++; idle >= 3 || p.FinishedAt != nil {
			if p.LastError != "" {
				return fmt.Errorf("re-embedding stopped: %s", p.LastError)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(e.errOut, "stopped watching (the job keeps running on the server)")
			return nil
		case <-time.After(e.pollEvery):
		}
	}
}
