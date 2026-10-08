package main

import (
	"context"
	"flag"
	"fmt"
	iofs "io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/chat"
	"github.com/rafaelvieiras/m3m0s/internal/extract"
	"github.com/rafaelvieiras/m3m0s/internal/httpapi"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/selfindex"
	"github.com/rafaelvieiras/m3m0s/internal/store"
	"github.com/rafaelvieiras/m3m0s/webui"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.String("port", firstNonEmpty(os.Getenv("M3M0S_PORT"), "19234"), "HTTP port")
	dataDir := fs.String("data", firstNonEmpty(os.Getenv("M3M0S_DATA_DIR"), "~/.m3m0s/data"), "directory for persisted data")
	apiKey := fs.String("key", os.Getenv("OPENAI_API_KEY"), "OpenAI API key, used for embedding — when absent the server runs keyword-only (FTS5 search, exact-match dedup)")
	openaiBase := fs.String("openai-base", os.Getenv("M3M0S_OPENAI_BASE_URL"), "OpenAI-compatible base URL, optional")
	extractKey := fs.String("extract-key", os.Getenv("M3M0S_EXTRACT_API_KEY"), "OpenRouter API key for fact extraction (Phase 4, D12) — when absent, extraction is disabled")
	extractBase := fs.String("extract-base", firstNonEmpty(os.Getenv("M3M0S_EXTRACT_BASE_URL"), extract.DefaultBaseURL), "OpenAI-compatible base URL for fact extraction")
	extractModel := fs.String("extract-model", firstNonEmpty(os.Getenv("M3M0S_EXTRACT_MODEL"), extract.DefaultModel), "model used for fact extraction")
	jobsTick := fs.Duration("jobs-tick", firstNonEmptyDuration(os.Getenv("M3M0S_JOBS_TICK"), 30*time.Second), "check interval of the job scheduler (internal/jobs) — not the frequency of each job, just the clock tick")
	retroExtractInterval := fs.Duration("retro-extract-interval", firstNonEmptyDuration(os.Getenv("M3M0S_RETRO_EXTRACT_INTERVAL"), 2*time.Hour), "interval between runs of the retroactive extraction job (only runs with fact extraction enabled)")
	forgetSweepInterval := fs.Duration("forget-sweep-interval", firstNonEmptyDuration(os.Getenv("M3M0S_FORGET_SWEEP_INTERVAL"), 12*time.Hour), "interval between runs of the automatic forgetting job (D22, docs/decisions/ — closes A9); always runs, regardless of whether fact extraction is enabled")
	chatKey := fs.String("chat-key", os.Getenv("M3M0S_CHAT_API_KEY"), "OpenRouter API key for the WebUI floating chat (D23) — empty falls back to the value of -extract-key/M3M0S_EXTRACT_API_KEY; absent in both disables POST /api/v1/chat")
	chatBase := fs.String("chat-base", firstNonEmpty(os.Getenv("M3M0S_CHAT_BASE_URL"), chat.DefaultBaseURL), "OpenAI-compatible base URL for the WebUI chat")
	chatModel := fs.String("chat-model", firstNonEmpty(os.Getenv("M3M0S_CHAT_MODEL"), chat.DefaultModel), "model used for the WebUI chat")
	addSyncTimeout := fs.Duration("add-sync-timeout", firstNonEmptyDuration(os.Getenv("M3M0S_ADD_SYNC_TIMEOUT"), 300*time.Millisecond), "how long POST /api/v1/memories waits for Add to finish before degrading to 202 + poll on GET /memories/pending/:id (D26, docs/decisions/); <= 0 disables the async path and always waits for Add to really finish (pre-D26 behavior)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The in-memory index is large and long-lived, so the default GOGC=100
	// would let the heap sit at roughly twice its live size. Collect earlier
	// unless the operator chose a value (#38: -90 MB at the boot peak, same
	// idle).
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}

	if *apiKey == "" {
		log.Printf("OPENAI_API_KEY not set: running keyword-only (no embeddings; search uses FTS5, dedup is exact-match)")
	}

	dataDirExpanded, err := expandHome(*dataDir)
	if err != nil {
		return fmt.Errorf("expand -data: %w", err)
	}
	if err := os.MkdirAll(dataDirExpanded, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	memStore, err := store.New(dataDirExpanded, *apiKey, *openaiBase)
	if err != nil {
		return fmt.Errorf("start store: %w", err)
	}

	// One-time import of the 6 hand-rolled JSON files + chromem-go
	// (pre-D28 architecture, docs/decisions/) into SQLite — a no-op
	// if the entries table already has data (installation already migrated,
	// or new with no legacy data at all). It must run before anything else
	// touches the database (auth/jobs below), otherwise legacy api
	// keys/intervals would lose the race against the new-key bootstrap.
	if err := memStore.ImportLegacyData(context.Background()); err != nil {
		log.Printf("  legacy import: failed: %v", err)
	}

	// Auto-indexing of the documentation embedded in the binary (D25,
	// docs/decisions/) — runs in a separate goroutine so as not to delay the
	// server boot (it can take minutes the 1st time, same order of magnitude
	// observed by m3m0s watch in D24); idempotent across restarts via a hash
	// persisted in <dataDir>/selfdocs-state.json. Independent of whether fact
	// extraction is enabled in this run — without an Extractor, chunks go in
	// raw, but are already searchable via the chat's RAG (D23).
	log.Printf("  selfdocs: indexing embedded documentation in the background...")
	go func() {
		result, err := selfindex.Run(context.Background(), memStore, dataDirExpanded)
		if err != nil {
			log.Printf("  selfdocs: failed: %v", err)
			return
		}
		log.Printf("  selfdocs: done (%d file(s) total, %d reindexed, %d unchanged, %d failed, %d chunk(s) with errors)",
			result.FilesTotal, result.FilesIndexed, result.FilesUnchanged, result.FilesFailed, result.ChunksFailed)
	}()

	// Custom intervals set via the WebUI (PATCH /api/v1/jobs/:name, D27,
	// docs/decisions/) survive a restart through the SQLite job_intervals
	// table (D28) — if there is a saved override for the job name, it wins over
	// the flag/env value (*forgetSweepInterval/*retroExtractInterval below);
	// otherwise, behavior is identical to before D27.
	jobIntervalOverrides, err := jobs.LoadIntervals(context.Background(), memStore.DB())
	if err != nil {
		log.Printf("  jobs: failed to load custom intervals: %v", err)
		jobIntervalOverrides = map[string]time.Duration{}
	}
	resolveJobInterval := func(name string, def time.Duration) time.Duration {
		if v, ok := jobIntervalOverrides[name]; ok {
			return v
		}
		return def
	}

	// Job system (in-process routines, internal/jobs) — the scheduler is
	// always created, regardless of whether fact extraction is enabled:
	// forget-sweep (D22) only operates on already stored metadata (eligibility =
	// having metadata.source_content_ref, D12) and does not need an Extractor
	// configured in this run to have work to do.
	sched := jobs.NewScheduler(*jobsTick)
	forgetSweepIntervalResolved := resolveJobInterval("forget-sweep", *forgetSweepInterval)
	sched.Register(jobs.Job{
		Name:         "forget-sweep",
		Interval:     forgetSweepIntervalResolved,
		InitialDelay: 5 * time.Minute,
		Run: func(ctx context.Context) (string, error) {
			result, err := memStore.ForgetSweep(ctx, false, time.Now())
			if err != nil {
				return "", err
			}
			if len(result.SoftDeleted) == 0 && len(result.HardDeleted) == 0 {
				return "", nil
			}
			summary := fmt.Sprintf("%d evaluated, %d soft-deleted, %d hard-deleted",
				result.Evaluated, len(result.SoftDeleted), len(result.HardDeleted))
			log.Printf("  forget-sweep: %s", summary)
			return summary, nil
		},
	})

	// LLM fact extraction (Phase 4, D12) — off by default, enabled only when
	// M3M0S_EXTRACT_API_KEY/-extract-key is present (same toggle pattern that
	// -openai-base already uses for embeddings).
	if *extractKey != "" {
		memStore.SetExtractor(extract.New(*extractKey, *extractBase, *extractModel))
		log.Printf("  fact extraction: enabled (model=%s)", *extractModel)

		// Retroactive fact extraction (internal/store/retroextract.go) scans
		// memories that never went through extraction (extraction disabled at
		// Add time, or that fell back to raw content because of a network
		// failure — a real finding in production, see docs/decisions/) and runs
		// extraction on them. Only registered when extraction is enabled —
		// without an Extractor, the job would be a perpetual no-op.
		retroExtractIntervalResolved := resolveJobInterval("retroactive-extraction", *retroExtractInterval)
		sched.Register(jobs.Job{
			Name:         "retroactive-extraction",
			Interval:     retroExtractIntervalResolved,
			InitialDelay: 2 * time.Minute,
			Run: func(ctx context.Context) (string, error) {
				processed, err := memStore.RunRetroactiveExtraction(ctx)
				if err != nil {
					return "", err
				}
				if processed == 0 {
					return "", nil
				}
				summary := fmt.Sprintf("%d memory(ies) processed", processed)
				log.Printf("  retroactive extraction: %s", summary)
				return summary, nil
			},
		})
		log.Printf("  jobs: retroactive extraction every %s (tick of %s)", retroExtractIntervalResolved, *jobsTick)
	} else {
		log.Printf("  fact extraction: disabled (M3M0S_EXTRACT_API_KEY absent)")
	}

	go sched.Start(context.Background())
	log.Printf("  jobs: forget-sweep every %s (tick of %s)", forgetSweepIntervalResolved, *jobsTick)

	keyStore, err := auth.LoadStore(memStore.DB())
	if err != nil {
		return fmt.Errorf("load api keys: %w", err)
	}
	// First boot: the original memory-server had no auth by default and was
	// reachable without any key — here that stops being acceptable as soon as
	// serve becomes reachable over the network (docs/api-contract.md, Auth
	// section). Generates a bootstrap key and shows it only this once.
	if keyStore.IsEmpty() {
		rawKey, _, err := keyStore.Create("bootstrap")
		if err != nil {
			return fmt.Errorf("generate initial api key: %w", err)
		}
		fmt.Fprintf(os.Stderr,
			"No API key found — generated a new one. Save it, it will not be shown again:\n  %s\nSet M3M0S_API_KEY=%s in the clients.\n",
			rawKey, rawKey)
	}

	// webui.DistFS embeds the whole dist/ directory (go:embed preserves the
	// "dist/" prefix in the path); fs.Sub strips that to serve from the right
	// root (docs/api-contract.md, "/api/v1 prefix and embedded WebUI"
	// section).
	webUIAssets, err := iofs.Sub(webui.DistFS, "dist")
	if err != nil {
		return fmt.Errorf("prepare embedded webui assets: %w", err)
	}

	// WebUI floating chat (D23, docs/decisions/) — enabled when there is a
	// resolved API key, with -chat-key/M3M0S_CHAT_API_KEY taking priority and
	// falling back to the value already configured for extraction (D12) as a
	// convenience shortcut: whoever has already configured fact extraction gets
	// the chat "for free" with the same OpenRouter key, without having to
	// duplicate the environment variable.
	resolvedChatKey := firstNonEmpty(*chatKey, *extractKey)
	var chatClient *chat.Client
	if resolvedChatKey != "" {
		chatClient = chat.New(resolvedChatKey, *chatBase, *chatModel)
		log.Printf("  webui chat: enabled (model=%s)", *chatModel)
	} else {
		log.Printf("  webui chat: disabled (M3M0S_CHAT_API_KEY/M3M0S_EXTRACT_API_KEY absent)")
	}

	srv := httpapi.New(memStore, keyStore, webUIAssets, chatClient, *addSyncTimeout, sched, memStore.DB())
	log.Printf("  async add: timeout of %s before degrading to 202 + poll (D26; <= 0 disables)", *addSyncTimeout)

	addr := ":" + *port
	log.Printf("m3m0s serve listening on %s", addr)
	log.Printf("  data: %s | model: %s", dataDirExpanded, httpapi.EmbeddingModel)
	log.Printf("  webui: served on %s (embedded)", addr)

	return http.ListenAndServe(addr, srv.Handler())
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyDuration parses envVal as a time.Duration (e.g. "30s",
// "2h") if non-empty; an empty or invalid envVal falls back to the default —
// same spirit as firstNonEmpty, but for the jobs' duration flags
// (M3M0S_JOBS_TICK/M3M0S_RETRO_EXTRACT_INTERVAL).
func firstNonEmptyDuration(envVal string, def time.Duration) time.Duration {
	if envVal == "" {
		return def
	}
	d, err := time.ParseDuration(envVal)
	if err != nil {
		log.Printf("m3m0s: invalid duration value %q, using default %s: %v", envVal, def, err)
		return def
	}
	return d
}
