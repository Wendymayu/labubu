# Agent Behavior Analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add agent behavior analysis to Labubu — tool call success rates, retry tracking, loop detection — embedded in TraceDetail and SessionDetail pages.

**Architecture:** Hybrid approach — Trace-level metrics computed client-side from GetTrace API data, Session-level metrics computed server-side via new `/api/v1/sessions/{id}/agent-stats` API endpoint. GenAI semantic attributes (`gen_ai.tool.name`, `gen_ai.system`) identify tool calls and LLM calls in spans.

**Tech Stack:** Go backend (Store interface extension, new handler, memstore computation), Vue 3 Composition API frontend (new components, i18n), OTel GenAI semantic conventions.

---

### Task 1: Add AgentStats and ToolUsageItem types to storage.go

**Files:**
- Modify: `internal/storage/storage.go` (after SpanDetail struct, ~line 227)
- Modify: `internal/storage/storage.go` (Store interface, ~line 357)

- [ ] **Step 1: Add ToolUsageItem struct**

Add after the `SpanDetail` struct (after line 227):

```go
// ToolUsageItem holds statistics for a single tool across a session.
type ToolUsageItem struct {
	ToolName    string  `json:"tool_name"`
	CallCount   int     `json:"call_count"`
	SuccessRate float64 `json:"success_rate"`
	AvgRetries  float64 `json:"avg_retries"`
	MaxLoop     int     `json:"max_loop"`
}
```

- [ ] **Step 2: Add AgentStats struct**

Add after `ToolUsageItem`:

```go
// AgentStats holds aggregate agent behavior statistics for a session.
type AgentStats struct {
	TraceSuccessRate    float64        `json:"trace_success_rate"`
	AvgToolSuccessRate  float64        `json:"avg_tool_success_rate"`
	AvgRetries          float64        `json:"avg_retries"`
	AvgLoopDepth        float64        `json:"avg_loop_depth"`
	MaxLoopDepth        int            `json:"max_loop_depth"`
	SpanPerTrace        float64        `json:"span_per_trace"`
	TotalToolCalls      int            `json:"total_tool_calls"`
	SuccessfulToolCalls int            `json:"successful_tool_calls"`
	ToolUsage           []ToolUsageItem `json:"tool_usage"`
	Insights            []string       `json:"insights"`
}
```

- [ ] **Step 3: Add IsToolCall, ToolName, GenAISystem fields to SpanDetail**

Add three new fields after `GenAIRequestModel` (line 226):

```go
	GenAISystem       *string           `json:"gen_ai_system"`        // Attributes["gen_ai.system"]
	ToolName          *string           `json:"tool_name"`            // Attributes["gen_ai.tool.name"]
	IsToolCall        bool              `json:"is_tool_call"`         // ToolName != nil
```

- [ ] **Step 4: Add GetSessionAgentStats method to Store interface**

Add after `UpsertDiagnosisResult` (line 357):

```go
	// GetSessionAgentStats computes agent behavior statistics for a session.
	GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error)
```

- [ ] **Step 5: Run `go build` to verify compilation fails (missing implementations)**

Run: `cd D:/opensource/github/labubu && go build -tags dev ./cmd/labubu`

Expected: compile errors about `GetSessionAgentStats` not implemented in memStore, sqliteStore, and mock stores.

- [ ] **Step 6: Commit**

```bash
cd D:/opensource/github/labubu && git add internal/storage/storage.go && git commit -m "feat: add AgentStats, ToolUsageItem types and GetSessionAgentStats to Store interface"
```

---

### Task 2: Implement GetSessionAgentStats in memstore

**Files:**
- Modify: `internal/storage/memstore.go` (add new method)

- [ ] **Step 1: Add GetSessionAgentStats method to memStore**

Add a new method after the `UpsertDiagnosisResult` method. The implementation queries all traces in the session, then iterates their spans to compute agent metrics:

```go
func (m *memStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect all traces for this session.
	var sessionTraces []Trace
	for _, t := range m.traces {
		if t.SessionID == sessionID {
			sessionTraces = append(sessionTraces, t)
		}
	}

	if len(sessionTraces) == 0 {
		return nil, nil
	}

	// Collect all spans for these traces.
	var allSpans []Span
	traceIDs := make(map[[16]byte]struct{})
	for _, t := range sessionTraces {
		traceIDs[t.TraceID] = struct{}{}
	}
	for _, s := range m.spans {
		if _, ok := traceIDs[s.TraceID]; ok {
			allSpans = append(allSpans, s)
		}
	}

	return computeAgentStats(sessionTraces, allSpans), nil
}
```

- [ ] **Step 2: Add computeAgentStats helper function**

Add a standalone helper function (not a method) in `memstore.go`. This function computes all agent stats from trace and span data:

