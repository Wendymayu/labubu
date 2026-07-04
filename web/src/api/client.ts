const BASE_URL = '/api/v1'

export interface TraceListItem {
  trace_id_hex: string
  root_span_id: string
  root_name: string
  root_service: string
  start_time_ms: number
  duration_ms: number
  span_count: number
  status: string
  total_tokens?: number
  cost?: number
  cost_currency?: string
  input_messages?: string
}

export interface Pagination {
  page: number
  page_size: number
  total: number
}

// TimeRangeSelection is the payload emitted by the shared TimeRangePicker
// component. `start`/`end` are epoch ms; both are undefined for the "all"
// preset (no time filter). For presets (today/7d/30d) and custom, both are set.
export interface TimeRangeSelection {
  period: string
  start?: number
  end?: number
}

export interface TraceListResponse {
  traces: TraceListItem[]
  pagination: Pagination
}

export interface SpanDetail {
  span_id: string
  parent_span_id: string
  name: string
  kind: string
  start_time_ms: number
  duration_ms: number
  attributes: Record<string, string>
  events: Array<{ time_ms: number; name: string; attributes: Record<string, string> }>
  links: Array<{ trace_id: string; span_id: string; attributes: Record<string, string> }>
  status: string
  status_message?: string
  input_tokens?: number
  output_tokens?: number
  total_tokens?: number
  cache_creation_tokens?: number // prompt-caching write tokens (Claude/Anthropic)
  cache_read_tokens?: number     // prompt-caching read tokens (Claude/Anthropic)
  gen_ai_request_model?: string
  gen_ai_system?: string       // Attributes["gen_ai.system"]
  tool_name?: string           // Attributes["gen_ai.tool.name"]
  is_tool_call: boolean        // tool_name != null
}

/**
 * One LLM call in a trace, for the context-change bar chart.
 * Derived from spans where total_tokens > 0, sorted by start_time_ms.
 * input + cacheRead + cacheCreation + output == total_tokens.
 */
export interface ContextPoint {
  index: number          // 1-based position after sorting by start_time_ms
  spanId: string
  spanName: string
  model: string          // gen_ai_request_model ?? ''
  input: number          // input_tokens ?? 0
  cacheRead: number      // cache_read_tokens ?? 0
  cacheCreation: number  // cache_creation_tokens ?? 0
  output: number         // output_tokens ?? 0
  contextWindow?: number   // model's max context window in tokens; 0/undefined = unknown
  usagePct?: number | null // total / contextWindow, 0..1; null when no window
}

/**
 * One agent session's context trajectory — LLM calls sharing the same owning
 * `.invoke` span (root agent or a subagent invocation). Subagent sessions have
 * independent context that resets on dispatch, so they must be charted
 * separately rather than merged with the main session.
 */
export interface ContextSession {
  id: string            // owning invoke span id (or '__root__' fallback)
  agentName: string     // gen_ai.agent.name of the owning invoke span
  isMain: boolean       // true for the root agent's session
  startMs: number       // owner span start_time_ms, for ordering
  points: ContextPoint[]
}

export interface ScopeDetail {
  name: string
  version: string
  attributes: Record<string, string>
}

export interface TraceDetailResponse {
  trace: {
    trace_id_hex: string
    root_span_id: string
    span_count: number
    start_time_ms: number
    duration_ms: number
    resource_attributes: Record<string, string>
    scope: ScopeDetail
    cost?: number
    cost_currency?: string
    unpriced_spans?: number
    session_id?: string
    spans: SpanDetail[]
  }
}

export interface TraceQuery {
  page?: number
  page_size?: number
  service?: string
  status?: string
  q?: string
  start?: number
  end?: number
  min_duration?: number | ''
  max_duration?: number | ''
  min_spans?: number | ''
  max_spans?: number | ''
  min_cost?: number | ''
  max_cost?: number | ''
}

