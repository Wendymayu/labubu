# LLM-Powered Trace Diagnosis Design Spec

> **Goal:** Allow users to trigger LLM-based comprehensive quality evaluation on individual traces, with structured scoring across latency/cost/error/efficiency dimensions, results persisted for reuse.

> **Status:** ✅ Approved (2026-06-12)

---

## Architecture

```
User clicks "诊断" on TraceDetail
  → POST /api/v1/traces/{id}/diagnose
    → Backend: Load trace (spans + logs) from store
    → Backend: Build prompt (summary + full data + JSON schema)
    → Backend: Call LLM via stored llm_config (is_default=true)
    → Backend: Parse structured JSON response
    → Backend: Upsert into diagnosis_results table
    → Return result to frontend
  → Frontend: Render scoring cards + findings list in "诊断" tab
  → Click finding → jump to relevant span in waterfall
```

**Key principle:** LLM config infrastructure already exists (`llm_configs` table + CRUD API) but has never been used to call an LLM. This is the first consumer. API key stays server-side — never exposed to frontend.

---

## Data Model

### New table: `diagnosis_results`

```sql
CREATE TABLE IF NOT EXISTS diagnosis_results (
    trace_id        FixedString(16),
    model_name      String,
    scores          String,       -- JSON: {"latency":85,"cost":62,"error":45,"efficiency":88}
    overall_score   UInt8,        -- 0-100
    findings        String,       -- JSON array of finding objects
    summary         String,       -- One-line natural language summary
    spans_snapshot  String,       -- Span fingerprint for staleness detection
    raw_response    String,       -- Full LLM response for debugging
    created_at      DateTime64(3),
    PRIMARY KEY (trace_id)
)
```

### Go types (`internal/storage/storage.go`)

```go
type DiagnosisFinding struct {
    Severity    string `json:"severity"`    // "error" | "warning" | "info"
    Dimension   string `json:"dimension"`   // "latency" | "cost" | "error" | "efficiency"
    Title       string `json:"title"`
    Description string `json:"description"`
    Suggestion  string `json:"suggestion"`
    SpanName    string `json:"span_name,omitempty"` // optional: related span
    SpanIndex   int    `json:"span_index,omitempty"` // optional: span position in waterfall
}

type DiagnosisScores struct {
    Latency     int `json:"latency"`
    Cost        int `json:"cost"`
    Error       int `json:"error"`
    Efficiency  int `json:"efficiency"`
}

type DiagnosisResult struct {
    TraceID       [16]byte          `json:"trace_id"`
    TraceIDHex    string            `json:"trace_id_hex"`
    ModelName     string            `json:"model_name"`
    Scores        DiagnosisScores   `json:"scores"`
    OverallScore  uint8             `json:"overall_score"`
    Findings      []DiagnosisFinding `json:"findings"`
    Summary       string            `json:"summary"`
    SpansSnapshot string            `json:"-"`
    RawResponse   string            `json:"-"`
    CreatedAt     time.Time         `json:"created_at"`
    Stale         bool              `json:"stale"`
}
```

### Store interface additions

```go
GetDiagnosisResult(ctx context.Context, traceID [16]byte) (*DiagnosisResult, error)
UpsertDiagnosisResult(ctx context.Context, result *DiagnosisResult) error
```

### Staleness detection

`spans_snapshot` stores a deterministic fingerprint of the trace at diagnosis time:

```
{span_count}|{sorted "spanID:status:duration_ms" joined by comma}
```

Example: `12|span1:OK:2300,span2:OK:500,span3:OK:1200,span4:ERROR:4100,...`

On every GET/POST, the backend recomputes the snapshot from current trace data and compares against the stored value. If they differ, `stale: true` — frontend shows "Trace 数据已更新，建议重新诊断" banner.

---

## API Design

### `GET /api/v1/traces/{traceIdHex}/diagnosis`

Retrieve existing diagnosis result without triggering a new one. Returns the stored result if it exists and is not stale.

**Response (200):** Same structure as POST response. `stale: true` if trace data has changed since diagnosis.

**Response (404):** `{"error":"no_diagnosis"}` — no diagnosis exists for this trace.

### `POST /api/v1/traces/{traceIdHex}/diagnose`

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `force` | query bool | `false` | Skip cached result, re-diagnose |

**Process flow:**

