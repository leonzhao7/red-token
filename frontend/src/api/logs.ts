const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export interface UsageLog {
  id: number
  request_id: string
  trace_id?: string
  created_at: string
  client_name: string
  model: string
  backend_name: string
  proxy_name?: string
  status_code: number
  status_family: string
  input_tokens: number
  output_tokens: number
  input_cache_tokens?: number
  duration_ms?: number
  request_bytes?: number
  response_bytes?: number
  path?: string
  method?: string
  client_ip?: string
  user_agent?: string
  error_message?: string
  request_body_preview?: string
  response_body_preview?: string
}

export interface LogOptions {
  backends: string[]
  models: string[]
  client_keys: string[]
  proxies: string[]
}

export interface LogFilters {
  q?: string
  model?: string
  client_key?: string
  backend?: string
  status?: string
  date_from?: string
  date_to?: string
  page?: number
  limit?: number
}

interface PagedResponse<T> {
  items: T[]
  total: number
  page: number
  limit: number
}

class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body != null) headers.set('Content-Type', 'application/json')
  const response = await fetch(apiBase + path, { ...init, headers })
  const ct = response.headers.get('Content-Type') || ''
  const payload = ct.includes('application/json')
    ? await response.json().catch(() => null)
    : await response.text().catch(() => '')
  if (!response.ok) {
    const message =
      payload && typeof payload === 'object' && 'error' in payload
        ? String((payload as any).error?.message || response.statusText)
        : typeof payload === 'string' && payload.trim()
          ? payload.trim()
          : response.statusText || `请求失败（${response.status}）`
    throw new ApiError(message, response.status)
  }
  return payload as T
}

function buildQuery(filters: LogFilters): string {
  const p = new URLSearchParams()
  if (filters.q) p.set('q', filters.q)
  if (filters.model) p.set('model', filters.model)
  if (filters.client_key) p.set('client_key', filters.client_key)
  if (filters.backend) p.set('backend', filters.backend)
  if (filters.status) p.set('status', filters.status)
  if (filters.date_from) p.set('date_from', filters.date_from)
  if (filters.date_to) p.set('date_to', filters.date_to)
  p.set('page', String(filters.page ?? 1))
  p.set('limit', String(filters.limit ?? 20))
  return p.toString()
}

export function listLogs(filters: LogFilters = {}) {
  return request<PagedResponse<UsageLog>>(`/admin/api/usage-logs?${buildQuery(filters)}`)
}

export function getLogOptions() {
  return request<LogOptions>('/admin/api/usage-log-options')
}

export function clearLogs(filters: LogFilters = {}) {
  const p = new URLSearchParams()
  if (filters.q) p.set('q', filters.q)
  if (filters.model) p.set('model', filters.model)
  if (filters.client_key) p.set('client_key', filters.client_key)
  if (filters.backend) p.set('backend', filters.backend)
  if (filters.status) p.set('status', filters.status)
  const qs = p.toString()
  return request<{ cleared: boolean; deleted: number }>(`/admin/api/usage-logs${qs ? '?' + qs : ''}`, {
    method: 'DELETE'
  })
}
