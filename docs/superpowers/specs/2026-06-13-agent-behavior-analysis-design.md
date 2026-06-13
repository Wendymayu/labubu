# Agent Behavior Analysis — Design Spec

Date: 2026-06-13

## Problem

Labubu currently shows traces, sessions, metrics, and cost, but lacks **agent behavior analysis** — the ability to answer "is this agent reliable?". Mainstream observability platforms (Langfuse, Phoenix, Braintrust) all provide tool call success rates, retry tracking, and loop detection. Labubu needs equivalent functionality.

## Scope

- **Trace level**: Tool call chain visualization, success rate, loop detection, retry counting
- **Session level**: Aggregate agent stats (success rate trends, tool usage breakdown, average metrics)
- **Embedded in existing pages**: New tab in TraceDetail, new section in SessionDetail
- **Data source**: OTel GenAI semantic attributes (`gen_ai.tool.name`, `gen_ai.system`, etc.)
- **Not in scope**: Prompt playground, evals framework, trace diff, live tail (separate specs)

## Architecture Decision: Hybrid Approach

| Level | Calculation | Reason |
|-------|-------------|---------|
| Trace | Frontend (from GetTrace API data) | Single trace has ~5-50 spans, client-side computation is fast |
| Session | Backend API (`/sessions/{id}/agent-stats`) | Session may span 10-100+ traces, loading all trace data client-side is impractical |

No pre-aggregation storage — the session-level API computes stats on-the-fly from stored spans. Sessions typically have 10-50 traces, so real-time computation is sufficient (<100ms). This avoids schema migrations and cache staleness issues.

## Core Metrics

### Trace-Level Metrics (per single execution)

| Metric | Definition | Calculation |
|--------|-----------|-------------|
| Tool call success rate | Successful tool calls / total tool calls | Count spans with `IsToolCall=true` and `Status=ok` |
| Retry count | Same-name tool consecutive failures followed by success | Detect `error→error→ok` pattern per tool |
| Max loop depth | Maximum consecutive calls of the same tool | `max(count of same ToolName in sequence)` |
| Tool call chain | Ordered sequence of tool + LLM calls | Sort spans by `start_time`, show only tool and LLM spans |
| Token consumption | Input + output tokens for this trace | Already available via `input_tokens` + `output_tokens` |

### Session-Level Metrics (aggregate trends)

| Metric | Definition | Calculation |
|--------|-----------|-------------|
| Trace success rate | OK traces / total traces | Count traces where overall status is `ok` |
| Avg tool success rate | Mean of per-trace tool success rates | Compute per trace, then average |
| Avg retries | Mean of per-trace retry counts | Compute per trace, then average |
| Max/Avg loop depth | Maximum and mean of per-trace loop depths | Compute per trace, then aggregate |
| Tool usage breakdown | Per-tool call count, success rate, avg retries, max loop | Group all tool spans by `ToolName` |
| Span-per-trace | Total spans / total traces | Simple division |

## GenAI Attribute Identification

A span is classified based on its attributes:

| Condition | Classification | In stats? |
|-----------|---------------|-----------|
| `gen_ai.tool.name` present | Tool call | Yes — all tool metrics |
| `gen_ai.system` present | LLM call | Token stats only, not tool metrics |
| Neither present | Generic logic span | Shown in call chain, excluded from stats |

**Compatibility**: Many agents don't send `gen_ai.tool.call.status`. Tool call success/failure is determined by the span's `Status` field (`ok` / `error`). If both exist, `Status` takes precedence.

## Backend Changes

### SpanDetail Field Extensions

Add parsed GenAI fields to the API response type (not stored separately — extracted from `Attributes`):

```go
type SpanDetail struct {
    // ... existing fields ...
    ToolName    string  // Attributes["gen_ai.tool.name"]
    GenAISystem string  // Attributes["gen_ai.system"]
    IsToolCall  bool    // ToolName != ""
}
```

The `buildSpanDetail()` function extracts these from `span.Attributes` when constructing the API response.

### New API Endpoint