1. Parse `traceIdHex` → `[16]byte`
2. If `!force` and stored result exists with matching snapshot → return cached (200)
3. Load default `llm_config` (`is_default=true`). None → return 400 `{"error":"no_default_model"}`
4. Load full trace data (spans + logs) from store
5. Build prompt:
   - **System prompt:** Evaluation rules + required output JSON schema
   - **User prompt:** Trace summary + full span list + associated logs
   - If estimated tokens > 80K, trim: keep all ERROR spans, top-5 slowest, all LLM spans. Other spans reduced to name+duration+status. Still exceeds → return 413
6. Call LLM API using `provider_url` + `api_key` from config. Timeout 60s. Failure → 502
7. Parse JSON response, validate against schema. Failure → 500 with `raw_response` logged
8. Compute `spans_snapshot`, upsert `diagnosis_results`
9. Return result (200)

**Response (200):**

```json
{
  "trace_id_hex": "abc123...",
  "model_name": "claude-sonnet-4-6",
  "overall_score": 72,
  "scores": {
    "latency": 85,
    "cost": 62,
    "error": 45,
    "efficiency": 88
  },
  "findings": [
    {
      "severity": "error",
      "dimension": "error",
      "title": "第二个 LLM 调用返回限流错误",
      "description": "Span #4 chat/completions 返回 status=ERROR, message='rate_limit exceeded'",
      "suggestion": "检查 API 配额，增加指数退避重试逻辑，考虑降低并发请求数",
      "span_name": "chat/completions",
      "span_index": 3
    }
  ],
  "summary": "本次调用存在限流错误导致整体失败，且系统提示词占比过高推高了成本。延迟和工具调用效率表现良好。",
  "created_at": "2026-06-12T10:30:00Z",
  "stale": false
}
```

**Error responses:**

| Status | Error code | When |
|--------|-----------|------|
| 400 | `no_default_model` | No `llm_config` with `is_default=true` |
| 404 | `trace_not_found` | `traceIdHex` doesn't exist |
| 409 | `diagnosis_in_flight` | Another diagnosis is already running for this trace |
| 413 | `trace_too_large` | Even after trimming, context exceeds limit |
| 502 | `llm_call_failed` | LLM API error or timeout (60s) |
| 500 | `parse_error` | LLM returned invalid JSON |

---

## Prompt Design

### System prompt (core rules, shared across all diagnoses)

```
You are an LLM observability expert analyzing OTLP trace data from AI agent applications.
Evaluate the trace across four dimensions on a 0-100 scale:

1. **Latency (延迟):** Is the overall duration acceptable? Are there unnecessary delays or slow spans?
2. **Cost (成本):** Is token usage efficient? Is context window utilization reasonable? Are there redundant calls?
3. **Error (错误):** Are there ERROR spans, exceptions, or failures? How severe are they?
4. **Efficiency (效率):** Are tool calls well-structured? Could parallelization reduce time? Are there redundant or circular tool calls?

Scoring guidelines:
- 90-100: Excellent, no issues
- 80-89: Good, minor optimizations possible
- 60-79: Fair, notable issues to address
- 40-59: Poor, significant problems
- 0-39: Critical, major failures

For each dimension scoring below 80, provide specific findings with titles, descriptions, and actionable suggestions.
Return ONLY valid JSON matching the schema — no markdown, no preamble.
```

### User prompt (dynamic, built per trace)

The user prompt is built per-trace with the following sections. If a trace has no logs, the Logs section is omitted. If a trace has no LLM spans, token/cost fields show 0 and the cost dimension still evaluates (efficiency of non-LLM tool calls etc.).

```
Trace Summary:
- Service: {service.name}
- Total spans: 12
- Total duration: 8.5s
- Total tokens: 45,230
- Total cost: $0.23 USD

Spans:
[1] chat/completions | LLM | 2.3s | input=12000 output=500 total=12500 | model=claude-sonnet-4-6 | status=OK
[2] read_file | INTERNAL | 0.5s | status=OK
[3] search_code | INTERNAL | 1.2s | status=OK
[4] chat/completions | LLM | 4.1s | input=32000 output=730 total=32730 | model=claude-sonnet-4-6 | status=ERROR | message="rate_limit exceeded"
  Events: [{"name":"exception","timestamp":...,"attributes":{"exception.message":"rate_limit exceeded","exception.type":"RateLimitError"}}]
...

Logs:
[ERROR] Rate limit exceeded for model claude-sonnet-4-6
[INFO] Retry attempt 1/3 failed
[INFO] Retry attempt 2/3 failed
```

### Output JSON schema (enforced in prompt)

```json
{
  "overall_score": 72,
  "scores": {
    "latency": 85,
    "cost": 62,
    "error": 45,
    "efficiency": 88
  },
  "summary": "一句话总结",
  "findings": [
    {
      "severity": "error|warning|info",
      "dimension": "latency|cost|error|efficiency",
      "title": "简短标题",
      "description": "详细描述，引用具体 span 和数据",
      "suggestion": "可操作的改进建议"
    }
  ]
}
```

