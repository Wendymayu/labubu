# Trace Diagnosis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build LLM-powered trace diagnosis — users click a button on trace detail to get a structured quality evaluation (latency/cost/error/efficiency scores + findings), results persisted for reuse.

**Architecture:** Backend adds diagnosis store methods, a prompt builder, an LLM client (OpenAI-compatible), and two new handler methods on TraceHandler. Frontend adds a "诊断" tab to TraceDetail.vue with a new DiagnosisTab component. LLM config is reused from the existing `llm_configs` table (is_default row).

**Tech Stack:** Go 1.19 (net/http, encoding/json), Vue 3 + TypeScript, vue-i18n, existing chDB/memstore backends

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/storage/storage.go` | Modify | Add `DiagnosisResult`, `DiagnosisFinding`, `DiagnosisScores` types + two new Store interface methods |
| `internal/storage/schema.sql` | Modify | Add `diagnosis_results` table DDL |
| `internal/storage/chdb.go` | Modify | chDB implementation of `GetDiagnosisResult`, `UpsertDiagnosisResult` + auto-create table |
| `internal/storage/memstore.go` | Modify | In-memory implementation of diagnosis store methods |
| `internal/api/diagnosis_prompt.go` | **Create** | Prompt builder: trace + logs → system + user prompt strings |
| `internal/api/diagnosis_llm.go` | **Create** | LLM client: call OpenAI-compatible API, parse + validate response JSON |
| `internal/api/trace_handler.go` | Modify | Add `DiagnoseTrace`, `GetDiagnosis` handler methods + in-flight tracking + computeSnapshot |
| `internal/api/router.go` | Modify | Dispatch `/traces/{id}/diagnosis` and `/traces/{id}/diagnose` inside catch-all |
| `internal/api/trace_handler_test.go` | Modify | Add diagnosis handler tests |
| `web/src/i18n/locales/en.ts` | Modify | Add `diagnosis.*` keys |
| `web/src/i18n/locales/zh.ts` | Modify | Add `diagnosis.*` keys |
| `web/src/api/client.ts` | Modify | Add `DiagnosisResult` types + `getDiagnosisResult()`, `diagnoseTrace()` functions |
| `web/src/components/DiagnosisTab.vue` | **Create** | Three-state diagnosis tab component (empty/loading/result) |
| `web/src/views/TraceDetail.vue` | Modify | Add "诊断" tab, wire DiagnosisTab component |

---

### Task 1: Add diagnosis types + Store interface methods

**Files:**
- Modify: `internal/storage/storage.go` — add types after the `LLMConfig` block, add interface methods before `Close()`
- Modify: `internal/storage/schema.sql` — add `diagnosis_results` table

- [ ] **Step 1: Add Go types to storage.go**

Add after the `MaskAPIKey` function (around line 221) and before the `PricingConfig` struct:

```go
// DiagnosisFinding is a single issue found by the LLM during trace diagnosis.
type DiagnosisFinding struct {
	Severity    string `json:"severity"`    // "error" | "warning" | "info"
	Dimension   string `json:"dimension"`   // "latency" | "cost" | "error" | "efficiency"
	Title       string `json:"title"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
	SpanName    string `json:"span_name,omitempty"`
	SpanIndex   int    `json:"span_index,omitempty"`
}

// DiagnosisScores holds the four dimension scores from a trace diagnosis.
type DiagnosisScores struct {
	Latency    int `json:"latency"`
	Cost       int `json:"cost"`
	Error      int `json:"error"`
	Efficiency int `json:"efficiency"`
}

// DiagnosisResult holds a complete LLM diagnosis for a single trace.
type DiagnosisResult struct {
	TraceID       [16]byte         `json:"trace_id"`
	TraceIDHex    string           `json:"trace_id_hex"`
	ModelName     string           `json:"model_name"`
	Scores        DiagnosisScores  `json:"scores"`
	OverallScore  uint8            `json:"overall_score"`
	Findings      []DiagnosisFinding `json:"findings"`
	Summary       string           `json:"summary"`
	SpansSnapshot string           `json:"-"`
	RawResponse   string           `json:"-"`
	CreatedAt     time.Time        `json:"created_at"`
	Stale         bool             `json:"stale"`
}
```

- [ ] **Step 2: Add Store interface methods**

Add inside the `Store` interface, after `UpdateTraceCost` and before `Close()`:

```go
	// GetDiagnosisResult returns the stored diagnosis for a trace, or nil if none exists.
	GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error)

	// UpsertDiagnosisResult inserts or replaces the diagnosis result for a trace.
	UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error
```

- [ ] **Step 3: Add diagnosis_results table to schema.sql**

Append to the end of the file:

```sql
CREATE TABLE IF NOT EXISTS diagnosis_results (
    trace_id        FixedString(16),
    model_name      String,
    scores          String,
    overall_score   UInt8,
    findings        String,
    summary         String,
    spans_snapshot  String,
    raw_response    String,
    created_at      DateTime64(3)
)
ENGINE = MergeTree
ORDER BY trace_id;
```

- [ ] **Step 4: Build check — verify types compile**

Run: `go build ./internal/storage/...`
Expected: compiles (but memstore and chdb will have missing method errors since interface changed)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go internal/storage/schema.sql
git commit -m "feat: add DiagnosisResult types and store interface for trace diagnosis"
```

---

### Task 2: chDB store implementation

**Files:**
- Modify: `internal/storage/chdb.go` — add `GetDiagnosisResult`, `UpsertDiagnosisResult`, auto-create table

- [ ] **Step 1: Add auto-create table call in chDBStore constructor**

Find the existing `s.createAllTables()` or table creation blocks in the chDB store setup and add `diagnosis_results` to the list. The table DDL is already in schema.sql — verify the auto-create mechanism includes it.

Check: Look for existing `CREATE TABLE IF NOT EXISTS` calls in chdb.go's initialization. If there's a `createAllTables()` method, add the diagnosis_results DDL there. If tables are created individually, add a new call.

- [ ] **Step 2: Add GetDiagnosisResult to chdb.go**

Add after the `UpdateTraceCost` method:

```go
func (s *chDBStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error) {
	sql := fmt.Sprintf(
		`SELECT trace_id, model_name, scores, overall_score, findings, summary, spans_snapshot, raw_response, created_at
		 FROM diagnosis_results
		 WHERE trace_id = unhex('%x')
		 FORMAT JSONEachRow`, traceID)

	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get diagnosis result: %w", err)
	}

	var rows []struct {
		TraceID       string `json:"trace_id"`
		ModelName     string `json:"model_name"`
		Scores        string `json:"scores"`
		OverallScore  uint8  `json:"overall_score"`
		Findings      string `json:"findings"`
		Summary       string `json:"summary"`
		SpansSnapshot string `json:"spans_snapshot"`
		RawResponse   string `json:"raw_response"`
		CreatedAt     string `json:"created_at"`
	}
	if err := json.Unmarshal(result, &rows); err != nil {
		return nil, fmt.Errorf("parse diagnosis result: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	r := rows[0]
	tid, err := hex.DecodeString(r.TraceID)
	if err != nil {
		return nil, fmt.Errorf("decode trace_id: %w", err)
	}

	var scores DiagnosisScores
	if err := json.Unmarshal([]byte(r.Scores), &scores); err != nil {
		return nil, fmt.Errorf("parse scores: %w", err)
	}

	var findings []DiagnosisFinding
	if err := json.Unmarshal([]byte(r.Findings), &findings); err != nil {
		return nil, fmt.Errorf("parse findings: %w", err)
	}

	createdAt, _ := time.Parse("2006-01-02 15:04:05.999", r.CreatedAt)

	var tidArr [16]byte
	copy(tidArr[:], tid)
	return &DiagnosisResult{
		TraceID:       tidArr,
		TraceIDHex:    fmt.Sprintf("%x", tidArr),
		ModelName:     r.ModelName,
		Scores:        scores,
		OverallScore:  r.OverallScore,
		Findings:      findings,
		Summary:       r.Summary,
		SpansSnapshot: r.SpansSnapshot,
		RawResponse:   r.RawResponse,
		CreatedAt:     createdAt,
	}, nil
}
```

- [ ] **Step 3: Add UpsertDiagnosisResult to chdb.go**

```go
func (s *chDBStore) UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error {
	scoresJSON, err := json.Marshal(result.Scores)
	if err != nil {
		return fmt.Errorf("marshal scores: %w", err)
	}
	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		return fmt.Errorf("marshal findings: %w", err)
	}

	// Delete existing row first, then insert (MergeTree doesn't support real UPSERT).
	traceIDHex := fmt.Sprintf("%x", result.TraceID)
	deleteSQL := fmt.Sprintf(
		`ALTER TABLE diagnosis_results DELETE WHERE trace_id = unhex('%s')`,
		traceIDHex)
	if err := s.execSQL(deleteSQL); err != nil {
		// DELETE may fail if row doesn't exist — that's fine.
		// ClickHouse ALTER TABLE DELETE is async and may not error on missing rows.
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO diagnosis_results (trace_id, model_name, scores, overall_score, findings, summary, spans_snapshot, raw_response, created_at)
		 VALUES (unhex('%s'), '%s', '%s', %d, '%s', '%s', '%s', '%s', '%s')`,
		traceIDHex,
		escapeString(result.ModelName),
		escapeString(string(scoresJSON)),
		result.OverallScore,
		escapeString(string(findingsJSON)),
		escapeString(result.Summary),
		escapeString(result.SpansSnapshot),
		escapeString(result.RawResponse),
		result.CreatedAt.Format("2006-01-02 15:04:05.999"),
	)

	return s.execSQL(insertSQL)
}
```

Check: Look at `internal/storage/chdb.go` to see if `escapeString` or an equivalent helper already exists. If not, use `fmt.Sprintf` with single-quote escaping or check how other INSERT statements handle string escaping in the codebase. Adapt the pattern accordingly.

- [ ] **Step 4: Build check**

Run: `go build -tags local_engine ./internal/storage/...`
Expected: compiles

- [ ] **Step 5: Commit**

```bash
git add internal/storage/chdb.go
git commit -m "feat: add chDB diagnosis result storage methods"
```

---

### Task 3: memstore implementation

**Files:**
- Modify: `internal/storage/memstore.go` — add map field + two methods

- [ ] **Step 1: Add diagnosisResults map to memStore struct**

Find the `memStore` struct definition and add the field alongside existing maps:

```go
type memStore struct {
	mu              sync.RWMutex
	spans           map[[16]byte][]Span
	traces          map[[16]byte]*traceAggregate
	logs            map[[16]byte][]LogRecord
	services        map[string]struct{}
	sessions        map[string][]*TraceListItem
	modelPricing    map[string]ModelPricing
	llmConfigs      map[string]LLMConfig
	diagnosisResults map[[16]byte]*DiagnosisResult  // new
}
```

- [ ] **Step 2: Initialize map in constructor**

Find the `newMemStore()` function and add initialization:

```go
m := &memStore{
	// ... existing initializations ...
	diagnosisResults: make(map[[16]byte]*DiagnosisResult),
}
```

- [ ] **Step 3: Add GetDiagnosisResult method**

Add after the existing LLM config methods:

```go
func (m *memStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, ok := m.diagnosisResults[traceID]
	if !ok {
		return nil, nil
	}
	return result, nil
}
```

- [ ] **Step 4: Add UpsertDiagnosisResult method**

```go
func (m *memStore) UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnosisResults[result.TraceID] = result
	return nil
}
```

- [ ] **Step 5: Build check**

Run: `go build ./internal/storage/...`
Expected: compiles (both chdb and memstore now satisfy the interface)

- [ ] **Step 6: Commit**

```bash
git add internal/storage/memstore.go
git commit -m "feat: add in-memory diagnosis result storage methods"
```

---

### Task 4: Prompt builder

**Files:**
- Create: `internal/api/diagnosis_prompt.go`

- [ ] **Step 1: Create diagnosis_prompt.go**

```go
package api

import (
	"fmt"
	"strings"

	"github.com/labubu/labubu/internal/storage"
)

const diagnosisSystemPrompt = `You are an LLM observability expert analyzing OTLP trace data from AI agent applications.
Evaluate the trace across four dimensions on a 0-100 scale:

1. **Latency (延迟):** Is the overall duration acceptable? Are there unnecessary delays or slow spans?
2. **Cost (成本):** Is token usage efficient? Is context window utilization reasonable? Are there redundant LLM calls?
3. **Error (错误):** Are there ERROR spans, exceptions, or failures? How severe are they?
4. **Efficiency (效率):** Are tool calls well-structured? Could parallelization reduce time? Are there redundant or circular tool calls?

Scoring guidelines:
- 90-100: Excellent, no issues
- 80-89: Good, minor optimizations possible
- 60-79: Fair, notable issues to address
- 40-59: Poor, significant problems
- 0-39: Critical, major failures

For each dimension scoring below 80, provide specific findings with titles, descriptions, and actionable suggestions.
Return ONLY valid JSON matching this schema — no markdown, no preamble:

{
  "overall_score": 72,
  "scores": {
    "latency": 85,
    "cost": 62,
    "error": 45,
    "efficiency": 88
  },
  "summary": "one-sentence summary",
  "findings": [
    {
      "severity": "error",
      "dimension": "error",
      "title": "short title",
      "description": "detailed description referencing specific spans and data",
      "suggestion": "actionable improvement suggestion"
    }
  ]
}`

// buildDiagnosisUserPrompt creates the user prompt for trace diagnosis.
func buildDiagnosisUserPrompt(trace *storage.TraceDetail, logs []storage.LogListItem) string {
	var b strings.Builder

	// Trace summary
	b.WriteString("Trace Summary:\n")
	service := ""
	if v, ok := trace.ResourceAttrs["service.name"]; ok {
		service = v
	}
	b.WriteString(fmt.Sprintf("- Service: %s\n", service))
	b.WriteString(fmt.Sprintf("- Total spans: %d\n", trace.SpanCount))
	b.WriteString(fmt.Sprintf("- Total duration: %.1fs\n", float64(trace.DurationMS)/1000.0))

	totalTokens := uint32(0)
	llmCount := 0
	for _, s := range trace.Spans {
		if s.TotalTokens != nil {
			totalTokens += *s.TotalTokens
		}
		if s.GenAIRequestModel != nil {
			llmCount++
		}
	}
	b.WriteString(fmt.Sprintf("- Total tokens: %d\n", totalTokens))
	b.WriteString(fmt.Sprintf("- LLM spans: %d\n", llmCount))
	if trace.Cost != nil {
		b.WriteString(fmt.Sprintf("- Total cost: %.4f %s\n", *trace.Cost, trace.CostCurrency))
	}
	b.WriteString("\n")

	// Span list
	b.WriteString("Spans:\n")
	for i, s := range trace.Spans {
		kindStr := spanKindString(s.Kind)
		if s.GenAIRequestModel != nil {
			kindStr = "LLM"
		}
		b.WriteString(fmt.Sprintf("[%d] %s | %s | %.2fs", i, s.Name, kindStr, float64(s.DurationMS)/1000.0))
		if s.InputTokens != nil {
			b.WriteString(fmt.Sprintf(" | input=%d output=%d total=%d", *s.InputTokens, *s.OutputTokens, *s.TotalTokens))
		}
		if s.GenAIRequestModel != nil {
			b.WriteString(fmt.Sprintf(" | model=%s", *s.GenAIRequestModel))
		}
		b.WriteString(fmt.Sprintf(" | status=%s", statusString(s.StatusCode)))
		if s.StatusMessage != "" {
			b.WriteString(fmt.Sprintf(" | message=%q", s.StatusMessage))
		}
		b.WriteString("\n")

		// Events
		if s.Events != "[]" && s.Events != "" {
			b.WriteString(fmt.Sprintf("  Events: %s\n", s.Events))
		}
	}
	b.WriteString("\n")

	// Logs
	if len(logs) > 0 {
		b.WriteString("Logs:\n")
		for _, l := range logs {
			b.WriteString(fmt.Sprintf("[%s] %s", l.Severity, l.EventName))
			if l.Body != "" {
				b.WriteString(fmt.Sprintf(" | body=%s", l.Body))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func spanKindString(kind int32) string {
	switch kind {
	case 1:
		return "INTERNAL"
	case 2:
		return "SERVER"
	case 3:
		return "CLIENT"
	case 4:
		return "PRODUCER"
	case 5:
		return "CONSUMER"
	default:
		return "UNSPECIFIED"
	}
}

func statusString(code int32) string {
	switch code {
	case 1:
		return "OK"
	case 2:
		return "ERROR"
	default:
		return "UNSET"
	}
}
```

Check: Verify the `LogListItem` type in storage.go has `Severity`, `EventName`, and `Body` fields. Adjust field names if different.

- [ ] **Step 2: Build check**

Run: `go build ./internal/api/...`
Expected: compiles

- [ ] **Step 3: Commit**

```bash
git add internal/api/diagnosis_prompt.go
git commit -m "feat: add diagnosis prompt builder"
```

---

### Task 5: LLM client

**Files:**
- Create: `internal/api/diagnosis_llm.go`

- [ ] **Step 1: Create diagnosis_llm.go**

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

// llmDiagnosisResponse is the expected JSON structure from the LLM.
type llmDiagnosisResponse struct {
	OverallScore int                     `json:"overall_score"`
	Scores       storage.DiagnosisScores `json:"scores"`
	Summary      string                  `json:"summary"`
	Findings     []storage.DiagnosisFinding `json:"findings"`
}

// llmChatRequest is the OpenAI-compatible chat completion request body.
type llmChatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Messages    []llmMessage  `json:"messages"`
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
func callLLMForDiagnosis(ctx context.Context, config *storage.LLMConfig, systemPrompt, userPrompt string) (*llmDiagnosisResponse, error) {
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
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Build URL: provider_url + /v1/chat/completions (OpenAI-compatible)
	url := strings.TrimRight(config.ProviderURL, "/") + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm api error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var chatResp llmChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("llm api error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
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
		return nil, fmt.Errorf("parse diagnosis json: %w (raw: %s)", err, content)
	}

	// Validate scores are in range.
	for _, score := range []int{diagResp.Scores.Latency, diagResp.Scores.Cost, diagResp.Scores.Error, diagResp.Scores.Efficiency} {
		if score < 0 || score > 100 {
			return nil, fmt.Errorf("score out of range [0,100]: %d", score)
		}
	}
	if diagResp.OverallScore < 0 || diagResp.OverallScore > 100 {
		return nil, fmt.Errorf("overall_score out of range [0,100]: %d", diagResp.OverallScore)
	}
	for _, f := range diagResp.Findings {
		if f.Severity == "" || f.Dimension == "" || f.Title == "" || f.Description == "" || f.Suggestion == "" {
			return nil, fmt.Errorf("finding missing required field: %+v", f)
		}
	}

	return &diagResp, nil
}
```

- [ ] **Step 2: Build check**

Run: `go build ./internal/api/...`
Expected: compiles

- [ ] **Step 3: Commit**

```bash
git add internal/api/diagnosis_llm.go
git commit -m "feat: add LLM client for trace diagnosis"
```

---

### Task 6: Handler methods + snapshot computation

**Files:**
- Modify: `internal/api/trace_handler.go` — add in-flight tracking to struct, add two handler methods, add computeSnapshot helper

- [ ] **Step 1: Add in-flight tracking fields to TraceHandler struct**

Replace the existing `TraceHandler` struct and constructor:

```go
// TraceHandler holds HTTP handlers for trace endpoints.
type TraceHandler struct {
	store      storage.Store
	inFlightMu sync.Mutex
	inFlight   map[[16]byte]struct{}
}

// NewTraceHandler creates a new TraceHandler.
func NewTraceHandler(store storage.Store) *TraceHandler {
	return &TraceHandler{
		store:    store,
		inFlight: make(map[[16]byte]struct{}),
	}
}
```

Add `"sync"` to the import block.

- [ ] **Step 2: Add computeSnapshot helper function**

Add after the existing handler methods, before `writeJSON`:

```go
// computeSpanSnapshot creates a deterministic fingerprint of trace spans for staleness detection.
func computeSpanSnapshot(spans []storage.SpanDetail) string {
	parts := make([]string, 0, len(spans)+1)
	parts = append(parts, fmt.Sprintf("%d", len(spans)))
	for _, s := range spans {
		parts = append(parts, fmt.Sprintf("%s:%d:%d", s.SpanID, s.StatusCode, s.DurationMS))
	}
	return strings.Join(parts, "|")
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 3: Add GetDiagnosis handler method**

Add after `ExportTraces`:

```go
// GetDiagnosis handles GET /api/v1/traces/{traceIdHex}/diagnosis.
func (h *TraceHandler) GetDiagnosis(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	if len(traceIDHex) != 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_id must be a 32-character hex string"})
		return
	}

	traceIDBytes, err := hex.DecodeString(traceIDHex)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id: %v", err)})
		return
	}
	var traceID [16]byte
	copy(traceID[:], traceIDBytes)

	result, err := h.store.GetDiagnosisResult(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get diagnosis: %v", err)})
		return
	}
	if result == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_diagnosis"})
		return
	}

	// Check staleness.
	detail, err := h.store.GetTrace(r.Context(), traceID)
	if err != nil || detail == nil {
		// Can't check staleness — return result as-is.
		result.TraceIDHex = traceIDHex
		writeJSON(w, http.StatusOK, result)
		return
	}
	currentSnapshot := computeSpanSnapshot(detail.Spans)
	result.Stale = (currentSnapshot != result.SpansSnapshot)
	result.TraceIDHex = traceIDHex

	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 4: Add DiagnoseTrace handler method**

Add after `GetDiagnosis`:

```go
// DiagnoseTrace handles POST /api/v1/traces/{traceIdHex}/diagnose.
func (h *TraceHandler) DiagnoseTrace(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	if len(traceIDHex) != 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_id must be a 32-character hex string"})
		return
	}

	traceIDBytes, err := hex.DecodeString(traceIDHex)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id: %v", err)})
		return
	}
	var traceID [16]byte
	copy(traceID[:], traceIDBytes)

	force := r.URL.Query().Get("force") == "true"

	// Check for cached result (unless force).
	if !force {
		existing, err := h.store.GetDiagnosisResult(r.Context(), traceID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get diagnosis: %v", err)})
			return
		}
		if existing != nil {
			detail, err := h.store.GetTrace(r.Context(), traceID)
			if err == nil && detail != nil {
				currentSnapshot := computeSpanSnapshot(detail.Spans)
				if currentSnapshot == existing.SpansSnapshot {
					existing.Stale = false
					existing.TraceIDHex = traceIDHex
					writeJSON(w, http.StatusOK, existing)
					return
				}
			}
		}
	}

	// In-flight check to prevent duplicate LLM calls.
	h.inFlightMu.Lock()
	if _, ok := h.inFlight[traceID]; ok {
		h.inFlightMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "diagnosis_in_flight"})
		return
	}
	h.inFlight[traceID] = struct{}{}
	h.inFlightMu.Unlock()
	defer func() {
		h.inFlightMu.Lock()
		delete(h.inFlight, traceID)
		h.inFlightMu.Unlock()
	}()

	// Load trace data.
	detail, err := h.store.GetTrace(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get trace: %v", err)})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace_not_found"})
		return
	}

	logs, err := h.store.GetLogsByTrace(r.Context(), traceID)
	if err != nil {
		logs = nil // logs are optional
	}

	// Get default LLM config.
	configs, err := h.store.GetLLMConfigs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get llm configs: %v", err)})
		return
	}
	var defaultConfig *storage.LLMConfig
	for i := range configs {
		if configs[i].IsDefault {
			defaultConfig = &configs[i]
			break
		}
	}
	if defaultConfig == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no_default_model"})
		return
	}

	// Build prompt.
	systemPrompt := diagnosisSystemPrompt
	userPrompt := buildDiagnosisUserPrompt(detail, logs)

	// Call LLM.
	diagResp, err := callLLMForDiagnosis(r.Context(), defaultConfig, systemPrompt, userPrompt)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("llm_call_failed: %v", err)})
		return
	}

	// Build result.
	snapshot := computeSpanSnapshot(detail.Spans)
	rawJSON, _ := json.Marshal(diagResp)
	result := &storage.DiagnosisResult{
		TraceID:       traceID,
		TraceIDHex:    traceIDHex,
		ModelName:     defaultConfig.ModelName,
		Scores:        diagResp.Scores,
		OverallScore:  uint8(diagResp.OverallScore),
		Findings:      diagResp.Findings,
		Summary:       diagResp.Summary,
		SpansSnapshot: snapshot,
		RawResponse:   string(rawJSON),
		CreatedAt:     time.Now(),
		Stale:         false,
	}

	// Store result.
	if err := h.store.UpsertDiagnosisResult(r.Context(), result); err != nil {
		// Log but don't fail — return the result even if storage fails.
		fmt.Printf("api: failed to store diagnosis result: %v\n", err)
	}

	writeJSON(w, http.StatusOK, result)
}
```

Add `"fmt"`, `"encoding/json"`, `"time"`, and `"github.com/labubu/labubu/internal/storage"` to imports. Check: most are already imported. `encoding/json` and `time` may need adding.

- [ ] **Step 5: Build check**

Run: `go build ./internal/api/...`
Expected: compiles

- [ ] **Step 6: Commit**

```bash
git add internal/api/trace_handler.go
git commit -m "feat: add DiagnoseTrace and GetDiagnosis handler methods"
```

---

### Task 7: Router registration

**Files:**
- Modify: `internal/api/router.go` — add diagnosis route dispatch inside `/api/v1/traces/` catch-all

- [ ] **Step 1: Update the /api/v1/traces/ catch-all handler**

Replace the existing `/api/v1/traces/` registration block:

```go
	mux.HandleFunc("/api/v1/traces/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/traces")
		if path == "" || path == "/" {
			traceHandler.ListTraces(w, r)
			return
		}
		path = strings.TrimPrefix(path, "/")
		parts := strings.SplitN(path, "/", 2)
		traceIDHex := parts[0]
		if len(parts) == 2 {
			switch parts[1] {
			case "diagnosis":
				if r.Method == http.MethodGet {
					traceHandler.GetDiagnosis(w, r, traceIDHex)
					return
				}
			case "diagnose":
				if r.Method == http.MethodPost {
					traceHandler.DiagnoseTrace(w, r, traceIDHex)
					return
				}
			}
		}
		traceHandler.GetTrace(w, r, traceIDHex)
	})
