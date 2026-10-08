package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecallAt(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	if got := RecallAt(ids, []string{"a", "d"}, 3); got != 0.5 {
		t.Errorf("recall@3 = %v, want 0.5", got)
	}
	if got := RecallAt(ids, []string{"a", "d"}, 4); got != 1 {
		t.Errorf("recall@4 = %v, want 1", got)
	}
	if got := RecallAt(nil, []string{"x"}, 5); got != 0 {
		t.Errorf("empty result = %v, want 0", got)
	}
}

func TestRunAveragesAndCountsFailures(t *testing.T) {
	g := Golden{Queries: []Query{
		{Query: "q1", Expect: []string{"a"}},
		{Query: "q2", Expect: []string{"b"}},
		{Query: "q3", Expect: []string{"c"}},
	}}
	search := func(_ context.Context, q Query, mode string, limit int) (Result, error) {
		switch q.Query {
		case "q1":
			return Result{IDs: []string{"a", "x"}, Tokens: 100}, nil
		case "q2":
			return Result{IDs: []string{"x", "b"}, Tokens: 200}, nil
		}
		return Result{}, errors.New("boom")
	}
	rep := Run(context.Background(), g, "hybrid", []int{1, 2}, search)
	if rep.Failed != 1 {
		t.Errorf("failed = %d, want 1", rep.Failed)
	}
	if rep.Recall[1] != 1.0/3 || rep.Recall[2] != 2.0/3 {
		t.Errorf("recall = %v", rep.Recall)
	}
	if want := (1.0 + 0.5) / 3; rep.MRR != want { // q1 rank 1, q2 rank 2, q3 failed
		t.Errorf("MRR = %v, want %v", rep.MRR, want)
	}
	if rep.MeanTokens != 100 { // (100+200+0)/3
		t.Errorf("mean tokens = %v", rep.MeanTokens)
	}
}

func TestLoadValidates(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	if _, err := Load(write("ok.json", `{"queries":[{"query":"x","domain":"d","expect":["a"]}]}`)); err != nil {
		t.Errorf("valid file rejected: %v", err)
	}
	for name, body := range map[string]string{"empty.json": `{"queries":[]}`, "noexpect.json": `{"queries":[{"query":"x"}]}`, "bad.json": `{`} {
		if _, err := Load(write(name, body)); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}