### LLM API call format

Uses OpenAI-compatible chat completions API (most providers support this):

```
POST {provider_url}/v1/chat/completions
Authorization: Bearer {api_key}
Content-Type: application/json

{
  "model": "{model_name}",
  "temperature": {temperature},
  "max_tokens": {max_tokens},
  "messages": [
    {"role": "system", "content": "<system prompt>"},
    {"role": "user", "content": "<user prompt>"}
  ]
}
```

`provider_url`, `api_key`, `model_name`, `temperature`, `max_tokens` all come from the default `llm_config` row.

### Backend response validation

After parsing the LLM JSON response, the backend validates:
- All four scores present and in range 0-100
- `overall_score` in range 0-100; if it deviates >15 from the average of four dimensions, log a warning but accept it
- `findings` is an array; each finding has required fields (`severity`, `dimension`, `title`, `description`, `suggestion`)
- `severity` is one of `error|warning|info`; `dimension` is one of `latency|cost|error|efficiency`

Any validation failure → 500 `parse_error`, with raw LLM response logged server-side.

---

## Frontend Design

### New "诊断" Tab

Added to `TraceDetail.vue` tab bar, between "Spans" and "Logs" tabs.

**Three states:**

#### State 1: Empty (no diagnosis yet)

- Icon + "尚未对此 Trace 进行诊断" message
- "开始诊断" button (disabled if no default LLM config → shows guidance link to `/llm-configs`)
- Hint text about what the evaluation covers

#### State 2: Loading (diagnosis in progress)

- Spinner + "正在分析中..." with estimated time (10-30s)
- Skeleton placeholder for score cards
- Button shows "诊断中..." (disabled)

#### State 3: Result (diagnosis complete)

- **Header row:** Overall score badge (color-coded) + model name + "重新诊断" button + relative timestamp
- **Stale banner** (conditional): Yellow banner "Trace 数据已更新，建议重新诊断" if `stale: true`
- **Score cards:** 4-column grid, each with dimension label (i18n), numeric score, and color (🟢 80+ / 🟡 60-79 / 🔴 <60)
- **Findings section:** Grouped by severity (error → warning → info), each finding as an expandable card with:
  - Color-coded severity badge
  - Dimension tag
  - Title
  - Description (with highlighted span references)
  - Suggestion
  - Click span reference → navigate waterfall to that span + open detail drawer

### TypeScript types (`web/src/api/client.ts`)

```ts
interface DiagnosisFinding {
  severity: 'error' | 'warning' | 'info'
  dimension: 'latency' | 'cost' | 'error' | 'efficiency'
  title: string
  description: string
  suggestion: string
  span_name?: string
  span_index?: number
}

interface DiagnosisScores {
  latency: number
  cost: number
  error: number
  efficiency: number
}

interface DiagnosisResult {
  trace_id_hex: string
  model_name: string
  overall_score: number
  scores: DiagnosisScores
  findings: DiagnosisFinding[]
  summary: string
  created_at: string
  stale: boolean
}

// API functions
function diagnoseTrace(traceIdHex: string, force?: boolean): Promise<DiagnosisResult>
function getDiagnosisResult(traceIdHex: string): Promise<DiagnosisResult | null>
```

### i18n keys

New keys under `diagnosis.*` in both `en.ts` and `zh.ts`:

| Key | en | zh |
|-----|----|----|
| `diagnosis.tab` | Diagnosis | 诊断 |
| `diagnosis.empty` | No diagnosis yet for this trace | 尚未对此 Trace 进行诊断 |
| `diagnosis.start` | Start Diagnosis | 开始诊断 |
| `diagnosis.rediagnose` | Re-diagnose | 重新诊断 |
| `diagnosis.analyzing` | Analyzing... | 正在分析中... |
| `diagnosis.est_time` | Estimated 10-30 seconds | 预计耗时 10-30 秒 |
| `diagnosis.no_model` | Please configure a default LLM model first | 请先配置默认 LLM 模型 |
| `diagnosis.stale` | Trace data has changed, consider re-diagnosing | Trace 数据已更新，建议重新诊断 |
| `diagnosis.overall` | Overall Score | 综合评分 |
| `diagnosis.latency` | Latency | 延迟 |
| `diagnosis.cost` | Cost | 成本 |
| `diagnosis.error` | Error | 错误 |
| `diagnosis.efficiency` | Efficiency | 效率 |
| `diagnosis.critical` | Critical Issues | 关键问题 |
| `diagnosis.suggestions` | Suggestions | 优化建议 |
| `diagnosis.model_label` | Model | 模型 |
| `diagnosis.timeout` | Diagnosis timed out, please retry | 诊断超时，请重试 |
| `diagnosis.format_error` | Unexpected response format, please retry | 诊断结果格式异常，请重试 |
| `diagnosis.too_large` | This trace is too large for diagnosis | 此 Trace 规模过大，暂不支持诊断 |