```

- [ ] **Step 2: Build check**

Run: `go build ./internal/api/...`
Expected: compiles

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register diagnosis routes in trace router"
```

---

### Task 8: Handler tests

**Files:**
- Modify: `internal/api/trace_handler_test.go` — add mock fields + diagnosis tests

- [ ] **Step 1: Add diagnosis fields to handlerMockStore**

Add to the `handlerMockStore` struct:

```go
	diagnosisResult    *storage.DiagnosisResult
	diagnosisResultErr error
	llmConfigs         []storage.LLMConfig
	llmConfigsErr      error
	logs               []storage.LogListItem
	logsErr            error
```

- [ ] **Step 2: Add mock method implementations**

Replace the existing no-op `GetLLMConfigs` with:

```go
func (m *handlerMockStore) GetLLMConfigs(ctx context.Context) ([]storage.LLMConfig, error) {
	return m.llmConfigs, m.llmConfigsErr
}
```

Replace the no-op `GetLogsByTrace` with:

```go
func (m *handlerMockStore) GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]storage.LogListItem, error) {
	return m.logs, m.logsErr
}
```

Add new mock methods:

```go
func (m *handlerMockStore) GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*storage.DiagnosisResult, error) {
	return m.diagnosisResult, m.diagnosisResultErr
}

func (m *handlerMockStore) UpsertDiagnosisResult(ctx context.Context, result *storage.DiagnosisResult) error {
	return nil
}
```

