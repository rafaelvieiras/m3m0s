// Package store implements the MemoryStore: the only layer that talks to
// the database (D3 requires a single central storage service).
//
// Since D28 (docs/decisions/) the backend is SQLite (internal/sqlstore, with
// versioned migrations via goose) — it replaces the 6 hand-rolled JSON files
// and the chromem-go that existed before. It also keeps its own in-memory
// index (same role it had before D28) that is the source of truth for
// /recent and /stats: it fixes the original memory-server bug, where /recent
// read from a cache capped at 500 entries that could drift out of sync with
// the real index. Here recent/stats always reflect exactly what was
// inserted, because the in-memory index is reloaded from SQLite at boot and
// updated in the same critical section as every write.
//
// Vector search: no ANN index nor native extension like sqlite-vec (D28 —
// it would require cgo, breaking the static binary the Dockerfile builds
// with CGO_ENABLED=0 for a small ARM host, D2/D4). Each entry's embedding
// lives in a BLOB column; cosine similarity (similarity.go) is computed in
// Go over the set already filtered by domain — the same brute-force
// strategy chromem-go already used, suited to m3m0s's personal-scale
// volume.
package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/ingestfilter"
	"github.com/rafaelvieiras/m3m0s/internal/redact"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// metadataKeySourceContent is the metadata key reserved by fact extraction
// (Phase 5, D13, item 3/docs/api-contract.md) — it carries the original
// (already redacted) text that generated the fact, only when Add really ran
// the extraction pipeline (Extracted == true).
const metadataKeySourceContent = "source_content"

// telemetryKeyTokensSaved is the key used in the telemetry table (D28) for
// the counter that used to live alone in telemetry.json.
const telemetryKeyTokensSaved = "tokens_saved_by_compaction"

// DedupSimilarityThreshold is the cosine-similarity floor for treating a
// new `add` as a duplicate within the same domain (pipeline described in
// docs/api-contract.md), instead of inserting a new memory.
const DedupSimilarityThreshold = 0.95

// RelatedSimilarityThreshold is the cosine-similarity floor for considering
// two memories "related" (Phase 7, D15 in docs/decisions/) — below what is
// needed for a dedup merge (DedupSimilarityThreshold), but high enough not
// to link anything in the same domain. It reuses the same query as
// findDuplicate, with no extra embedding call: purely vector-based, works
// even with fact extraction (Phase 4/D12) turned off.
const RelatedSimilarityThreshold = 0.65

// metadataKeyRelatedTo and metadataKeyValidUntil are the keys reserved by
// Phase 7 (D15) inside Metadata — same convention as
// metadataKeySourceContent: they are not first-class fields of
// schema.Entry, they live inside the free-form metadata_json
// (docs/memory-schema.md).
const (
	metadataKeyRelatedTo  = "related_to"
	metadataKeyValidUntil = "valid_until"
)

// Relations the Classifier may return (Phase 7, D15) — any value other
// than relationContradicts (including an error, relationUnrelated, or an
// unrecognized string) falls back to the safe relationRelated: it links the
// two memories without invalidating anything.
const (
	relationContradicts = "contradicts"
	relationUnrelated   = "unrelated"
	relationRelated     = "related"
)

// maxRelatedExpansion is the cap on entries brought in by 1-hop expansion
// per main result in search/recent (Phase 7, D15) — it keeps the result
// from growing unbounded when a memory accumulates many related_to.
const maxRelatedExpansion = 2

// maxEmbedChars limits the text sent to the embeddings API (not the
// stored/returned content, which stays intact). text-embedding-3-small
// accepts at most 8191 tokens; 12000 characters is a conservative ceiling
// even for text dense in code/JSON (~2.5 chars/token), with plenty of
// margin. Without it, any Add with large content (e.g. a whole-session
// digest from m3m0s hook claude-code, Phase 8/D14, without fact extraction
// enabled to chop it into short facts first) fails with 400 Bad Request
// both in the dedup query and in storing itself — found while validating
// Phase 8 in production (16/Jul/2026).
const maxEmbedChars = 12000

// truncateForEmbedding cuts s (by rune, not by byte, so as not to split
// multi-byte UTF-8) to maxEmbedChars — only used on the text sent to
// embedFunc, never on what is persisted.
func truncateForEmbedding(s string) string {
	runes := []rune(s)
	if len(runes) <= maxEmbedChars {
		return s
	}
	return string(runes[:maxEmbedChars])
}

// Extractor is the interface the Add pipeline uses to turn raw content
// into atomic facts (Phase 4, D12) — satisfied by *extract.Extractor in
// production, and by fakes in this package's tests. internal/store does
// not import internal/extract directly to avoid coupling the add pipeline
// to a specific provider; whoever assembles the MemoryStore
// (cmd/m3m0s/serve.go) decides the concrete implementation.
type Extractor interface {
	Extract(ctx context.Context, content, domain string) ([]string, error)
}

// FallbackExtractor is satisfied by extractors with a secondary model
// (*extract.Extractor with M3M0S_EXTRACT_FALLBACK_MODEL set): the retry
// attempt of a piece uses it when the primary timed out or failed (#65).
type FallbackExtractor interface {
	HasFallback() bool
	ExtractFactsFallback(ctx context.Context, content, domain string) ([]factkind.Fact, error)
}

// KindExtractor is satisfied by extractors that return, in the same call, the
// nature (kind, D38) of each fact — *extract.Extractor in production. An
// optional type assertion over the configured Extractor (like Classifier), so
// fakes that only cover Extract keep working; their facts get a heuristic
// kind instead.
type KindExtractor interface {
	ExtractFacts(ctx context.Context, content, domain string) ([]factkind.Fact, error)
}

// Classifier is satisfied by extractors that also judge the semantic
// relation between a new fact and an existing, already stored candidate
// (Phase 7, D15) — today only *extract.Extractor, via a Classify method.
// An optional type assertion over the configured Extractor (same pattern as
// modelProvider/Model() below), so as not to force test fakes that only
// cover Extract to implement this too. When the configured Extractor does
// not satisfy Classifier (or there is no Extractor at all), the Add
// pipeline falls back to the safe path described in linkRelated: it links
// the two memories, never invalidates.
type Classifier interface {
	Classify(ctx context.Context, newFact, existingFact string) (string, error)
}

// MemoryStore is the only component that touches SQLite directly (D28).
type MemoryStore struct {
	db        *sqlstore.DB
	emb       embedState           // runtime embedding configuration (#39, D36)
	re        reembedState         // re-embedding job (#39 phase 2)
	ins       insightsState        // hygiene analysis job (D37)
	kb        kindBackfillState    // fact-kind backfill job (D38)
	extractor Extractor            // nil = extraction disabled (D12, item 1)
	filter    *ingestfilter.Engine // boilerplate filter for automatic capture (#53)

	retModel  RetentionModel  // forget-sweep score (D39); guarded by mu
	retParams RetentionParams // parameters of the per-kind score (D39); guarded by mu

	mu                      sync.RWMutex
	index                   map[string]schema.Entry // in-memory cache, without embedding — reloaded from SQLite at boot
	vecs                    *vecIndex               // compact int8 vector index for candidate selection (F5)
	dataDir                 string
	vocabState              vocabState // query-term correction vocabulary (D51)
	tokensSavedByCompaction int64      // telemetry (GET /stats) — see recordCompactionSavings
}

// DB returns the underlying *sqlstore.DB (D28) — used by
// cmd/m3m0s/serve.go to open internal/auth and internal/jobs over the SAME
// SQLite connection/file the MemoryStore already opened, instead of each
// package opening its own (avoids redundant lock contention on the same
// file).
func (s *MemoryStore) DB() *sqlstore.DB {
	return s.db
}

// SetExtractor turns fact extraction via LLM on (or off, passing nil) in
// the Add pipeline (D12). Called by cmd/m3m0s/serve.go only when
// M3M0S_EXTRACT_API_KEY/-extract-key is present — when absent, the
// MemoryStore is born with a nil extractor and Add behaves exactly as
// before Phase 4 (1 input = 1 entry, Extracted: false).
func (s *MemoryStore) SetExtractor(e Extractor) {
	s.extractor = e
}

