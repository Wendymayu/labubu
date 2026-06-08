package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labubu/labubu/internal/storage"
)

// LLMConfigHandler holds HTTP handlers for LLM config endpoints.
type LLMConfigHandler struct {
	store storage.Store
}

// NewLLMConfigHandler creates a new LLMConfigHandler.
func NewLLMConfigHandler(store storage.Store) *LLMConfigHandler {
	return &LLMConfigHandler{store: store}
}

// ServeHTTP dispatches LLM config requests.
func (h *LLMConfigHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/llm-configs")
	id := strings.TrimPrefix(path, "/")

	if path == "" || path == "/" {
		// No ID in path — list or create.
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	// ID in path — update or delete.
	switch r.Method {
	case http.MethodPut:
		h.update(w, r, id)
	case http.MethodDelete:
		h.del(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *LLMConfigHandler) list(w http.ResponseWriter, r *http.Request) {
	configs, err := h.store.GetLLMConfigs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if configs == nil {
		configs = []storage.LLMConfig{}
	}
	// Mask API keys in response.
	for i := range configs {
		configs[i].APIKey = storage.MaskAPIKey(configs[i].APIKey)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"configs": configs})
}

func (h *LLMConfigHandler) create(w http.ResponseWriter, r *http.Request) {
	var c storage.LLMConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if c.ModelName == "" || c.ProviderURL == "" || c.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model_name, provider_url, and api_key are required"})
		return
	}
	// Set defaults.
	if c.Temperature == 0 {
		c.Temperature = 0.7
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 4096
	}
	c.ID = uuid.NewString()

	if err := h.store.CreateLLMConfig(r.Context(), &c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	c.APIKey = storage.MaskAPIKey(c.APIKey)
	writeJSON(w, http.StatusCreated, c)
}

func (h *LLMConfigHandler) update(w http.ResponseWriter, r *http.Request, id string) {
	var c storage.LLMConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	c.ID = id

	if err := h.store.UpdateLLMConfig(r.Context(), &c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *LLMConfigHandler) del(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.DeleteLLMConfig(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