---

## Error Handling Matrix

| Scenario | HTTP Status | Error Code | Frontend Behavior |
|----------|------------|------------|-------------------|
| No default LLM config | 400 | `no_default_model` | Button disabled + link to LLM config page |
| Trace not found | 404 | `trace_not_found` | Standard 404 page |
| Diagnosis already in progress | 409 | `diagnosis_in_flight` | Button already shows loading (prevented in UI) |
| Trace too large after trimming | 413 | `trace_too_large` | Toast: "Trace 规模过大，暂不支持诊断" |
| LLM timeout (60s) | 502 | `llm_call_failed` | Toast: "诊断超时，请重试" |
| LLM API error (auth, rate limit, etc.) | 502 | `llm_call_failed` | Toast with upstream error message |
| LLM returned unparseable JSON | 500 | `parse_error` | Toast: "诊断结果格式异常，请重试" |

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/storage/storage.go` | Modify | Add `DiagnosisResult`, `DiagnosisFinding`, `DiagnosisScores` types + `GetDiagnosisResult`/`UpsertDiagnosisResult` to `Store` interface |
| `internal/storage/schema.sql` | Modify | Add `diagnosis_results` table DDL |
| `internal/storage/chdb.go` | Modify | chDB implementation of diagnosis store methods + schema migration |
| `internal/storage/memstore.go` | Modify | In-memory implementation of diagnosis store methods |
| `internal/api/trace_handler.go` | Modify | Add `DiagnoseTrace` and `GetDiagnosis` handlers |
| `internal/api/router.go` | Modify | Register `GET /api/v1/traces/{traceIdHex}/diagnosis` and `POST /api/v1/traces/{traceIdHex}/diagnose` routes |
| `internal/api/diagnosis_prompt.go` | **Create** | Prompt builder: trace → system + user prompt strings |
| `internal/api/diagnosis_llm.go` | **Create** | LLM client: call provider API, parse response JSON |
| `internal/api/trace_handler_test.go` | Modify | Add table-driven tests for diagnose endpoint |
| `web/src/api/client.ts` | Modify | Add `DiagnosisResult` types + `diagnoseTrace()` / `getDiagnosisResult()` functions |
| `web/src/views/TraceDetail.vue` | Modify | Add "诊断" tab, three-state rendering, finding→span navigation |
| `web/src/components/DiagnosisTab.vue` | **Create** | Standalone diagnosis tab component (empty/loading/result states) |
| `web/src/i18n/locales/en.ts` | Modify | Add `diagnosis.*` keys |
| `web/src/i18n/locales/zh.ts` | Modify | Add `diagnosis.*` keys |

---

## Testing Strategy

### Go tests (`internal/api/trace_handler_test.go`)

| Test Case | What It Verifies |
|-----------|-----------------|
| GET diagnosis for trace with no result → 404 | `no_diagnosis` error |
| GET diagnosis for trace with cached result → 200 | Cache retrieval without LLM call |
| POST diagnose, no default LLM config → 400 | Error response with `no_default_model` |
| POST diagnose, cached result exists → 200 (no LLM call) | Cache hit on POST |
| POST diagnose with `force=true` → re-diagnoses | Force override |
| POST diagnose, trace not found → 404 | Standard 404 |
| POST diagnose, LLM returns invalid JSON → 500 | Parse error handling |
| POST diagnose, LLM timeout → 502 | Timeout handling |
| POST diagnose, empty trace (no spans) → 200 | Edge case |
| POST diagnose, trace with no LLM spans → 200 | Cost dimension handles missing data |
| POST diagnose, stale snapshot → returns fresh result | Staleness detection |

### Manual verification

1. Configure a default LLM model in Settings → LLM Configs
2. Open a trace with LLM spans in Trace Detail
3. Click "诊断" tab → "开始诊断"
4. Verify loading state → result displayed
5. Verify score cards color-coded correctly
6. Click a finding's span reference → verify waterfall navigation
7. Click "重新诊断" → verify fresh diagnosis
8. Open the same trace again → verify cached result loads instantly
9. Delete default LLM config → verify button disabled with guidance
