// Runtime embedding configuration (#39, D36): GET/PUT /api/v1/embedding-config.
// The API key is write-only and never appears in a response.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

func toEmbeddingResponse(st store.EmbeddingStatus) apitypes.EmbeddingConfigResponse {
	out := apitypes.EmbeddingConfigResponse{
		Enabled: st.Enabled, BaseURL: st.BaseURL, Model: st.Model, Dimensions: st.Dimensions,
		VectorDims: st.VectorDims, Source: st.Source, HasAPIKey: st.HasAPIKey, KeySource: st.KeySource,
		Active:  st.Active,
		Vectors: apitypes.EmbeddingVectors{Total: st.Total, Current: st.Current, Stale: st.Stale},
	}
	if !st.UpdatedAt.IsZero() {
		t := st.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func (s *Server) handleEmbeddingConfigGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.EmbeddingStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "embedding_config_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toEmbeddingResponse(st))
}

func (s *Server) handleEmbeddingConfigPut(w http.ResponseWriter, r *http.Request) {
	var req apitypes.EmbeddingConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	st, err := s.store.SetEmbeddingSettings(r.Context(), store.EmbeddingUpdate{
		EmbeddingSettings: store.EmbeddingSettings{Enabled: req.Enabled, BaseURL: req.BaseURL, Model: req.Model, Dimensions: req.Dimensions},
		APIKey:            req.APIKey,
	}, req.ConfirmReembed)
	var reembed *store.ReembedRequiredError
	switch {
	case errors.As(err, &reembed):
		writeError(w, http.StatusConflict, "reembed_required", reembed.Error())
	case errors.Is(err, store.ErrEmbeddingInvalid):
		writeError(w, http.StatusUnprocessableEntity, "embedding_config_invalid", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "embedding_config_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, toEmbeddingResponse(st))
	}
}
