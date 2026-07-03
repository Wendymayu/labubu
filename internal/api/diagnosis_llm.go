package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// diagnosisMessage is the unified prompt input for all adapters.
type diagnosisMessage struct {
	Role    string // "system" or "user"
	Content string
}

// llmDiagnosisResponse is the expected JSON structure from the LLM.
type llmDiagnosisResponse struct {
	OverallScore int                       `json:"overall_score"`
	Scores       storage.DiagnosisScores   `json:"scores"`
	Summary      string                    `json:"summary"`
	Findings     []storage.DiagnosisFinding `json:"findings"`
}

// --- Shared post-processing functions ---

// stripMarkdownFences removes markdown code fences from LLM output.
func stripMarkdownFences(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	return content
}

// parseDiagnosisContent parses diagnosis JSON from raw LLM text output.
func parseDiagnosisContent(rawContent string) (*llmDiagnosisResponse, error) {
	content := stripMarkdownFences(rawContent)
	var diag llmDiagnosisResponse
	if err := json.Unmarshal([]byte(content), &diag); err != nil {
		return nil, fmt.Errorf("parse diagnosis json: %w", err)
	}
	// Validate scores are in range.
	for _, score := range []int{diag.Scores.Latency, diag.Scores.Cost, diag.Scores.Error, diag.Scores.Efficiency} {
		if score < 0 || score > 100 {
			return nil, fmt.Errorf("score out of range [0,100]: %d", score)
		}
	}
	if diag.OverallScore < 0 || diag.OverallScore > 100 {
		return nil, fmt.Errorf("overall_score out of range [0,100]: %d", diag.OverallScore)
	}
	for _, f := range diag.Findings {
		if f.Severity == "" || f.Dimension == "" || f.Title == "" || f.Description == "" || f.Suggestion == "" {
			return nil, fmt.Errorf("finding missing required field: %+v", f)
		}
	}
	return &diag, nil
}

// --- Dispatcher ---

func callLLMForDiagnosis(ctx context.Context, config *storage.LLMConfig, systemPrompt, userPrompt string) (*llmDiagnosisResponse, string, error) {
	msgs := []diagnosisMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	switch config.ProviderType {
	case "openai", "":
		return callOpenAI(ctx, config, msgs)
	case "anthropic":
		return callAnthropic(ctx, config, msgs)
	default:
		return nil, "", fmt.Errorf("unsupported provider_type: %s", config.ProviderType)
	}
}

// --- OpenAI Adapter ---

// buildOpenAIURL constructs the full chat completions URL from a base ProviderURL.
// Handles common patterns:
//   - Already ends with /chat/completions → use directly
//   - Ends with /v1 → append /chat/completions (OpenAI, DeepSeek, DashScope convention)
//   - Other base URLs → append /chat/completions (ZhipuAI /v4, custom paths)
func buildOpenAIURL(providerURL string) string {
	base := strings.TrimRight(providerURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/chat/completions"
}

type llmChatRequest struct {
	Model       string       `json:"model"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
	Messages    []llmMessage `json:"messages"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// extractContent handles polymorphic content field from OpenAI-compatible providers.
// Standard format returns content as a string. ZhipuAI/GLM returns it as an array
// of content blocks [{"type":"text","text":"..."}].
func extractContent(raw json.RawMessage) string {
	// Try string first (standard OpenAI, DeepSeek, Qwen).
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Try array (ZhipuAI/GLM polymorphic content).
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" {
				return b.Text
			}
		}
	}
	return string(raw) // fallback: return raw JSON string
}

func callOpenAI(ctx context.Context, config *storage.LLMConfig, msgs []diagnosisMessage) (*llmDiagnosisResponse, string, error) {
	// Build messages for OpenAI format.
	chatMsgs := make([]llmMessage, len(msgs))
	for i, m := range msgs {
		chatMsgs[i] = llmMessage{Role: m.Role, Content: m.Content}
	}

	reqBody := llmChatRequest{
		Model:       config.ModelName,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Messages:    chatMsgs,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	// Build OpenAI-compatible URL: if ProviderURL is a base URL (e.g. ending with /v1
	// or just a domain), append /chat/completions. If it already ends with
	// /chat/completions, use it directly.
	url := buildOpenAIURL(config.ProviderURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("llm call failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	rawResponse := string(respBytes)

	if resp.StatusCode != http.StatusOK {
		return nil, rawResponse, fmt.Errorf("llm api error (status %d): %s", resp.StatusCode, rawResponse)
	}

	var chatResp llmChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, rawResponse, fmt.Errorf("parse llm response: %w", err)
	}

	if chatResp.Error != nil {
		return nil, rawResponse, fmt.Errorf("llm api error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, rawResponse, fmt.Errorf("llm returned no choices")
	}

	content := extractContent(chatResp.Choices[0].Message.Content)

	diagResp, err := parseDiagnosisContent(content)
	if err != nil {
		return nil, rawResponse, err
	}

	return diagResp, rawResponse, nil
}

// --- Anthropic Adapter ---

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"` // "text"
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func callAnthropic(ctx context.Context, config *storage.LLMConfig, msgs []diagnosisMessage) (*llmDiagnosisResponse, string, error) {
	// Extract system message — Anthropic requires it as a top-level field.
	var systemPrompt string
	var chatMsgs []anthropicMessage
	for _, m := range msgs {
		if m.Role == "system" {
			systemPrompt = m.Content
		} else {
			chatMsgs = append(chatMsgs, anthropicMessage{Role: m.Role, Content: m.Content})
		}
	}

	reqBody := anthropicRequest{
		Model:       config.ModelName,
		MaxTokens:   config.MaxTokens,
		System:      systemPrompt,
		Messages:    chatMsgs,
		Temperature: config.Temperature,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshal anthropic request: %w", err)
	}

	url := strings.TrimRight(config.ProviderURL, "/")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, "", fmt.Errorf("create anthropic request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Set both auth headers: native Anthropic uses x-api-key,
	// Anthropic-compatible providers (DashScope, etc.) use Authorization: Bearer.
	// The endpoint will accept whichever it expects and ignore the other.
	httpReq.Header.Set("x-api-key", config.APIKey)
	httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("anthropic call failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read anthropic response: %w", err)
	}

	rawResponse := string(respBytes)

	if resp.StatusCode != http.StatusOK {
		// Try to parse Anthropic error format for a cleaner message.
		var errResp anthropicResponse
		if json.Unmarshal(respBytes, &errResp) == nil && errResp.Error != nil {
			return nil, rawResponse, fmt.Errorf("anthropic api error: %s", errResp.Error.Message)
		}
		return nil, rawResponse, fmt.Errorf("anthropic api error (status %d): %s", resp.StatusCode, rawResponse)
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(respBytes, &antResp); err != nil {
		return nil, rawResponse, fmt.Errorf("parse anthropic response: %w", err)
	}

	if antResp.Error != nil {
		return nil, rawResponse, fmt.Errorf("anthropic api error: %s", antResp.Error.Message)
	}

	if len(antResp.Content) == 0 {
		return nil, rawResponse, fmt.Errorf("anthropic returned no content")
	}

	// Find first text content block.
	var content string
	for _, block := range antResp.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}
	if content == "" {
		return nil, rawResponse, fmt.Errorf("anthropic returned no text content")
	}

	diagResp, err := parseDiagnosisContent(content)
	if err != nil {
		return nil, rawResponse, err
	}

	return diagResp, rawResponse, nil
}
