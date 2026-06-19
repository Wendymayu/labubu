# Multi-Provider LLM Diagnosis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-provider support (OpenAI-compatible + Anthropic) for LLM-powered trace diagnosis, so users can switch models without errors.

**Architecture:** Add `provider_type` field to `LLMConfig`, refactor `callLLMForDiagnosis` into a dispatcher that routes to OpenAI or Anthropic adapter functions. OpenAI adapter handles polymorphic content (ZhipuAI/GLM). Anthropic adapter uses Messages API format. Frontend adds provider_type dropdown. Database schema adds new column.

**Tech Stack:** Go 1.19 backend, Vue 3/TypeScript frontend, SQLite/ClickHouse storage, json.RawMessage for polymorphic parsing

---

### Task 1: Add `provider_type` to `LLMConfig` struct and handler validation

**Files:**
- Modify: `internal/storage/storage.go:263-272`
- Modify: `internal/api/llm_config_handler.go:67-92`

- [ ] **Step 1: Add `ProviderType` field to `LLMConfig` struct**

In `internal/storage/storage.go`, add the field after `ModelName`:

```go
type LLMConfig struct {
    ID           string  `json:"id"`
    ModelName    string  `json:"model_name"`
    ProviderType string  `json:"provider_type"` // "openai" or "anthropic", default "openai"
    ProviderURL  string  `json:"provider_url"`
    APIKey       string  `json:"api_key"`     // plaintext at rest, masked on GET
    IsDefault    bool    `json:"is_default"`
    Temperature  float64 `json:"temperature"` // default 0.7
    MaxTokens    int     `json:"max_tokens"`  // default 4096
}
```

- [ ] **Step 2: Add default and validation in handler create()**

In `internal/api/llm_config_handler.go`, update the `create()` method. After the existing defaults block (lines 78-83), add provider_type default:

```go
// Set defaults.
if c.Temperature == 0 {
    c.Temperature = 0.7
}
if c.MaxTokens == 0 {
    c.MaxTokens = 4096
}
if c.ProviderType == "" {
    c.ProviderType = "openai"
}
```

Also add validation after the existing required-fields check (line 73):

```go
if c.ProviderType != "" && c.ProviderType != "openai" && c.ProviderType != "anthropic" {
    writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider_type must be 'openai' or 'anthropic'"})
    return
}
```

