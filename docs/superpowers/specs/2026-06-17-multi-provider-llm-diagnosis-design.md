# Multi-Provider LLM Diagnosis Support — Design Spec

Date: 2026-06-17

## Problem

`diagnosis_llm.go` only supports OpenAI-compatible API format. When users configure an Anthropic (Claude) model, the call fails because:

- URL path is wrong: code appends `/v1/chat/completions` but Anthropic uses `/v1/messages`
- Auth header is wrong: code sends `Authorization: Bearer {key}` but Anthropic needs `x-api-key: {key}` + `anthropic-version` header
- Request body is wrong: Anthropic Messages API uses `system` as a top-level field, not inside `messages[]`
- Response format is wrong: Anthropic returns `content[0].text` array, not `choices[0].message.content`

DeepSeek, Ollama, Groq, Together AI, Qwen/DashScope etc. are OpenAI-compatible and work fine. ZhipuAI/GLM is also OpenAI-compatible but has a polymorphic `content` field that must be handled. Anthropic and its compatible providers are the main gap.

## Scope

- Support `openai` (OpenAI-compatible) and `anthropic` (Anthropic Messages API) provider types
- Add `provider_type` field to `LLMConfig`
- Backend adapter pattern with per-provider request/response handling
- Frontend dropdown for provider type selection
- Database schema migration for new column
- Not in scope: Azure OpenAI (different auth + URL format, separate spec if needed), auto-detection of provider type from URL

## Data Model

`LLMConfig` gains a `provider_type` field:

```go
type LLMConfig struct {
    ID           string  `json:"id"`
    ModelName    string  `json:"model_name"`
    ProviderType string  `json:"provider_type"` // "openai" or "anthropic", default "openai"
    ProviderURL  string  `json:"provider_url"`
    APIKey       string  `json:"api_key"`
    IsDefault    bool    `json:"is_default"`
    Temperature  float64 `json:"temperature"`
    MaxTokens    int     `json:"max_tokens"`
}
```

Rules:
- Only `"openai"` or `"anthropic"` values, default `"openai"` on creation
- OpenAI-compatible providers (DeepSeek, Ollama, Groq, Together AI, Qwen/DashScope, ZhipuAI/GLM) use `openai`
- Anthropic-compatible providers use `anthropic`

## Backend Adapter Architecture

### Entry Point

`callLLMForDiagnosis` becomes a dispatcher:

```go
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
```

Empty `ProviderType` defaults to `"openai"` for backward compatibility with existing configs.

### Common Types

```go
type diagnosisMessage struct {
    Role    string // "system" or "user"
    Content string
}
```

### OpenAI Adapter (existing logic, refactored)

- URL: use `config.ProviderURL` directly — remove the `/v1/chat/completions` auto-append logic. User fills the complete endpoint URL (e.g. `https://api.openai.com/v1/chat/completions`, `https://api.deepseek.com/chat/completions`, `https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions`, `https://open.bigmodel.cn/api/paas/v4/chat/completions`)
- Auth: `Authorization: Bearer {APIKey}`
- Request: existing `llmChatRequest` unchanged (`model`, `temperature`, `max_tokens`, `messages[]`)
- Response: **must handle polymorphic `content` field** — ZhipuAI/GLM returns `choices[0].message.content` as either a string or an array `[{"type":"text","text":"..."}]`. All other OpenAI-compatible providers return it as a string.

Polymorphic content handling:

```go
// llmChatResponse Message.Content parsed as json.RawMessage
type llmChatMessage struct {
    Role    string          `json:"role"`
    Content json.RawMessage `json:"content"`
}

// extractContent handles both string and array formats
func extractContent(raw json.RawMessage) string {
    // Try string first (standard OpenAI, DeepSeek, Qwen)
    var s string
    if json.Unmarshal(raw, &s) == nil {
        return s
    }
    // Try array (ZhipuAI/GLM polymorphic content)
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
    return string(raw) // fallback
}
```

### Anthropic Adapter (new)

