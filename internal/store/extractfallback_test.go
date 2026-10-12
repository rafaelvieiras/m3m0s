package store

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
)

type fallbackFake struct {
	primary, fallback atomic.Int32
	primaryHangs      bool
}

func (f *fallbackFake) Extract(ctx context.Context, c, d string) ([]string, error) {
	facts, err := f.ExtractFacts(ctx, c, d)
	if err != nil {
		return nil, err
	}
	return []string{facts[0].Text}, nil
}

func (f *fallbackFake) ExtractFacts(ctx context.Context, _, _ string) ([]factkind.Fact, error) {
	f.primary.Add(1)
	if f.primaryHangs {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, errors.New("primary down")
}

func (f *fallbackFake) HasFallback() bool { return true }

func (f *fallbackFake) ExtractFactsFallback(context.Context, string, string) ([]factkind.Fact, error) {
	f.fallback.Add(1)
	return []factkind.Fact{{Text: "fact from fallback", Kind: "decision"}}, nil
}

// #65: when the primary model fails, the retry attempt goes to the fallback.
func TestExtractChunkRetriesWithFallbackModel(t *testing.T) {
	s := newTestStore(t)
	f := &fallbackFake{}
	s.SetExtractor(f)

	facts, err := s.extractChunk(context.Background(), "piece", "code:m3m0s")
	if err != nil || len(facts) != 1 || facts[0].Text != "fact from fallback" {
		t.Fatalf("facts=%v err=%v", facts, err)
	}
	if f.primary.Load() != 1 || f.fallback.Load() != 1 {
		t.Fatalf("primary=%d fallback=%d, want 1/1", f.primary.Load(), f.fallback.Load())
	}
}

// A primary that hangs may only use part of the budget, leaving room for the
// fallback inside the same total.
func TestExtractChunkHungPrimaryStillReachesFallback(t *testing.T) {
	s := newTestStore(t)
	orig := extractionChunkBudget
	extractionChunkBudget = 300 * time.Millisecond
	t.Cleanup(func() { extractionChunkBudget = orig })

	f := &fallbackFake{primaryHangs: true}
	s.SetExtractor(f)

	facts, err := s.extractChunk(context.Background(), "piece", "code:m3m0s")
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%v err=%v", facts, err)
	}
	if f.fallback.Load() != 1 {
		t.Fatalf("fallback calls = %d, want 1", f.fallback.Load())
	}
}
