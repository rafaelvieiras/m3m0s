package apitypes

import (
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
)

// Fact nature ("kind", D38): the labelling sample, and the optional backfill of
// facts stored before the tag existed.

// KindSampleResponse is the body of GET /kinds/sample.
type KindSampleResponse struct {
	Items []factkind.SampleItem `json:"items"`
}

// KindBackfillEstimateResponse is the body of POST /kinds/backfill/estimate.
// Mode "llm" spends extraction-provider tokens (Approximate: characters / 4);
// "heuristic" (no extractor configured) is free. The cost is null unless the
// operator set M3M0S_EXTRACT_PRICE_IN_PER_MTOK and M3M0S_EXTRACT_PRICE_OUT_PER_MTOK.
type KindBackfillEstimateResponse struct {
	Mode            string   `json:"mode"`
	Model           string   `json:"model,omitempty"`
	Pending         int      `json:"pending"`
	Requests        int      `json:"requests"`
	InputTokens     int64    `json:"input_tokens"`
	OutputTokens    int64    `json:"output_tokens"`
	Approximate     bool     `json:"approximate"`
	PriceInPerMTok  *float64 `json:"price_in_per_million_tokens_usd"`
	PriceOutPerMTok *float64 `json:"price_out_per_million_tokens_usd"`
	EstimatedCost   *float64 `json:"estimated_cost_usd"`
	EstimatedSecond int      `json:"estimated_seconds"`
}

// KindBackfillStartRequest is the body of POST /kinds/backfill. Confirm must be
// true: the run spends provider tokens.
type KindBackfillStartRequest struct {
	Confirm bool `json:"confirm"`
}

// KindBackfillProgress is the state of the backfill job (GET /kinds/backfill).
// Done + Errors + Skipped == Total once a run ends cleanly.
type KindBackfillProgress struct {
	Running    bool       `json:"running"`
	Mode       string     `json:"mode,omitempty"`
	Total      int        `json:"total"`
	Done       int        `json:"done"`
	Errors     int        `json:"errors"`
	Skipped    int        `json:"skipped"`
	ETASeconds int        `json:"eta_seconds"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

// KindBackfillStartResponse is the body of POST /kinds/backfill.
type KindBackfillStartResponse struct {
	Started bool `json:"started"`
}

// KindBackfillCancelResponse is the body of DELETE /kinds/backfill.
type KindBackfillCancelResponse struct {
	Cancelled bool `json:"cancelled"`
}
