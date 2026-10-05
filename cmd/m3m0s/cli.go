package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// stringSlice implements flag.Value to allow repeatable flags
// (--tag foo --tag bar), which the stdlib "flag" package does not natively support.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// reorderFlagsFirst rearranges args to put all flags before the positional
// arguments, preserving the relative order within each group. Needed
// because the stdlib "flag" package stops recognizing flags as soon as it
// finds the first argument that does not start with "-" (documented
// flag.Parse behavior) — and the CLI syntax described in
// docs/api-contract.md puts the positional (content/query) BEFORE the
// flags, e.g. `m3m0s add "<content>" --domain codigo:m3m0s`. Without this,
// `fs.Parse` would never see "--domain" in that example. boolFlags lists
// the flag names (without "-") that do not consume a separate value.
func reorderFlagsFirst(args []string, boolFlags ...string) []string {
	isBool := make(map[string]bool, len(boolFlags))
	for _, f := range boolFlags {
		isBool[f] = true
	}

	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") || isBool[name] {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func runAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	domain := fs.String("domain", "", "domain in the format category:instance (required)")
	source := fs.String("source", "", "originating tool (default: manual)")
	agent := fs.String("agent", "", "optional named agent identity")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	var tags stringSlice
	fs.Var(&tags, "tag", "tag (repeatable)")
	var metaPairs stringSlice
	fs.Var(&metaPairs, "meta", "key=value pair (repeatable)")
	if err := fs.Parse(reorderFlagsFirst(args, "json")); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s add "<content>" --domain category:instance [flags]`)
	}
	if *domain == "" {
		return errors.New("--domain is required")
	}

	metadata, err := parseMetaPairs(metaPairs)
	if err != nil {
		return err
	}

	resp, err := client.NewFromEnv().Add(ctx, apitypes.AddRequest{
		Content:  fs.Arg(0),
		Domain:   *domain,
		Source:   *source,
		Agent:    *agent,
		Tags:     tags,
		Metadata: metadata,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(resp)
	}
	extractedStr := "no"
	if resp.Extracted {
		extractedStr = "yes"
	}
	fmt.Printf("%d memory(ies) stored (extraction: %s)\n", len(resp.Entries), extractedStr)
	for _, e := range resp.Entries {
		dedupedStr := "no"
		if e.Deduped {
			dedupedStr = "yes"
		}
		fmt.Printf("  id=%s deduped=%s domain=%s\n", e.Entry.ID, dedupedStr, e.Entry.Domain)
	}
	return nil
}

func runSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	limit := fs.Int("limit", 5, "maximum number of results")
	domain := fs.String("domain", "", "exact domain filter")
	domainCategory := fs.String("domain-category", "", "exact domain_category filter")
	source := fs.String("source", "", "exact source filter")
	agent := fs.String("agent", "", "exact agent filter")
	mode := fs.String("mode", "", "ranking: hybrid (default), vector or keyword")
	maxTokens := fs.Int("max-tokens", 0, "cap the estimated tokens of the results (0 = no cap)")
	jsonOut := fs.Bool("json", false, "print raw results as JSON instead of the contextBlock")
	var tags stringSlice
	fs.Var(&tags, "tag", "post-filter by tag, AND across repetitions")
	if err := fs.Parse(reorderFlagsFirst(args, "json")); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s search "<query>" [flags]`)
	}

	resp, err := client.NewFromEnv().Search(ctx, apitypes.SearchRequest{
		Query:          fs.Arg(0),
		Limit:          *limit,
		Domain:         *domain,
		DomainCategory: *domainCategory,
		Source:         *source,
		Agent:          *agent,
		Tags:           tags,
		Mode:           *mode,
		MaxTokens:      *maxTokens,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(resp.Results)
	}
	fmt.Println(resp.ContextBlock)
	return nil
}