async function get<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
  const url = new URL(path, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== '') {
        url.searchParams.set(k, String(v))
      }
    })
  }
  const res = await fetch(url.toString())
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function listTraces(query: TraceQuery): Promise<TraceListResponse> {
  return get<TraceListResponse>(`${BASE_URL}/traces`, {
    page: query.page,
    page_size: query.page_size,
    service: query.service,
    status: query.status,
    q: query.q,
    start: query.start,
    end: query.end,
    min_duration: query.min_duration,
    max_duration: query.max_duration,
    min_spans: query.min_spans,
    max_spans: query.max_spans,
    min_cost: query.min_cost,
    max_cost: query.max_cost,
  })
}

export async function getTrace(traceIdHex: string): Promise<TraceDetailResponse> {
  return get<TraceDetailResponse>(`${BASE_URL}/traces/${traceIdHex}`)
}

export async function getServices(): Promise<string[]> {
  const data = await get<{ services: string[] }>(`${BASE_URL}/services`)
  return data.services
}

export interface ExportRequest {
  trace_ids: string[]
  format: string
}

export async function exportTraces(traceIds: string[], format: string): Promise<any> {
  const res = await fetch(`${BASE_URL}/traces/export`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ trace_ids: traceIds, format } as ExportRequest),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Export failed: ${res.status}`)
  }
  return res.json()
}

export interface ImportResult {
  imported: number
  skipped: number
}

export async function importTraces(jsonData: string): Promise<ImportResult> {
  const res = await fetch(`${BASE_URL}/traces/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: jsonData,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Import failed: ${res.status}`)
  }
  return res.json()
}

export interface DeleteTracesResult {
  deleted_traces: number
  deleted_logs: number
}

export async function deleteTraces(traceIds: string[]): Promise<DeleteTracesResult> {
  const res = await fetch(`${BASE_URL}/traces/delete`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ trace_ids: traceIds }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Delete failed: ${res.status}`)
  }
  return res.json()
}

// --- Dashboard types and API ---

export interface PanelConfig {
  id: string
  title: string
  expressionType: 'single' | 'ratio'
  metric: string                       // Single: main metric; Ratio: denominator
  numeratorMetric?: string             // Ratio: numerator (only when expressionType='ratio')
  labels: Record<string, string>
  func: 'none' | 'rate' | 'increase'
  aggregation: 'none' | 'sum' | 'avg' | 'max' | 'min'
  groupBy?: string                     // label key for grouping
  chartType: 'line' | 'bar' | 'stat'
  step?: number
}

export interface DashboardItem {
  id: string
  name: string
  created_at: string
  panels: PanelConfig[]
}

export interface DashboardListResponse {
  dashboards: DashboardItem[]
}

export async function listDashboards(): Promise<DashboardListResponse> {
  return get<DashboardListResponse>(`${BASE_URL}/dashboards`)
}