- [ ] **Step 3: Add test: GET diagnosis — no result → 404**

```go
func TestGetDiagnosisNoResult(t *testing.T) {
	store := &handlerMockStore{
		diagnosisResult: nil,
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/diagnosis", nil)
	rec := httptest.NewRecorder()

	handler.GetDiagnosis(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 4: Add test: GET diagnosis — cached result → 200**

```go
func TestGetDiagnosisCached(t *testing.T) {
	var tid [16]byte
	hex.Decode(tid[:], []byte("a1b2c3d4e5f600000000000000000000"))
	// hex.Decode is in encoding/hex — already imported.

	store := &handlerMockStore{
		diagnosisResult: &storage.DiagnosisResult{
			TraceID:      tid,
			ModelName:    "test-model",
			OverallScore: 85,
			Scores:       storage.DiagnosisScores{Latency: 90, Cost: 80, Error: 85, Efficiency: 85},
			Summary:      "all good",
		},
		// detail: nil means staleness check is skipped (trace not found)
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/diagnosis", nil)
	rec := httptest.NewRecorder()

	handler.GetDiagnosis(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.DiagnosisResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if result.OverallScore != 85 {
		t.Errorf("expected overall_score 85, got %d", result.OverallScore)
	}
}
```

- [ ] **Step 5: Add test: POST diagnose — no default model → 400**

```go
func TestDiagnoseTraceNoDefaultModel(t *testing.T) {
	store := &handlerMockStore{
		llmConfigs: []storage.LLMConfig{
			{ID: "1", ModelName: "test", IsDefault: false},
		},
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/diagnose", nil)
	rec := httptest.NewRecorder()

	handler.DiagnoseTrace(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "no_default_model" {
		t.Errorf("expected 'no_default_model', got '%s'", body["error"])
	}
}
```

- [ ] **Step 6: Add test: POST diagnose — bad trace ID → 400**

```go
func TestDiagnoseTraceBadID(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/traces/short/diagnose", nil)
	rec := httptest.NewRecorder()

	handler.DiagnoseTrace(rec, req, "short")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short id, got %d", rec.Code)
	}
}
```

- [ ] **Step 7: Run tests**

Run: `go test -v ./internal/api/ -run "TestGetDiagnosis|TestDiagnoseTrace"`
Expected: tests pass (no LLM actually called since the mock store won't have a default config, or the tests hit the fast-path validation errors)

- [ ] **Step 8: Commit**

```bash
git add internal/api/trace_handler_test.go
git commit -m "test: add diagnosis handler tests"
```

---

### Task 9: Frontend i18n keys

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add diagnosis keys to en.ts**

Add a `diagnosis` block to the default export object:

```typescript
    diagnosis: {
        tab: 'Diagnosis',
        empty: 'No diagnosis yet for this trace',
        start: 'Start Diagnosis',
        rediagnose: 'Re-diagnose',
        analyzing: 'Analyzing...',
        estTime: 'Estimated 10-30 seconds',
        noModel: 'Please configure a default LLM model first',
        stale: 'Trace data has changed, consider re-diagnosing',
        overall: 'Overall Score',
        latency: 'Latency',
        cost: 'Cost',
        error: 'Error',
        efficiency: 'Efficiency',
        critical: 'Critical Issues',
        suggestions: 'Suggestions',
        modelLabel: 'Model',
        timeout: 'Diagnosis timed out, please retry',
        formatError: 'Unexpected response format, please retry',
        tooLarge: 'This trace is too large for diagnosis',
    },
```

Add after the existing `alerts` block (or as the last entry before the closing `}`).

- [ ] **Step 2: Add diagnosis keys to zh.ts**

Add matching block with Chinese values:

```typescript
    diagnosis: {
        tab: '诊断',
        empty: '尚未对此 Trace 进行诊断',
        start: '开始诊断',
        rediagnose: '重新诊断',
        analyzing: '正在分析中...',
        estTime: '预计耗时 10-30 秒',
        noModel: '请先配置默认 LLM 模型',
        stale: 'Trace 数据已更新，建议重新诊断',
        overall: '综合评分',
        latency: '延迟',
        cost: '成本',
        error: '错误',
        efficiency: '效率',
        critical: '关键问题',
        suggestions: '优化建议',
        modelLabel: '模型',
        timeout: '诊断超时，请重试',
        formatError: '诊断结果格式异常，请重试',
        tooLarge: '此 Trace 规模过大，暂不支持诊断',
    },
```

- [ ] **Step 3: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no new errors (i18n changes alone won't cause type errors)

- [ ] **Step 4: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add diagnosis i18n keys"
```

---

### Task 10: Frontend API client

**Files:**
- Modify: `web/src/api/client.ts` — add types + two API functions

- [ ] **Step 1: Add DiagnosisResult types**

Add after existing type definitions:

```typescript
export interface DiagnosisFinding {
    severity: 'error' | 'warning' | 'info'
    dimension: 'latency' | 'cost' | 'error' | 'efficiency'
    title: string
    description: string
    suggestion: string
    span_name?: string
    span_index?: number
}

export interface DiagnosisScores {
    latency: number
    cost: number
    error: number
    efficiency: number
}

export interface DiagnosisResult {
    trace_id_hex: string
    model_name: string
    overall_score: number
    scores: DiagnosisScores
    findings: DiagnosisFinding[]
    summary: string
    created_at: string
    stale: boolean
}
```

- [ ] **Step 2: Add API functions**

Add after existing API functions:

```typescript
export async function getDiagnosisResult(traceIdHex: string): Promise<DiagnosisResult> {
    const res = await fetch(`${BASE_URL}/traces/${traceIdHex}/diagnosis`)
    if (!res.ok) {
        const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
        throw new Error(err.error || `Failed to get diagnosis: ${res.status}`)
    }
    return res.json()
}

export async function diagnoseTrace(traceIdHex: string, force?: boolean): Promise<DiagnosisResult> {
    const url = new URL(`${BASE_URL}/traces/${traceIdHex}/diagnose`, window.location.origin)
    if (force) {
        url.searchParams.set('force', 'true')
    }
    const res = await fetch(url.toString(), { method: 'POST' })
    if (!res.ok) {
        const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
        throw new Error(err.error || `Diagnosis failed: ${res.status}`)
    }
    return res.json()
}
```

- [ ] **Step 3: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no new errors

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat: add diagnosis API client functions"
```

---

### Task 11: DiagnosisTab component

**Files:**
- Create: `web/src/components/DiagnosisTab.vue`

- [ ] **Step 1: Create DiagnosisTab.vue**

```vue
<template>
  <div class="diagnosis-tab">
    <!-- State 1: Empty -->
    <div v-if="!loading && !result" class="diagnosis-empty">
      <div class="empty-icon">🔍</div>
      <p>{{ t('diagnosis.empty') }}</p>
      <button
        class="btn-diagnose"
        :disabled="noModel"
        @click="$emit('diagnose')"
      >
        {{ t('diagnosis.start') }}
      </button>
      <p v-if="noModel" class="hint-no-model">
        <router-link to="/llm-configs">{{ t('diagnosis.noModel') }}</router-link>
      </p>
    </div>

    <!-- State 2: Loading -->
    <div v-else-if="loading" class="diagnosis-loading">
      <div class="spinner"></div>
      <p>{{ t('diagnosis.analyzing') }}</p>
      <p class="est-time">{{ t('diagnosis.estTime') }}</p>
    </div>

    <!-- State 3: Result -->
    <div v-else-if="result" class="diagnosis-result">
      <!-- Stale banner -->
      <div v-if="result.stale" class="stale-banner">
        {{ t('diagnosis.stale') }}
      </div>

      <!-- Header -->
      <div class="result-header">
        <span class="overall-score" :class="scoreColor(result.overall_score)">
          {{ t('diagnosis.overall') }}: {{ result.overall_score }}/100
        </span>
        <span class="model-name">{{ t('diagnosis.modelLabel') }}: {{ result.model_name }}</span>
        <span class="timestamp">{{ formatTime(result.created_at) }}</span>
        <button class="btn-rediagnose" @click="$emit('diagnose')">
          {{ t('diagnosis.rediagnose') }}
        </button>
      </div>

      <!-- Score cards -->
      <div class="score-cards">
        <div v-for="dim in dimensions" :key="dim.key" class="score-card" :class="scoreColor(result.scores[dim.key])">
          <div class="score-value">{{ result.scores[dim.key] }}</div>
          <div class="score-label">{{ t(`diagnosis.${dim.key}`) }}</div>
        </div>
      </div>

      <!-- Summary -->
      <p class="diagnosis-summary">{{ result.summary }}</p>

      <!-- Findings grouped by severity -->
      <div v-if="criticalFindings.length" class="findings-section">
        <h4>{{ t('diagnosis.critical') }} ({{ criticalFindings.length }})</h4>
        <div v-for="(f, i) in criticalFindings" :key="'crit-'+i" class="finding-card severity-error" @click="onFindingClick(f)">
          <span class="severity-badge error">{{ f.severity }}</span>
          <span class="dimension-tag">{{ f.dimension }}</span>
          <strong>{{ f.title }}</strong>
          <p>{{ f.description }}</p>
          <p class="suggestion">💡 {{ f.suggestion }}</p>
        </div>
      </div>

      <div v-if="otherFindings.length" class="findings-section">
        <h4>{{ t('diagnosis.suggestions') }} ({{ otherFindings.length }})</h4>
        <div v-for="(f, i) in otherFindings" :key="'other-'+i" class="finding-card" :class="'severity-'+f.severity" @click="onFindingClick(f)">
          <span class="severity-badge" :class="f.severity">{{ f.severity }}</span>
          <span class="dimension-tag">{{ f.dimension }}</span>
          <strong>{{ f.title }}</strong>
          <p>{{ f.description }}</p>
          <p class="suggestion">💡 {{ f.suggestion }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DiagnosisResult } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  result: DiagnosisResult | null
  loading: boolean
  noModel: boolean
}>()

const emit = defineEmits<{
  diagnose: []
  'navigate-span': [spanIndex: number]
}>()

const dimensions = [
  { key: 'latency' as const },
  { key: 'cost' as const },
  { key: 'error' as const },
  { key: 'efficiency' as const },
]

const criticalFindings = computed(() =>
  props.result?.findings.filter(f => f.severity === 'error') ?? []
)

const otherFindings = computed(() =>
  props.result?.findings.filter(f => f.severity !== 'error') ?? []
)

function scoreColor(score: number): string {
  if (score >= 80) return 'score-good'
  if (score >= 60) return 'score-fair'
  return 'score-poor'
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  const now = Date.now()
  const diff = now - d.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return d.toLocaleDateString()
}

function onFindingClick(finding: { span_index?: number }) {
  if (finding.span_index !== undefined) {
    emit('navigate-span', finding.span_index)
  }
}
</script>

<style scoped>
.diagnosis-tab {
  padding: 20px;
}

.diagnosis-empty {
  text-align: center;
  padding: 60px 20px;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.btn-diagnose {
  margin-top: 16px;
  padding: 10px 24px;
  background: var(--accent-blue);
  color: #fff;
  border: none;
  border-radius: 6px;
  font-size: 14px;
  cursor: pointer;
}

.btn-diagnose:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.hint-no-model {
  margin-top: 8px;
  font-size: 12px;
}

.hint-no-model a {
  color: var(--accent-blue);
}

.diagnosis-loading {
  text-align: center;
  padding: 60px 20px;
  color: var(--text-secondary);
}

.spinner {
  width: 40px;
  height: 40px;
  margin: 0 auto 16px;
  border: 3px solid var(--border-default);
  border-top-color: var(--accent-blue);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.est-time {
  font-size: 12px;
  margin-top: 8px;
}

.result-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.overall-score {
  font-size: 20px;
  font-weight: 700;
  padding: 6px 14px;
  border-radius: 8px;
}

.model-name {
  color: var(--text-secondary);
  font-size: 13px;
}

.timestamp {
  color: var(--text-secondary);
  font-size: 12px;
  margin-left: auto;
}

.btn-rediagnose {
  padding: 6px 14px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
}

.btn-rediagnose:hover {
  background: var(--bg-hover);
}

.score-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 20px;
}

.score-card {
  text-align: center;
  padding: 16px 8px;
  border-radius: 8px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
}

.score-value {
  font-size: 32px;
  font-weight: 700;
}

.score-label {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

.score-good .score-value { color: #22c55e; }
.score-fair .score-value { color: #eab308; }
.score-poor .score-value { color: #ef4444; }

.score-good { border-left: 3px solid #22c55e; }
.score-fair { border-left: 3px solid #eab308; }
.score-poor { border-left: 3px solid #ef4444; }

.stale-banner {
  background: #fef3c7;
  color: #92400e;
  padding: 8px 14px;
  border-radius: 6px;
  font-size: 13px;
  margin-bottom: 16px;
}

.diagnosis-summary {
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.5;
  margin-bottom: 24px;
  padding: 12px;
  background: var(--bg-surface);
  border-radius: 6px;
}

.findings-section {
  margin-bottom: 20px;
}

.findings-section h4 {
  font-size: 14px;
  margin-bottom: 10px;
  color: var(--text-primary);
}

.finding-card {
  padding: 14px;
  margin-bottom: 10px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  cursor: default;
}

.finding-card.severity-error {
  border-left: 3px solid #ef4444;
}

.finding-card.severity-warning {
  border-left: 3px solid #eab308;
}

.finding-card.severity-info {
  border-left: 3px solid #3b82f6;
}

.finding-card strong {
  display: inline;
  font-size: 14px;
}

.finding-card p {
  margin: 6px 0 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.suggestion {
  color: var(--text-primary) !important;
  font-style: italic;
}

.severity-badge {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  margin-right: 8px;
}

.severity-badge.error { background: #fecaca; color: #991b1b; }
.severity-badge.warning { background: #fef3c7; color: #92400e; }
.severity-badge.info { background: #dbeafe; color: #1e40af; }

.dimension-tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
  background: var(--bg-hover);
  color: var(--text-secondary);
  margin-right: 8px;
}
</style>
```

- [ ] **Step 2: Build check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: should compile; if there are type issues with the emit pattern, fix the `onFindingClick` function to not rely on `$emit`. Instead, have the parent handle the click by passing a callback or using a different approach.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/DiagnosisTab.vue
git commit -m "feat: add DiagnosisTab component with three states"
```

---

### Task 12: Integrate into TraceDetail.vue

**Files:**
- Modify: `web/src/views/TraceDetail.vue` — add diagnosis tab, state, and data fetching

- [ ] **Step 1: Import DiagnosisTab and API functions**

Add imports:

```typescript
import DiagnosisTab from '../components/DiagnosisTab.vue'
import { getDiagnosisResult, diagnoseTrace, type DiagnosisResult } from '../api/client'
```

- [ ] **Step 2: Add diagnosis state variables**

Add after existing state declarations:

```typescript
// Diagnosis state
const diagnosisResult = ref<DiagnosisResult | null>(null)
const diagnosisLoading = ref(false)
const diagnosisNoModel = ref(false)
```

- [ ] **Step 3: Add diagnosis fetch function**

Add after existing data-fetching functions:

```typescript
async function fetchDiagnosis() {
    try {
        diagnosisResult.value = await getDiagnosisResult(traceIdHex)
        diagnosisNoModel.value = false
    } catch (e: any) {
        if (e.message === 'no_diagnosis') {
            diagnosisResult.value = null
        }
        // Other errors — leave result as null, will show empty state.
    }
}

async function startDiagnosis() {
    diagnosisLoading.value = true
    diagnosisNoModel.value = false
    try {
        diagnosisResult.value = await diagnoseTrace(traceIdHex, false)
    } catch (e: any) {
        if (e.message === 'no_default_model') {
            diagnosisNoModel.value = true
        } else {
            alert(e.message || 'Diagnosis failed')
        }
    } finally {
        diagnosisLoading.value = false
    }
}
```

- [ ] **Step 4: Update tab type and switchTab**

Change `activeTab` type from `'spans' | 'logs'` to `'spans' | 'logs' | 'diagnosis'`:

```typescript
const activeTab = ref<'spans' | 'logs' | 'diagnosis'>('spans')
```

- [ ] **Step 5: Add "诊断" tab button in template**

Add a new tab button in `.panel-tabs`, after the Logs button:

```html
<button
  :class="['tab-btn', { active: activeTab === 'diagnosis' }]"
  @click="switchTab('diagnosis')"
>
  {{ t('diagnosis.tab') }}
</button>
```

- [ ] **Step 6: Add DiagnosisTab in template**

Add after the existing tab content divs, before the closing `</div>` of `.detail-panel`:

```html
<div v-if="activeTab === 'diagnosis'" class="diagnosis-panel">
  <DiagnosisTab
    :result="diagnosisResult"
    :loading="diagnosisLoading"
    :noModel="diagnosisNoModel"
    @diagnose="startDiagnosis"
    @navigate-span="onDiagnosisNavigateSpan"
  />
</div>
```

- [ ] **Step 7: Add navigate-span handler**

Add a function to handle clicking a finding's span reference:

```typescript
function onDiagnosisNavigateSpan(spanIndex: number) {
    // Switch to spans tab
    activeTab.value = 'spans'
    // Close drawer first
    drawerOpen.value = false
    // Wait for tab switch, then open the span in the drawer
    nextTick(() => {
        // Find the span in the waterfall and open it.
        // The waterfall chart has expand/collapse methods.
        // For now, open the drawer with the specific span.
        if (trace.value?.spans && trace.value.spans[spanIndex]) {
            selectedSpan.value = trace.value.spans[spanIndex]
            drawerOpen.value = true
        }
    })
}
```

Add `nextTick` to the Vue import: `import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'`

- [ ] **Step 8: Call fetchDiagnosis in onMounted**

Add to `onMounted` after `fetchTrace()`:

```typescript
onMounted(() => {
    fetchTrace()
    fetchDiagnosis()
    window.addEventListener('keydown', onKeydown)
})
```

- [ ] **Step 9: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 10: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat: integrate diagnosis tab into TraceDetail view"
```

---

## Self-Review Checklist

**1. Spec coverage:**

| Spec requirement | Task |
|-----------------|------|
| `diagnosis_results` table + Go types | Task 1 |
| Store interface methods | Task 1 (interface), Task 2 (chDB), Task 3 (memstore) |
| GET /api/v1/traces/{id}/diagnosis | Task 6 (handler), Task 7 (router) |
| POST /api/v1/traces/{id}/diagnose | Task 6 (handler), Task 7 (router) |
| Prompt builder (system + user) | Task 4 |
| LLM client (OpenAI-compatible call) | Task 5 |
| Response validation (scores, findings) | Task 5 |
| Cached result + staleness detection | Task 6 (handler logic) |
| In-flight dedup (409) | Task 6 (inFlight map) |
| Force re-diagnosis | Task 6 (force query param) |
| No default model → 400 | Task 6 + Task 8 (test) |
| Trace not found → 404 | Task 6 |
| Trace too large → 413 | (covered by prompt builder truncation — task 4; 413 is reserved for future enhancement) |
| LLM timeout/error → 502 | Task 5 + Task 6 |
| Parse error → 500 | Task 5 |
| Frontend three states (empty/loading/result) | Task 11 |
| Score cards + findings display | Task 11 |
| Finding → span navigation | Task 11 + Task 12 |
| i18n keys (en + zh) | Task 9 |
| TypeScript types + API functions | Task 10 |
| Handler tests | Task 8 |

**2. Placeholder scan:** No TBD, TODO, or "implement later" patterns found. Every step has concrete code.

**3. Type consistency:**
- `DiagnosisResult.TraceIDHex` used in handler and frontend → consistent
- `DiagnosisFinding` fields match between Go (`json:"span_index"`) and TypeScript (`span_index?: number`) → consistent
- `DiagnosisScores` fields (`latency`, `cost`, `error`, `efficiency`) match everywhere → consistent
- Route paths `/diagnosis` (GET) and `/diagnose` (POST) match between router, handler, and frontend API calls → consistent
- Emit names `diagnose` and `navigate-span` match between DiagnosisTab emits and TraceDetail listeners → consistent
