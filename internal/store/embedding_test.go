package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// dimsEmbedServer is an embeddings server that honors "dimensions" (default
// 8) and rejects the model "bad-model".
func dimsEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input      string `json:"input"`
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model == "bad-model" {
			http.Error(w, "no such model", http.StatusNotFound)
			return
		}
		dims := 8
		if req.Dimensions > 0 {
			dims = req.Dimensions
		}
		v := fakeVector(req.Input)
		out := make([]float32, dims)
		for i := range out {
			out[i] = v[i%len(v)]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": out}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func addNote(t *testing.T, s *MemoryStore, content string) {
	t.Helper()
	if _, err := s.Add(context.Background(), AddInput{Content: content, Domain: "personal:test"}, time.Now()); err != nil {
		t.Fatalf("Add(%q): %v", content, err)
	}
}

func TestEmbeddingConfigSwitchRequiresConfirmAndKeepsServing(t *testing.T) {
	srv := dimsEmbedServer(t)
	dir := t.TempDir()
	s, err := New(dir, "env-key", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	addNote(t, s, "alpha note about gardening")
	addNote(t, s, "beta note about sailing")

	st, err := s.EmbeddingStatus(ctx)
	if err != nil || st.Source != "environment" || !st.Active || st.Total != 2 || st.Stale != 0 || st.VectorDims != 8 {
		t.Fatalf("initial status = %+v, %v", st, err)
	}

	// Smaller vectors make the two stored ones stale: needs confirmation.
	shrink := EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "text-embedding-3-small", Dimensions: 4}}
	_, err = s.SetEmbeddingSettings(ctx, shrink, false)
	var need *ReembedRequiredError
	if !errors.As(err, &need) || need.Stale != 2 {
		t.Fatalf("want ReembedRequiredError{2}, got %v", err)
	}
	if st, _ := s.EmbeddingStatus(ctx); st.Dimensions != 0 || st.Source != "environment" {
		t.Fatalf("rejected change altered the config: %+v", st)
	}

	st, err = s.SetEmbeddingSettings(ctx, shrink, true)
	if err != nil || st.Source != "database" || st.VectorDims != 4 || st.Stale != 2 || st.Current != 0 {
		t.Fatalf("after switch = %+v, %v", st, err)
	}

	// Still serving: keyword side finds the old rows, a new write uses the
	// new space and is indexed.
	got, err := s.Search(ctx, "gardening", 5, Filter{}, time.Now())
	if err != nil || len(got) == 0 {
		t.Fatalf("search during transition: %v, %d results", err, len(got))
	}
	addNote(t, s, "gamma note about climbing")
	if st, _ = s.EmbeddingStatus(ctx); st.Current != 1 || st.Stale != 2 || s.vecs.len() != 1 {
		t.Fatalf("after new add: %+v, indexed=%d", st, s.vecs.len())
	}

	// A restart keeps the saved config and indexes only the matching space.
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := New(dir, "env-key", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ = s2.EmbeddingStatus(ctx); st.Source != "database" || st.Dimensions != 4 || s2.vecs.len() != 1 {
		t.Fatalf("after restart: %+v, indexed=%d", st, s2.vecs.len())
	}
}

func TestEmbeddingConfigRejectsBadProviderWithoutChange(t *testing.T) {
	srv := dimsEmbedServer(t)
	s, err := New(t.TempDir(), "env-key", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	addNote(t, s, "one note")

	for name, in := range map[string]EmbeddingUpdate{
		"unknown model": {EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "bad-model"}},
		"no model":      {EmbeddingSettings: EmbeddingSettings{Enabled: true}},
		"dead url":      {EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "m", BaseURL: "http://127.0.0.1:1"}},
	} {
		if _, err := s.SetEmbeddingSettings(ctx, in, true); !errors.Is(err, ErrEmbeddingInvalid) {
			t.Errorf("%s: want ErrEmbeddingInvalid, got %v", name, err)
		}
	}
	st, _ := s.EmbeddingStatus(ctx)
	if st.Source != "environment" || st.Model != "text-embedding-3-small" || !st.Active || st.Stale != 0 {
		t.Fatalf("a rejected change altered the active config: %+v", st)
	}
}

func TestEmbeddingConfigDisableGoesKeywordOnlyAndKeyIsKept(t *testing.T) {
	srv := dimsEmbedServer(t)
	s, err := New(t.TempDir(), "", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if s.HasEmbeddings() {
		t.Fatal("no key and no config must be keyword-only")
	}
	key := "db-key"
	st, err := s.SetEmbeddingSettings(ctx, EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "text-embedding-3-small"}, APIKey: &key}, false)
	if err != nil || !st.Active || st.KeySource != "database" || st.VectorDims != 8 {
		t.Fatalf("enable with a database key: %+v, %v", st, err)
	}
	st, err = s.SetEmbeddingSettings(ctx, EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: false, Model: "text-embedding-3-small"}}, false)
	if err != nil || st.Active || s.HasEmbeddings() || !st.HasAPIKey {
		t.Fatalf("disable: %+v, %v", st, err)
	}
}