export async function createDashboard(name: string): Promise<DashboardItem> {
  const res = await fetch(`${BASE_URL}/dashboards`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function renameDashboard(id: string, name: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

export async function deleteDashboard(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${id}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

export async function createPanel(dashboardId: string, panel: Omit<PanelConfig, 'id'>): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards/${dashboardId}/panels`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function updatePanel(dashboardId: string, panel: PanelConfig): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards/${dashboardId}/panels/${panel.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function deletePanel(dashboardId: string, panelId: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${dashboardId}/panels/${panelId}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

export async function getMetricNames(): Promise<string[]> {
  const data = await get<{ status: string; data: string[] }>(`${BASE_URL}/metric-names`)
  return data.data || []
}

export async function getLabels(): Promise<string[]> {
  const data = await get<{ status: string; data: string[] }>(`${BASE_URL}/labels`)
  return data.data || []
}

export async function getLabelValues(name: string): Promise<string[]> {
  const data = await get<{ status: string; data: string[] }>(`${BASE_URL}/label/${name}/values`)
  return data.data || []
}

// Prometheus API response types for query results.
export interface QueryResult {
  status: string
  data: {
    resultType: 'vector' | 'matrix'
    result: Array<{
      metric: Record<string, string>
      value?: [number, string]
      values?: Array<[number, string]>
    }>
  }
  error?: string
}

// --- Model Pricing types and API ---

export interface ModelPricing {
  model_name: string
  input_price: number
  output_price: number
  currency: string
  context_window: number
}

export interface ModelPricingListResponse {
  models: ModelPricing[]
}

export async function getModelPricing(): Promise<ModelPricingListResponse> {
  return get<ModelPricingListResponse>(`${BASE_URL}/model-pricing`)
}

export async function saveModelPricing(p: ModelPricing): Promise<ModelPricing> {
  const res = await fetch(`${BASE_URL}/model-pricing`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(p),
  })
  if (!res.ok) throw new Error(`API error: ${res.status}`)
  return res.json()
}

export async function deleteModelPricing(modelName: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/model-pricing/${encodeURIComponent(modelName)}`, {
    method: 'DELETE',
  })
  if (!res.ok) throw new Error(`API error: ${res.status}`)
}

export async function recalcCosts(): Promise<{ status: string; traces_updated: number; sessions_updated: number }> {
  const res = await fetch(`${BASE_URL}/model-pricing/recalc`, { method: 'POST' })
  if (!res.ok) throw new Error(`API error: ${res.status}`)
  return res.json()
}

// --- Cost Dashboard types and API ---

export interface CostOverview {
  total_cost: number
  total_tokens: number
  total_input_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_output_tokens: number
  avg_cost_per_trace: number
  trace_count: number
}

export interface ModelCost {
  model: string
  cost: number
  tokens: number
  input_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  output_tokens: number
  trace_count: number
  avg_cost: number
}

export interface ServiceCost {
  service: string
  cost: number
  tokens: number
  input_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  output_tokens: number
  trace_count: number
  avg_cost: number
}

export interface CostSummary {
  period: string
  currency: string
  overview: CostOverview
  group_by: 'model' | 'service'
  by_model?: ModelCost[]
  by_service?: ServiceCost[]
}

export async function getCostSummary(
  period: string,
  groupBy: 'model' | 'service' = 'model',
  range?: { start: number; end: number }
): Promise<CostSummary> {
  // When `range` is set (custom time), start/end (epoch ms) override the preset
  // period on the backend. The get() helper skips undefined params.
  return get<CostSummary>(`${BASE_URL}/cost-summary`, {
    period,
    group_by: groupBy,
    start: range?.start,
    end: range?.end,
  })
}

// --- LLM Config types and API ---

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

export interface LlmConfigListResponse {
  configs: LlmConfig[]
}

export async function listLlmConfigs(): Promise<LlmConfigListResponse> {
  return get<LlmConfigListResponse>(`${BASE_URL}/llm-configs`)
}

export async function createLlmConfig(config: Omit<LlmConfig, 'id'>): Promise<LlmConfig> {
  const res = await fetch(`${BASE_URL}/llm-configs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `API error: ${res.status}`)
  }
  return res.json()
}

export async function updateLlmConfig(id: string, config: LlmConfig): Promise<void> {
  const res = await fetch(`${BASE_URL}/llm-configs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `API error: ${res.status}`)
  }
}

export async function deleteLlmConfig(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/llm-configs/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

// --- Session types and API ---

export interface SessionListItem {
  session_id: string
  trace_count: number
  total_tokens?: number
  cost?: number
  cost_currency?: string
  total_duration_ms: number
  max_duration_ms: number
  avg_duration_ms: number
  error_count: number
  error_rate: number
  first_active_ms: number
  last_active_ms: number
}

export interface SessionDetail {
  session: SessionListItem
  traces: TraceListItem[]
  pagination: Pagination
}

export interface SessionQuery {
  page?: number
  page_size?: number
  service?: string
  q?: string
  start?: number
  end?: number
}

export interface SessionListResponse {
  sessions: SessionListItem[]
  pagination: Pagination
}

export async function listSessions(query: SessionQuery): Promise<SessionListResponse> {
  return get<SessionListResponse>(`${BASE_URL}/sessions`, {
    page: query.page,
    page_size: query.page_size,
    service: query.service,
    q: query.q,
    start: query.start,
    end: query.end,
  })
}

export async function getSession(sessionId: string, page = 1, pageSize = 20): Promise<SessionDetail> {
  return get<SessionDetail>(`${BASE_URL}/sessions/${encodeURIComponent(sessionId)}`, {
    page,
    page_size: pageSize,
  })
}

/**
 * A main-agent LLM span in a session (subagent spans excluded), with the token
 * breakdown needed for the session context bar chart. Drives the same
 * ContextBarChart component used on the trace detail page.
 */
export interface SessionContextSpan {
  trace_id_hex: string
  span_id: string
  name: string
  start_time_ms: number
  input_tokens?: number
  output_tokens?: number
  total_tokens?: number
  cache_read_tokens?: number
  cache_creation_tokens?: number
  gen_ai_request_model?: string
}

export async function getSessionContext(sessionId: string): Promise<{ spans: SessionContextSpan[] }> {
  return get<{ spans: SessionContextSpan[] }>(`${BASE_URL}/sessions/${encodeURIComponent(sessionId)}/context`)
}

// --- Log types and API ---

export interface LogRecord {
  trace_id_hex: string
  span_id_hex: string
  timestamp: number
  severity: string
  event_name: string
  body: string
  attributes: Record<string, string>
}

export interface LogQuery {
  page?: number
  page_size?: number
  severity?: string
  event_name?: string
  q?: string
  trace_id?: string
  span_id?: string
  start?: number
  end?: number
  asc?: boolean
}

export interface LogListResponse {
  logs: LogRecord[]
  pagination: Pagination
}

export async function listLogs(query: LogQuery): Promise<LogListResponse> {
  return get<LogListResponse>(`${BASE_URL}/logs`, {
    page: query.page,
    page_size: query.page_size,
    severity: query.severity,
    event_name: query.event_name,
    q: query.q,
    trace_id: query.trace_id,
    span_id: query.span_id,
    start: query.start,
    end: query.end,
    asc: query.asc ? 'true' : undefined,
  })
}

export async function getLogsByTrace(traceIdHex: string): Promise<{ logs: LogRecord[] }> {
  return get<{ logs: LogRecord[] }>(`${BASE_URL}/logs/${traceIdHex}`)
}

export async function getLogCounts(traceIdHex: string): Promise<{ counts: Record<string, number> }> {
  return get<{ counts: Record<string, number> }>(`${BASE_URL}/logs/${traceIdHex}/counts`)
}

// --- Diagnosis types and API ---

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

export async function getDiagnosisResult(traceIdHex: string): Promise<DiagnosisResult> {
  const res = await fetch(`${BASE_URL}/traces/${traceIdHex}/diagnosis`)
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Failed to get diagnosis: ${res.status}`)
  }
  return res.json()
}

export async function diagnoseTrace(traceIdHex: string, force?: boolean, locale?: string): Promise<DiagnosisResult> {
  const url = new URL(`${BASE_URL}/traces/${traceIdHex}/diagnose`, window.location.origin)
  if (force) {
    url.searchParams.set('force', 'true')
  }
  if (locale) {
    url.searchParams.set('locale', locale)
  }
  const res = await fetch(url.toString(), { method: 'POST' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Diagnosis failed: ${res.status}`)
  }
  return res.json()
}

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

export async function getAgentStats(sessionId: string): Promise<AgentStats> {
  return get<AgentStats>(`${BASE_URL}/sessions/${sessionId}/agent-stats`)
}

export async function getLogEventNames(): Promise<string[]> {
  const data = await get<{ event_names: string[] }>(`${BASE_URL}/log-event-names`)
  return data.event_names || []
}
