export type BackendProtocol = 'openai' | 'anthropic' | 'both'
export type BackendType = '' | 'new-api' | 'sub2api'
export type BackendStatus = 'normal' | 'abnormal' | 'disabled'

export interface BackendApiKey {
  id: string
  key: string
  name: string
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

export interface SocksProxyListItem extends SocksProxyResponse {
  bound_backend_count: number
  request_count?: number
  traffic_bytes?: number
  avg_latency_ms?: number
  last_used_at?: string
}

export interface SocksProxyWrite {
  name: string
  address: string
  username: string
  password: string
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
  console_password: string
  new_api_refresh: string
  console_checkin_workflow_id: string
  console_headers: Record<string, string>
  console_account: string
  console_models: string
  notes: string
  proxy_id: number
  status: BackendStatus
  weight: number
  created_at: string
  updated_at: string
  avg_latency_ms: number
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
  console_checkin_workflow_id?: string
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

export interface BackendConsoleRequestLog {
  time: string
  method?: string
  path: string
  status_code: number
  body: string
}

export interface BackendWorkflowDebugLog {
  time: string
  level: 'debug' | 'info' | 'warn' | 'error' | string
  step_id?: string
  step_name?: string
  phase: string
  message: string
  duration_ms?: number
  details?: Record<string, unknown>
}

export interface BackendSyncResponse {
  backend: BackendResponse
  status?: Record<string, unknown>
  checkin?: Record<string, unknown> | null
  account?: Record<string, unknown>
  pricing?: Record<string, unknown>
  requests: BackendConsoleRequestLog[]
  debug_logs?: BackendWorkflowDebugLog[]
}

export type BackendConsoleStreamEvent =
  | { type: 'request'; request: BackendConsoleRequestLog }
  | { type: 'workflow_log'; log: BackendWorkflowDebugLog }
  | { type: 'complete'; response: BackendSyncResponse }
  | { type: 'error'; status?: number; message?: string; requests?: BackendConsoleRequestLog[] }

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
  return request<PagedResponse<SocksProxyListItem>>('/admin/api/socks-proxies?page=1&limit=10000')
}

export function createSocksProxy(payload: SocksProxyWrite) {
  return request<SocksProxyResponse>('/admin/api/socks-proxies', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function updateSocksProxy(id: number, payload: SocksProxyWrite) {
  return request<SocksProxyResponse>(`/admin/api/socks-proxies/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function deleteSocksProxy(id: number) {
  return request<{ deleted: number }>(`/admin/api/socks-proxies/${id}`, { method: 'DELETE' })
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

export async function syncBackendStream(
  id: number,
  onRequest: (request: BackendConsoleRequestLog) => void,
  options: {
    audit?: boolean
    checkin?: boolean
    onWorkflowLog?: (log: BackendWorkflowDebugLog) => void
  } = {}
): Promise<BackendSyncResponse> {
  const audit = options.audit !== false
  const params = new URLSearchParams({ stream: '1' })
  if (!audit) params.set('audit', '0')
  if (options.checkin) params.set('checkin', '1')

  const response = await fetch(`${apiBase}/admin/api/backends/${id}/console/sync?${params}`, {
    method: 'POST',
    headers: { Accept: 'application/x-ndjson' }
  })

  if (!response.ok) {
    const text = await response.text().catch(() => '')
    let message = response.statusText || `请求失败（${response.status}）`
    try {
      const json = JSON.parse(text)
      if (json?.error?.message) message = json.error.message
      else if (json?.error) message = String(json.error)
    } catch {}
    throw new ApiError(message, response.status)
  }

  const reader = response.body?.getReader()
  if (!reader) throw new ApiError('Stream not available', 0)

  const decoder = new TextDecoder()
  let buffer = ''
  let finalResponse: BackendSyncResponse | null = null

  while (true) {
    const { done, value } = await reader.read()
    if (value) buffer += decoder.decode(value, { stream: true })

    const lines = buffer.split('\n')
    buffer = lines.pop() || ''

    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed) continue
      try {
        const event: BackendConsoleStreamEvent = JSON.parse(trimmed)
        if (event.type === 'request') {
          onRequest(event.request)
        } else if (event.type === 'workflow_log') {
          options.onWorkflowLog?.(event.log)
        } else if (event.type === 'complete') {
          finalResponse = event.response
        } else if (event.type === 'error') {
          const err = new ApiError(event.message || 'Console sync failed', event.status || 0)
          ;(err as any).requests = event.requests
          throw err
        }
      } catch (e) {
        if (e instanceof ApiError) throw e
      }
    }

    if (done) break
  }

  if (buffer.trim()) {
    try {
      const event: BackendConsoleStreamEvent = JSON.parse(buffer.trim())
      if (event.type === 'request') onRequest(event.request)
      else if (event.type === 'workflow_log') options.onWorkflowLog?.(event.log)
      else if (event.type === 'complete') finalResponse = event.response
      else if (event.type === 'error') {
        const err = new ApiError(event.message || 'Console sync failed', event.status || 0)
        ;(err as any).requests = event.requests
        throw err
      }
    } catch (e) {
      if (e instanceof ApiError) throw e
    }
  }

  if (!finalResponse) throw new ApiError('Stream ended without response', 0)
  return finalResponse
}

export function recordBackendSyncSummary(total: number, successCount: number, failureCount: number) {
  return request<{ total: number; success_count: number; failure_count: number }>('/admin/api/backends/console/sync-summary', {
    method: 'POST',
    body: JSON.stringify({ total, success_count: successCount, failure_count: failureCount })
  })
}