```go
// computeAgentStats calculates agent behavior statistics from trace and span data.
func computeAgentStats(traces []Trace, spans []Span) *AgentStats {
	stats := &AgentStats{}

	// Trace success rate.
	totalTraces := len(traces)
	okTraces := 0
	for _, t := range traces {
		if StatusCodeToString(t.StatusCode) == "ok" {
			okTraces++
		}
	}
	if totalTraces > 0 {
		stats.TraceSuccessRate = float64(okTraces) / float64(totalTraces)
	}

	// Span-per-trace.
	stats.SpanPerTrace = float64(len(spans)) / float64(totalTraces)

	// Identify tool spans and LLM spans.
	toolSpans := make(map[string][]Span) // tool_name -> spans
	var orderedToolSpans []Span           // all tool spans in time order

	for _, s := range spans {
		toolName := s.Attributes["gen_ai.tool.name"]
		if toolName != "" {
			orderedToolSpans = append(orderedToolSpans, s)
			toolSpans[toolName] = append(toolSpans[toolName], s)
		}
	}

	// Sort ordered tool spans by start time.
	sort.Slice(orderedToolSpans, func(i, j int) bool {
		return orderedToolSpans[i].StartTimeMS < orderedToolSpans[j].StartTimeMS
	})

	stats.TotalToolCalls = len(orderedToolSpans)

	// Count successful tool calls.
	successCount := 0
	for _, s := range orderedToolSpans {
		if StatusCodeToString(s.StatusCode) == "ok" {
			successCount++
		}
	}
	stats.SuccessfulToolCalls = successCount

	if len(orderedToolSpans) > 0 {
		stats.AvgToolSuccessRate = float64(successCount) / float64(len(orderedToolSpans))
	}

	// Per-tool stats.
	var toolUsage []ToolUsageItem
	for toolName, toolSpanList := range toolSpans {
		sort.Slice(toolSpanList, func(i, j int) bool {
			return toolSpanList[i].StartTimeMS < toolSpanList[j].StartTimeMS
		})

		item := ToolUsageItem{ToolName: toolName, CallCount: len(toolSpanList)}

		// Success rate per tool.
		okCount := 0
		for _, s := range toolSpanList {
			if StatusCodeToString(s.StatusCode) == "ok" {
				okCount++
			}
		}
		if len(toolSpanList) > 0 {
			item.SuccessRate = float64(okCount) / float64(len(toolSpanList))
		}

		// Retry detection per tool.
		totalRetries := 0
		retryGroups := 0
		consecutiveErrors := 0
		for _, s := range toolSpanList {
			status := StatusCodeToString(s.StatusCode)
			if status == "error" {
				consecutiveErrors++
			} else if status == "ok" && consecutiveErrors > 0 {
				totalRetries += consecutiveErrors
				retryGroups++
				consecutiveErrors = 0
			} else {
				consecutiveErrors = 0
			}
		}
		if retryGroups > 0 {
			item.AvgRetries = float64(totalRetries) / float64(retryGroups)
		}

		// Loop depth per tool: max consecutive same-tool calls across all ordered tool spans.
		// Already computed below in the global loop depth section.
		// For per-tool max loop, count max consecutive occurrences in orderedToolSpans.
		maxConsecutive := 1
		currentConsecutive := 1
		for i := 1; i < len(orderedToolSpans); i++ {
			if orderedToolSpans[i].Attributes["gen_ai.tool.name"] == orderedToolSpans[i-1].Attributes["gen_ai.tool.name"] &&
				orderedToolSpans[i].Attributes["gen_ai.tool.name"] == toolName {
				currentConsecutive++
				if currentConsecutive > maxConsecutive {
					maxConsecutive = currentConsecutive
				}
			} else {
				if orderedToolSpans[i].Attributes["gen_ai.tool.name"] == toolName {
					currentConsecutive = 1
				}
			}
		}
		item.MaxLoop = maxConsecutive

		toolUsage = append(toolUsage, item)
	}

	// Sort tool usage by call count descending.
	sort.Slice(toolUsage, func(i, j int) bool {
		return toolUsage[i].CallCount > toolUsage[j].CallCount
	})
	stats.ToolUsage = toolUsage

	// Global loop depth: max consecutive same-tool calls in orderedToolSpans.
	if len(orderedToolSpans) > 0 {
		maxLoop := 1
		currentLoop := 1
		for i := 1; i < len(orderedToolSpans); i++ {
			if orderedToolSpans[i].Attributes["gen_ai.tool.name"] == orderedToolSpans[i-1].Attributes["gen_ai.tool.name"] {
				currentLoop++
				if currentLoop > maxLoop {
					maxLoop = currentLoop
				}
			} else {
				currentLoop = 1
			}
		}
		stats.MaxLoopDepth = maxLoop
	}

	// Global avg retries and avg loop depth.
	totalRetriesAll := 0
	totalRetryGroups := 0
	for _, item := range toolUsage {
		if item.AvgRetries > 0 {
			// Weighted by the number of retry groups (approximation).
			totalRetriesAll += int(item.AvgRetries * float64(item.CallCount))
			totalRetryGroups += item.CallCount
		}
	}
	if totalRetryGroups > 0 {
		stats.AvgRetries = float64(totalRetriesAll) / float64(totalRetryGroups)
	}

	// Avg loop depth: average of per-tool max loops weighted by call count.
	totalLoops := 0
	totalCallsForLoops := 0
	for _, item := range toolUsage {
		if item.MaxLoop > 1 {
			totalLoops += item.MaxLoop * item.CallCount
			totalCallsForLoops += item.CallCount
		}
	}
	if totalCallsForLoops > 0 {
		stats.AvgLoopDepth = float64(totalLoops) / float64(totalCallsForLoops)
	} else {
		stats.AvgLoopDepth = 1.0
	}

	// Generate insights.
	stats.Insights = generateInsights(stats)

	return stats
}
```

- [ ] **Step 3: Add generateInsights function**

```go
// generateInsights creates auto-generated observations from agent stats.
func generateInsights(stats *AgentStats) []string {
	var insights []string

	// Loop detection insight.
	for _, item := range stats.ToolUsage {
		if item.MaxLoop >= 3 {
			insights = append(insights, fmt.Sprintf("%s has max loop depth %d — agent may be stuck in a retry loop", item.ToolName, item.MaxLoop))
		}
	}

	// Low success rate insight.
	for _, item := range stats.ToolUsage {
		if item.SuccessRate < 0.8 && item.CallCount >= 3 {
			insights = append(insights, fmt.Sprintf("%s has low success rate (%d%%) — consider adding fallback logic", item.ToolName, int(item.SuccessRate*100)))
		}
	}

	// Low trace success rate insight.
	if stats.TraceSuccessRate < 0.7 && stats.TraceSuccessRate > 0 {
		insights = append(insights, fmt.Sprintf("Over %d%% of traces failed — agent configuration may need adjustment", int((1-stats.TraceSuccessRate)*100)))
	}

	// High retry count insight.
	if stats.AvgRetries > 1.0 {
		insights = append(insights, fmt.Sprintf("High average retry count (%.1f) — tool calls frequently fail on first attempt", stats.AvgRetries))
	}

	// Cap at 4 insights.
	if len(insights) > 4 {
		insights = insights[:4]
	}

	return insights
}
```

- [ ] **Step 4: Add required imports to memstore.go**

Ensure `memstore.go` has these imports (add any missing):

```go
import (
	"fmt"
	"sort"
	// ... existing imports ...
)
```

- [ ] **Step 5: Run `go build` to verify compilation**

Run: `cd D:/opensource/github/labubu && go build -tags dev ./cmd/labubu`

Expected: Should compile (sqliteStore and test mocks still missing `GetSessionAgentStats` — fix in next tasks).

- [ ] **Step 6: Commit**

```bash
cd D:/opensource/github/labubu && git add internal/storage/memstore.go && git commit -m "feat: implement GetSessionAgentStats and computeAgentStats in memstore"
```

