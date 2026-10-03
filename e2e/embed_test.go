//go:build e2e

package e2e

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"
)

const fakeEmbeddingDims = 64

// fakeEmbedding generates a deterministic "embedding" via bag-of-words
// feature hashing: each word picks a dimension and a sign from its hash,
// the votes accumulate, and the final vector is normalized to L2 norm = 1.
// It is not a real semantic embedding, but it has the minimal property the
// tests need: texts with many words in common have high cosine similarity,
// identical text gives similarity of exactly 1.0, and completely unrelated
// texts give low similarity — enough to really validate dedup, search
// ranking and filter isolation, without a network.
func fakeEmbedding(text string) []float32 {
	v := make([]float32, fakeEmbeddingDims)

	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		sum := h.Sum32()
		dim := int(sum % fakeEmbeddingDims)
		sign := float32(1)
		if (sum>>8)&1 == 1 {
			sign = -1
		}
		v[dim] += sign
	}

	var normSq float64
	for _, x := range v {
		normSq += float64(x) * float64(x)
	}
	if normSq == 0 {
		// Text with no recognizable word (rare) — return a fixed unit
		// vector instead of dividing by zero.
		v[0] = 1
		return v
	}
	norm := math.Sqrt(normSq)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

type fakeEmbedRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type fakeEmbedDatum struct {
	Embedding []float32 `json:"embedding"`
}

type fakeEmbedResponse struct {
	Data []fakeEmbedDatum `json:"data"`
}

// newFakeEmbedServer starts a local HTTP server that speaks the same
// contract chromem.NewEmbeddingFuncOpenAICompat expects: POST /embeddings
// with {"input":...,"model":...}, response {"data":[{"embedding":[...]}]} —
// see github.com/philippgille/chromem-go@v0.7.0/embed_openai.go. `store.New`
// always passes normalized=true when an -openai-base is used, so the vector
// must already come normalized to norm 1 from here (done in
// fakeEmbedding), otherwise the similarity scores come out wrong.
func newFakeEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req fakeEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := fakeEmbedResponse{Data: []fakeEmbedDatum{{Embedding: fakeEmbedding(req.Input)}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
