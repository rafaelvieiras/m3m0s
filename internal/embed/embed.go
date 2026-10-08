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
	"strings"
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
//
// dimensions > 0 sends the optional "dimensions" parameter (supported by the
// OpenAI text-embedding-3 family and compatible servers) to shrink the vector;
// 0 leaves the provider default.
func New(baseURL, apiKey, model string, dimensions int) Func {
	client := &http.Client{}
	return func(ctx context.Context, text string) ([]float32, error) {
		body := map[string]any{"input": text, "model": model}
		if dimensions > 0 {
			body["dimensions"] = dimensions
		}
		reqBody, err := json.Marshal(body)
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
			return nil, &StatusError{Code: resp.StatusCode}
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		var parsed response
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
			return nil, errors.New("response contains no embeddings")
		}
		return parsed.Data[0].Embedding, nil
	}
}

// BatchFunc embeds several texts in one request and returns their vectors in
// input order (used by the re-embedding job, D36).
type BatchFunc func(ctx context.Context, texts []string) ([][]float32, error)

// StatusError is the error of a non-200 embeddings response. It never carries
// the response body or any credential, only the status.
type StatusError struct{ Code int }

func (e *StatusError) Error() string {
	return fmt.Sprintf("embeddings API response: %d %s", e.Code, http.StatusText(e.Code))
}

// IsTransient reports whether err is worth retrying with a backoff: network
// errors, 408, 429 and 5xx. Context cancellation is not transient.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusRequestTimeout || se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	return strings.HasPrefix(err.Error(), "send request:") || strings.HasPrefix(err.Error(), "read response:")
}

// IsFatal reports whether err means every further request with the same
// configuration will fail too (bad key, forbidden, unknown model or endpoint).
func IsFatal(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden || se.Code == http.StatusNotFound
	}
	return false
}

type batchResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// NewBatch returns a BatchFunc that sends the texts as the array form of the
// "input" parameter of POST {baseURL}/embeddings. Same parameters as New.
func NewBatch(baseURL, apiKey, model string, dimensions int) BatchFunc {
	client := &http.Client{}
	return func(ctx context.Context, texts []string) ([][]float32, error) {
		body := map[string]any{"input": texts, "model": model}
		if dimensions > 0 {
			body["dimensions"] = dimensions
		}
		reqBody, err := json.Marshal(body)
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
			return nil, &StatusError{Code: resp.StatusCode}
		}
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		var parsed batchResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(parsed.Data) != len(texts) {
			return nil, fmt.Errorf("response has %d embeddings for %d inputs", len(parsed.Data), len(texts))
		}
		out := make([][]float32, len(texts))
		for pos, d := range parsed.Data {
			i := d.Index
			if i < 0 || i >= len(texts) || out[i] != nil {
				i = pos // providers that omit "index" answer in input order
				if out[i] != nil {
					return nil, errors.New("response has duplicate indexes")
				}
			}
			if len(d.Embedding) == 0 {
				return nil, errors.New("response contains an empty embedding")
			}
			out[i] = d.Embedding
		}
		return out, nil
	}
}
