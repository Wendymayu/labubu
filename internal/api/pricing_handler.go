package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labubu/labubu/internal/storage"
)

// PricingHandler holds HTTP handlers for model pricing endpoints.
type PricingHandler struct {
	store storage.Store
}

// NewPricingHandler creates a new PricingHandler.
func NewPricingHandler(store storage.Store) *PricingHandler {
	return &PricingHandler{store: store}
}

// ServeHTTP dispatches pricing requests.
func (h *PricingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/model-pricing")

	switch r.Method {
	case http.MethodGet:
		h.list(w, r)
	case http.MethodPost:
		if path == "/recalc" {
			h.recalc(w, r)
		} else {
			h.upsert(w, r)
		}
	case http.MethodDelete:
		modelName := strings.TrimPrefix(path, "/")
		h.del(w, r, modelName)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *PricingHandler) list(w http.ResponseWriter, r *http.Request) {
	models, err := h.store.GetModelPricing(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if models == nil {
		models = []storage.ModelPricing{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": models})
}

func (h *PricingHandler) upsert(w http.ResponseWriter, r *http.Request) {
	var p storage.ModelPricing
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if p.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model_name is required"})
		return
	}
	if err := h.store.UpsertModelPricing(r.Context(), p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *PricingHandler) del(w http.ResponseWriter, r *http.Request, modelName string) {
	if modelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model_name is required"})
		return
	}
	if err := h.store.DeleteModelPricing(r.Context(), modelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *PricingHandler) recalc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// Iterate all traces via ListTraces with a large page size.
	q := storage.TraceQuery{Page: 1, PageSize: 10000}
	result, err := h.store.ListTraces(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	tracesUpdated := 0
	for _, t := range result.Traces {
		traceIDBytes, err := hex.DecodeString(t.TraceIDHex)
		if err != nil {
			continue
		}
		var traceID [16]byte
		copy(traceID[:], traceIDBytes)
		if err := h.store.UpdateTraceCost(r.Context(), traceID); err != nil {
			continue
		}
		tracesUpdated++
	}

	// Recalculate session costs similarly.
	sq := storage.SessionQuery{Page: 1, PageSize: 10000}
	sResult, err := h.store.ListSessions(r.Context(), sq)
	sessionsUpdated := 0
	if err == nil {
		for range sResult.Sessions {
			sessionsUpdated++
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "recalc complete",
		"traces_updated":   tracesUpdated,
		"sessions_updated": sessionsUpdated,
	})
}
