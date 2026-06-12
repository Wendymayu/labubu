# Labubu Feature Roadmap Design — Agent Observability Gap Analysis

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Define a comprehensive feature roadmap for Labubu to close the observability gap with Langfuse, Arize Phoenix, and Datadog, while maintaining local-first single-developer differentiation.

**Architecture:** Tiered feature roadmap organized by impact and local-first fit. Tier 1 features (evaluation, annotations, agent stats) are highest priority and naturally fit local deployment. Tier 2 (playground, trace diff, live tail) build on Tier 1 foundations. Tier 3 (guardrails, prompt versioning) are extensions.

**Tech Stack:** Go 1.19 backend + Vue 3 TypeScript frontend + existing Store interface + LLMConfig for evaluation calls + WebSocket for live tail + JSON file storage for annotations/prompts

---

## Competitive Landscape Summary

### Feature Gap Matrix

| Feature | Langfuse | Arize Phoenix | Datadog | Labubu Now |
|----------|----------|---------------|---------|-------------|
| Trace Visualization | ✅ Tree/waterfall | ✅ Waterfall + color-coded | ✅ Flame graph + LLM span types | ✅ Waterfall + span detail |
| Session Tracking | ✅ User-level attribution | ✅ Basic | ✅ Session-level traces | ✅ Session list + detail |
| **Quality Evaluation** | ✅ LLM-as-judge + custom scorer + human annotation | ✅ Hallucination/relevance/toxicity | ✅ Built-in + custom assessments | ❌ None |
| **Prompt Management** | ✅ Versioning + Playground + SDK deployment | ✅ Version iteration | ❌ None independent | ⚠️ LLM config only, no prompt |
| **Feedback/Annotation** | ✅ Thumbs + rating + comments | ✅ Thumbs + labels + golden dataset | ✅ Human annotation workflows | ❌ None |
| Embeddings/Drift | ❌ | ✅ 3D UMAP + HDBSCAN | ❌ | ❌ |
| Guardrails/Safety | ❌ | ❌ | ✅ Toxicity/PII/factual detection | ❌ |
| Cost Tracking | ✅ Model + user + feature | ✅ Token + basic dashboard | ✅ Budget alerts + team attribution | ✅ Cost dashboard completed |
| Latency Monitoring | ✅ P50/P90/P99 | ✅ Sort + filter | ✅ Watchdog anomaly detection | ✅ Visible via spans |
| **Playground** | ✅ Multi-model comparison | ✅ A/B testing | ❌ | ❌ |
| Alerts | ✅ Webhook + threshold | ✅ Basic (Cloud) | ✅ Full APM alerts | ✅ Completed |
| Agent Success Rate | ✅ Via evaluation | ✅ Reasoning step evaluation | ✅ Tool call tracing + retry tracking | 📋 Planned (#18) |
| Trace Comparison | ❌ | ❌ | ✅ Span links cross-trace | 📋 Planned (#19) |
| Live Tail | ❌ | ❌ | ✅ Live Search | 📋 Planned (#20) |

**Key gaps:** Quality Evaluation, Prompt Playground, Feedback/Annotation are completely missing.

---

## Tier 1 — High Impact, Naturally Fits Local-First

### Feature 1: LLM-as-judge Automatic Evaluation

**Description:** Extend the existing DiagnosisResult infrastructure to provide LLM-powered automatic evaluation of trace quality across four dimensions.

**Evaluation Dimensions:**

| Dimension | Meaning | Score Range |
|-----------|---------|-------------|
| Latency | Is response latency reasonable? | 0-100 |
| Cost | Is token consumption efficient? | 0-100 |
| Error | Are there error/failure spans? | 0-100 |
| Accuracy | Is output content correct/useful? | 0-100 |
| Efficiency | Are tool calls minimal without redundancy? | 0-100 |

Note: Error dimension is already implemented in DiagnosisResult. Accuracy is the new addition for output quality assessment.

**Implementation Approach:**
- Reuse existing DiagnoseTrace handler, DiagnosisResult storage, and LLMConfig infrastructure
- Extend the evaluation prompt to include span output content assessment (Accuracy as 5th dimension alongside existing Latency/Cost/Error/Efficiency)
- Frontend: Add Diagnosis/Evaluation panel in TraceDetail page showing overall score + dimension breakdown + findings list + start/re-evaluate buttons

**Data Flow:**
```
User clicks "Start Evaluation"
  → POST /api/v1/traces/{id}/diagnose
  → Backend fetches trace spans + constructs evaluation prompt
  → Calls default LLM model (existing LLMConfig)
  → Parses LLM JSON response with scores + findings
  → Stores DiagnosisResult (existing UpsertDiagnosisResult)
  → GET /api/v1/traces/{id}/diagnosis returns cached result
```

**New vs. Existing:**
- Backend: Already fully implemented (DiagnoseTrace, GetDiagnosis, DiagnosisResult)
- Frontend: Diagnosis tab/panel not yet built — this is the main new work
- Prompt: Enhance evaluation prompt to include output accuracy assessment

**Files:**
- Create: `web/src/views/TraceDetail.vue` — add diagnosis panel section (modify existing)
- Create: `web/src/components/DiagnosisPanel.vue` — new component for evaluation display
- Modify: `web/src/api/client.ts` — add diagnosis API calls
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — add diagnosis i18n keys (already partially present)

---

### Feature 2: Simple Annotation/Feedback

**Description:** Lightweight single-user annotation system for traces and spans. Thumbs up/down, 1-5 star rating, free-text comments. No team collaboration workflows.

**Annotation Types:**

| Type | Description | Level |
|------|-------------|-------|
| 👍/👎 Thumbs | Quick quality indicator | Span-level |
| ⭐ Rating | 1-5 star overall quality | Trace-level |
| 💬 Comment | Free-text debugging notes | Span-level |

**Data Model:**
```go
type SpanAnnotation struct {
    SpanID    [8]byte   `json:"span_id"`
    TraceID   [16]byte  `json:"trace_id"`
    Thumbs    *bool     `json:"thumbs"`       // true=up, false=down, nil=unset
    Rating    *int      `json:"rating"`        // 1-5, nil=unset
    Comment   string    `json:"comment"`       // free text
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

**Storage:** JSON file `~/.labubu/annotations.json` — appropriate for local-first single-user scenario with small data volume. Not through Store interface — annotations are UI-layer metadata, not trace data.

**API:**
```
POST   /api/v1/annotations          — Create or update annotation
GET    /api/v1/annotations?trace_id= — Get all annotations for a trace
DELETE /api/v1/annotations/{id}      — Delete annotation
```

**UI Location:**
- Span Detail Drawer: Add annotation area at bottom (thumbs buttons, star rating, comment input)
- Trace Detail page header: Add trace-level star rating display

**Files:**
- Create: `internal/api/annotation_handler.go` — AnnotationHandler with JSON file storage
- Create: `internal/api/annotation_handler_test.go` — tests
- Create: `web/src/components/AnnotationPanel.vue` — annotation UI component
- Modify: `web/src/components/SpanDetail.vue` — embed AnnotationPanel
- Modify: `web/src/views/TraceDetail.vue` — add trace-level rating
- Modify: `web/src/api/client.ts` — annotation API types and functions
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — annotation i18n keys
- Modify: `internal/api/router.go` — register annotation routes
- Modify: `cmd/labubu/main.go` — wire annotation handler

---

### Feature 3: Agent Success Rate Analysis (Roadmap #18)

**Description:** Agent execution stability metrics computed from existing span data — tool call success rate, retry count, loop depth, error rate.

**Core Metrics:**

| Metric | Calculation | Display Location |
|--------|-------------|------------------|
| Tool call success rate | OK tool spans / total tool spans | Session detail |
| Retry count | Same-name tool span repeated appearance count | Trace detail |
| Loop depth | Agent cycle count within a trace | Trace detail |
| Error rate | ERROR status spans / total spans | Session detail (already exists) |
| Avg completion turns | Span count per trace | Session detail |

**Data Source:** Calculated from existing Span data. Span `Kind` distinguishes span types, span `Attributes` identifies tool calls via `gen_ai.tool.name`. No new storage needed — aggregation computation only.

**API:**
```
GET /api/v1/sessions/{id}/agent-stats
```

**Response Structure:**
```go
type AgentStats struct {
    ToolCallSuccessRate  float64        `json:"tool_call_success_rate"`
    AvgToolCallsPerTrace float64        `json:"avg_tool_calls_per_trace"`
    RetryCount           int            `json:"retry_count"`
    MaxLoopDepth         int            `json:"max_loop_depth"`
    AvgSpanPerTrace      float64        `json:"avg_span_per_trace"`
    ToolBreakdown        []ToolStatItem `json:"tool_breakdown"`
}
type ToolStatItem struct {
    ToolName   string `json:"tool_name"`
    CallCount  int    `json:"call_count"`
    ErrorCount int    `json:"error_count"`
    AvgDurMS   uint64 `json:"avg_duration_ms"`
}
```

**UI Location:**
- Session detail page: New Agent Stats card section
- Trace detail page: Span attributes show retry/loop indicators (no new page needed)

**Files:**
- Create: `internal/api/agent_stats_handler.go` — AgentStatsHandler
- Create: `internal/api/agent_stats_handler_test.go` — tests
- Modify: `internal/storage/storage.go` — add AgentStats types
- Modify: `internal/storage/memstore.go` — implement AgentStats computation
- Modify: `internal/api/router.go` — register agent-stats route
- Create: `web/src/components/AgentStatsCard.vue` — stats display component
- Modify: `web/src/views/SessionDetail.vue` — add AgentStats section
- Modify: `web/src/api/client.ts` — agent stats API
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — agent stats i18n keys

---

## Tier 2 — High Impact, Builds on Tier 1 Foundations

### Feature 4: Prompt Playground

**Description:** Interactive playground for testing prompts against configured LLM models with parameter adjustment and output comparison. Each playground call auto-generates a trace for later review.

**Core Features:**

| Feature | Description |
|---------|-------------|
| Prompt input | Multi-line text input with template variable `{{var}}` support |
| Model selection | Dropdown from configured LLMConfig list |
| Parameter adjustment | Temperature and Max Tokens sliders |
| Send & compare | One-click send, results panel shows completion |
| Side-by-side comparison | Two results displayed left/right for comparison |
| Auto trace recording | Each playground call generates a trace for later review |

**Data Flow:**
```
User enters prompt + selects model
  → POST /api/v1/playground/run
  → Backend uses specified LLMConfig to call model API
  → Returns completion + token usage + latency
  → Async writes a Playground trace (reviewable later)
```

**API:**
```
POST /api/v1/playground/run
Body: { model_id, prompt, variables, temperature, max_tokens }
Response: { completion, input_tokens, output_tokens, duration_ms, trace_id_hex }

GET /api/v1/playground/history
Response: [ { id, prompt, model, created_at, trace_id_hex } ]
```

**UI — New standalone page:**
```
Playground page
├── Left panel
│   ├── Model selection dropdown
│   ├── Prompt input (multi-line)
│   ├── Temperature slider
│   ├── Max Tokens input
│   └── "Send" button
├── Right panel
│   ├── Completion output
│   ├── Token usage + latency stats
│   ├── "View Trace" link
│   └── History list (last 20 calls)
```

**Navigation:** New top-level nav item or under Settings nav-group.

**Files:**
- Create: `internal/api/playground_handler.go` — PlaygroundHandler
- Create: `internal/api/playground_handler_test.go` — tests
- Create: `web/src/views/Playground.vue` — playground page
- Modify: `web/src/api/client.ts` — playground API types and functions
- Modify: `web/src/router.ts` — add /playground route
- Modify: `web/src/App.vue` — add playground nav link
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — playground i18n keys
- Modify: `internal/api/router.go` — register playground routes
- Modify: `cmd/labubu/main.go` — wire playground handler

---

### Feature 5: Trace Comparison Diff (Roadmap #19)

**Description:** Select two traces and compare side-by-side — span tree, token usage, tool call path differences highlighted.

**Core Features:**

| Feature | Description |
|---------|-------------|
| Side-by-side waterfall | Two span trees aligned left-right |
| Difference highlighting | New spans green, missing red, changed yellow |
| Token comparison | Left-right token usage numbers compared |
| Tool path comparison | Left-right tool call sequence compared |
| Selection from Trace List | Select two traces → click "Compare" |

**API:** No new API needed — reuse `GET /api/v1/traces/{id}` for both traces, frontend computes diff.

**UI:**
```
Trace Diff page (/traces/compare?a={id}&b={id})
├── Left Trace A Waterfall
├── Right Trace B Waterfall
├── Diff summary cards (token diff, span count diff, duration diff)
├── Tool call path comparison list
```

**Files:**
- Create: `web/src/views/TraceDiff.vue` — diff page
- Create: `web/src/components/DiffSummary.vue` — diff summary cards
- Modify: `web/src/views/TraceList.vue` — add multi-select + compare button
- Modify: `web/src/router.ts` — add /traces/compare route
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — diff i18n keys

---

### Feature 6: Live Tail Real-time Stream (Roadmap #20)

**Description:** WebSocket-based real-time trace stream. New traces appear at the top of a live list with auto-scroll, configurable filters.

**Core Features:**

| Feature | Description |
|---------|-------------|
| WebSocket connection | `ws://localhost:8080/api/v1/traces/stream` |
| Real-time trace cards | New trace inserted at list top on arrival |
| Auto-scroll | New content auto-scrolls to top, pauseable |
| Filter | Real-time filter by service/status |
| Click to navigate | Click card to jump to trace detail |

**Backend Implementation:**
```
pipeline.go InsertSpans success
  → Broadcast new trace ID to WebSocket hub
  → Hub maintains connection list, pushes JSON messages
```

New WebSocket hub component integrated with existing pipeline.

**API:**
```
WebSocket: /api/v1/traces/stream
Message format: { "type": "new_trace", "trace_id_hex": "...", "root_name": "...", "duration_ms": 123, "status": "OK" }
```

**Files:**
- Create: `internal/api/ws_hub.go` — WebSocket hub + handler
- Create: `web/src/views/LiveTail.vue` — live tail page
- Create: `web/src/composables/useWebSocket.ts` — WebSocket composable
- Modify: `internal/pipeline/pipeline.go` — broadcast on insert
- Modify: `internal/api/router.go` — register WebSocket route
- Modify: `web/src/router.ts` — add /live route
- Modify: `web/src/App.vue` — add live nav link
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — live tail i18n keys

---

## Tier 3 — Medium Impact, Extension Features

### Feature 7: Guardrails Detection

**Description:** Safety detection as an extension of the evaluation system — toxicity marking, PII leakage detection, factual correctness checking. Implemented as additional dimensions in the diagnosis prompt, not a standalone feature.

**Detection Types:**

| Type | Description | Implementation |
|------|-------------|---------------|
| Toxicity marking | Span output contains harmful content | LLM-as-judge extension dimension |
| PII leakage | Output contains personal info (email, phone) | Regex + LLM dual detection |
| Factual correctness | Output contradicts known facts | LLM-as-judge extension dimension |

**Implementation:** Not standalone — extend the DiagnosisResult evaluation prompt to include toxicity/PII/factual dimensions. LLM returns these scores alongside existing latency/cost/accuracy/efficiency scores.

**UI:** Add Guardrails section in diagnosis panel showing detection results.

**Files:**
- Modify: `internal/storage/storage.go` — extend DiagnosisScores with Accuracy dimension (5th dimension)
- Modify: `internal/api/trace_handler.go` — extend diagnosis prompt to include output accuracy
- Modify: `web/src/components/DiagnosisPanel.vue` — add guardrails display section
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — guardrails i18n keys

---

### Feature 8: Prompt Version Management

**Description:** Extension of Playground — save tested prompts as versions with history, comparison, and trace association.

**Core Features:**

| Feature | Description |
|---------|-------------|
| Save prompt version | From Playground, save current prompt as v1/v2/v3... |
| Version list | View all historical versions of a prompt |
| Version comparison | Diff between two versions |
| Trace association | Each version records associated trace IDs |

**Storage:** JSON file `~/.labubu/prompts.json`, consistent with annotation storage approach (local-first simplified storage).

**API:**
```
POST   /api/v1/prompts          — Create prompt version
GET    /api/v1/prompts           — List all prompts
GET    /api/v1/prompts/{id}      — Get prompt version detail
PUT    /api/v1/prompts/{id}      — Update prompt version
DELETE /api/v1/prompts/{id}      — Delete prompt version
```

**Files:**
- Create: `internal/api/prompt_handler.go` — PromptHandler with JSON file storage
- Create: `internal/api/prompt_handler_test.go` — tests
- Modify: `web/src/views/Playground.vue` — add "Save as version" button + version list
- Modify: `web/src/api/client.ts` — prompt API types and functions
- Modify: `web/src/i18n/locales/en.ts` and `zh.ts` — prompt versioning i18n keys
- Modify: `internal/api/router.go` — register prompt routes
- Modify: `cmd/labubu/main.go` — wire prompt handler

---

## Excluded Features (Not Suitable for Local-First Single-User)

| Feature | Reason |
|---------|--------|
| Embeddings drift detection | Requires UMAP/HDBSCAN computation and large-scale datasets; low value for single-user local-first scenario |
| Team annotation workflows | Requires user management, role permissions, annotation campaigns; not suitable for single-user scenario |
| Budget alerts / team attribution | Requires org/team structure; not suitable for single-user scenario |
| Multi-user permission management | Local-first scenario has no demand for this |