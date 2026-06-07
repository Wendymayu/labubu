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

export async function listDashboards(): Promise<{ panels: PanelConfig[] }> {
  return get<{ panels: PanelConfig[] }>(`${BASE_URL}/dashboards`)
}

export async function createDashboard(panel: Omit<PanelConfig, 'id'>): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function updateDashboard(panel: PanelConfig): Promise<PanelConfig> {
  const res = await fetch(`${BASE_URL}/dashboards/${panel.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(panel),
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function deleteDashboard(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/dashboards/${id}`, { method: 'DELETE' })
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

// --- Session types and API ---

export interface SessionListItem {
  session_id: string
  trace_count: number
  total_tokens?: number
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

export async function getLogEventNames(): Promise<string[]> {
  const data = await get<{ event_names: string[] }>(`${BASE_URL}/log-event-names`)
  return data.event_names || []
}