- URL: use `config.ProviderURL` directly (user fills e.g. `https://api.anthropic.com/v1/messages`)
- Auth headers: `x-api-key: {APIKey}` + `anthropic-version: 2023-06-01` + `Content-Type: application/json`
- Request body:

```go
type anthropicRequest struct {
    Model       string             `json:"model"`
    MaxTokens   int                `json:"max_tokens"`          // required by Anthropic
    System      string             `json:"system"`              // system prompt as top-level field
    Messages    []anthropicMessage `json:"messages"`            // user/assistant only
    Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
    Role    string `json:"role"`    // "user" or "assistant"
    Content string `json:"content"`
}
```

- Response parsing:

```go
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
```

Extract diagnosis JSON from `content[0].text`, then same post-processing as OpenAI: strip markdown fences → parse `llmDiagnosisResponse` → validate scores/findings.

### Shared Post-Processing

Both adapters share the same diagnosis JSON parsing after extracting raw text:

```go
func parseDiagnosisContent(rawContent string) (*llmDiagnosisResponse, error) {
    content := stripMarkdownFences(rawContent)
    var diag llmDiagnosisResponse
    if err := json.Unmarshal([]byte(content), &diag); err != nil {
        return nil, fmt.Errorf("parse diagnosis json: %w", err)
    }
    validateDiagnosisScores(&diag)
    return &diag, nil
}
```

## Frontend Changes

### LlmConfig.vue

Add provider type dropdown after Model Name field:

- Dropdown options: "OpenAI Compatible" / "Anthropic"
- Default selection: "openai" on new config creation
- Placeholder for Provider URL changes based on selection:
  - `openai` → `https://api.openai.com/v1/chat/completions`
  - `anthropic` → `https://api.anthropic.com/v1/messages`
- Table adds "Provider Type" column showing friendly label

### client.ts

`LlmConfig` interface adds `provider_type: string`. Create/update request bodies include the field.

### i18n

```ts
// en.ts
llmConfig: {
  providerType: 'Provider Type',
  providerTypeOpenai: 'OpenAI Compatible',
  providerTypeAnthropic: 'Anthropic',
}

// zh.ts
llmConfig: {
  providerType: '提供商类型',
  providerTypeOpenai: 'OpenAI 兼容',
  providerTypeAnthropic: 'Anthropic',
}
```

## Database Migration

- SQLite: `ALTER TABLE llm_configs ADD COLUMN provider_type TEXT DEFAULT 'openai'`
- ClickHouse: `ALTER TABLE llm_configs ADD COLUMN provider_type String DEFAULT 'openai'`
- memStore: field added to struct, existing configs get `"openai"` default
- Schema files (`sqlite_schema.sql`, `schema.sql`) updated for new table creation
- Migration runs on app startup in SQLite store init (existing migration pattern)

## Backward Compatibility

- Empty/missing `provider_type` treated as `"openai"` — existing configs work without changes
- URL auto-append logic removed from OpenAI adapter: existing users who relied on `/v1/chat/completions` being appended need to update their `ProviderURL` to include the full endpoint path. This is a one-time migration. Handler validation will remind users if URL doesn't end with a valid path pattern.
- The handler `create()` defaults `provider_type` to `"openai"` when not provided

## Error Handling

- `unsupported provider_type`: clear error message returned to frontend, displayed in DiagnosisTab
- Anthropic API errors: parse `anthropicResponse.Error.Message` for user-friendly display
- HTTP status errors: same pattern as current — return status code + raw response body

## Testing

- Unit tests for `callOpenAI` and `callAnthropic` with mock HTTP server
- Test Anthropic request format (system as top-level field, required max_tokens)
- Test Anthropic response parsing (content array, error format)
- Test polymorphic content parsing: string content (DeepSeek/Qwen) and array content (ZhipuAI/GLM)
- Test dispatcher with empty provider_type defaults to openai
- Test unsupported provider_type returns error
- Handler tests for create/update with provider_type field