```
GET /api/v1/sessions/{sessionId}/agent-stats
```

Response:
```json
{
  "trace_success_rate": 0.75,
  "avg_tool_success_rate": 0.92,
  "avg_retries": 0.4,
  "avg_loop_depth": 1.2,
  "max_loop_depth": 3,
  "span_per_trace": 5.2,
  "total_tool_calls": 57,
  "successful_tool_calls": 52,
  "tool_usage": [
    {
      "tool_name": "file_read",
      "call_count": 28,
      "success_rate": 1.0,
      "avg_retries": 0,
      "max_loop": 3
    }
  ],
  "insights": [
    "file_read has max loop depth 3 — agent sometimes reads the same file repeatedly",
    "web_search has the lowest success rate (80%) — consider adding fallback logic"
  ]
}
```

Implementation: queries all traces in the session via `ListTraces`, then iterates spans to compute metrics. No new storage table required.

### Insight Generation Logic

Auto-generated observations surfaced in the API response:

1. Tools with `max_loop >= 3` → "Agent may be stuck in a loop with {tool_name}"
2. Tools with `success_rate < 0.8` → "{tool_name} has low success rate ({rate}), consider fallback"
3. `trace_success_rate < 0.7` → "Over {rate}% of traces failed — agent configuration may need adjustment"
4. `avg_retries > 1.0` → "High average retry count ({avg}) — tool calls frequently fail on first attempt"

## Frontend Changes

### TraceDetail — Agent Behavior Tab

New tab alongside Waterfall, Logs, Diagnosis:

**Score cards** (4 cards in a row):
- Tool Success Rate (green/yellow/red based on value)
- Max Loop Depth (yellow if ≥3, red if ≥5)
- Total Retries (red if >0)
- Tokens Used (neutral)

**Tool Call Chain** (ordered list):
- Each entry shows: icon (🤖 for LLM, 🔧 for tool), name, semantic attribute label, duration/token count, status badge (OK/ERROR)
- Failed tool calls highlighted with red background
- Retry attempts annotated (e.g., "❌ attempt 1/3", "✅ attempt 3/3")
- Consecutive same-tool calls grouped visually with loop indicator

**Loop Warning** (conditional banner):
- Yellow warning box when `MaxLoopDepth >= 3`
- Shows which tool and how many consecutive calls
- Suggests the agent may be stuck

**Empty state**: "No tool calls detected in this trace" when no GenAI spans exist.

All metrics computed client-side from `GetTrace` response data.

### SessionDetail — Agent Stats Section

New section between overview cards and trace list:

**Overview cards** (4 cards):
- Trace Success Rate
- Avg Tool Success Rate
- Avg Retries
- Avg Span/Trace

**Tool Usage Breakdown** (table):
- Columns: Tool, Calls, Success Rate, Avg Retries, Max Loop
- Color-coded success rates (green ≥90%, yellow ≥70%, red <70%)
- ⚠️ emoji next to Max Loop values ≥3

**Insight card** (auto-generated text):
- Lists top 2-3 most actionable observations
- Generated from API `insights` field

**Empty state**: Hidden entirely when no GenAI data in the session.

Data fetched from `/api/v1/sessions/{sessionId}/agent-stats` API.

## Loop Detection Algorithm

```
Input: spans sorted by start_time, filtered to IsToolCall=true
Maintain: consecutive counter map[string]int, lastToolName string

For each tool span:
  if span.ToolName == lastToolName:
    counter[span.ToolName] += 1
  else:
    counter[span.ToolName] = 1
    lastToolName = span.ToolName
  MaxLoopDepth = max(all counter values)
```

## Retry Detection Algorithm