// modelProvider is satisfied by extractors that expose the configured model
// (today only *extract.Extractor, via internal/extract.Extractor.Model) — an
// optional type assertion, not an extra method on the Extractor interface,
// so as not to force this package's test fakes (which only implement
// Extract) to implement Model() too.
type modelProvider interface {
	Model() string
}

// ExtractionInfo returns whether fact extraction (D12) is enabled on this
// instance and, if so, which model is configured — used by GET /stats
// (Phase 5, D13) to build the `extraction` field. model is empty when the
// configured extractor does not expose Model() (should not happen in
// production, only in tests with minimal fakes).
func (s *MemoryStore) ExtractionInfo() (enabled bool, model string) {
	if s.extractor == nil {
		return false, ""
	}
	if mp, ok := s.extractor.(modelProvider); ok {
		return true, mp.Model()
	}
	return true, ""
}

// tokenUsageProvider is satisfied by extractors that account for tokens
// consumed in provider calls (today only *extract.Extractor, via
// TokensUsed) — same optional type-assertion pattern as modelProvider, so
// as not to force test fakes to implement this too.
type tokenUsageProvider interface {
	TokensUsed() (prompt, completion int64)
}

// ExtractionTokensUsed returns the total tokens (prompt+completion) spent
// by the configured Extractor since the process started — 0 when
// extraction is disabled or the Extractor does not expose TokensUsed. Used
// by GET /stats (telemetry, `extractionTokensUsed`).
func (s *MemoryStore) ExtractionTokensUsed() int64 {
	if s.extractor == nil {
		return 0
	}
	if tp, ok := s.extractor.(tokenUsageProvider); ok {
		prompt, completion := tp.TokensUsed()
		return prompt + completion
	}
	return 0
}

// TokensSavedByCompaction returns the accumulated total (persisted in the
// telemetry table) of tokens saved by fact extraction — see
// recordCompactionSavings. Used by GET /stats (telemetry,
// `tokensSavedByCompaction`).
func (s *MemoryStore) TokensSavedByCompaction() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokensSavedByCompaction
}

// DiskUsageBytes sums the size of all files in dataDir (the SQLite
// database — m3m0s.db + m3m0s.db-wal/-shm — and any legacy file not yet
// manually removed after the migration to SQLite, D28) — used by
// GET /stats (telemetry, `databaseSizeMB`). The goal is "how much disk
// space m3m0s takes today", not a per-file breakdown.
func (s *MemoryStore) DiskUsageBytes() (int64, error) {
	var total int64
	err := filepath.Walk(s.dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("compute size of %s: %w", s.dataDir, err)
	}
	return total, nil
}

// approxCharsPerToken is the same order of magnitude documented in
// maxEmbedChars above (~2.5-4 characters per token, depending on language
// and text density) — approxTokens does not really tokenize (that would
// pull in a dependency just for this), it is an estimate for
// `tokensSavedByCompaction` in GET /stats.
const approxCharsPerToken = 4

// approxTokens estimates how many tokens a text takes, by rune count (not
// bytes, so as not to skew with multi-byte UTF-8). It never returns 0 for
// non-empty text, otherwise a short fact (fewer than approxCharsPerToken
// characters) would weigh "0 tokens" and distort the savings sum.
func approxTokens(s string) int64 {
	n := int64(len([]rune(s)))
	if n == 0 {
		return 0
	}
	if tokens := n / approxCharsPerToken; tokens > 0 {
		return tokens
	}
	return 1
}

// recordCompactionSavings accumulates, in tokensSavedByCompaction, how much
// fact extraction (Phase 4/D12) shrank the original content in this Add
// call — only when extraction actually ran (facts came from the extractor,
// not from the raw-content fallback). It only adds when positive: an
// extraction that produced facts longer than the original (rare, but
// possible with very short texts) must not show up as negative savings in
// GET /stats. Persists in the telemetry table to survive a restart.
func (s *MemoryStore) recordCompactionSavings(ctx context.Context, original string, facts []string) {
	factsTokens := int64(0)
	for _, f := range facts {
		factsTokens += approxTokens(f)
	}
	saved := approxTokens(original) - factsTokens
	if saved <= 0 {
		return
	}

	s.mu.Lock()
	s.tokensSavedByCompaction += saved
	total := s.tokensSavedByCompaction
	s.mu.Unlock()
	if err := s.db.SetTelemetryInt64(ctx, telemetryKeyTokensSaved, total); err != nil {
		log.Printf("m3m0s: failed to persist telemetry (tokensSavedByCompaction): %v", err)
	}
}

// New creates (or reopens) the store persisted in dataDir — it opens
// SQLite (internal/sqlstore.Open already runs the pending migrations, D28)
// and loads the in-memory index from it.
func New(dataDir, apiKey, baseURL string) (*MemoryStore, error) {
	db, err := sqlstore.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open sqlstore: %w", err)
	}

	s := &MemoryStore{
		db:      db,
		index:   make(map[string]schema.Entry),
		vecs:    newVecIndex(),
		dataDir: dataDir,
		filter:  ingestfilter.Default(),

		retModel:  RetentionLegacy,
		retParams: DefaultRetentionParams(),
	}
	// No API key = keyword-only mode (F5): entries are stored without an
	// embedding, dedup falls back to exact content match and search to FTS5.
	if err := s.loadEmbedding(context.Background(), apiKey, baseURL); err != nil {
		return nil, fmt.Errorf("load embedding config: %w", err)
	}
	if e := s.currentEmbedder(); e != nil && e.dims > 0 {
		s.vecs.reset(e.dims)
	}
	if err := s.loadIndex(); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}
	if err := s.loadVectors(context.Background()); err != nil {
		return nil, err
	}
	if err := s.loadTelemetry(); err != nil {
		return nil, fmt.Errorf("load telemetry: %w", err)
	}
	// Loading leaves up to one heap's worth of unreachable garbage that, with
	// no further allocation, no GC cycle would reclaim for a long time. Collect
	// it and hand the pages back once, so a long-lived idle server starts from
	// its live size instead of the boot peak (#37).
	debug.FreeOSMemory()
	return s, nil
}

