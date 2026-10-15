package store

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/fuzzy"
)

// vocabTTL is how long the query-correction vocabulary is reused before it
// is rebuilt from the keyword index (D51). New names enter within minutes;
// building it is a single scan of the term list.
const vocabTTL = 5 * time.Minute

type vocabState struct {
	mu      sync.Mutex
	vocab   *fuzzy.Vocabulary
	builtAt time.Time
}

// vocabulary returns the (cached) correction vocabulary, or nil when it
// cannot be built (the search then simply runs uncorrected).
func (s *MemoryStore) vocabulary(ctx context.Context) *fuzzy.Vocabulary {
	s.vocabState.mu.Lock()
	defer s.vocabState.mu.Unlock()
	if s.vocabState.vocab != nil && time.Since(s.vocabState.builtAt) < vocabTTL {
		return s.vocabState.vocab
	}
	df, err := s.db.FTSVocabulary(ctx)
	if err != nil {
		log.Printf("m3m0s: query-correction vocabulary unavailable: %v", err)
		return s.vocabState.vocab
	}
	// Project and domain names are the terms people misspell most; they are
	// usually in the content too, but count them so a quiet project is known.
	s.mu.RLock()
	for _, e := range s.index {
		for _, w := range fuzzy.Words(e.Domain) {
			if len([]rune(w)) >= fuzzy.Correctable {
				df[w]++
			}
		}
	}
	s.mu.RUnlock()
	s.vocabState.vocab = fuzzy.NewVocabulary(df)
	s.vocabState.builtAt = time.Now()
	return s.vocabState.vocab
}

// correctQuery repairs misspelled terms of a search query. It returns the
// corrected query (equal to query when nothing changed) and the
// substitutions made.
func (s *MemoryStore) correctQuery(ctx context.Context, query string) (string, map[string]string) {
	v := s.vocabulary(ctx)
	if v == nil {
		return query, nil
	}
	return v.Query(query)
}
