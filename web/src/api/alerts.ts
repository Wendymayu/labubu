const BASE_URL = '/api/v1/alerts'

export interface AlertCondition {
  field: string
  op: string
  value: string
}

export interface NotifierConfig {
  type: string
  smtp_host: string
  smtp_port: number
  username: string
  password: string
  recipients: string[]
}

export interface AlertRule {
  id: string
  name: string
  enabled: boolean
  metric: string
  conditions: AlertCondition[]
  for_duration: number
  interval: number
  notifier: NotifierConfig
  created_at: string
  updated_at: string
}

export interface AlertState {
  id: string
  rule_id: string
  trace_id_hex: string
  status: 'ok' | 'pending' | 'firing' | 'resolved'
  triggered_at: string
  last_fired_at?: string
  resolved_at?: string
}

export interface AlertNotification {
  id: string
  rule_id: string
  trace_id_hex: string
  action: 'firing' | 'resolved'
  channel: string
  recipient: string
  sent_at: string
  success: boolean
  error_msg?: string
}

export interface AlertRuleListResponse {
  rules: AlertRule[]
}

export interface AlertStateListResponse {
  states: AlertState[]
}

export interface AlertNotificationListResponse {
  notifications: AlertNotification[]
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

// Rules
export async function listRules(): Promise<AlertRuleListResponse> {
  return get<AlertRuleListResponse>(`${BASE_URL}/rules`)
}

export async function getRule(id: string): Promise<AlertRule> {
  return get<AlertRule>(`${BASE_URL}/rules/${encodeURIComponent(id)}`)
}

export async function createRule(rule: Omit<AlertRule, 'id' | 'created_at' | 'updated_at'>): Promise<AlertRule> {
  const res = await fetch(`${BASE_URL}/rules`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Create failed: ${res.status}`)
  }
  return res.json()
}

export async function updateRule(id: string, rule: Partial<AlertRule>): Promise<AlertRule> {
  const res = await fetch(`${BASE_URL}/rules/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Update failed: ${res.status}`)
  }
  return res.json()
}

export async function deleteRule(id: string): Promise<void> {
  const res = await fetch(`${BASE_URL}/rules/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
}

// States
export async function listStates(statusFilter?: string): Promise<AlertStateListResponse> {
  return get<AlertStateListResponse>(`${BASE_URL}/states`, { status: statusFilter })
}

// Notifications
export async function listNotifications(ruleId?: string): Promise<AlertNotificationListResponse> {
  return get<AlertNotificationListResponse>(`${BASE_URL}/notifications`, { rule_id: ruleId })
}
