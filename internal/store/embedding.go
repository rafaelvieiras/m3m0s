package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/embed"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// Runtime embedding configuration (#39, D36). The environment (OPENAI_API_KEY,
// M3M0S_OPENAI_BASE_URL) still bootstraps a server that never saved a
// configuration; once the admin saves one, the database row wins.

// embedder is the live embedding provider plus the vector space it produces.
type embedder struct {
	fn    embed.Func
	batch embed.BatchFunc // nil = embed one text at a time with fn (re-embedding job)
	model string
	dims  int // vector length, 0 = not known yet (learned from existing rows or the first vector)
}

func (e *embedder) space() sqlstore.EmbeddingSpace {
	return sqlstore.EmbeddingSpace{Model: e.model, Dims: e.dims}
}

// embedState is the configuration side of the store, guarded by embMu.
type embedState struct {
	mu      sync.RWMutex
	emb     *embedder // nil = keyword-only
	cfg     EmbeddingSettings
	envBase string
	envKey  string
}

// EmbeddingSettings is the admin-editable embedding configuration. The API
// key is never part of it: see EmbeddingUpdate and EmbeddingStatus.HasAPIKey.
type EmbeddingSettings struct {
	Enabled    bool
	BaseURL    string // empty = the environment/default endpoint
	Model      string
	Dimensions int // requested "dimensions" parameter, 0 = provider default
}

// EmbeddingUpdate is a requested change. APIKey nil keeps the saved key; a
// pointer to "" clears it (the environment key applies again).
type EmbeddingUpdate struct {
	EmbeddingSettings
	APIKey *string
}

// EmbeddingStatus describes the active configuration and the state of the
// stored vectors.
type EmbeddingStatus struct {
	EmbeddingSettings
	Source     string // "database" or "environment"
	HasAPIKey  bool
	KeySource  string // "database", "environment" or "none"
	VectorDims int    // 0 = not known yet
	Active     bool   // vector search is running
	Total      int    // stored vectors
	Current    int    // vectors from the active model/dimensions
	Stale      int    // vectors from another model/dimensions, to re-embed
	UpdatedAt  time.Time
	Reembed    ReembedProgress // state of the re-embedding job
}

// ErrEmbeddingInvalid is wrapped by every rejected configuration; the active
// configuration is left untouched.
var ErrEmbeddingInvalid = errors.New("invalid embedding configuration")

// ReembedRequiredError means the change would leave Stale stored vectors
// unusable until they are re-embedded; repeat with confirmation.
type ReembedRequiredError struct{ Stale int }

func (e *ReembedRequiredError) Error() string {
	return fmt.Sprintf("changing model or dimensions leaves %d stored vectors to re-embed; confirm to proceed", e.Stale)
}

func normalizeBase(b string) string { return strings.TrimRight(strings.TrimSpace(b), "/") }

// loadEmbedding resolves the configuration at boot (database row, else
// environment) and builds the embedder. Called from New before the index load.
func (s *MemoryStore) loadEmbedding(ctx context.Context, envKey, envBase string) error {
	st := &s.emb
	st.envKey, st.envBase = envKey, envBase
	st.cfg = EmbeddingSettings{Enabled: envKey != "", Model: sqlstore.DefaultEmbeddingModel}
	dims := 0
	row, saved, err := s.db.GetEmbeddingConfig(ctx)
	if err != nil {
		return err
	}
	key := envKey
	if saved {
		st.cfg = EmbeddingSettings{Enabled: row.Enabled, BaseURL: row.BaseURL, Model: row.Model, Dimensions: row.Dimensions}
		dims = row.VectorDims
		if row.APIKey != "" {
			key = row.APIKey
		}
	} else if counts, err := s.db.EmbeddingSpaceCounts(ctx); err == nil {
		// No saved config: learn the dimension from the vectors already stored
		// for the default model, so the index does not depend on the first
		// vector it happens to see.
		best := 0
		for sp, n := range counts {
			if sp.Model == st.cfg.Model && n > best {
				best, dims = n, sp.Dims
			}
		}
	}
	if st.cfg.Enabled && key != "" {
		base := s.baseFor(st.cfg)
		st.emb = &embedder{fn: embed.New(base, key, st.cfg.Model, st.cfg.Dimensions), batch: embed.NewBatch(base, key, st.cfg.Model, st.cfg.Dimensions), model: st.cfg.Model, dims: dims}
	}
	return nil
}

func (s *MemoryStore) baseFor(c EmbeddingSettings) string {
	if b := normalizeBase(c.BaseURL); b != "" {
		return b
	}
	if s.emb.envBase != "" {
		return s.emb.envBase
	}
	return embed.DefaultBaseURL
}

func (s *MemoryStore) currentEmbedder() *embedder {
	s.emb.mu.RLock()
	defer s.emb.mu.RUnlock()
	return s.emb.emb
}

// loadVectors rebuilds the int8 index from the stored vectors of the active
// space. Nothing is indexed when the server runs keyword-only.
func (s *MemoryStore) loadVectors(ctx context.Context) error {
	e := s.currentEmbedder()
	if e == nil {
		return nil
	}
	if n, err := s.db.CountEmbeddings(ctx, e.space()); err == nil {
		s.vecs.reserve(n)
	}
	if err := s.db.ForEachEmbedding(ctx, e.space(), s.vecs.upsert); err != nil {
		return fmt.Errorf("load vector index: %w", err)
	}
	return nil
}

