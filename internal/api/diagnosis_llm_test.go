package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func TestCallLLMForDiagnosis_UnsupportedProvider(t *testing.T) {
	config := &storage.LLMConfig{
		ProviderType: "azure",
		ProviderURL:  "https://example.com",
		APIKey:       "test-key",
		ModelName:    "test-model",
		Temperature:  0.7,
		MaxTokens:    100,
	}
	_, _, err := callLLMForDiagnosis(context.Background(), config, "sys", "user")
	if err == nil {
		t.Fatal("expected error for unsupported provider_type")
	}
	if err.Error() == "" || !stringsContains(err.Error(), "unsupported provider_type") {
		t.Fatalf("expected unsupported provider_type error, got: %v", err)
	}
}

func TestExtractContent_StringFormat(t *testing.T) {
	raw := json.RawMessage(`"hello world"`)
	result := extractContent(raw)
	if result != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", result)
	}
}

func TestExtractContent_ArrayFormat(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"diagnosis result"},{"type":"web_search","text":"search data"}]`)
	result := extractContent(raw)
	if result != "diagnosis result" {
		t.Fatalf("expected 'diagnosis result', got '%s'", result)
	}
}

func TestExtractContent_ArrayNoTextBlock(t *testing.T) {
	raw := json.RawMessage(`[{"type":"image","url":"http://x"}]`)
	result := extractContent(raw)
	if result == "" {
		t.Fatal("expected fallback to raw string, got empty")
	}
}

func TestStripMarkdownFences(t *testing.T) {
	input := "```json\n{\"overall_score\":85}\n```"
	result := stripMarkdownFences(input)
	if result != "{\"overall_score\":85}" {
		t.Fatalf("expected stripped JSON, got '%s'", result)
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBuildOpenAIURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com", "https://api.openai.com/chat/completions"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
		{"https://api.deepseek.com", "https://api.deepseek.com/chat/completions"},
		{"https://api.deepseek.com/v1", "https://api.deepseek.com/v1/chat/completions"},
		{"https://open.bigmodel.cn/api/paas/v4/chat/completions", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
	}
	for _, tt := range tests {
		result := buildOpenAIURL(tt.input)
		if result != tt.expected {
			t.Errorf("buildOpenAIURL(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestCallAnthropic_Success(t *testing.T) {
	validDiagnosis := `{"overall_score":85,"scores":{"latency":80,"cost":90,"error":95,"efficiency":85},"summary":"Good trace","findings":[{"severity":"info","dimension":"efficiency","title":"Efficient","description":"Well structured","suggestion":"Keep it up"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Anthropic headers — both auth methods are sent for compatibility
		if r.Header.Get("x-api-key") != "ant-test-key" {
			t.Error("expected x-api-key header for Anthropic")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("expected anthropic-version header")
		}
			if r.Header.Get("Authorization") != "Bearer ant-test-key" {
				t.Error("expected Authorization: Bearer header for Anthropic-compatible providers")
			}

		// Verify request body has system as top-level field
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["system"] == nil {
			t.Error("expected 'system' as top-level field in Anthropic request")
		}
		if reqBody["model"] != "claude-test" {
			t.Error("expected model field in Anthropic request")
		}

		resp := anthropicResponse{}
		resp.Content = []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "text", Text: validDiagnosis},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := &storage.LLMConfig{
		ProviderType: "anthropic",
		ProviderURL:  server.URL,
		APIKey:       "ant-test-key",
		ModelName:    "claude-test",
		Temperature:  0.7,
		MaxTokens:    100,
	}
	diag, _, err := callLLMForDiagnosis(context.Background(), config, "sys prompt", "user prompt")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if diag.OverallScore != 85 {
		t.Fatalf("expected overall_score 85, got %d", diag.OverallScore)
	}
}

func TestCallAnthropic_ApiError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		resp := anthropicResponse{}
		resp.Error = &struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		}{Message: "invalid request", Type: "invalid_request_error"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := &storage.LLMConfig{
		ProviderType: "anthropic",
		ProviderURL:  server.URL,
		APIKey:       "ant-test-key",
		ModelName:    "claude-test",
		Temperature:  0.7,
		MaxTokens:    100,
	}
	_, _, err := callLLMForDiagnosis(context.Background(), config, "sys", "user")
	if err == nil {
		t.Fatal("expected error for Anthropic API error response")
	}
}