- [ ] **Step 3: Run Go tests to verify nothing broke**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/storage/... ./internal/api/... -run TestLLMConfig`
Expected: All existing tests still pass (provider_type defaults to "openai" for backward compat)

- [ ] **Step 4: Commit**

```bash
git add internal/storage/storage.go internal/api/llm_config_handler.go
git commit -m "feat: add provider_type field to LLMConfig struct and handler validation"
```

---

### Task 2: Update database schema and store implementations for `provider_type`

**Files:**
- Modify: `internal/storage/sqlite_schema.sql:84-93`
- Modify: `internal/storage/schema.sql:80-91`
- Modify: `internal/storage/sqlite_store.go:797-892`
- Modify: `internal/storage/memstore.go:779-835`
- Modify: `internal/storage/chdb_query.go:550-587`
- Modify: `internal/storage/chdb.go:462-511`

- [ ] **Step 1: Update SQLite schema to include `provider_type` column**

In `internal/storage/sqlite_schema.sql`, update the `llm_configs` CREATE TABLE:

```sql
CREATE TABLE IF NOT EXISTS llm_configs (
    id            TEXT NOT NULL PRIMARY KEY,
    model_name    TEXT NOT NULL,
    provider_type TEXT NOT NULL DEFAULT 'openai',
    provider_url  TEXT NOT NULL,
    api_key       TEXT NOT NULL DEFAULT '',
    is_default    INTEGER NOT NULL DEFAULT 0,
    temperature   REAL NOT NULL DEFAULT 0.7,
    max_tokens    INTEGER NOT NULL DEFAULT 4096,
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
```

- [ ] **Step 2: Update ClickHouse schema to include `provider_type` column**

In `internal/storage/schema.sql`, update the `llm_configs` CREATE TABLE and add ALTER TABLE for existing databases:

```sql
CREATE TABLE IF NOT EXISTS llm_configs (
    id            String,
    model_name    String,
    provider_type String DEFAULT 'openai',
    provider_url  String,
    api_key       String,
    is_default    UInt8,
    temperature   Float64,
    max_tokens    Int32,
    updated_at    DateTime DEFAULT now()
)
ENGINE = MergeTree
ORDER BY id;

ALTER TABLE llm_configs ADD COLUMN IF NOT EXISTS provider_type String DEFAULT 'openai';
```

- [ ] **Step 3: Update SQLite store — GetLLMConfigs query to include provider_type**

In `internal/storage/sqlite_store.go`, update `GetLLMConfigs` (line 801-802):

```go
rows, err := s.db.QueryContext(ctx,
    `SELECT id, model_name, provider_type, provider_url, api_key, is_default, temperature, max_tokens
     FROM llm_configs ORDER BY model_name`,
)
```

And the Scan (line 814):

```go
var c LLMConfig
var isDefault int
if err := rows.Scan(&c.ID, &c.ModelName, &c.ProviderType, &c.ProviderURL, &c.APIKey, &isDefault, &c.Temperature, &c.MaxTokens); err != nil {
    return nil, fmt.Errorf("scan llm config: %w", err)
}
c.IsDefault = isDefault != 0
```

- [ ] **Step 4: Update SQLite store — CreateLLMConfig INSERT to include provider_type**

In `internal/storage/sqlite_store.go`, update `CreateLLMConfig` INSERT (line 842-844):

```go
_, err := s.db.ExecContext(ctx,
    `INSERT INTO llm_configs (id, model_name, provider_type, provider_url, api_key, is_default, temperature, max_tokens)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    c.ID, c.ModelName, c.ProviderType, c.ProviderURL, c.APIKey, isDefault, c.Temperature, c.MaxTokens,
)
```

- [ ] **Step 5: Update SQLite store — UpdateLLMConfig UPDATE to include provider_type**

In `internal/storage/sqlite_store.go`, update `UpdateLLMConfig` UPDATE (line 878-881):

```go
_, err := s.db.ExecContext(ctx,
    `UPDATE llm_configs SET model_name=?, provider_type=?, provider_url=?, api_key=?, is_default=?, temperature=?, max_tokens=?
     WHERE id=?`,
    c.ModelName, c.ProviderType, c.ProviderURL, apiKey, isDefault, c.Temperature, c.MaxTokens, c.ID,
)
```

- [ ] **Step 6: Add SQLite migration for existing databases**

In `internal/storage/sqlite_store.go`, after the schema exec (line 52), add column migration:

```go
// Apply schema (embedded at compile time)
if _, err := db.Exec(sqliteSchemaSQL); err != nil {
    db.Close()
    return nil, fmt.Errorf("create schema: %w", err)
}

// Migrate: add provider_type column to existing llm_configs tables.
db.Exec(`ALTER TABLE llm_configs ADD COLUMN provider_type TEXT NOT NULL DEFAULT 'openai'`)
```

Note: `db.Exec` returns nil error if column already exists in SQLite (it's silently ignored), which is fine for migration idempotency.

- [ ] **Step 7: Update memStore — no structural change needed since struct already has field**

The memStore uses `LLMConfig` struct directly, so `ProviderType` is automatically included. No code change needed.

- [ ] **Step 8: Update chDB query builders — SELECT, INSERT, UPDATE**

In `internal/storage/chdb_query.go`, update `buildLLMConfigSelectSQL` (around line 550):

```go
func buildLLMConfigSelectSQL() string {
    return `SELECT id, model_name, provider_type, provider_url, api_key, is_default, temperature, max_tokens, updated_at FROM llm_configs ORDER BY model_name`
}
```

Update `buildLLMConfigInsertSQL` (around line 557) — add `provider_type` to INSERT:

```go
func buildLLMConfigInsertSQL(c LLMConfig) string {
    isDefault := 0
    if c.IsDefault {
        isDefault = 1
    }
    return fmt.Sprintf(
        `INSERT INTO llm_configs (id, model_name, provider_type, provider_url, api_key, is_default, temperature, max_tokens, updated_at) VALUES ('%s', '%s', '%s', '%s', '%s', %d, %f, %d, now())`,
        escapeSQL(c.ID), escapeSQL(c.ModelName), escapeSQL(c.ProviderType), escapeSQL(c.ProviderURL), escapeSQL(c.APIKey), isDefault, c.Temperature, c.MaxTokens,
    )
}
```

Update `buildLLMConfigUpdateSQL` (around line 567) — add `provider_type` to UPDATE:

```go
func buildLLMConfigUpdateSQL(c LLMConfig) string {
    isDefault := 0
    if c.IsDefault {
        isDefault = 1
    }
    return fmt.Sprintf(
        `ALTER TABLE llm_configs UPDATE model_name = '%s', provider_type = '%s', provider_url = '%s', api_key = '%s', is_default = %d, temperature = %f, max_tokens = %d, updated_at = now() WHERE id = '%s'`,
        escapeSQL(c.ModelName), escapeSQL(c.ProviderType), escapeSQL(c.ProviderURL), escapeSQL(c.APIKey), isDefault, c.Temperature, c.MaxTokens, escapeSQL(c.ID),
    )
}
```

- [ ] **Step 9: Update chDB store — scan fields for GetLLMConfigs**

In `internal/storage/chdb.go`, find the `GetLLMConfigs` method and update the scan to include `provider_type`. The exact scan pattern depends on the chDB implementation, but add `&c.ProviderType` after `&c.ModelName` in the Scan call.

- [ ] **Step 10: Run tests**

Run: `cd D:/code/opensource/github/labubu && make test-nocgo`
Expected: All existing tests pass with default provider_type='openai'

- [ ] **Step 11: Commit**

```bash
git add internal/storage/sqlite_schema.sql internal/storage/schema.sql internal/storage/sqlite_store.go internal/storage/memstore.go internal/storage/chdb_query.go internal/storage/chdb.go
git commit -m "feat: add provider_type column to database schemas and store implementations"
```

---

### Task 3: Refactor `diagnosis_llm.go` — add dispatcher, OpenAI adapter, shared parsing

**Files:**
- Modify: `internal/api/diagnosis_llm.go` (full rewrite of types and callLLMForDiagnosis)

- [ ] **Step 1: Write failing test for the dispatcher**

Create a new test file `internal/api/diagnosis_llm_test.go` (note: this file needs CGO build tag if it imports chDB, but we'll use pure Go mock HTTP tests). Actually, since `diagnosis_llm.go` is in package `api` which has build tags, we should put the test in a separate file with appropriate tags. Let's check — the existing handler test uses `//go:build cgo && local_engine`. For the LLM adapter tests, we can create a pure-Go test that doesn't depend on chDB.

Create `internal/api/diagnosis_llm_test.go`:

```go
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
    if !contains(err.Error(), "unsupported provider_type") {
        t.Fatalf("expected unsupported provider_type error, got: %v", err)
    }
}

func TestCallLLMForDiagnosis_DefaultProviderIsOpenAI(t *testing.T) {
    // Empty ProviderType should route to OpenAI adapter
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("Authorization") != "Bearer test-key" {
            t.Error("expected Bearer auth header for OpenAI provider")
        }
        resp := llmChatResponse{}
        resp.Choices = []struct {
            Message struct { Content json.RawMessage `json:"content"` }
        }{
            {Message: struct { Content json.RawMessage `json:"content"` }{Content: json.RawMessage(`"test"`)}},
        }
        json.NewEncoder(w).Encode(resp)
    }))
    defer server.Close()

    config := &storage.LLMConfig{
        ProviderType: "", // empty, should default to openai
        ProviderURL:  server.URL,
        APIKey:       "test-key",
        ModelName:    "test-model",
        Temperature:  0.7,
        MaxTokens:    100,
    }
    // This test just verifies the dispatcher routes empty provider_type to OpenAI.
    // The actual response parsing failure is expected since "test" is not valid diagnosis JSON.
    _, rawResp, err := callLLMForDiagnosis(context.Background(), config, "sys prompt", "user prompt")
    // We expect it to reach the server (no routing error), but fail on diagnosis parsing.
    if err != nil && contains(err.Error(), "unsupported provider_type") {
        t.Fatal("empty provider_type should not give unsupported error")
    }
    // Raw response should be non-empty if the server was reached.
    _ = rawResp // just verifying it didn't fail at routing
}

func contains(s, sub string) bool {
    return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
    for i := 0; i <= len(s)-len(sub); i++ {
        if s[i:i+len(sub)] == sub {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/api/ -run TestCallLLMForDiagnosis_Unsupported`
Expected: FAIL — `callLLMForDiagnosis` doesn't check provider_type yet

- [ ] **Step 3: Refactor `diagnosis_llm.go` — add types, dispatcher, shared parsing, and OpenAI adapter**

Rewrite `internal/api/diagnosis_llm.go` with the following structure:

```go
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

    // Use ProviderURL directly — no auto-appending.
    url := strings.TrimRight(config.ProviderURL, "/")

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
```

- [ ] **Step 4: Run tests to verify dispatcher and OpenAI adapter work**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/api/ -run TestCallLLMForDiagnosis`
Expected: UNSUPPORTED provider test passes, DefaultProvider test reaches mock server

- [ ] **Step 5: Add tests for polymorphic content extraction**

Add to `internal/api/diagnosis_llm_test.go`:

```go
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
```

- [ ] **Step 6: Run extraction tests**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/api/ -run TestExtractContent`
Expected: All three tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/api/diagnosis_llm.go internal/api/diagnosis_llm_test.go
git commit -m "feat: refactor diagnosis_llm with dispatcher, OpenAI adapter, polymorphic content"
```

---

### Task 4: Add Anthropic adapter

**Files:**
- Modify: `internal/api/diagnosis_llm.go` (add Anthropic types and callAnthropic function)
- Modify: `internal/api/diagnosis_llm_test.go` (add Anthropic tests)

- [ ] **Step 1: Write failing test for Anthropic adapter**

Add to `internal/api/diagnosis_llm_test.go`:

```go
func TestCallAnthropic_Success(t *testing.T) {
    validDiagnosis := `{"overall_score":85,"scores":{"latency":80,"cost":90,"error":95,"efficiency":85},"summary":"Good trace","findings":[{"severity":"info","dimension":"efficiency","title":"Efficient","description":"Well structured","suggestion":"Keep it up"}]}`

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify Anthropic-specific headers
        if r.Header.Get("x-api-key") != "ant-test-key" {
            t.Error("expected x-api-key header for Anthropic")
        }
        if r.Header.Get("anthropic-version") != "2023-06-01" {
            t.Error("expected anthropic-version header")
        }
        if r.Header.Get("Authorization") != "" {
            t.Error("should NOT have Bearer auth for Anthropic")
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/api/ -run TestCallAnthropic`
Expected: FAIL — `callAnthropic` and `anthropicResponse` types don't exist yet

- [ ] **Step 3: Add Anthropic types and callAnthropic function to diagnosis_llm.go**

Add after the OpenAI adapter section in `internal/api/diagnosis_llm.go`:

```go
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
    httpReq.Header.Set("x-api-key", config.APIKey)
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
```

- [ ] **Step 4: Run Anthropic tests**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/api/ -run TestCallAnthropic`
Expected: Both success and error tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/api/diagnosis_llm.go internal/api/diagnosis_llm_test.go
git commit -m "feat: add Anthropic Messages API adapter for LLM diagnosis"
```

---

### Task 5: Update frontend — API client, i18n, LlmConfig.vue

**Files:**
- Modify: `web/src/api/client.ts:335-343`
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`
- Modify: `web/src/views/LlmConfig.vue`

- [ ] **Step 1: Update TypeScript LlmConfig interface**

In `web/src/api/client.ts`, add `provider_type` to the interface:

```ts
export interface LlmConfig {
  id: string
  model_name: string
  provider_type: string // "openai" or "anthropic"
  provider_url: string
  api_key: string
  is_default: boolean
  temperature: number
  max_tokens: number
}
```

- [ ] **Step 2: Add i18n keys**

In `web/src/i18n/locales/en.ts`, add a `llmConfig` section after the `agentStats` section:

```ts
llmConfig: {
  providerType: 'Provider Type',
  providerTypeOpenai: 'OpenAI Compatible',
  providerTypeAnthropic: 'Anthropic',
},
```

In `web/src/i18n/locales/zh.ts`, add the same section:

```ts
llmConfig: {
  providerType: '提供商类型',
  providerTypeOpenai: 'OpenAI 兼容',
  providerTypeAnthropic: 'Anthropic',
},
```

- [ ] **Step 3: Update LlmConfig.vue — add provider_type dropdown and table column**

Update the template. Add dropdown after Model Name label (between lines 14-19):

```html
<label>{{ t('llmConfig.providerType') }}:
  <select v-model="form.provider_type">
    <option value="openai">{{ t('llmConfig.providerTypeOpenai') }}</option>
    <option value="anthropic">{{ t('llmConfig.providerTypeAnthropic') }}</option>
  </select>
</label>
```

Update the Provider URL input placeholder to be dynamic:

```html
<label>Provider URL:
  <input v-model="form.provider_url" :placeholder="form.provider_type === 'anthropic' ? 'https://api.anthropic.com/v1/messages' : 'https://api.openai.com/v1/chat/completions'" />
</label>
```

Add Provider Type column to the table header (after Model Name `<th>`):

```html
<th>{{ t('llmConfig.providerType') }}</th>
```

Add Provider Type cell in the table body (after `{{ c.model_name }}` td):

```html
<td>{{ c.provider_type === 'anthropic' ? t('llmConfig.providerTypeAnthropic') : t('llmConfig.providerTypeOpenai') }}</td>
```

Add `<select>` styling in the `<style scoped>` section:

```css
.form-box select {
  padding: 6px 10px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--text-primary);
}
```

- [ ] **Step 4: Update script section — add i18n import and provider_type to form**

Add `useI18n` import:

```ts
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LlmConfig } from '../api/client'
import {
  listLlmConfigs, createLlmConfig, updateLlmConfig, deleteLlmConfig,
} from '../api/client'

const { t } = useI18n()
```

Add `provider_type` to the form reactive object:

```ts
const form = reactive<Omit<LlmConfig, 'id'> & { id?: string }>({
  model_name: '',
  provider_type: 'openai',
  provider_url: '',
  api_key: '',
  is_default: false,
  temperature: 0.7,
  max_tokens: 4096,
})
```

Update `openAdd()` to reset provider_type:

```ts
function openAdd() {
  editing.value = null
  form.model_name = ''
  form.provider_type = 'openai'
  form.provider_url = ''
  form.api_key = ''
  form.is_default = false
  form.temperature = 0.7
  form.max_tokens = 4096
  form.id = undefined
  saveError.value = ''
  showForm.value = true
}
```

Update `editConfig()` to set provider_type:

```ts
function editConfig(c: LlmConfig) {
  editing.value = c
  form.model_name = c.model_name
  form.provider_type = c.provider_type || 'openai'
  form.provider_url = c.provider_url
  form.api_key = '***'
  form.is_default = c.is_default
  form.temperature = c.temperature
  form.max_tokens = c.max_tokens
  form.id = c.id
  saveError.value = ''
  showForm.value = true
}
```

- [ ] **Step 5: Run TypeScript check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: No type errors

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts web/src/views/LlmConfig.vue
git commit -m "feat: add provider_type dropdown and i18n to LlmConfig frontend"
```

---

### Task 6: Update handler test for provider_type

**Files:**
- Modify: `internal/api/llm_config_handler_test.go`

- [ ] **Step 1: Add provider_type to existing test create bodies**

In `TestLLMConfigHandler_CreateAndList` (line 28-32), add provider_type to createBody:

```go
createBody := map[string]interface{}{
    "model_name":    "claude-opus-4-8",
    "provider_type": "anthropic",
    "provider_url":  "https://api.anthropic.com/v1/messages",
    "api_key":       "sk-ant-full-key-12345",
}
```

After checking defaults (after line 59), add provider_type check:

```go
if created.ProviderType != "anthropic" {
    t.Fatalf("expected provider_type anthropic, got %s", created.ProviderType)
}
```

In `TestLLMConfigHandler_UpdateAndDelete` (line 110-116), add provider_type:

```go
createBody := map[string]interface{}{
    "model_name":    "original-name",
    "provider_type": "openai",
    "provider_url":  "https://original.example.com",
    "api_key":       "sk-original-key-123",
    "temperature":   0.5,
    "max_tokens":    2048,
}
```

In the update body (line 124-131), add provider_type:

```go
updateBody := map[string]interface{}{
    "model_name":    "updated-name",
    "provider_type": "anthropic",
    "provider_url":  "https://updated.example.com",
    "api_key":       "***",
    "temperature":   1.0,
    "max_tokens":    8192,
    "is_default":    true,
}
```

- [ ] **Step 2: Add test for invalid provider_type**

Add a new test function:

```go
func TestLLMConfigHandler_InvalidProviderType(t *testing.T) {
    handler := setupLLMConfigHandler(t)

    body := map[string]interface{}{
        "model_name":    "test-model",
        "provider_type": "azure",
        "provider_url":  "https://example.com",
        "api_key":       "sk-key-test",
    }
    rec, req := doJSON(t, http.MethodPost, "/api/v1/llm-configs", body)
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusBadRequest {
        t.Fatalf("expected 400 for invalid provider_type, got %d", rec.Code)
    }
}
```

- [ ] **Step 3: Run handler tests**

Run: `cd D:/code/opensource/github/labubu && go test -v ./internal/api/ -run TestLLMConfigHandler`
Expected: All tests pass, including new invalid provider_type test

- [ ] **Step 4: Commit**

```bash
git add internal/api/llm_config_handler_test.go
git commit -m "test: add provider_type to LLM config handler tests"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run full Go test suite**

Run: `cd D:/code/opensource/github/labubu && make test-nocgo`
Expected: All tests pass

- [ ] **Step 2: Run TypeScript check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: No type errors

- [ ] **Step 3: Build check**

Run: `cd D:/code/opensource/github/labubu && make build-nocgo`
Expected: Build succeeds

- [ ] **Step 4: Manual smoke test — start server and verify UI**

Run: `cd D:/code/opensource/github/labubu && make run`

Then in browser at http://localhost:8080:
1. Navigate to Settings → LLM Configs
2. Click "Add Model" — verify Provider Type dropdown appears with "OpenAI Compatible" and "Anthropic"
3. Select "Anthropic" — verify Provider URL placeholder changes to Anthropic endpoint
4. Select "OpenAI Compatible" — verify placeholder changes to OpenAI endpoint
5. Create a config with provider_type "anthropic" and verify it appears in the table

- [ ] **Step 5: Final commit if any fixups needed**

```bash
git add -A
git commit -m "fix: address smoke test findings"
```
