// Package embed implements the OpenAI-compatible embeddings HTTP call
// that internal/store needs (D28, docs/decisions/) — a faithful port of
// chromem-go's NewEmbeddingFuncOpenAI/OpenAICompat (chromem-go itself was
// removed along with the vector layer it provided; only the part that
// "calls /embeddings and returns the vector" is still useful).
//
// Unlike chromem-go, it does not carry the `normalized` flag — the cosine
// similarity computed in internal/store (internal/store/similarity.go)
// always normalizes by the magnitudes of both vectors, so it works
// correctly with normalized or non-normalized embeddings without needing
// to know which one it is.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// DefaultBaseURL is the base URL of the public OpenAI API.
const DefaultBaseURL = "https://api.openai.com/v1"

// Func is the embedding function type used by internal/store — same
// signature chromem.EmbeddingFunc had, to minimize the diff for existing
// consumers.
type Func func(ctx context.Context, text string) ([]float32, error)

type response struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// New returns a Func that calls POST {baseURL}/embeddings in the
// OpenAI-compatible format (used both by real OpenAI and by any compatible
// proxy, via -openai-base/M3M0S_OPENAI_BASE_URL).
func New(baseURL, apiKey, model string) Func {
	client := &http.Client{}
	return func(ctx context.Context, text string) ([]float32, error) {
		reqBody, err := json.Marshal(map[string]string{"input": text, "model": model})
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/embeddings", bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("embeddings API response: %s", resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		var parsed response
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
			return nil, errors.New("response contains no embeddings")
		}
		return parsed.Data[0].Embedding, nil
	}
}
