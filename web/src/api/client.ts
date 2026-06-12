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
}

export interface Pagination {
  page: number
  page_size: number
  total: number
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
  gen_ai_request_model?: string
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
  min_duration?: number
  max_duration?: number
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

// --- Dashboard types and API ---

export interface PanelConfig {
  id: string
  title: string
  metric: string
  labels: Record<string, string>
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
  total_output_tokens: number
  avg_cost_per_trace: number
  trace_count: number
}

export interface ModelCost {
  model: string
  cost: number
  tokens: number
  input_tokens: number
  output_tokens: number
  trace_count: number
  avg_cost: number
}

export interface CostSummary {
  period: string
  currency: string
  overview: CostOverview
  by_model: ModelCost[]
}

export async function getCostSummary(period: string): Promise<CostSummary> {
  return get<CostSummary>(`${BASE_URL}/cost-summary?period=${period}`)
}

// --- LLM Config types and API ---

export interface LlmConfig {
  id: string
  model_name: string
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

export async function getSession(sessionId: string): Promise<SessionDetail> {
  return get<SessionDetail>(`${BASE_URL}/sessions/${encodeURIComponent(sessionId)}`)
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
  start?: number
  end?: number
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
    start: query.start,
    end: query.end,
  })
}

export async function getLogsByTrace(traceIdHex: string): Promise<{ logs: LogRecord[] }> {
  return get<{ logs: LogRecord[] }>(`${BASE_URL}/logs/${traceIdHex}`)
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

export async function getLogEventNames(): Promise<string[]> {
  const data = await get<{ event_names: string[] }>(`${BASE_URL}/log-event-names`)
  return data.event_names || []
}