func rowToEntry(r sqlstore.EntryRow) schema.Entry {
	return schema.Entry{
		ID:             r.ID,
		Content:        r.Content,
		Domain:         r.Domain,
		DomainCategory: r.DomainCategory,
		Source:         r.Source,
		Agent:          r.Agent,
		Model:          r.Model,
		Project:        r.Project,
		Tags:           r.Tags,
		Metadata:       r.Metadata,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

// entryRow builds the row to persist, tagging the vector with the model that
// produced it. A vector whose size does not match the active configuration
// (the admin changed it while this write was in flight) is dropped instead of
// being stored in the wrong space; the re-embedding job fills it in later.
func (s *MemoryStore) entryRow(e schema.Entry, embedding []float32) (sqlstore.EntryRow, []float32) {
	model := ""
	if len(embedding) > 0 {
		s.emb.mu.Lock()
		if cur := s.emb.emb; cur != nil {
			if cur.dims == 0 {
				cur.dims = len(embedding) // first vector of a config whose size was unknown
			}
			if len(embedding) == cur.dims {
				model = cur.model
			} else {
				log.Printf("m3m0s: dropping a %d-dimension vector for %s: the active embedding configuration expects %d", len(embedding), e.ID, cur.dims)
				embedding = nil
			}
		}
		s.emb.mu.Unlock()
	}
	row := entryToRow(e, embedding)
	row.EmbeddingModel = model
	return row, embedding
}

func entryToRow(e schema.Entry, embedding []float32) sqlstore.EntryRow {
	return sqlstore.EntryRow{
		ID:             e.ID,
		Content:        e.Content,
		Domain:         e.Domain,
		DomainCategory: e.DomainCategory,
		Source:         e.Source,
		Agent:          e.Agent,
		Model:          e.Model,
		Project:        e.Project,
		Tags:           e.Tags,
		Metadata:       e.Metadata,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
		Embedding:      embedding,
	}
}

func (s *MemoryStore) loadIndex() error {
	ctx := context.Background()
	if n, err := s.db.CountEntries(ctx); err == nil {
		s.index = make(map[string]schema.Entry, n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A few hundred distinct values repeat across tens of thousands of entries
	// (domain, source, agent, model, project): keep one copy of each.
	seen := make(map[string]string)
	intern := func(v string) string {
		if c, ok := seen[v]; ok {
			return c
		}
		seen[v] = v
		return v
	}
	internPtr := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := intern(*p)
		return &v
	}
	return s.db.ForEachEntry(ctx, func(r sqlstore.EntryRow) {
		r.Domain, r.DomainCategory, r.Source = intern(r.Domain), intern(r.DomainCategory), intern(r.Source)
		r.Agent, r.Model, r.Project = internPtr(r.Agent), internPtr(r.Model), internPtr(r.Project)
		e := rowToEntry(r)
		if e.Metadata == nil {
			// Legacy round-trip (the in-memory index should never reintroduce nil —
			// it would serialize JSON `null` in responses, breaking clients typed as
			// object, e.g. the WebUI's Zod schema). Same normalization that already
			// existed before D28.
			e.Metadata = map[string]string{}
		}
		s.index[e.ID] = e
	})
}

func (s *MemoryStore) loadTelemetry() error {
	v, err := s.db.GetTelemetryInt64(context.Background(), telemetryKeyTokensSaved)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokensSavedByCompaction = v
	return nil
}

// AddInput are the input fields of an `add`, before redaction/dedup.
type AddInput struct {
	Content string
	Domain  string
	Source  string
	Agent   string
	// Model is the LLM that produced the content (provenance, optional).
	Model string
	// Project scopes the memory to a project; empty = global.
	Project  string
	Tags     []string
	Metadata map[string]string
	// IDPrefix is optional (default "mem", see schema.DefaultIDPrefix) —
	// m3m0s watch (D11) passes "chunk" to make it clear in the ID itself that
	// the memory came from a reindexed file chunk. Only used when the memory
	// is new; on a duplicate (dedup) the existing ID is preserved.
	IDPrefix string
	// Capture marks an automatic capture path (the Claude Code hook): the
	// only inputs the ingestion boilerplate filter (#53) may act on. Explicit
	// writes (memory_add, the API, chat, watch) leave it false and are never
	// filtered, nor is content carrying metadata pinned=true.
	Capture bool
	// RequireExtraction makes Add return ErrExtractionFailed, storing nothing,
	// when the extractor is configured but a piece could not be extracted —
	// instead of inserting that piece raw. The ingest queue (D40) sets it on
	// every attempt but the last so a slow provider is retried, not recorded
	// as noise.
	RequireExtraction bool
}

// ErrExtractionFailed is returned by Add when AddInput.RequireExtraction is
// set and at least one piece fell back to raw.
var ErrExtractionFailed = errors.New("fact extraction failed")

// metadataKeyExtraction / metadataExtractionRawFallback tag an entry that was
// stored raw because extraction failed (D40); the retroactive job repairs it.
const (
	metadataKeyExtraction         = "extraction"
	metadataExtractionRawFallback = "raw-fallback"
	// metadataExtractionNoFacts tags an entry kept raw because the model found
	// nothing durable in it (D42); the retroactive job skips it.
	metadataExtractionNoFacts = "no-durable-facts"
)

// validateProvenance checks the optional model/project of an add or update;
// empty values are valid (unknown model, global entry).
func validateProvenance(model, project string) error {
	if model != "" {
		if err := schema.ValidateModel(model); err != nil {
			return &ValidationError{Code: "invalid_model", Message: err.Error()}
		}
	}
	if project != "" {
		if err := schema.ValidateProject(project); err != nil {
			return &ValidationError{Code: "invalid_project", Message: err.Error()}
		}
	}
	return nil
}

// setProvenance stores model/project on e, leaving a field nil when empty.
func setProvenance(e *schema.Entry, model, project string) {
	if model != "" {
		e.Model = &model
	}
	if project != "" {
		e.Project = &project
	}
}

// AddedEntry is 1 fact/entry of the Add pipeline result — before Phase 4
// there was always exactly 1 per call; with extraction enabled (D12) there
// may be N, one per extracted atomic fact.
type AddedEntry struct {
	Entry   schema.Entry
	Deduped bool
}

// AddOutput is the result of the add pipeline described in
// docs/api-contract.md. Extracted indicates whether AT LEAST 1 piece of
// the content actually went through fact extraction (D12, item 2; D18 —
// content may have been sliced into several pieces, each extracted
// independently) — false only when extraction is disabled on the server OR
// when ALL pieces failed and fell back to raw content (D12, item 5); in
// that case Entries always has exactly 1 item. When Extracted is true but
// not every piece was extracted (partial failure, D18), Entries may mix
// entries with and without metadata.source_content — the per-entry signal
// (presence of source_content) is always more precise than this aggregate
// bool.
type AddOutput struct {
	Entries   []AddedEntry
	Extracted bool
	// Filtered counts the segments and facts the ingestion boilerplate filter
	// dropped (#53); Entries may be empty when everything was boilerplate.
	Filtered int
}

// Add runs the full pipeline (Phase 1 + Phase 4/D12 extraction, sliced
// into pieces since D18): 1) redacts secrets from content (D6); 2) if an
// extractor is configured, splits the already redacted content into pieces
// (extractPieces/splitForExtraction, D18 — small content always becomes a
// single piece) and extracts atomic facts from each independently — a
// piece that fails (network, timeout, invalid response) falls back to raw
// content ONLY FOR ITSELF, never taking down the others nor propagating an
// error to the whole Add; 3) for each fact of each piece: redacts again
// (defense in depth — D12, item 4, the content may now have left the
// process for the extraction provider) → dedup check → insert or update.
func (s *MemoryStore) Add(ctx context.Context, in AddInput, now time.Time) (AddOutput, error) {
	in.Domain = schema.CanonicalDomain(in.Domain)
	if err := schema.ValidateDomain(in.Domain); err != nil {
		return AddOutput{}, err
	}
	if err := validateProvenance(in.Model, in.Project); err != nil {
		return AddOutput{}, err
	}

	redResult := redact.Redact(in.Content)
	content := redResult.Text

	filtering := s.filteringApplies(in)
	filtered := 0
	if filtering {
		kept, hits := s.filter.FilterSegments(content)
		filtered += logFilterHits("segment", in.Domain, hits)
		if strings.TrimSpace(kept) == "" {
			return AddOutput{Filtered: filtered}, nil
		}
		content = kept
	}

	var pieces []extractedPiece
	if s.extractor != nil {
		pieces = s.extractPieces(ctx, content, in.Domain)
		if in.RequireExtraction {
			for _, p := range pieces {
				if !p.extracted && !p.noFacts {
					return AddOutput{}, ErrExtractionFailed
				}
			}
		}
	} else {
		pieces = []extractedPiece{{facts: []string{content}}}
	}

	entries := make([]AddedEntry, 0, len(pieces))
	for _, piece := range pieces {
		for fi, fact := range piece.facts {
			factContent := redact.Redact(fact).Text
			factIn := in
			factIn.Metadata = withKind(factIn.Metadata, factContent, piece.kindAt(fi))
			if filtering {
				d := s.filter.CheckFact(factContent)
				if d.Matched() {
					log.Printf("m3m0s: ingestion filter: rule %s %s a fact (domain=%s, %d chars)", d.RuleID, d.Action, in.Domain, len([]rune(factContent)))
				}
				if d.Action == ingestfilter.ActionDrop {
					filtered++
					continue
				}
				if d.Action == ingestfilter.ActionTag {
					factIn.Metadata = withMetadata(factIn.Metadata, ingestfilter.MetadataKey, d.RuleID)
				}
				if !piece.extracted {
					// Raw fallback of a captured piece: remember it came from automatic
					// capture, so the retroactive job may filter it when re-extracting.
					factIn.Metadata = withMetadata(factIn.Metadata, metadataKeyCapture, metadataCaptureAuto)
				}
			}
			if piece.noFacts {
				factIn.Metadata = withMetadata(factIn.Metadata, metadataKeyExtraction, metadataExtractionNoFacts)
			} else if !piece.extracted && s.extractor != nil {
				factIn.Metadata = withMetadata(factIn.Metadata, metadataKeyExtraction, metadataExtractionRawFallback)
			}
			if piece.extracted {
				// Retroactive traceability (Phase 5, D13, item 3): each extracted fact
				// carries metadata.source_content = the original text of the PIECE it
				// came from (D18 — no longer the whole Add content, since it may now
				// have been sliced), so the WebUI can show "extracted from: ..." after
				// the add. withSourceContent copies in.Metadata instead of mutating it —
				// each fact needs its own map (otherwise multiple entries would end up
				// pointing to the same map[string]string, and a future mutation in one
				// would leak into another). It deliberately overwrites a source_content
				// key the caller may have sent manually — the key is reserved by
				// extraction when extracted == true (docs/api-contract.md).
				factIn.Metadata = withSourceContent(factIn.Metadata, piece.sourceContent)
			}
			added, err := s.addOne(ctx, factContent, factIn, now)
			if err != nil {
				return AddOutput{}, err
			}
			entries = append(entries, added)
		}
	}

	return AddOutput{Entries: entries, Extracted: anyExtracted(pieces), Filtered: filtered}, nil
}

// metadataKeyCapture marks a raw (not extracted) entry stored from an
// automatic capture path; the retroactive extraction job only applies the
// boilerplate filter to entries that carry it.
const (
	metadataKeyCapture  = "capture"
	metadataCaptureAuto = "auto"
)

// filteringApplies says whether the boilerplate filter may act on in: only on
// automatic capture, never on pinned content.
func (s *MemoryStore) filteringApplies(in AddInput) bool {
	return s.filter != nil && in.Capture && in.Metadata[metadataKeyPinned] != "true"
}

// logFilterHits logs each hit by rule id (never the content) and returns the
// number of dropped segments.
func logFilterHits(what, domain string, hits []ingestfilter.Decision) int {
	dropped := 0
	for _, h := range hits {
		log.Printf("m3m0s: ingestion filter: rule %s %s a %s (domain=%s)", h.RuleID, h.Action, what, domain)
		if h.Action == ingestfilter.ActionDrop {
			dropped++
		}
	}
	return dropped
}

// withMetadata returns a copy of base with key set; it never mutates base.
func withMetadata(base map[string]string, key, value string) map[string]string {
	md := make(map[string]string, len(base)+1)
	for k, v := range base {
		md[k] = v
	}
	md[key] = value
	return md
}

// SetFilter replaces the boilerplate filter (nil disables it).
func (s *MemoryStore) SetFilter(f *ingestfilter.Engine) { s.filter = f }

// FilterStats returns the rules and counters of the boilerplate filter.
func (s *MemoryStore) FilterStats() ingestfilter.Stats {
	if s.filter == nil {
		return ingestfilter.Stats{Mode: string(ingestfilter.ActionOff)}
	}
	return s.filter.Stats()
}

// withSourceContent returns a new map with all of base's keys plus
// metadataKeySourceContent = sourceContent (overwriting if it already
// existed). Never mutates base. The raw text stays here only in passing:
// internalizeMetadata (called from addOne/Update before any persistence)
// converts this key into a deduplicated reference before touching the
// index or SQLite (D21, ported to sqlstore in D28).
func withSourceContent(base map[string]string, sourceContent string) map[string]string {
	md := make(map[string]string, len(base)+1)
	for k, v := range base {
		md[k] = v
	}
	md[metadataKeySourceContent] = sourceContent
	return md
}

// internalizeMetadata returns a copy of md with metadataKeySourceContent
// replaced by metadataKeySourceContentRef (D21, docs/decisions/ — fixes
// A7): it stores the text in the source_blobs table deduplicated by hash
// and never lets the full text reach SQLite (metadata_json column).
// Without that key in md, it returns md without copying — there is nothing
// to internalize.
func (s *MemoryStore) internalizeMetadata(ctx context.Context, md map[string]string) (map[string]string, error) {
	text, has := md[metadataKeySourceContent]
	if !has {
		return md, nil
	}
	// Redact the blob too (D6): the source text is stored apart from the
	// entry content and used to reach disk verbatim. The hash is taken over
	// the redacted text so the dedup key never depends on a secret.
	text = redact.Redact(text).Text
	hash := hashSourceContent(text)
	if err := s.db.PutSourceBlob(ctx, hash, text); err != nil {
		return nil, fmt.Errorf("store source blob: %w", err)
	}
	out := make(map[string]string, len(md))
	for k, v := range md {
		if k == metadataKeySourceContent {
			continue
		}
		out[k] = v
	}
	out[metadataKeySourceContentRef] = hash
	return out, nil
}

// externalizeEntry returns a copy of e with metadataKeySourceContentRef
// resolved back to the full text under metadataKeySourceContent (D21) —
// applied to every public output of the MemoryStore (Get/Search/Recent/
// Add/Update), so the API contract (docs/api-contract.md) stays identical
// despite the internal deduplication: API callers never see
// source_content_ref, only the resolved source_content, exactly as before.
// Without that key in e.Metadata, it returns e without copying. A hash
// with no matching blob (should not happen outside external manual editing
// of the database) does not bring down the whole response — it just does
// not resolve, logs and carries on without the key.
func (s *MemoryStore) externalizeEntry(ctx context.Context, e schema.Entry) schema.Entry {
	hash, has := e.Metadata[metadataKeySourceContentRef]
	if !has {
		return e
	}
	text, ok, err := s.db.GetSourceBlob(ctx, hash)
	if err != nil {
		log.Printf("m3m0s: error reading source blob %s referenced by %s: %v", hash, e.ID, err)
		return e
	}
	if !ok {
		log.Printf("m3m0s: source blob %s referenced by %s not found", hash, e.ID)
		return e
	}
	md := make(map[string]string, len(e.Metadata))
	for k, v := range e.Metadata {
		if k == metadataKeySourceContentRef {
			continue
		}
		md[k] = v
	}
	md[metadataKeySourceContent] = text
	e.Metadata = md
	return e
}

// externalizeEntries applies externalizeEntry to each entry of entries, in
// the same order.
func (s *MemoryStore) externalizeEntries(ctx context.Context, entries []schema.Entry) []schema.Entry {
	out := make([]schema.Entry, len(entries))
	for i, e := range entries {
		out[i] = s.externalizeEntry(ctx, e)
	}
	return out
}

// similarityBand classifies a similarity score into the 3 buckets of the
// addOne pipeline (Phase 1 + Phase 7/D15): "dedup" (>=
// DedupSimilarityThreshold, merges with the candidate), "related" (>=
// RelatedSimilarityThreshold, but below the dedup floor — candidate for
// related_to/valid_until), or "none" (too low, candidate ignored).
func similarityBand(similarity float64) string {
	switch {
	case similarity >= DedupSimilarityThreshold:
		return "dedup"
	case similarity >= RelatedSimilarityThreshold:
		return "related"
	default:
		return "none"
	}
}

// addOne inserts or updates (dedup) 1 already redacted fact/entry — the
// unit Add calls per extracted fact (or for the single "fact" when
// extraction is disabled/failed). content has already gone through
// redact.Redact before getting here. Besides dedup (Phase 1), it decides
// the light KG/temporal fact (Phase 7, D15): when the nearest candidate is
// not a duplicate but is in the "related" band, linkRelated decides the
// related_to/valid_until.
func (s *MemoryStore) addOne(ctx context.Context, content string, in AddInput, now time.Time) (AddedEntry, error) {
	candidate, similarity, contentVec, err := s.findDuplicate(ctx, content, in.Domain, in.Project)
	if err != nil {
		return AddedEntry{}, err
	}

	band := "none"
	if candidate != nil {
		band = similarityBand(similarity)
	}

	var entry schema.Entry
	deduped := band == "dedup"
	if deduped {
		// Filter markers describe the incoming fact, not the entry it merges into.
		// The kind tag is the exception that needs care: the existing entry keeps
		// its own (never overwriting a manual one), and only adopts the incoming
		// one when that is a better source (see adoptKind).
		incoming := in.Metadata
		in.Metadata = withoutKey(withoutKey(in.Metadata, ingestfilter.MetadataKey), metadataKeyCapture)
		in.Metadata = withoutKey(withoutKey(in.Metadata, factkind.MetadataKey), factkind.MetadataSourceKey)
		entry = mergeForDedup(*candidate, content, in, now)
		entry.Metadata = adoptKind(entry.Metadata, incoming)
	} else {
		entry, err = schema.NewEntry(content, in.Domain, in.Source, in.Agent, in.Tags, in.Metadata, now, in.IDPrefix)
		if err != nil {
			return AddedEntry{}, err
		}
		setProvenance(&entry, in.Model, in.Project)
		if id, ok := s.findOverridden(ctx, entry, contentVec, now); ok {
			entry.Metadata = withOverrides(entry.Metadata, id)
		}
		if band == "related" {
			if err := s.linkRelated(ctx, &entry, candidate, now); err != nil {
				return AddedEntry{}, err
			}
		}
	}

	internalMD, err := s.internalizeMetadata(ctx, entry.Metadata)
	if err != nil {
		return AddedEntry{}, err
	}
	entry.Metadata = internalMD

	vec := contentVec
	if entry.Content != content || vec == nil {
		vec, err = s.embedText(ctx, entry.Content)
		if err != nil && !errors.Is(err, errNoEmbedding) {
			return AddedEntry{}, fmt.Errorf("generate embedding: %w", err)
		}
	}
	row, vec := s.entryRow(entry, vec)
	if err := s.db.UpsertEntry(ctx, row); err != nil {
		return AddedEntry{}, err
	}

	s.vecs.upsert(entry.ID, entry.Domain, vec)
	s.mu.Lock()
	s.index[entry.ID] = entry
	s.mu.Unlock()

	return AddedEntry{Entry: s.externalizeEntry(ctx, entry), Deduped: deduped}, nil
}

// linkRelated decides the relation between entry (just created, not yet
// persisted) and candidate (already existing, found by findDuplicate in
// the "related" band — Phase 7, D15). With no Classifier available
// (extraction off, or the configured Extractor does not implement
// Classify), or on error in the call, or an unrecognized answer, it uses
// the safe fallback: it links the two without invalidating anything. Only
// an exact "contradicts" answer marks candidate as expired; "unrelated"
// links nothing.
func (s *MemoryStore) linkRelated(ctx context.Context, entry *schema.Entry, candidate *schema.Entry, now time.Time) error {
	relation := relationRelated
	if cl, ok := s.extractor.(Classifier); ok {
		r, err := cl.Classify(ctx, entry.Content, candidate.Content)
		if err != nil {
			log.Printf("m3m0s: relation classification failed, treating as %q (domain=%s): %v", relationRelated, entry.Domain, err)
		} else if r == relationContradicts || r == relationUnrelated {
			relation = r
		}
		// any other value (including relationRelated or an unrecognized string)
		// keeps the safe default relationRelated.
	}

	if relation == relationUnrelated {
		return nil
	}

	entry.Metadata = withRelatedTo(entry.Metadata, candidate.ID)
	return s.touchCandidate(ctx, candidate.ID, entry.ID, relation == relationContradicts, now)
}

// touchCandidate persists the metadata changes on an already existing
// candidate (Phase 7, D15): it always adds relatedID to its related_to
// (bidirectional link), and if invalidate=true it also sets
// metadata.valid_until = now ("contradicts" relation). It is not an error
// for the candidate to have been removed between findDuplicate and here
// (rare concurrency) — in that case it is a no-op.
func (s *MemoryStore) touchCandidate(ctx context.Context, candidateID, relatedID string, invalidate bool, now time.Time) error {
	s.mu.Lock()
	entry, ok := s.index[candidateID]
	s.mu.Unlock()
	if !ok {
		return nil
	}

	entry.Metadata = withRelatedTo(entry.Metadata, relatedID)
	if invalidate {
		entry.Metadata = withValidUntil(entry.Metadata, now)
	}

	// Passes entry.UpdatedAt (not now): linking/invalidating a candidate via
	// relation (Phase 7/D15) never changed "when this entry was actually
	// edited" before D28 — same rationale as touchAccess/softDeleteOne in
	// decay.go.
	if err := s.db.UpdateMetadata(ctx, entry.ID, entry.Metadata, entry.UpdatedAt); err != nil {
		return err
	}

	s.mu.Lock()
	s.index[entry.ID] = entry
	s.mu.Unlock()
	return nil
}

// withRelatedTo returns a new map with relatedID added to the list already
// present in metadataKeyRelatedTo (comma-separated string, same convention
// as before D28). Never duplicates an ID already present, never mutates
// base.
func withRelatedTo(base map[string]string, relatedID string) map[string]string {
	md := make(map[string]string, len(base)+1)
	for k, v := range base {
		md[k] = v
	}
	for _, id := range splitRelatedTo(md[metadataKeyRelatedTo]) {
		if id == relatedID {
			return md
		}
	}
	ids := append(splitRelatedTo(md[metadataKeyRelatedTo]), relatedID)
	md[metadataKeyRelatedTo] = strings.Join(ids, ",")
	return md
}

// splitRelatedTo breaks metadataKeyRelatedTo back into a list of IDs — nil
// if the key is absent/empty.
func splitRelatedTo(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// withValidUntil returns a new map with metadataKeyValidUntil = now
// (RFC3339) — marks the entry as invalidated by a more recent fact that
// contradicts it (Phase 7, D15). Never mutates base.
func withValidUntil(base map[string]string, now time.Time) map[string]string {
	md := make(map[string]string, len(base)+1)
	for k, v := range base {
		md[k] = v
	}
	md[metadataKeyValidUntil] = now.UTC().Format(time.RFC3339)
	return md
}

// isExpired reports whether e has metadata.valid_until in the past or equal
// to now (Phase 7, D15) — expired memories are excluded by default from
// search/recent, with no new parameter to turn that off in this v1. A
// malformed value (should never happen — only withValidUntil writes this
// key) is not treated as a fatal error, it just does not filter.
func isExpired(e schema.Entry, now time.Time) bool {
	raw := e.Metadata[metadataKeyValidUntil]
	if raw == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	return !t.After(now)
}

// expandRelated adds, for each entry of primary that has related_to, up to
// maxRelatedExpansion related entries (read from the own index — requires
// s.mu already read-locked, same contract as findDuplicate/Search). It
// marks each expanded one with RelatedVia = ID of the main result that
// brought it in. It never duplicates an ID already present in seen, never
// includes an expired related one, and never includes a related one that
// does not match filter.Tags — without this, an entry explicitly excluded
// by the tag post-filter (matchesTags, in Search's main loop) could come
// back through the back door just for being linked via related_to to a
// result that passed the filter (found in an E2E test: searching with
// `tags: ["golang"]` leaked an entry with only the "python" tag that was
// related to the "golang" entry). Like the seen/expired skip, a candidate
// that fails the tag filter does not consume the maxRelatedExpansion cap —
// another candidate can take the slot.
func (s *MemoryStore) expandRelated(primary []schema.Entry, seen map[string]struct{}, filter Filter, now time.Time) []schema.Entry {
	out := primary
	for _, e := range primary {
		added := 0
		for _, id := range splitRelatedTo(e.Metadata[metadataKeyRelatedTo]) {
			if added >= maxRelatedExpansion {
				break
			}
			if _, dup := seen[id]; dup {
				continue
			}
			related, ok := s.index[id]
			if !ok || isExpired(related, now) || isSoftDeleted(related) || !matchesTags(related, filter.Tags) || !filter.matchesProject(related) {
				continue
			}
			via := e.ID
			related.RelatedVia = &via
			out = append(out, related)
			seen[id] = struct{}{}
			added++
		}
	}
	return out
}

// mergeForDedup merges a new entry into an already existing entry: the
// most recent content prevails (it is what motivated the new add), tags
// are the union of both, new metadata overrides repeated keys but keeps
// the old ones, and the original id/createdAt/domain are preserved — only
// the most recent source and the agent (if provided this time) replace the
// old ones, since they reflect who touched the memory last.
func mergeForDedup(existing schema.Entry, newContent string, in AddInput, now time.Time) schema.Entry {
	tagSet := make(map[string]struct{}, len(existing.Tags)+len(in.Tags))
	for _, t := range existing.Tags {
		tagSet[t] = struct{}{}
	}
	for _, t := range in.Tags {
		tagSet[t] = struct{}{}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	metadata := make(map[string]string, len(existing.Metadata)+len(in.Metadata))
	for k, v := range existing.Metadata {
		metadata[k] = v
	}
	for k, v := range in.Metadata {
		metadata[k] = v
	}

	source := in.Source
	if source == "" {
		source = existing.Source
	}

	agent := existing.AgentValue()
	if in.Agent != "" {
		agent = in.Agent
	}

	updated := existing
	if in.Model != "" {
		model := in.Model
		updated.Model = &model
	}
	updated.Content = newContent
	updated.Source = source
	updated.Tags = tags
	updated.Metadata = metadata
	updated.UpdatedAt = now
	if agent != "" {
		updated.Agent = &agent
	} else {
		updated.Agent = nil
	}
	return updated
}

// candidateScore is 1 candidate from the in-memory index together with its
// cosine similarity against a query vector — used by findDuplicate/Search
// to sort without needing an ANN index (D28).
type candidateScore struct {
	entry      schema.Entry
	similarity float64
}

// initialCandidates is how many nearest candidates each ranker proposes
// before filtering; collectCandidates doubles it while filters (category,
// tags, expired, soft-deleted...) leave fewer results than wanted.
const initialCandidates = 200

// Ranking modes of Search (Filter.Mode).
const (
	ModeHybrid  = "hybrid"
	ModeVector  = "vector"
	ModeKeyword = "keyword"
)

// rrfK is the reciprocal-rank-fusion constant (the usual 60): an entry's
// fused score is the sum of 1/(rrfK+rank) over the rankers that returned it.
const rrfK = 60

// keywordFusionDepth is how many keyword (bm25) hits take part in hybrid
// fusion. Keyword search earns its place by promoting exact terms
// (identifiers, names, numbers) that embeddings blur, not by adding recall:
// letting deeper, weaker keyword hits into the fusion pushed correct vector
// hits out of the top results on the golden set (see D32).
const keywordFusionDepth = 3

// collectCandidates returns up to limit entries accepted by accept, best
// first. The vector side ranks with the int8 index; the keyword side with
// FTS5 bm25; hybrid fuses both with reciprocal-rank fusion. Every returned
// entry carries its EXACT cosine similarity (0 when it has no embedding or
// the query could not be embedded), which stays the public Score. If the
// query cannot be embedded (provider down or not configured) hybrid and
// vector degrade to keyword instead of failing. When filters reject too
// many candidates the window doubles until limit is reached or both rankers
// are exhausted.
func (s *MemoryStore) collectCandidates(ctx context.Context, domain, text string, queryVec []float32, limit int, mode string, accept func(schema.Entry) bool) ([]candidateScore, error) {
	useVec := mode != ModeKeyword && queryVec != nil
	useFTS := mode != ModeVector && text != ""
	k := max(initialCandidates, limit*10)
	for {
		var vecIDs, ftsIDs []string
		var moreVec bool
		if useVec {
			vecIDs, moreVec = s.vecs.top(domain, queryVec, k)
		}
		if useFTS {
			var err error
			ftsIDs, err = s.db.SearchFTS(ctx, domain, text, k)
			if err != nil {
				return nil, err
			}
		}
		fusedFTS := ftsIDs
		if useVec {
			// Depth counts hits the filters accept, so expired, soft-deleted or
			// filtered-out entries do not use up the few keyword slots.
			fusedFTS = make([]string, 0, keywordFusionDepth)
			s.mu.RLock()
			for _, id := range ftsIDs {
				if e, ok := s.index[id]; ok && accept(e) {
					fusedFTS = append(fusedFTS, id)
					if len(fusedFTS) == keywordFusionDepth {
						break
					}
				}
			}
			s.mu.RUnlock()
		}

		// Exact vectors for every candidate of both rankers. The int8 index
		// only selects candidates: its internal order is approximate, so the
		// vector list is re-sorted by exact cosine BEFORE fusion (fusing the
		// approximate ranks measurably hurt recall on the golden set).
		var embeddings map[string][]float32
		if queryVec != nil {
			var err error
			if embeddings, err = s.db.EmbeddingsByIDs(ctx, append(append([]string(nil), vecIDs...), fusedFTS...)); err != nil {
				return nil, fmt.Errorf("fetch embeddings: %w", err)
			}
			sort.SliceStable(vecIDs, func(i, j int) bool {
				return cosineSimilarity(queryVec, embeddings[vecIDs[i]]) > cosineSimilarity(queryVec, embeddings[vecIDs[j]])
			})
		}
		// Rejected entries (expired, soft-deleted, filtered out) must not hold
		// rank positions in the vector list either.
		accepted := vecIDs[:0:0]
		s.mu.RLock()
		for _, id := range vecIDs {
			if e, ok := s.index[id]; ok && accept(e) {
				accepted = append(accepted, id)
			}
		}
		s.mu.RUnlock()
		ranked := fuse(accepted, fusedFTS)
		if len(ranked) == 0 {
			return nil, nil
		}
		scores := make([]candidateScore, 0, len(ranked))
		s.mu.RLock()
		for _, id := range ranked {
			e, ok := s.index[id]
			if !ok {
				continue
			}
			scores = append(scores, candidateScore{entry: e, similarity: cosineSimilarity(queryVec, embeddings[id])})
		}
		s.mu.RUnlock()
		// Pure vector mode orders by exact cosine; the others keep fusion order.
		if mode == ModeVector {
			sort.SliceStable(scores, func(i, j int) bool { return scores[i].similarity > scores[j].similarity })
		}

		out := make([]candidateScore, 0, limit)
		for _, c := range scores {
			if accept(c.entry) {
				out = append(out, c)
				if len(out) >= limit {
					return out, nil
				}
			}
		}
		moreFTS := useFTS && len(ftsIDs) >= k
		if !moreVec && !moreFTS {
			return out, nil
		}
		k *= 2
	}
}

// fuse merges up to two ranked id lists with reciprocal-rank fusion and
// returns the union, best first (ties keep the first list's order).
func fuse(lists ...[]string) []string {
	score := map[string]float64{}
	var order []string
	for _, l := range lists {
		for rank, id := range l {
			if _, seen := score[id]; !seen {
				order = append(order, id)
			}
			score[id] += 1.0 / float64(rrfK+rank+1)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return score[order[i]] > score[order[j]] })
	return order
}

// findDuplicate finds the most similar memory within the same domain and
// returns the content's embedding so addOne does not embed it a second time.
// Finding nothing is not an error — it returns (nil, 0, vec, nil). A
// soft-deleted candidate (D22) is never returned as a duplicate. Without an
// embedding provider only an exact content match counts as a duplicate.
func (s *MemoryStore) findDuplicate(ctx context.Context, content, domain, project string) (*schema.Entry, float64, []float32, error) {
	queryVec, err := s.embedText(ctx, content)
	if errors.Is(err, errNoEmbedding) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, e := range s.index {
			if e.Domain == domain && e.ProjectValue() == project && e.Content == content && !isSoftDeleted(e) {
				c := e
				return &c, 1, nil, nil
			}
		}
		return nil, 0, nil, nil
	}
	if err != nil {
		return nil, 0, nil, fmt.Errorf("generate embedding: %w", err)
	}
	// D22: a soft-deleted candidate should never silently absorb a new add via
	// dedup/related — that would revive a memory the ForgetSweep decided to
	// forget without going through Restore. It is skipped (the next candidate
	// is considered instead).
	scores, err := s.collectCandidates(ctx, domain, "", queryVec, 1, ModeVector, func(e schema.Entry) bool { return e.ProjectValue() == project && !isSoftDeleted(e) })
	if err != nil {
		return nil, 0, nil, fmt.Errorf("dedup query: %w", err)
	}
	if len(scores) == 0 {
		return nil, 0, queryVec, nil
	}
	return &scores[0].entry, scores[0].similarity, queryVec, nil
}

// errNoEmbedding means no embedding provider is configured (keyword-only
// mode); callers treat it as "skip the vector side", not as a failure.
var errNoEmbedding = errors.New("no embedding provider configured")

// embedText embeds text (truncated to maxEmbedChars), or returns
// errNoEmbedding when the server runs keyword-only.
func (s *MemoryStore) embedText(ctx context.Context, text string) ([]float32, error) {
	e := s.currentEmbedder()
	if e == nil {
		return nil, errNoEmbedding
	}
	return e.fn(ctx, truncateForEmbedding(text))
}

// HasEmbeddings reports whether vector search is available.
func (s *MemoryStore) HasEmbeddings() bool { return s.currentEmbedder() != nil }

// Filter gathers the exact filters common to search/recent, per
// docs/api-contract.md.
type Filter struct {
	Domain         string
	DomainCategory string
	Source         string
	// ExcludeSource, when set, drops entries whose source equals it (the
	// WebUI hides the built-in "selfdocs" this way); empty applies no exclusion.
	ExcludeSource string
	Agent         string
	Model         string
	// Project, when set, keeps only entries of that project; empty applies no
	// project filter. With a Project and no Scope the recall is layered:
	// global entries plus that project's (D34).
	Project string
	// Scope is "", ScopeGlobal or ScopeProjectOnly.
	Scope string
	Tags  []string
	// Kind, when set, keeps only entries of that nature (D38); "other" also
	// matches entries that were never classified.
	Kind string
	// Mode selects the ranking for Search: "" or "hybrid" (default: vector +
	// keyword fused with RRF), "vector" or "keyword". Ignored by Recent.
	Mode string
}

func (f Filter) matchesIndexEntry(e schema.Entry) bool {
	if f.Domain != "" && e.Domain != f.Domain {
		return false
	}
	if f.DomainCategory != "" && e.DomainCategory != f.DomainCategory {
		return false
	}
	if f.Source != "" && e.Source != f.Source {
		return false
	}
	if f.ExcludeSource != "" && e.Source == f.ExcludeSource {
		return false
	}
	if f.Agent != "" && e.AgentValue() != f.Agent {
		return false
	}
	if f.Model != "" && e.ModelValue() != f.Model {
		return false
	}
	if f.Kind != "" && e.KindValue() != f.Kind {
		return false
	}
	if !f.matchesProject(e) {
		return false
	}
	return matchesTags(e, f.Tags)
}

func matchesTags(e schema.Entry, tags []string) bool {
	for _, t := range tags {
		if !e.HasTag(t) {
			return false
		}
	}
	return true
}

// Search runs the semantic search (docs/api-contract.md: memory_search).
// filter.Domain is required for the similarity computation (D28:
// candidates come from the in-memory index filtered by domain — the same
// filter chromem-go received via `where`); DomainCategory/Source/Agent/Tags
// remain a post-filter in Go, as before.
func (s *MemoryStore) Search(ctx context.Context, query string, limit int, filter Filter, now time.Time) ([]schema.Entry, error) {
	out, _, err := s.SearchCorrected(ctx, query, limit, filter, now)
	return out, err
}

// SearchCorrected is Search plus the query actually searched when misspelled
// terms were repaired (D51); the second result is "" when the query was used
// as typed.
func (s *MemoryStore) SearchCorrected(ctx context.Context, query string, limit int, filter Filter, now time.Time) ([]schema.Entry, string, error) {
	filter.Domain = schema.CanonicalDomain(filter.Domain)
	filter.DomainCategory = schema.CanonicalCategory(filter.DomainCategory)
	if limit <= 0 {
		limit = 5
	}

	mode := filter.Mode
	if mode == "" {
		mode = ModeHybrid
	}
	// Misspelled names are repaired against the store vocabulary: the
	// corrected text is embedded, and the keyword side searches both forms.
	typed, keywordText, correctedQuery := query, query, ""
	if corrected, subs := s.correctQuery(ctx, query); len(subs) > 0 {
		query, keywordText, correctedQuery = corrected, typed+" "+corrected, corrected
	}
	var queryVec []float32
	if mode != ModeKeyword {
		var err error
		queryVec, err = s.embedText(ctx, query)
		if err != nil {
			if errors.Is(err, errNoEmbedding) || mode == ModeHybrid {
				// Keyword-only server, or the provider failed: keep answering
				// from the keyword side instead of failing the search.
				if !errors.Is(err, errNoEmbedding) {
					log.Printf("m3m0s: embedding failed, searching by keyword only: %v", err)
				}
				queryVec, mode = nil, ModeKeyword
			} else {
				return nil, "", fmt.Errorf("generate embedding: %w", err)
			}
		}
	}

	// filter.Domain empty sweeps every domain in one pass of the vector
	// index (docs/api-contract.md recommends always providing it).
	scores, err := s.collectCandidates(ctx, filter.Domain, keywordText, queryVec, limit, mode, func(e schema.Entry) bool {
		if filter.DomainCategory != "" && e.DomainCategory != filter.DomainCategory {
			return false
		}
		if filter.Source != "" && e.Source != filter.Source {
			return false
		}
		if filter.ExcludeSource != "" && e.Source == filter.ExcludeSource {
			return false
		}
		if filter.Agent != "" && e.AgentValue() != filter.Agent {
			return false
		}
		if filter.Model != "" && e.ModelValue() != filter.Model {
			return false
		}
		if filter.Kind != "" && e.KindValue() != filter.Kind {
			return false
		}
		if !filter.matchesProject(e) {
			return false
		}
		if !matchesTags(e, filter.Tags) {
			return false
		}
		return !isExpired(e, now) && !isSoftDeleted(e)
	})
	if err != nil {
		return nil, "", fmt.Errorf("search query: %w", err)
	}

	out := make([]schema.Entry, 0, limit)
	primaryIDs := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, c := range scores {
		e := c.entry
		if queryVec != nil { // keyword-only results have no cosine score
			score := c.similarity
			e.Score = &score
		}
		out = append(out, e)
		seen[e.ID] = struct{}{}
		primaryIDs = append(primaryIDs, e.ID)
	}

	// 1-hop expansion (Phase 7, D15).
	s.mu.RLock()
	out = s.expandRelated(out, seen, filter, now)
	s.mu.RUnlock()
	if filter.layered() {
		out = dropOverridden(out)
	}

	// Reinforcement (D22): only the PRIMARY results of the search count as
	// "access" — entries brought in by 1-hop expansion were not deliberately
	// found by this query, they are incidental.
	for _, id := range primaryIDs {
		s.touchAccess(ctx, id, now)
	}

	return s.externalizeEntries(ctx, out), correctedQuery, nil
}

// Recent lists the most recent memories from the own index — SQLite is
// not queried directly here, the in-memory index (reloaded from it at boot
// and kept in sync on every write) is already enough.
func (s *MemoryStore) Recent(limit int, filter Filter, now time.Time) []schema.Entry {
	filter.Domain = schema.CanonicalDomain(filter.Domain)
	filter.DomainCategory = schema.CanonicalCategory(filter.DomainCategory)
	if limit <= 0 {
		limit = 10
	}

	s.mu.RLock()
	all := make([]schema.Entry, 0, len(s.index))
	for _, e := range s.index {
		if filter.matchesIndexEntry(e) && !isExpired(e, now) && !isSoftDeleted(e) {
			all = append(all, e)
		}
	}
	s.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	if len(all) > limit {
		all = all[:limit]
	}

	seen := make(map[string]struct{}, len(all))
	for _, e := range all {
		seen[e.ID] = struct{}{}
	}
	s.mu.RLock()
	out := s.expandRelated(all, seen, filter, now)
	s.mu.RUnlock()
	if filter.layered() {
		out = dropOverridden(out)
	}
	return s.externalizeEntries(context.Background(), out)
}

// Stats returns global counts and counts by domain_category — the new
// countByDomainCategory field of docs/api-contract.md, made possible by
// the domain_category derived in D9/memory-schema.md.
func (s *MemoryStore) Stats() (count int, countByDomainCategory map[string]int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	countByDomainCategory = make(map[string]int)
	for _, e := range s.index {
		countByDomainCategory[e.DomainCategory]++
	}
	return len(s.index), countByDomainCategory
}

// Delete removes a memory by ID (D11/docs/api-contract.md, memory_delete
// section — minimal scope for the cascading purge of m3m0s watch). It
// returns (false, nil), not an error, when the ID does not exist: the
// HTTP layer decides 404 vs 204, not the store.
func (s *MemoryStore) Delete(ctx context.Context, id string) (bool, error) {
	s.mu.Lock()
	_, ok := s.index[id]
	if ok {
		delete(s.index, id)
	}
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	if err := s.db.DeleteEntry(ctx, id); err != nil {
		return false, fmt.Errorf("delete entry: %w", err)
	}
	s.vecs.remove(id)
	return true, nil
}

// DeleteByFilePath removes all chunks whose metadata.file_path exactly
// matches filePath within the domain — it is the mechanism `m3m0s watch`
// uses to reindex (deletes the old chunks before inserting the new ones)
// and to purge in cascade (file deleted). The filter is done in Go over
// the own index, like the tag post-filter: file_path is not a first-class
// column of the entries table (it only lives inside metadata_json), so the
// filter stays in Go — but it is already cheap this way, the index already
// holds the Entry with decoded Metadata.
func (s *MemoryStore) DeleteByFilePath(ctx context.Context, domain, filePath string) (int, error) {
	s.mu.Lock()
	var ids []string
	for id, e := range s.index {
		if e.Domain == domain && e.Metadata["file_path"] == filePath {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		delete(s.index, id)
	}
	s.mu.Unlock()

	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.db.DeleteEntries(ctx, ids); err != nil {
		return 0, fmt.Errorf("delete entries: %w", err)
	}
	for _, id := range ids {
		s.vecs.remove(id)
	}
	return len(ids), nil
}

// ErrNotFound is returned by Get/Update when the id does not exist — the
// HTTP layer maps it to 404 not_found (the same code already used by
// DELETE /memories/:id).
var ErrNotFound = errors.New("memory not found")

// Get returns 1 memory by id (elaboration after Phase 5/D13, memory_get
// section of docs/api-contract.md) — a read over the own index, the same
// mechanism Update uses to resolve the id before applying the patch. Since
// D22, it is no longer entirely free of side effects: it records access
// reinforcement (touchAccess) and treats a soft-deleted entry as
// nonexistent (ErrNotFound) — "hidden" has the same public semantics as
// "does not exist", only Restore (or direct metadata editing via Update)
// reverts that.
func (s *MemoryStore) Get(id string, now time.Time) (schema.Entry, error) {
	s.mu.RLock()
	entry, ok := s.index[id]
	s.mu.RUnlock()
	if !ok || isSoftDeleted(entry) {
		return schema.Entry{}, ErrNotFound
	}

	s.touchAccess(context.Background(), id, now)
	entry.Metadata = bumpAccessMetadata(entry.Metadata, now)
	return s.externalizeEntry(context.Background(), entry), nil
}

// ── memory_update (Phase 5, D13/docs/api-contract.md) ──

// ValidationError is returned by Update when a field present in the patch
// fails validation (docs/api-contract.md, memory_update section, "Errors").
// Code is the public error code (invalid_domain/content_required/
// source_required) that the HTTP layer passes straight through in the
// ErrorBody.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// UpdatePatch holds the fields of PATCH /memories/:id already resolved by
// presence: nil = field absent from the body (does not touch the current
// value); non-nil pointer to a zero value = field present and must
// apply/clear. The presence decision is the caller's responsibility —
// internal/httpapi decodes the raw body via json.RawMessage
// (encoding/json does not distinguish "key absent" from "key present with
// null value" when decoding straight into plain pointers) before building
// this struct, per docs/api-contract.md.
type UpdatePatch struct {
	Content  *string
	Domain   *string
	Source   *string
	Agent    *string // present and "" clears the agent (equivalent to "agent": null in the body)
	Model    *string // present and "" clears the model
	Project  *string // present and "" makes the entry global
	Tags     *[]string
	Metadata *map[string]string
	// Kind corrects the nature of the fact (D38): it must be one of
	// factkind.All and marks kind_source=manual, which no backfill overwrites.
	Kind *string
}

// Update partially overwrites 1 existing entry by id (D13,
// docs/api-contract.md, memory_update section) — a manual, deliberate
// edit: unlike Add, it runs neither fact extraction (D12) nor dedup
// (DedupSimilarityThreshold), the PATCH's target id is always the one
// updated, never another. It always re-embeds when it writes, even when
// only tags/metadata changed — there is no content diff to decide whether
// to recompute the embedding (same behavior as before D28).
func (s *MemoryStore) Update(ctx context.Context, id string, patch UpdatePatch, now time.Time) (schema.Entry, error) {
	s.mu.RLock()
	existing, ok := s.index[id]
	s.mu.RUnlock()
	if !ok {
		return schema.Entry{}, ErrNotFound
	}

	entry := existing

	if patch.Content != nil {
		if strings.TrimSpace(*patch.Content) == "" {
			return schema.Entry{}, &ValidationError{Code: "content_required", Message: "content is required"}
		}
		entry.Content = redact.Redact(*patch.Content).Text
	}

	if patch.Domain != nil {
		canon := schema.CanonicalDomain(*patch.Domain)
		patch.Domain = &canon
		if err := schema.ValidateDomain(*patch.Domain); err != nil {
			return schema.Entry{}, &ValidationError{Code: "invalid_domain", Message: err.Error()}
		}
		entry.Domain = *patch.Domain
		entry.DomainCategory = schema.DomainCategory(*patch.Domain)
	}

	if patch.Source != nil {
		if strings.TrimSpace(*patch.Source) == "" {
			return schema.Entry{}, &ValidationError{Code: "source_required", Message: "source is required"}
		}
		entry.Source = *patch.Source
	}

	if patch.Agent != nil {
		if *patch.Agent == "" {
			entry.Agent = nil
		} else {
			agent := *patch.Agent
			entry.Agent = &agent
		}
	}

	if patch.Model != nil || patch.Project != nil {
		model, project := entry.ModelValue(), entry.ProjectValue()
		if patch.Model != nil {
			model = *patch.Model
		}
		if patch.Project != nil {
			project = *patch.Project
		}
		if err := validateProvenance(model, project); err != nil {
			return schema.Entry{}, err
		}
		scopeChanged := project != entry.ProjectValue()
		entry.Model, entry.Project = nil, nil
		setProvenance(&entry, model, project)
		if scopeChanged {
			// overrides only make sense for the project they were decided in
			entry.Metadata = withoutKey(entry.Metadata, metadataKeyOverrides)
		}
	}

	if patch.Tags != nil {
		entry.Tags = schema.NormalizeTags(*patch.Tags)
	}

	if patch.Kind != nil && !factkind.Valid(*patch.Kind) {
		return schema.Entry{}, &ValidationError{Code: "invalid_kind", Message: "kind must be one of " + strings.Join(factkind.All, ", ")}
	}

	if patch.Metadata != nil {
		// A null/empty replacement yields an empty map, never nil (it would
		// serialize JSON `null`, which breaks clients typed as object, e.g. the
		// WebUI's Zod schema). The kind tag is reserved like source_content: a
		// replacement cannot set it (use the kind field) and drops a machine tag
		// (the backfill can redo it), but a human correction survives.
		md := withoutKey(withoutKey(*patch.Metadata, factkind.MetadataKey), factkind.MetadataSourceKey)
		if keep := keepKind(existing.Metadata); keep[factkind.MetadataSourceKey] == factkind.SourceManual {
			for k, v := range keep {
				md[k] = v
			}
		}
		entry.Metadata = md
	}

	if patch.Kind != nil {
		entry.Metadata = withMetadata(withMetadata(entry.Metadata, factkind.MetadataKey, *patch.Kind), factkind.MetadataSourceKey, factkind.SourceManual)
	}

	entry.UpdatedAt = now

	internalMD, err := s.internalizeMetadata(ctx, entry.Metadata)
	if err != nil {
		return schema.Entry{}, err
	}
	entry.Metadata = internalMD

	vec, err := s.embedText(ctx, entry.Content)
	if err != nil && !errors.Is(err, errNoEmbedding) {
		return schema.Entry{}, fmt.Errorf("generate embedding: %w", err)
	}
	row, vec := s.entryRow(entry, vec)
	if err := s.db.UpsertEntry(ctx, row); err != nil {
		return schema.Entry{}, err
	}

	s.vecs.upsert(entry.ID, entry.Domain, vec)
	s.mu.Lock()
	s.index[entry.ID] = entry
	s.mu.Unlock()

	return s.externalizeEntry(ctx, entry), nil
}

// withoutKey returns a copy of base without key.
func withoutKey(base map[string]string, key string) map[string]string {
	md := make(map[string]string, len(base))
	for k, v := range base {
		if k != key {
			md[k] = v
		}
	}
	return md
}

// ProjectCounts returns how many entries each project holds (global entries
// are not listed).
// SourceCounts counts memories per source (the tool that wrote them).
func (s *MemoryStore) SourceCounts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for _, e := range s.index {
		out[e.Source]++
	}
	return out
}

func (s *MemoryStore) ProjectCounts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for _, e := range s.index {
		if p := e.ProjectValue(); p != "" {
			out[p]++
		}
	}
	return out
}
