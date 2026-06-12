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

// llmDiagnosisResponse is the expected JSON structure from the LLM.
type llmDiagnosisResponse struct {
	OverallScore int                    `json:"overall_score"`
	Scores       storage.DiagnosisScores `json:"scores"`
	Summary      string                 `json:"summary"`
	Findings     []storage.DiagnosisFinding `json:"findings"`
}

// llmChatRequest is the OpenAI-compatible chat completion request body.
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

// llmChatResponse is the OpenAI-compatible chat completion response.
type llmChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// callLLMForDiagnosis sends the diagnosis prompt to the configured LLM and returns parsed results.
func callLLMForDiagnosis(ctx context.Context, config *storage.LLMConfig, systemPrompt, userPrompt string) (*llmDiagnosisResponse, string, error) {
	reqBody := llmChatRequest{
		Model:       config.ModelName,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Messages: []llmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	var url string
	// Build URL: provider_url + /chat/completions (OpenAI-compatible).
	// If provider_url already ends with /v1, just append /chat/completions.
	// Otherwise append /v1/chat/completions.
	baseURL := strings.TrimRight(config.ProviderURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		url = baseURL + "/chat/completions"
	} else {
		url = baseURL + "/v1/chat/completions"
	}

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

	content := chatResp.Choices[0].Message.Content
	// Strip markdown code fences if present.
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var diagResp llmDiagnosisResponse
	if err := json.Unmarshal([]byte(content), &diagResp); err != nil {
		return nil, rawResponse, fmt.Errorf("parse diagnosis json: %w (content: %s)", err, content)
	}

	// Validate scores are in range.
	for _, score := range []int{diagResp.Scores.Latency, diagResp.Scores.Cost, diagResp.Scores.Error, diagResp.Scores.Efficiency} {
		if score < 0 || score > 100 {
			return nil, rawResponse, fmt.Errorf("score out of range [0,100]: %d", score)
		}
	}
	if diagResp.OverallScore < 0 || diagResp.OverallScore > 100 {
		return nil, rawResponse, fmt.Errorf("overall_score out of range [0,100]: %d", diagResp.OverallScore)
	}
	for _, f := range diagResp.Findings {
		if f.Severity == "" || f.Dimension == "" || f.Title == "" || f.Description == "" || f.Suggestion == "" {
			return nil, rawResponse, fmt.Errorf("finding missing required field: %+v", f)
		}
	}

	return &diagResp, rawResponse, nil
}