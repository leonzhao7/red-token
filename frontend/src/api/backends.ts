export type BackendProtocol = 'openai' | 'anthropic' | 'both'
export type BackendType = '' | 'new-api' | 'sub2api'
export type BackendStatus = 'normal' | 'abnormal' | 'disabled'

export interface BackendApiKey {
  api_key: string
  group: string
  models: string[]
  model_mapping: Record<string, string>
  used_quota: number
}

export interface SocksProxyResponse {
  id: number
  name: string
  address: string
  username: string
  password?: string
  enabled: boolean
}

export interface BackendResponse {
  id: number
  name: string
  protocol: string
  backend_type: string
  base_url: string
  api_keys: BackendApiKey[]
  console_url: string
  tags: string[]
  console_username: string
  console_password?: string
  new_api_refresh?: string
  console_authorization?: string
  console_checkin_path?: string
  channel_url?: string
  console_cookie?: string
  console_headers?: Record<string, string>
  console_account_json: string
  console_pricing_json: string
  notes: string
  proxy_id: number
  proxy?: SocksProxyResponse
  status: BackendStatus
  consecutive_failures: number
  recover_at?: string
  weight: number
  created_at: string
  updated_at: string
  request_count?: number
  avg_latency_ms?: number
  last_used_at?: string
  model_count?: number
  hourly_requests?: number
  hourly_failures?: number
}

export interface BackendWritePayload {
  name?: string
  protocol?: BackendProtocol
  backend_type?: BackendType
  base_url?: string
  api_keys?: BackendApiKey[]
  console_url?: string
  tags?: string[]
  console_username?: string
  console_password?: string
  new_api_refresh?: string
  console_authorization?: string
  console_checkin_path?: string
  channel_url?: string
  console_headers?: Record<string, string>
  console_user_id?: string
  notes?: string
  proxy_id?: number
  status?: 'normal' | 'disabled'
  weight?: number
}

interface PagedResponse<T> {
  items: T[]
  total: number
  page: number
  limit: number
}

export interface BackendSyncResponse {
  backend: BackendResponse
  status?: Record<string, unknown>
  checkin?: Record<string, unknown> | null
  account?: Record<string, unknown>
  pricing?: Record<string, unknown>
  requests: Array<{
    time: string
    method: string
    path: string
    status_code: number
    body: string
  }>
}

const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export class ApiError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body != null) headers.set('Content-Type', 'application/json')

  const response = await fetch(apiBase + path, { ...init, headers })
  const contentType = response.headers.get('Content-Type') || ''
  const payload = contentType.includes('application/json')
    ? await response.json().catch(() => null)
    : await response.text().catch(() => '')

  if (!response.ok) {
    const message =
      payload && typeof payload === 'object' && 'error' in payload
        ? String((payload as { error?: { message?: unknown } }).error?.message || response.statusText)
        : typeof payload === 'string' && payload.trim()
          ? payload.trim()
          : response.statusText || `请求失败（${response.status}）`
    throw new ApiError(message, response.status)
  }

  return payload as T
}

export function listBackends() {
  return request<PagedResponse<BackendResponse>>('/admin/api/backends?page=1&limit=10000')
}

export function listSocksProxies() {
  return request<PagedResponse<SocksProxyResponse>>('/admin/api/socks-proxies?page=1&limit=10000')
}

export function createBackend(payload: BackendWritePayload) {
  return request<BackendResponse>('/admin/api/backends', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function updateBackend(id: number, payload: BackendWritePayload) {
  return request<BackendResponse>(`/admin/api/backends/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function deleteBackend(id: number) {
  return request<{ deleted: number }>(`/admin/api/backends/${id}`, { method: 'DELETE' })
}

export function syncBackend(id: number, audit = true) {
  const query = audit ? '' : '?audit=0'
  return request<BackendSyncResponse>(`/admin/api/backends/${id}/console/sync${query}`, { method: 'POST' })
}

export function recordBackendSyncSummary(total: number, successCount: number, failureCount: number) {
  return request<{ total: number; success_count: number; failure_count: number }>('/admin/api/backends/console/sync-summary', {
    method: 'POST',
    body: JSON.stringify({ total, success_count: successCount, failure_count: failureCount })
  })
}