```
Input: spans sorted by start_time, filtered to IsToolCall=true
For each unique ToolName:
  collect all spans for that tool in order
  scan for pattern: 1+ consecutive status=error followed by 1 status=ok
  each such pattern = 1 retry group, retry count = number of error spans before the ok
  if trailing error spans with no ok → not a retry, it's a final failure
Sum all retry counts across all tools
```

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Trace has no GenAI spans | Show empty state in Agent Behavior tab |
| Session has no GenAI data | Hide Agent Stats section entirely |
| `/agent-stats` API returns error | Show error message in Agent Stats section |
| `gen_ai.tool.call.status` missing | Use span `Status` field (`ok`/`error`) instead |
| `/agent-stats` computation slow | Add timeout (5s); if exceeded, show partial results or error |

## i18n Keys

New prefix: `agentStats.*` in both `en.ts` and `zh.ts`:

| Key | English | Chinese |
|-----|---------|---------|
| `agentStats.toolSuccessRate` | Tool Success Rate | 工具成功率 |
| `agentStats.maxLoopDepth` | Max Loop Depth | 最大循环深度 |
| `agentStats.totalRetries` | Total Retries | 总重试次数 |
| `agentStats.tokensUsed` | Tokens Used | Token 使用量 |
| `agentStats.toolCallChain` | Tool Call Chain | 工具调用链 |
| `agentStats.noToolCalls` | No tool calls detected in this trace | 该 trace 未检测到工具调用 |
| `agentStats.loopWarning` | Loop Detected: {tool} × {count} | 检测到循环：{tool} 连续调用 {count} 次 |
| `agentStats.loopWarningDesc` | The agent called {tool} {count} consecutive times. This may indicate a retry loop. | Agent 连续 {count} 次调用 {tool}，可能陷入重试循环 |
| `agentStats.traceSuccessRate` | Trace Success Rate | Trace 成功率 |
| `agentStats.avgToolSuccess` | Avg Tool Success Rate | 平均工具成功率 |
| `agentStats.avgRetries` | Avg Retries | 平均重试次数 |
| `agentStats.avgSpanPerTrace` | Avg Span/Trace | 平均 Span/Trace |
| `agentStats.toolUsage` | Tool Usage Breakdown | 工具使用分布 |
| `agentStats.insight` | Insight | 洞察 |
| `agentStats.calls` | Calls | 调用次数 |
| `agentStats.successRate` | Success Rate | 成功率 |
| `agentStats.avgRetriesCol` | Avg Retries | 平均重试 |
| `agentStats.maxLoop` | Max Loop | 最大循环 |
| `agentStats.agentBehavior` | Agent Behavior | Agent 行为 |
| `agentStats.agentStats` | Agent Behavior Stats | Agent 行为统计 |

## Files to Modify

### Backend (Go)
- `internal/storage/storage.go` — add `AgentStats`, `ToolUsageItem` types; add `ToolName`, `GenAISystem`, `IsToolCall` to `SpanDetail`
- `internal/storage/memstore.go` — implement `GetSessionAgentStats(sessionID)` method
- `internal/storage/sqlite_store.go` — implement `GetSessionAgentStats(sessionID)` method
- `internal/api/session_handler.go` — add `GetAgentStats` handler, register route
- `internal/api/trace_handler.go` — update `buildSpanDetail()` to extract GenAI attributes

### Frontend (Vue)
- `web/src/api/client.ts` — add `AgentStats`, `ToolUsageItem` interfaces; add `getAgentStats(sessionId)` function
- `web/src/views/TraceDetail.vue` — add "Agent Behavior" tab
- `web/src/components/AgentBehaviorTab.vue` — new component (score cards, tool call chain, loop warning)
- `web/src/views/SessionDetail.vue` — add Agent Stats section
- `web/src/components/AgentStatsSection.vue` — new component (overview cards, tool table, insights)
- `web/src/i18n/locales/en.ts` — add `agentStats.*` keys
- `web/src/i18n/locales/zh.ts` — add `agentStats.*` keys

## Testing

- Backend: table-driven tests for `GetSessionAgentStats` in `internal/api/session_handler_test.go`
- Backend: unit tests for loop detection and retry detection algorithms
- Frontend: TypeScript type check (`vue-tsc --noEmit`)
- Integration: `make run` → verify Agent Behavior tab shows data for traces with GenAI spans