// EmbeddingModel is the active model name (the configured one even while
// disabled), for GET /stats and the startup log.
func (s *MemoryStore) EmbeddingModel() string {
	s.emb.mu.RLock()
	defer s.emb.mu.RUnlock()
	return s.emb.cfg.Model
}

// EmbeddingStatus reports the active configuration and vector counts.
func (s *MemoryStore) EmbeddingStatus(ctx context.Context) (EmbeddingStatus, error) {
	s.emb.mu.RLock()
	cfg, e, envKey := s.emb.cfg, s.emb.emb, s.emb.envKey
	s.emb.mu.RUnlock()

	row, saved, err := s.db.GetEmbeddingConfig(ctx)
	if err != nil {
		return EmbeddingStatus{}, err
	}
	out := EmbeddingStatus{Reembed: s.ReembedStatus(), EmbeddingSettings: cfg, Source: "environment", KeySource: "none", Active: e != nil}
	switch {
	case saved && row.APIKey != "":
		out.HasAPIKey, out.KeySource = true, "database"
	case envKey != "":
		out.HasAPIKey, out.KeySource = true, "environment"
	}
	if saved {
		out.Source, out.UpdatedAt = "database", row.UpdatedAt
	}
	if e != nil {
		out.VectorDims = e.dims
	} else if saved {
		out.VectorDims = row.VectorDims
	}
	counts, err := s.db.EmbeddingSpaceCounts(ctx)
	if err != nil {
		return EmbeddingStatus{}, err
	}
	for sp, n := range counts {
		out.Total += n
		if sp.Model == cfg.Model && (out.VectorDims == 0 || sp.Dims == out.VectorDims) {
			out.Current += n
		}
	}
	out.Stale = out.Total - out.Current
	return out, nil
}

// SetEmbeddingSettings validates and applies a new configuration. A real
// test embedding call is made first; on any failure the active configuration
// stays as it was. When the change leaves stored vectors from another
// model/dimensions behind, confirmReembed must be true.
func (s *MemoryStore) SetEmbeddingSettings(ctx context.Context, in EmbeddingUpdate, confirmReembed bool) (EmbeddingStatus, error) {
	in.Model = strings.TrimSpace(in.Model)
	in.BaseURL = normalizeBase(in.BaseURL)
	if in.Model == "" {
		return EmbeddingStatus{}, fmt.Errorf("%w: model is required", ErrEmbeddingInvalid)
	}
	if in.Dimensions < 0 {
		return EmbeddingStatus{}, fmt.Errorf("%w: dimensions must be >= 0", ErrEmbeddingInvalid)
	}

	row, saved, err := s.db.GetEmbeddingConfig(ctx)
	if err != nil {
		return EmbeddingStatus{}, err
	}
	key := ""
	if saved {
		key = row.APIKey
	}
	if in.APIKey != nil {
		key = strings.TrimSpace(*in.APIKey)
	}
	effectiveKey := key
	if effectiveKey == "" {
		effectiveKey = s.emb.envKey
	}

	vectorDims := 0
	var fn embed.Func
	var batch embed.BatchFunc
	if in.Enabled {
		if effectiveKey == "" {
			return EmbeddingStatus{}, fmt.Errorf("%w: an API key is required to enable embeddings", ErrEmbeddingInvalid)
		}
		base := s.baseFor(in.EmbeddingSettings)
		fn = embed.New(base, effectiveKey, in.Model, in.Dimensions)
		batch = embed.NewBatch(base, effectiveKey, in.Model, in.Dimensions)
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		vec, err := fn(probeCtx, "m3m0s embedding configuration check")
		if err != nil {
			return EmbeddingStatus{}, fmt.Errorf("%w: test call failed: %v", ErrEmbeddingInvalid, err)
		}
		if in.Dimensions > 0 && len(vec) != in.Dimensions {
			return EmbeddingStatus{}, fmt.Errorf("%w: provider returned %d dimensions, %d requested", ErrEmbeddingInvalid, len(vec), in.Dimensions)
		}
		vectorDims = len(vec)

		counts, err := s.db.EmbeddingSpaceCounts(ctx)
		if err != nil {
			return EmbeddingStatus{}, err
		}
		stale := 0
		for sp, n := range counts {
			if sp.Model != in.Model || sp.Dims != vectorDims {
				stale += n
			}
		}
		if stale > 0 && !confirmReembed {
			return EmbeddingStatus{}, &ReembedRequiredError{Stale: stale}
		}
	} else if saved {
		vectorDims = row.VectorDims
	}

	if err := s.db.SetEmbeddingConfig(ctx, sqlstore.EmbeddingConfigRow{
		Enabled: in.Enabled, BaseURL: in.BaseURL, APIKey: key, Model: in.Model, Dimensions: in.Dimensions, VectorDims: vectorDims,
	}); err != nil {
		return EmbeddingStatus{}, err
	}

	s.emb.mu.Lock()
	s.emb.cfg = in.EmbeddingSettings
	if in.Enabled {
		s.emb.emb = &embedder{fn: fn, batch: batch, model: in.Model, dims: vectorDims}
	} else {
		s.emb.emb = nil
	}
	s.emb.mu.Unlock()

	s.vecs.reset(vectorDims)
	if err := s.loadVectors(ctx); err != nil {
		return EmbeddingStatus{}, err
	}
	return s.EmbeddingStatus(ctx)
}
