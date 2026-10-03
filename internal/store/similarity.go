package store

import "math"

// cosineSimilarity computes the cosine similarity between 2 vectors —
// replaces the vector search chromem-go did internally (D28,
// docs/decisions/). It normalizes by the magnitudes of both vectors, so it
// works correctly with normalized embeddings (OpenAI already normalizes)
// or not, without needing an extra flag to know which is the case.
// Vectors of different sizes or empty ones (should not happen outside
// corrupted data) return 0.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
