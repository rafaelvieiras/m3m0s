package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/eval"
)

// runEval is `m3m0s eval <golden.json>`: it runs a golden dataset (D30)
// against the running server and prints recall@k, mean injected tokens and
// mean latency per ranking mode, so a ranking change can be compared with
// the previous one on the same data.
func runEval(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	ks := fs.String("k", "3,5", "comma-separated k values for recall@k")
	modes := fs.String("mode", "vector,keyword,hybrid", "comma-separated ranking modes to compare")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s eval <golden.json> [-k 3,5] [-mode vector,keyword,hybrid]`)
	}
	golden, err := eval.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	var kList []int
	for _, p := range strings.Split(*ks, ",") {
		k, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || k <= 0 {
			return fmt.Errorf("invalid -k value %q", p)
		}
		kList = append(kList, k)
	}

	c := client.NewFromEnv()
	search := func(ctx context.Context, q eval.Query, mode string, limit int) (eval.Result, error) {
		resp, err := c.Search(ctx, apitypes.SearchRequest{Query: q.Query, Domain: q.Domain, Mode: mode, Limit: limit})
		if err != nil {
			return eval.Result{}, err
		}
		ids := make([]string, len(resp.Results))
		for i, e := range resp.Results {
			ids[i] = e.ID
		}
		return eval.Result{IDs: ids, Tokens: resp.EstimatedTokens}, nil
	}

	fmt.Printf("%d golden queries\n\n%-9s", len(golden.Queries), "mode")
	for _, k := range kList {
		fmt.Printf(" recall@%-3d", k)
	}
	fmt.Printf(" %-12s %-10s %s\n", "mean tokens", "mean ms", "failed")
	for _, mode := range strings.Split(*modes, ",") {
		mode = strings.TrimSpace(mode)
		rep := eval.Run(ctx, golden, mode, append([]int(nil), kList...), search)
		fmt.Printf("%-9s", mode)
		for _, k := range kList {
			fmt.Printf(" %-10.3f", rep.Recall[k])
		}
		fmt.Printf(" %-12.0f %-10.0f %d\n", rep.MeanTokens, rep.MeanMillis, rep.Failed)
	}
	return nil
}
