package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func doJSON(t *testing.T, method, url string, body interface{}) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return rec, req
}

func setupLLMConfigHandler(t *testing.T) *LLMConfigHandler {
	t.Helper()
	store, err := storage.NewChDBStore("")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewLLMConfigHandler(store)
}

func TestLLMConfigHandler_CreateAndList(t *testing.T) {
	handler := setupLLMConfigHandler(t)

	// Create a config.
	createBody := map[string]interface{}{
		"model_name":   "claude-opus-4-8",
		"provider_url": "https://api.anthropic.com/v1/messages",
		"api_key":      "sk-ant-full-key-12345",
	}
	rec, req := doJSON(t, http.MethodPost, "/api/v1/llm-configs", createBody)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created storage.LLMConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty id")
	}
	if created.ModelName != "claude-opus-4-8" {
		t.Fatalf("expected model_name claude-opus-4-8, got %s", created.ModelName)
	}
	// API key should be masked in response.
	if created.APIKey == "sk-ant-full-key-12345" {
		t.Fatal("api_key should be masked in response")
	}
	// Defaults should be set.
	if created.Temperature != 0.7 {
		t.Fatalf("expected temperature 0.7, got %f", created.Temperature)
	}
	if created.MaxTokens != 4096 {
		t.Fatalf("expected max_tokens 4096, got %d", created.MaxTokens)
	}

	// List configs.
	rec2, req2 := doJSON(t, http.MethodGet, "/api/v1/llm-configs", nil)
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	var listResp struct {
		Configs []storage.LLMConfig `json:"configs"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(listResp.Configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(listResp.Configs))
	}
	if listResp.Configs[0].APIKey == "sk-ant-full-key-12345" {
		t.Fatal("listed api_key should be masked")
	}
}

func TestLLMConfigHandler_CreateValidation(t *testing.T) {
	handler := setupLLMConfigHandler(t)

	tests := []struct {
		name string
		body map[string]interface{}
	}{
		{"missing model_name", map[string]interface{}{"provider_url": "https://x.com", "api_key": "sk-12345678x"}},
		{"missing provider_url", map[string]interface{}{"model_name": "gpt", "api_key": "sk-12345678x"}},
		{"missing api_key", map[string]interface{}{"model_name": "gpt", "provider_url": "https://x.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, req := doJSON(t, http.MethodPost, "/api/v1/llm-configs", tt.body)
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})
	}
}

func TestLLMConfigHandler_UpdateAndDelete(t *testing.T) {
	handler := setupLLMConfigHandler(t)

	// Create a config first.
	createBody := map[string]interface{}{
		"model_name":   "original-name",
		"provider_url": "https://original.example.com",
		"api_key":      "sk-original-key-123",
		"temperature":  0.5,
		"max_tokens":   2048,
	}
	rec, req := doJSON(t, http.MethodPost, "/api/v1/llm-configs", createBody)
	handler.ServeHTTP(rec, req)

	var created storage.LLMConfig
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Update the config.
	updateBody := map[string]interface{}{
		"model_name":   "updated-name",
		"provider_url": "https://updated.example.com",
		"api_key":      "***", // masked sentinel — store retains existing
		"temperature":  1.0,
		"max_tokens":   8192,
		"is_default":   true,
	}
	rec2, req2 := doJSON(t, http.MethodPut, "/api/v1/llm-configs/"+created.ID, updateBody)
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// Verify update via list.
	rec3, req3 := doJSON(t, http.MethodGet, "/api/v1/llm-configs", nil)
	handler.ServeHTTP(rec3, req3)
	var listResp struct {
		Configs []storage.LLMConfig `json:"configs"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &listResp)
	if len(listResp.Configs) != 1 {
		t.Fatalf("expected 1 config after update, got %d", len(listResp.Configs))
	}
	cfg := listResp.Configs[0]
	if cfg.ModelName != "updated-name" {
		t.Fatalf("expected model_name updated, got %s", cfg.ModelName)
	}
	if cfg.IsDefault != true {
		t.Fatal("expected is_default true")
	}
	if cfg.Temperature != 1.0 {
		t.Fatalf("expected temperature 1.0, got %f", cfg.Temperature)
	}

	// Delete the config.
	rec4, req4 := doJSON(t, http.MethodDelete, "/api/v1/llm-configs/"+created.ID, nil)
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d", rec4.Code)
	}

	// Verify empty after delete.
	rec5, req5 := doJSON(t, http.MethodGet, "/api/v1/llm-configs", nil)
	handler.ServeHTTP(rec5, req5)
	json.Unmarshal(rec5.Body.Bytes(), &listResp)
	if len(listResp.Configs) != 0 {
		t.Fatalf("expected 0 configs after delete, got %d", len(listResp.Configs))
	}
}

func TestLLMConfigHandler_DefaultUniqueness(t *testing.T) {
	handler := setupLLMConfigHandler(t)

	// Create two configs.
	var ids []string
	for _, name := range []string{"model-a", "model-b"} {
		body := map[string]interface{}{
			"model_name":   name,
			"provider_url": "https://example.com",
			"api_key":      "sk-key-" + name,
		}
		rec, req := doJSON(t, http.MethodPost, "/api/v1/llm-configs", body)
		handler.ServeHTTP(rec, req)
		var created storage.LLMConfig
		json.Unmarshal(rec.Body.Bytes(), &created)
		ids = append(ids, created.ID)

		// Set the second one as default.
		if name == "model-b" {
			updateBody := map[string]interface{}{
				"model_name":   name,
				"provider_url": "https://example.com",
				"api_key":      "***",
				"is_default":   true,
			}
			rec2, req2 := doJSON(t, http.MethodPut, "/api/v1/llm-configs/"+created.ID, updateBody)
			handler.ServeHTTP(rec2, req2)
		}
	}
	_ = ids

	// Verify only model-b is default.
	rec, req := doJSON(t, http.MethodGet, "/api/v1/llm-configs", nil)
	handler.ServeHTTP(rec, req)
	var listResp struct {
		Configs []storage.LLMConfig `json:"configs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listResp)
	defaultCount := 0
	for _, c := range listResp.Configs {
		if c.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly 1 default, got %d", defaultCount)
	}
}

func TestLLMConfigHandler_MethodNotAllowed(t *testing.T) {
	handler := setupLLMConfigHandler(t)

	// GET with an ID path should return 405 (only PUT/DELETE allowed with ID).
	rec, req := doJSON(t, http.MethodGet, "/api/v1/llm-configs/nonexistent", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