func runRecent(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("recent", flag.ExitOnError)
	limit := fs.Int("limit", 10, "maximum number of results")
	domain := fs.String("domain", "", "exact domain filter")
	domainCategory := fs.String("domain-category", "", "exact domain_category filter")
	source := fs.String("source", "", "exact source filter")
	agent := fs.String("agent", "", "exact agent filter")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resp, err := client.NewFromEnv().Recent(ctx, apitypes.RecentParams{
		Limit:          *limit,
		Domain:         *domain,
		DomainCategory: *domainCategory,
		Source:         *source,
		Agent:          *agent,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(resp.Results)
	}
	if len(resp.Results) == 0 {
		fmt.Println("(no memories found)")
		return nil
	}
	for _, e := range resp.Results {
		snippet := truncate(e.Content, 100)
		fmt.Printf("%s  %-30s  %-14s  %s\n", e.ID, e.Domain, e.Source, snippet)
	}
	return nil
}

// runUpdate implements `m3m0s update <id> [flags]` (D13/docs/api-contract.md,
// memory_update section). The stdlib "flag" package does not distinguish
// "flag not passed" from "flag passed with an empty value" — so the fields
// that can be cleared (tags/metadata/agent) get a dedicated boolean clear
// flag, mutually exclusive with the corresponding value flag, and the
// replace-only fields (content/domain/source, never "clearable") use
// fs.Visit to know whether they were actually passed before including the
// key in the PATCH body.
func runUpdate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	content := fs.String("content", "", "new content, replaces the current one (re-embeds)")
	domain := fs.String("domain", "", "new domain in the format category:instance")
	source := fs.String("source", "", "new source")
	agent := fs.String("agent", "", "new agent")
	clearTags := fs.Bool("clear-tags", false, "remove all tags (mutually exclusive with --tag)")
	clearMetadata := fs.Bool("clear-metadata", false, "remove all metadata (mutually exclusive with --meta)")
	clearAgent := fs.Bool("clear-agent", false, "remove the agent (mutually exclusive with --agent)")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	var tags stringSlice
	fs.Var(&tags, "tag", "tag (repeatable) — replaces the entire set of tags")
	var metaPairs stringSlice
	fs.Var(&metaPairs, "meta", "key=value pair (repeatable) — replaces the entire metadata")
	if err := fs.Parse(reorderFlagsFirst(args, "json", "clear-tags", "clear-metadata", "clear-agent")); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s update <id> [--content ...] [--domain ...] [--source ...] [--agent ...] [--tag ... --tag ...] [--meta key=value ...] [--clear-tags] [--clear-metadata] [--clear-agent]`)
	}
	id := fs.Arg(0)

	if *clearTags && len(tags) > 0 {
		return errors.New("--clear-tags and --tag are mutually exclusive")
	}
	if *clearMetadata && len(metaPairs) > 0 {
		return errors.New("--clear-metadata and --meta are mutually exclusive")
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if *clearAgent && visited["agent"] {
		return errors.New("--clear-agent and --agent are mutually exclusive")
	}

	var req apitypes.UpdateRequest
	if visited["content"] {
		req.Content = rawJSON(*content)
	}
	if visited["domain"] {
		req.Domain = rawJSON(*domain)
	}
	if visited["source"] {
		req.Source = rawJSON(*source)
	}
	switch {
	case *clearAgent:
		req.Agent = json.RawMessage("null")
	case visited["agent"]:
		req.Agent = rawJSON(*agent)
	}
	switch {
	case *clearTags:
		req.Tags = json.RawMessage("[]")
	case len(tags) > 0:
		req.Tags = rawJSON([]string(tags))
	}
	switch {
	case *clearMetadata:
		req.Metadata = json.RawMessage("{}")
	case len(metaPairs) > 0:
		metadata, err := parseMetaPairs(metaPairs)
		if err != nil {
			return err
		}
		req.Metadata = rawJSON(metadata)
	}

	if !req.HasAnyField() {
		return errors.New("no fields to update: provide at least one flag (--content/--domain/--source/--agent/--tag/--meta/--clear-tags/--clear-metadata/--clear-agent)")
	}

	entry, err := client.NewFromEnv().Update(ctx, id, req)
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(entry)
	}
	fmt.Printf("id=%s domain=%s updatedAt=%s\n", entry.ID, entry.Domain, entry.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Println(truncate(entry.Content, 200))
	return nil
}

// rawJSON marshals v (always a simple type here: string, []string or
// map[string]string) into json.RawMessage — it never fails in these cases,
// so it is not worth propagating an error to the caller.
func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// runGet implements `m3m0s get <id> [--json]` (a later elaboration after
// Phase 5/D13, docs/api-contract.md, memory_get section) — a pure read, with
// no edit flags (that is `m3m0s update`).
func runGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(reorderFlagsFirst(args, "json")); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s get <id> [--json]`)
	}
	id := fs.Arg(0)

	entry, err := client.NewFromEnv().Get(ctx, id)
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(entry)
	}
	fmt.Printf("id=%s domain=%s updatedAt=%s\n", entry.ID, entry.Domain, entry.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Println(truncate(entry.Content, 200))
	return nil
}

func runStats(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	resp, err := client.NewFromEnv().Stats(ctx)
	if err != nil {
		return err
	}
	return printJSON(resp)
}

// runForgetSweep implements `m3m0s forget-sweep [--dry-run] [--json]` (D22,
// docs/decisions/ — closes A9): automatic decay/forgetting of cold extracted
// facts. --dry-run only reports candidates, without changing anything.
func runForgetSweep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("forget-sweep", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "only report candidates, change nothing")
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(reorderFlagsFirst(args, "dry-run", "json")); err != nil {
		return err
	}

	resp, err := client.NewFromEnv().ForgetSweep(ctx, apitypes.ForgetSweepRequest{DryRun: *dryRun})
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(resp)
	}
	if resp.DryRun {
		fmt.Printf("dry-run: %d evaluated, %d soft-delete candidate(s), %d hard-delete candidate(s)\n",
			resp.Evaluated, len(resp.SoftDeleteCandidates), len(resp.HardDeleteCandidates))
	} else {
		fmt.Printf("%d evaluated, %d soft-deleted, %d hard-deleted\n",
			resp.Evaluated, len(resp.SoftDeleted), len(resp.HardDeleted))
	}
	return nil
}

// runRestore implements `m3m0s restore <id> [--json]` (D22) — reverts a
// soft-delete within the grace window.
func runRestore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response as JSON")
	if err := fs.Parse(reorderFlagsFirst(args, "json")); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s restore <id> [--json]`)
	}
	id := fs.Arg(0)

	entry, err := client.NewFromEnv().Restore(ctx, id)
	if err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(entry)
	}
	fmt.Printf("restored: id=%s domain=%s\n", entry.ID, entry.Domain)
	return nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func parseMetaPairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --meta (expected key=value): %q", p)
		}
		out[k] = v
	}
	return out, nil
}

func truncate(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "..."
}