---

### Task 3: Implement GetSessionAgentStats in sqlite_store and update buildSpanDetail

**Files:**
- Modify: `internal/storage/sqlite_store.go` (add method stub)
- Modify: `internal/api/trace_handler.go` (update buildSpanDetail to extract GenAI attributes)
- Modify: `internal/pipeline/pipeline_test.go` (add GetSessionAgentStats to mockStore)

- [ ] **Step 1: Add GetSessionAgentStats stub to sqlite_store.go**

Add method returning "not implemented" (same pattern as other unimplemented methods):

```go
func (s *sqliteStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 2: Update buildSpanDetail in trace_handler.go to extract GenAI attributes**

Find the `buildSpanDetail` function (or the span-to-SpanDetail conversion logic in `GetTrace` handler). After the existing SpanDetail fields are populated, add:

```go
	// Extract GenAI semantic attributes.
	if span.Attributes != nil {
		if v, ok := span.Attributes["gen_ai.system"]; ok {
			detail.GenAISystem = &v
		}
		if v, ok := span.Attributes["gen_ai.tool.name"]; ok {
			detail.ToolName = &v
			detail.IsToolCall = true
		}
	}
```

Note: The exact location depends on how SpanDetail is currently constructed. Find the code that creates `SpanDetail` from `Span` and add these three lines after the existing attribute assignment.

- [ ] **Step 3: Add GetSessionAgentStats to mockStore in pipeline_test.go**

Add to the mockStore in `internal/pipeline/pipeline_test.go`:

```go
func (m *mockStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Update all other mock stores that implement Store interface**

Search for any other mock/test stores in the codebase that implement `storage.Store`. Add `GetSessionAgentStats` stub to each. Key files to check:
- `internal/api/trace_handler_test.go` — `handlerMockStore`
- `internal/api/session_handler_test.go` — `sessionMockStore`
- `internal/api/metrics_handler_test.go` — any mock store
- `internal/api/dashboard_handler_test.go` — any mock store
- `internal/storage/memstore_purge_test.go` — any mock store

For each, add:

```go
func (m *mockType) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 5: Run all Go tests**

Run: `cd D:/opensource/github/labubu && go test -v ./internal/...`

Expected: All tests pass. No compile errors about missing Store interface methods.

- [ ] **Step 6: Commit**

```bash
cd D:/opensource/github/labubu && git add -A && git commit -m "feat: add GetSessionAgentStats stubs and GenAI attribute extraction in buildSpanDetail"
```

---

### Task 4: Add GetAgentStats handler and route

**Files:**
- Modify: `internal/api/session_handler.go` (add handler method)
- Modify: `internal/api/router.go` (add route)

- [ ] **Step 1: Add GetAgentStats handler method to SessionHandler**

Add after existing methods in `session_handler.go`:

```go
// GetAgentStats handles GET /api/v1/sessions/{sessionId}/agent-stats.
func (h *SessionHandler) GetAgentStats(w http.ResponseWriter, r *http.Request, sessionID string) {
	result, err := h.store.GetSessionAgentStats(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get agent stats: %v", err)})
		return
	}
	if result == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_agent_data"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 2: Add agent-stats route in router.go**

In the sessions route handler (`/api/v1/sessions/` dispatch), add a new case for `agent-stats`. Modify the existing session dispatch block (lines 69-78):

```go
		if sessionHandler != nil {
			mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
				path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions")
				if path == "" || path == "/" {
					sessionHandler.ListSessions(w, r)
					return
				}
				sessionID := strings.TrimPrefix(path, "/")
				// Check for sub-path: agent-stats
				parts := strings.SplitN(sessionID, "/", 2)
				if len(parts) == 2 && parts[1] == "agent-stats" {
					sessionHandler.GetAgentStats(w, r, parts[0])
					return
				}
				sessionHandler.GetSession(w, r, sessionID)
			})
			mux.HandleFunc("/api/v1/sessions", sessionHandler.ListSessions)
		}
```

- [ ] **Step 3: Write test for GetAgentStats handler**

Add to `internal/api/session_handler_test.go`:

```go
func TestGetAgentStats(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		stats      *storage.AgentStats
		statsErr   error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "success",
			sessionID:  "sess-1",
			stats: &storage.AgentStats{
				TraceSuccessRate:    0.75,
				AvgToolSuccessRate:  0.92,
				ToolUsage:           []storage.ToolUsageItem{{ToolName: "file_read", CallCount: 10, SuccessRate: 1.0}},
				Insights:            []string{"file_read is reliable"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "no data",
			sessionID:  "sess-2",
			stats:      nil,
			wantStatus: http.StatusNotFound,
			wantBody:   "no_agent_data",
		},
		{
			name:       "store error",
			sessionID:  "sess-3",
			statsErr:   fmt.Errorf("db error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &agentStatsMockStore{
				handlerMockStore: handlerMockStore{},
				agentStats:    tt.stats,
				agentStatsErr: tt.statsErr,
			}
			handler := NewSessionHandler(store)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+tt.sessionID+"/agent-stats", nil)
			rec := httptest.NewRecorder()
			handler.GetAgentStats(rec, req, tt.sessionID)

			if rec.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" {
				var resp map[string]string
				json.Unmarshal(rec.Body.Bytes(), &resp)
				if resp["error"] != tt.wantBody {
					t.Errorf("error: got %v, want %v", resp["error"], tt.wantBody)
				}
			}
			if tt.stats != nil {
				var result storage.AgentStats
				json.Unmarshal(rec.Body.Bytes(), &result)
				if result.TraceSuccessRate != tt.stats.TraceSuccessRate {
					t.Errorf("trace_success_rate: got %v, want %v", result.TraceSuccessRate, tt.stats.TraceSuccessRate)
				}
			}
		})
	}
}

type agentStatsMockStore struct {
	handlerMockStore
	agentStats    *storage.AgentStats
	agentStatsErr error
}

func (m *agentStatsMockStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return m.agentStats, m.agentStatsErr
}
```

- [ ] **Step 4: Add GetSessionAgentStats stub to handlerMockStore**

In `internal/api/trace_handler_test.go`, add to `handlerMockStore`:

```go
func (m *handlerMockStore) GetSessionAgentStats(ctx context.Context, sessionID string) (*storage.AgentStats, error) {
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 5: Run tests**

Run: `cd D:/opensource/github/labubu && go test -v ./internal/api/ -run TestGetAgentStats`

Expected: All 3 test cases pass.

- [ ] **Step 6: Run full test suite**

Run: `cd D:/opensource/github/labubu && go test -v ./internal/...`

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
cd D:/opensource/github/labubu && git add internal/api/session_handler.go internal/api/router.go internal/api/session_handler_test.go internal/api/trace_handler_test.go && git commit -m "feat: add GetAgentStats handler and route with tests"
```

---

### Task 5: Add unit tests for computeAgentStats algorithms

**Files:**
- Create: `internal/storage/agent_stats_test.go`

- [ ] **Step 1: Write test file with loop detection and retry detection tests**

```go
package storage

import (
	"testing"
)

func TestComputeAgentStatsEmpty(t *testing.T) {
	result := computeAgentStats(nil, nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestComputeAgentStatsTraceSuccessRate(t *testing.T) {
	traces := []Trace{
		{StatusCode: 1}, // ok
		{StatusCode: 1}, // ok
		{StatusCode: 2}, // error
		{StatusCode: 1}, // ok
	}
	spans := []Span{}
	result := computeAgentStats(traces, spans)
	if result.TraceSuccessRate != 0.75 {
		t.Errorf("trace_success_rate: got %v, want 0.75", result.TraceSuccessRate)
	}
}

func TestComputeAgentStatsToolSuccessRate(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 200, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 300, StatusCode: 2, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
	}
	result := computeAgentStats(traces, spans)
	if result.TotalToolCalls != 3 {
		t.Errorf("total_tool_calls: got %v, want 3", result.TotalToolCalls)
	}
	if result.SuccessfulToolCalls != 2 {
		t.Errorf("successful_tool_calls: got %v, want 2", result.SuccessfulToolCalls)
	}
	if result.AvgToolSuccessRate != 2.0/3.0 {
		t.Errorf("avg_tool_success_rate: got %v, want 0.667", result.AvgToolSuccessRate)
	}
}

func TestComputeAgentStatsLoopDetection(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 200, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 300, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 400, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
	}
	result := computeAgentStats(traces, spans)
	if result.MaxLoopDepth != 3 {
		t.Errorf("max_loop_depth: got %v, want 3", result.MaxLoopDepth)
	}
	// Check per-tool max loop.
	for _, item := range result.ToolUsage {
		if item.ToolName == "file_read" && item.MaxLoop != 3 {
			t.Errorf("file_read max_loop: got %v, want 3", item.MaxLoop)
		}
	}
}

func TestComputeAgentStatsRetryDetection(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 2, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
		{StartTimeMS: 200, StatusCode: 2, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
		{StartTimeMS: 300, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "web_search"}},
	}
	result := computeAgentStats(traces, spans)
	// web_search: 2 errors followed by 1 ok = retry group with retry count 2
	for _, item := range result.ToolUsage {
		if item.ToolName == "web_search" {
			if item.AvgRetries != 2.0 {
				t.Errorf("web_search avg_retries: got %v, want 2.0", item.AvgRetries)
			}
			if item.CallCount != 3 {
				t.Errorf("web_search call_count: got %v, want 3", item.CallCount)
			}
		}
	}
}

func TestComputeAgentStatsInsights(t *testing.T) {
	traces := []Trace{
		{StatusCode: 2}, // error
		{StatusCode: 2}, // error
		{StatusCode: 2}, // error
	}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 200, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
		{StartTimeMS: 300, StatusCode: 1, Attributes: map[string]string{"gen_ai.tool.name": "file_read"}},
	}
	result := computeAgentStats(traces, spans)
	// Should have: loop insight (file_read max_loop=3), trace failure insight (100% failed)
	if len(result.Insights) < 1 {
		t.Errorf("expected at least 1 insight, got %d", len(result.Insights))
	}
	hasLoopInsight := false
	for _, ins := range result.Insights {
		if ins == "file_read has max loop depth 3 — agent may be stuck in a retry loop" {
			hasLoopInsight = true
		}
	}
	if !hasLoopInsight {
		t.Errorf("expected loop insight for file_read, got: %v", result.Insights)
	}
}

func TestComputeAgentStatsNoGenAISpans(t *testing.T) {
	traces := []Trace{{StatusCode: 1}}
	spans := []Span{
		{StartTimeMS: 100, StatusCode: 1, Attributes: map[string]string{"service.name": "my-svc"}},
	}
	result := computeAgentStats(traces, spans)
	if result.TotalToolCalls != 0 {
		t.Errorf("total_tool_calls: got %v, want 0", result.TotalToolCalls)
	}
	if result.ToolUsage != nil && len(result.ToolUsage) != 0 {
		t.Errorf("tool_usage should be empty")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd D:/opensource/github/labubu && go test -v ./internal/storage/ -run TestComputeAgent`

Expected: All 6 test cases pass.

- [ ] **Step 3: Commit**

```bash
cd D:/opensource/github/labubu && git add internal/storage/agent_stats_test.go && git commit -m "test: add unit tests for computeAgentStats algorithms"
```

---

### Task 6: Add i18n keys for agentStats

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add agentStats keys to en.ts**

Add a new top-level `agentStats` section in `en.ts` (after the `diagnosis` section):

```typescript
  agentStats: {
    agentBehavior: 'Agent Behavior',
    agentStats: 'Agent Behavior Stats',
    toolSuccessRate: 'Tool Success Rate',
    maxLoopDepth: 'Max Loop Depth',
    totalRetries: 'Total Retries',
    tokensUsed: 'Tokens Used',
    toolCallChain: 'Tool Call Chain',
    noToolCalls: 'No tool calls detected in this trace',
    loopWarning: 'Loop Detected: {tool} × {count}',
    loopWarningDesc: 'The agent called {tool} {count} consecutive times. This may indicate a retry loop.',
    traceSuccessRate: 'Trace Success Rate',
    avgToolSuccess: 'Avg Tool Success Rate',
    avgRetries: 'Avg Retries',
    avgSpanPerTrace: 'Avg Span/Trace',
    toolUsage: 'Tool Usage Breakdown',
    insight: 'Insight',
    calls: 'Calls',
    successRate: 'Success Rate',
    avgRetriesCol: 'Avg Retries',
    maxLoop: 'Max Loop',
  },
```

- [ ] **Step 2: Add agentStats keys to zh.ts**

Add matching Chinese section in `zh.ts` (after the `diagnosis` section):

```typescript
  agentStats: {
    agentBehavior: 'Agent 行为',
    agentStats: 'Agent 行为统计',
    toolSuccessRate: '工具成功率',
    maxLoopDepth: '最大循环深度',
    totalRetries: '总重试次数',
    tokensUsed: 'Token 使用量',
    toolCallChain: '工具调用链',
    noToolCalls: '该 trace 未检测到工具调用',
    loopWarning: '检测到循环：{tool} 连续调用 {count} 次',
    loopWarningDesc: 'Agent 连续 {count} 次调用 {tool}，可能陷入重试循环',
    traceSuccessRate: 'Trace 成功率',
    avgToolSuccess: '平均工具成功率',
    avgRetries: '平均重试次数',
    avgSpanPerTrace: '平均 Span/Trace',
    toolUsage: '工具使用分布',
    insight: '洞察',
    calls: '调用次数',
    successRate: '成功率',
    avgRetriesCol: '平均重试',
    maxLoop: '最大循环',
  },
```

- [ ] **Step 3: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 4: Commit**

```bash
cd D:/opensource/github/labubu && git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts && git commit -m "feat: add agentStats i18n keys for en and zh locales"
```

---

### Task 7: Add TypeScript types and API function in client.ts

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add TypeScript interfaces**

Add after existing interfaces (e.g., after `DiagnosisResult`):

```typescript
export interface ToolUsageItem {
  tool_name: string
  call_count: number
  success_rate: number
  avg_retries: number
  max_loop: number
}

export interface AgentStats {
  trace_success_rate: number
  avg_tool_success_rate: number
  avg_retries: number
  avg_loop_depth: number
  max_loop_depth: number
  span_per_trace: number
  total_tool_calls: number
  successful_tool_calls: number
  tool_usage: ToolUsageItem[]
  insights: string[]
}
```

- [ ] **Step 2: Add getAgentStats API function**

Add after existing API functions (e.g., after `diagnoseTrace`):

```typescript
export async function getAgentStats(sessionId: string): Promise<AgentStats> {
  return get<AgentStats>(`${BASE_URL}/sessions/${sessionId}/agent-stats`)
}
```

- [ ] **Step 3: Update SpanDetail interface to include GenAI fields**

Find the `SpanDetail` interface in `client.ts` and add three new fields after `gen_ai_request_model`:

```typescript
export interface SpanDetail {
  // ... existing fields ...
  gen_ai_request_model?: string
  gen_ai_system?: string       // Attributes["gen_ai.system"]
  tool_name?: string           // Attributes["gen_ai.tool.name"]
  is_tool_call: boolean        // tool_name != null
}
```

- [ ] **Step 4: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 5: Commit**

```bash
cd D:/opensource/github/labubu && git add web/src/api/client.ts && git commit -m "feat: add AgentStats, ToolUsageItem interfaces and getAgentStats API function"
```

---

### Task 8: Create AgentBehaviorTab.vue component

**Files:**
- Create: `web/src/components/AgentBehaviorTab.vue`

- [ ] **Step 1: Create component file**

```vue
<template>
  <div class="agent-behavior-tab">
    <!-- Empty state -->
    <div v-if="!hasToolCalls" class="empty-state">
      {{ t('agentStats.noToolCalls') }}
    </div>

    <!-- Content state -->
    <template v-else>
      <!-- Score cards -->
      <div class="score-cards">
        <div class="score-card">
          <div class="score-label">{{ t('agentStats.toolSuccessRate') }}</div>
          <div :class="['score-value', rateClass(toolSuccessRate)]">{{ formatRate(toolSuccessRate) }}</div>
          <div class="score-detail">{{ successfulToolCalls }}/{{ totalToolCalls }} calls succeeded</div>
        </div>
        <div class="score-card">
          <div class="score-label">{{ t('agentStats.maxLoopDepth') }}</div>
          <div :class="['score-value', loopClass(maxLoopDepth)]">{{ maxLoopDepth }}</div>
          <div class="score-detail">{{ loopToolDetail }}</div>
        </div>
        <div class="score-card">
          <div class="score-label">{{ t('agentStats.totalRetries') }}</div>
          <div :class="['score-value', { 'rate-red': totalRetries > 0 }]">{{ totalRetries }}</div>
          <div class="score-detail">{{ retryToolDetail }}</div>
        </div>
        <div class="score-card">
          <div class="score-label">{{ t('agentStats.tokensUsed') }}</div>
          <div class="score-value">{{ formatTokens(totalTokens) }}</div>
          <div class="score-detail">{{ formatTokens(inputTokens) }} in + {{ formatTokens(outputTokens) }} out</div>
        </div>
      </div>

      <!-- Tool call chain -->
      <h3>{{ t('agentStats.toolCallChain') }}</h3>
      <div class="call-chain">
        <div
          v-for="(item, idx) in callChainItems"
          :key="idx"
          :class="['chain-item', { 'chain-error': item.status === 'error', 'chain-loop-group': item.isLoopPart }]"
        >
          <div :class="['chain-icon', item.iconType]">{{ item.icon }}</div>
          <div class="chain-name">
            <span class="chain-tool-name">{{ item.name }}</span>
            <span v-if="item.label" class="chain-attr-label">{{ item.label }}</span>
            <span v-if="item.attemptLabel" class="chain-attempt">{{ item.attemptLabel }}</span>
          </div>
          <div class="chain-meta">{{ item.meta }}</div>
          <div :class="['chain-badge', item.badgeClass]">{{ item.badge }}</div>
        </div>
      </div>

      <!-- Loop warning -->
      <div v-if="maxLoopDepth >= 3" class="loop-warning">
        <span class="warning-icon">⚠️</span>
        <div>
          <div class="warning-title">{{ t('agentStats.loopWarning', { tool: loopToolName, count: maxLoopDepth }) }}</div>
          <div class="warning-desc">{{ t('agentStats.loopWarningDesc', { tool: loopToolName, count: maxLoopDepth }) }}</div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SpanDetail as SpanDetailType } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  spans: SpanDetailType[]
}>()

// Filter spans that are tool calls or LLM calls.
const toolAndLLMSpans = computed(() =>
  props.spans.filter(s => s.is_tool_call || s.gen_ai_system)
)

const hasToolCalls = computed(() =>
  props.spans.some(s => s.is_tool_call)
)

const toolSpans = computed(() =>
  props.spans.filter(s => s.is_tool_call)
)

// --- Trace-level metrics (computed client-side) ---
const totalToolCalls = computed(() => toolSpans.value.length)
const successfulToolCalls = computed(() =>
  toolSpans.value.filter(s => s.status === 'ok').length
)
const toolSuccessRate = computed(() =>
  totalToolCalls.value > 0 ? successfulToolCalls.value / totalToolCalls.value : 0
)

// Loop depth: max consecutive same tool_name spans.
const maxLoopDepth = computed(() => {
  const spans = toolSpans.value
  if (spans.length === 0) return 0
  let maxLoop = 1
  let currentLoop = 1
  for (let i = 1; i < spans.length; i++) {
    if (spans[i].tool_name === spans[i - 1].tool_name) {
      currentLoop++
      if (currentLoop > maxLoop) maxLoop = currentLoop
    } else {
      currentLoop = 1
    }
  }
  return maxLoop
})

const loopToolName = computed(() => {
  // Find which tool has the max loop.
  const spans = toolSpans.value
  if (spans.length === 0) return ''
  let maxLoop = 1, currentLoop = 1, loopTool = spans[0].tool_name || ''
  for (let i = 1; i < spans.length; i++) {
    if (spans[i].tool_name === spans[i - 1].tool_name) {
      currentLoop++
      if (currentLoop > maxLoop) {
        maxLoop = currentLoop
        loopTool = spans[i].tool_name || ''
      }
    } else {
      currentLoop = 1
    }
  }
  return maxLoop >= 3 ? loopTool : ''
})

const loopToolDetail = computed(() => {
  if (maxLoopDepth.value === 0) return 'no tool calls'
  if (loopToolName.value) return `${loopToolName.value} called ${maxLoopDepth.value}× in a row`
  return 'no loops detected'
})

// Retry count: detect error→error→ok patterns per tool.
const totalRetries = computed(() => {
  const spans = toolSpans.value
  let retries = 0
  let consecutiveErrors = 0

  // Group by tool_name, then scan each group.
  const groups: Record<string, SpanDetailType[]> = {}
  for (const s of spans) {
    const name = s.tool_name || ''
    groups[name] = groups[name] || []
    groups[name].push(s)
  }

  for (const group of Object.values(groups)) {
    consecutiveErrors = 0
    for (const s of group) {
      if (s.status === 'error') {
        consecutiveErrors++
      } else if (s.status === 'ok' && consecutiveErrors > 0) {
        retries += consecutiveErrors
        consecutiveErrors = 0
      } else {
        consecutiveErrors = 0
      }
    }
  }
  return retries
})

const retryToolDetail = computed(() => {
  if (totalRetries.value === 0) return 'all calls succeeded first try'
  return `${totalRetries.value} retries across tools`
})

// Token consumption.
const totalTokens = computed(() => {
  let total = 0
  for (const s of props.spans) {
    total += s.total_tokens ?? 0
  }
  return total
})
const inputTokens = computed(() => {
  let total = 0
  for (const s of props.spans) {
    total += s.input_tokens ?? 0
  }
  return total
})
const outputTokens = computed(() => {
  let total = 0
  for (const s of props.spans) {
    total += s.output_tokens ?? 0
  }
  return total
})

// --- Call chain items ---
interface CallChainItem {
  name: string
  icon: string
  iconType: string // 'icon-llm' | 'icon-tool'
  label: string
  status: string
  meta: string
  badge: string
  badgeClass: string
  attemptLabel: string
  isLoopPart: boolean
}

const callChainItems = computed<CallChainItem[]>(() => {
  const items: CallChainItem[] = []
  // Track retry attempts per tool.
  const attemptTracker: Record<string, { consecutiveErrors: number; attemptNumber: number }> = {}

  for (const s of toolAndLLMSpans.value) {
    const toolName = s.tool_name || ''
    const llmSystem = s.gen_ai_system || ''

    if (s.is_tool_call) {
      // Track attempts for this tool.
      if (!attemptTracker[toolName]) {
        attemptTracker[toolName] = { consecutiveErrors: 0, attemptNumber: 0 }
      }
      const tracker = attemptTracker[toolName]
      tracker.attemptNumber++

      let attemptLabel = ''
      if (s.status === 'error') {
        tracker.consecutiveErrors++
        attemptLabel = `❌ attempt ${tracker.attemptNumber}`
      } else if (s.status === 'ok' && tracker.consecutiveErrors > 0) {
        const totalAttempts = tracker.attemptNumber
        attemptLabel = `✅ attempt ${totalAttempts}/${totalAttempts}`
        tracker.consecutiveErrors = 0
      } else {
        tracker.consecutiveErrors = 0
      }

      // Check if this span is part of a loop (same tool_name as previous tool span).
      const prevToolItems = items.filter(i => i.iconType === 'icon-tool')
      const isLoop = prevToolItems.length > 0 && prevToolItems[prevToolItems.length - 1].name === toolName

      items.push({
        name: toolName,
        icon: '🔧',
        iconType: 'icon-tool',
        label: 'gen_ai.tool.name',
        status: s.status,
        meta: s.duration_ms ? `${s.duration_ms}ms` : '',
        badge: s.status === 'ok' ? 'OK' : 'ERROR',
        badgeClass: s.status === 'ok' ? 'badge-ok' : 'badge-error',
        attemptLabel,
        isLoopPart: isLoop,
      })
    } else if (llmSystem) {
      const tokenMeta = s.total_tokens ? `${formatTokens(s.total_tokens)} tokens` : ''
      items.push({
        name: llmSystem,
        icon: '🤖',
        iconType: 'icon-llm',
        label: 'gen_ai.system',
        status: s.status,
        meta: tokenMeta,
        badge: s.status === 'ok' ? 'OK' : 'ERROR',
        badgeClass: s.status === 'ok' ? 'badge-ok' : 'badge-error',
        attemptLabel: '',
        isLoopPart: false,
      })
    }
  }
  return items
})

// --- Formatting helpers ---
function rateClass(rate: number): string {
  if (rate >= 0.9) return 'rate-green'
  if (rate >= 0.7) return 'rate-yellow'
  return 'rate-red'
}

function loopClass(depth: number): string {
  if (depth >= 5) return 'rate-red'
  if (depth >= 3) return 'rate-yellow'
  return 'rate-green'
}

function formatRate(rate: number): string {
  return `${Math.round(rate * 100)}%`
}

function formatTokens(tokens: number): string {
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}
</script>

<style scoped>
.agent-behavior-tab { padding: 0; }

.score-cards {
  display: flex;
  gap: 16px;
  margin-bottom: 20px;
}
.score-card {
  flex: 1;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}
.score-label {
  font-size: 12px;
  color: var(--text-secondary);
  text-transform: uppercase;
  margin-bottom: 4px;
}
.score-value {
  font-size: 32px;
  font-weight: 700;
  color: var(--text-primary);
}
.score-detail {
  font-size: 11px;
  color: var(--text-secondary);
}

.rate-green { color: #22c55e; }
.rate-yellow { color: #f59e0b; }
.rate-red { color: #ef4444; }

.empty-state {
  text-align: center;
  padding: 40px;
  color: var(--text-secondary);
}

.call-chain {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 16px;
}
.chain-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border-primary);
}
.chain-item:last-child { border-bottom: none; }
.chain-error { background: rgba(239, 68, 68, 0.05); }
.chain-loop-group { border-left: 3px solid #f59e0b; padding-left: 4px; }

.chain-icon {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
}
.icon-llm { background: #3b82f6; color: white; }
.icon-tool { background: #f59e0b; color: white; }

.chain-name { flex: 1; min-width: 0; }
.chain-tool-name { font-weight: 600; }
.chain-attr-label { color: var(--text-secondary); font-size: 12px; margin-left: 8px; }
.chain-attempt { color: var(--text-secondary); font-size: 12px; margin-left: 4px; }

.chain-meta { font-size: 12px; color: var(--text-secondary); white-space: nowrap; }
.chain-badge {
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}
.badge-ok { background: #22c55e; color: white; }
.badge-error { background: #ef4444; color: white; }

.loop-warning {
  margin-top: 16px;
  background: rgba(245, 158, 11, 0.1);
  border: 1px solid rgba(245, 158, 11, 0.3);
  border-radius: 8px;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.warning-icon { font-size: 20px; }
.warning-title { font-weight: 600; color: #f59e0b; }
.warning-desc { font-size: 13px; color: var(--text-secondary); }
</style>
```

- [ ] **Step 2: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 3: Commit**

```bash
cd D:/opensource/github/labubu && git add web/src/components/AgentBehaviorTab.vue && git commit -m "feat: create AgentBehaviorTab.vue component with score cards, call chain, loop warning"
```

---

### Task 9: Create AgentStatsSection.vue component

**Files:**
- Create: `web/src/components/AgentStatsSection.vue`

- [ ] **Step 1: Create component file**

```vue
<template>
  <div class="agent-stats-section" v-if="stats">
    <h3 class="section-title">
      🤖 {{ t('agentStats.agentStats') }}
      <span class="section-subtitle">— Is this agent reliable?</span>
    </h3>

    <!-- Overview cards -->
    <div class="overview-cards">
      <div class="overview-card">
        <div class="card-label">{{ t('agentStats.traceSuccessRate') }}</div>
        <div :class="['card-value', rateClass(stats.trace_success_rate)]">{{ formatRate(stats.trace_success_rate) }}</div>
      </div>
      <div class="overview-card">
        <div class="card-label">{{ t('agentStats.avgToolSuccess') }}</div>
        <div :class="['card-value', rateClass(stats.avg_tool_success_rate)]">{{ formatRate(stats.avg_tool_success_rate) }}</div>
      </div>
      <div class="overview-card">
        <div class="card-label">{{ t('agentStats.avgRetries') }}</div>
        <div :class="['card-value', { 'rate-red': stats.avg_retries > 1 }]">{{ stats.avg_retries.toFixed(1) }}</div>
      </div>
      <div class="overview-card">
        <div class="card-label">{{ t('agentStats.avgSpanPerTrace') }}</div>
        <div class="card-value">{{ stats.span_per_trace.toFixed(1) }}</div>
      </div>
    </div>

    <!-- Tool usage table -->
    <h4>{{ t('agentStats.toolUsage') }}</h4>
    <table class="tool-table">
      <thead>
        <tr>
          <th>{{ t('agentStats.toolCallChain') === 'Tool Call Chain' ? 'Tool' : '工具' }}</th>
          <th>{{ t('agentStats.calls') }}</th>
          <th>{{ t('agentStats.successRate') }}</th>
          <th>{{ t('agentStats.avgRetriesCol') }}</th>
          <th>{{ t('agentStats.maxLoop') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in stats.tool_usage" :key="item.tool_name">
          <td class="tool-name-cell">{{ item.tool_name }}</td>
          <td class="numeric-cell">{{ item.call_count }}</td>
          <td :class="['numeric-cell', rateCellClass(item.success_rate)]">{{ formatRate(item.success_rate) }}</td>
          <td class="numeric-cell">{{ item.avg_retries.toFixed(1) }}</td>
          <td class="numeric-cell">
            {{ item.max_loop }}
            <span v-if="item.max_loop >= 3" class="loop-warning-emoji">⚠️</span>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Insights -->
    <div v-if="stats.insights.length > 0" class="insight-card">
      <div class="insight-icon">💡</div>
      <div class="insight-content">
        <strong>{{ t('agentStats.insight') }}:</strong>
        <ul>
          <li v-for="insight in stats.insights" :key="insight">{{ insight }}</li>
        </ul>
      </div>
    </div>
  </div>

  <!-- Loading state -->
  <div v-else-if="loading" class="loading-state">{{ t('common.loading') }}</div>

  <!-- Error state -->
  <div v-else-if="error" class="error-state">{{ error }}</div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { AgentStats } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  stats: AgentStats | null
  loading: boolean
  error: string
}>()

function rateClass(rate: number): string {
  if (rate >= 0.9) return 'rate-green'
  if (rate >= 0.7) return 'rate-yellow'
  return 'rate-red'
}

function rateCellClass(rate: number): string {
  if (rate >= 0.9) return 'cell-green'
  if (rate >= 0.7) return 'cell-yellow'
  return 'cell-red'
}

function formatRate(rate: number): string {
  return `${Math.round(rate * 100)}%`
}
</script>

<style scoped>
.agent-stats-section { margin-bottom: 24px; }

.section-title { margin-bottom: 16px; font-size: 18px; }
.section-subtitle { font-size: 12px; color: var(--text-secondary); font-weight: 400; }

.overview-cards {
  display: flex;
  gap: 16px;
  margin-bottom: 20px;
}
.overview-card {
  flex: 1;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}
.card-label { font-size: 12px; color: var(--text-secondary); text-transform: uppercase; margin-bottom: 4px; }
.card-value { font-size: 32px; font-weight: 700; }

.rate-green { color: #22c55e; }
.rate-yellow { color: #f59e0b; }
.rate-red { color: #ef4444; }

.tool-table {
  width: 100%;
  border-collapse: collapse;
  background: var(--bg-secondary);
  border-radius: 8px;
  margin-bottom: 16px;
}
.tool-table th {
  padding: 10px 16px;
  text-align: left;
  font-size: 12px;
  color: var(--text-secondary);
  border-bottom: 2px solid var(--border-primary);
}
.tool-table td {
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-primary);
}
.tool-name-cell { font-weight: 600; }
.numeric-cell { text-align: center; }
.cell-green { color: #22c55e; font-weight: 600; }
.cell-yellow { color: #f59e0b; font-weight: 600; }
.cell-red { color: #ef4444; font-weight: 600; }
.loop-warning-emoji { font-size: 12px; }

.insight-card {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 12px 16px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.insight-icon { font-size: 14px; }
.insight-content { font-size: 13px; color: var(--text-secondary); }
.insight-content ul { margin: 4px 0 0 0; padding-left: 16px; }
.insight-content li { margin-bottom: 4px; }

.loading-state, .error-state { text-align: center; padding: 24px; color: var(--text-secondary); }
.error-state { color: #ef4444; }
</style>
```

- [ ] **Step 2: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 3: Commit**

```bash
cd D:/opensource/github/labubu && git add web/src/components/AgentStatsSection.vue && git commit -m "feat: create AgentStatsSection.vue component with overview cards, tool table, insights"
```

---

### Task 10: Integrate AgentBehaviorTab into TraceDetail.vue

**Files:**
- Modify: `web/src/views/TraceDetail.vue`

- [ ] **Step 1: Import AgentBehaviorTab**

Add import after the DiagnosisTab import (line 179):

```typescript
import AgentBehaviorTab from '../components/AgentBehaviorTab.vue'
```

- [ ] **Step 2: Extend activeTab type**

Change line 197 from:

```typescript
const activeTab = ref<'spans' | 'logs' | 'diagnosis'>('spans')
```

to:

```typescript
const activeTab = ref<'spans' | 'logs' | 'diagnosis' | 'agent'>('spans')
```

- [ ] **Step 3: Add "Agent Behavior" tab button**

Add after the Diagnosis tab button (line 71):

```html
<button :class="['tab-btn', { active: activeTab === 'agent' }]" @click="switchTab('agent')">
  {{ t('agentStats.agentBehavior') }}
</button>
```

- [ ] **Step 4: Add Agent Behavior tab content**

Add after the Diagnosis tab content block (after line 110):

```html
          <div v-if="activeTab === 'agent'" class="agent-behavior-panel">
            <AgentBehaviorTab :spans="trace.spans" />
          </div>
```

- [ ] **Step 5: Update switchTab function signature**

Change line 412 from:

```typescript
function switchTab(tab: 'spans' | 'logs' | 'diagnosis') {
```

to:

```typescript
function switchTab(tab: 'spans' | 'logs' | 'diagnosis' | 'agent') {
```

- [ ] **Step 6: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 7: Commit**

```bash
cd D:/opensource/github/labubu && git add web/src/views/TraceDetail.vue && git commit -m "feat: integrate AgentBehaviorTab into TraceDetail as new tab"
```

---

### Task 11: Integrate AgentStatsSection into SessionDetail.vue

**Files:**
- Modify: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Import AgentStatsSection and getAgentStats**

Add to imports:

```typescript
import AgentStatsSection from '../components/AgentStatsSection.vue'
import { getAgentStats, type AgentStats } from '../api/client'
```

- [ ] **Step 2: Add state refs**

Add after existing state refs:

```typescript
const agentStats = ref<AgentStats | null>(null)
const agentStatsLoading = ref(false)
const agentStatsError = ref('')
```

- [ ] **Step 3: Add fetchAgentStats function**

Add after `fetchSession` function:

```typescript
async function fetchAgentStats() {
  if (!route.params.sessionId) return
  agentStatsLoading.value = true
  agentStatsError.value = ''
  try {
    agentStats.value = await getAgentStats(route.params.sessionId as string)
  } catch (e: any) {
    if (e.message === 'no_agent_data') {
      agentStats.value = null
    } else {
      agentStatsError.value = e.message || 'Failed to load agent stats'
    }
  } finally {
    agentStatsLoading.value = false
  }
}
```

- [ ] **Step 4: Call fetchAgentStats on mount**

Add `fetchAgentStats()` call inside `onMounted()`, after existing fetch calls:

```typescript
onMounted(() => {
  fetchSession()
  fetchAgentStats()
})
```

- [ ] **Step 5: Add AgentStatsSection in template**

Add after the session summary section (the `<div class="summary-grid">` block) and before the trace list:

```html
    <AgentStatsSection
      :stats="agentStats"
      :loading="agentStatsLoading"
      :error="agentStatsError"
    />
```

- [ ] **Step 6: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 7: Commit**

```bash
cd D:/opensource/github/labubu && git add web/src/views/SessionDetail.vue && git commit -m "feat: integrate AgentStatsSection into SessionDetail page"
```

---

### Task 12: Run full test suite and build check

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

Run: `cd D:/opensource/github/labubu && go test -v ./internal/...`

Expected: All tests pass, including new `TestGetAgentStats` and `TestComputeAgentStats*` tests.

- [ ] **Step 2: Run TypeScript check**

Run: `cd D:/opensource/github/labubu/web && npx vue-tsc --noEmit`

Expected: No type errors.

- [ ] **Step 3: Run build check**

Run: `cd D:/opensource/github/labubu && make build-nocgo`

Expected: Build succeeds.

- [ ] **Step 4: Final commit (if any fixes needed)**

If any fixes were needed during verification, commit them:

```bash
cd D:/opensource/github/labubu && git add -A && git commit -m "fix: address issues found during full test suite verification"
```

- [ ] **Step 5: Push to develop**

```bash
cd D:/opensource/github/labubu && git push origin develop
```
