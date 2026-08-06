const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export interface ClientKeyResponse {
  id: number
  name: string
  token: string
  masked_token: string
  allowed_models: string
  enabled: boolean
  usage_count: number
  last_used_at?: string
  created_at?: string
  token_input?: number
  token_output?: number
  req_success?: number
  req_fail?: number
}

export interface ClientKeyListItem extends ClientKeyResponse {
  usage_count: number
  last_used_at?: string
}

export interface ClientKeyWrite {
  name: string
  token: string
  allowed_models: string
  enabled: boolean
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

export function listClientKeys() {
  return request<PagedResponse<ClientKeyListItem>>('/admin/api/client-keys?page=1&limit=10000')
}

export function createClientKey(payload: ClientKeyWrite) {
  return request<{ client: ClientKeyResponse; issued_token: string }>('/admin/api/client-keys', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function updateClientKey(id: number, payload: ClientKeyWrite) {
  return request<{ client: ClientKeyResponse; issued_token: string }>(`/admin/api/client-keys/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function deleteClientKey(id: number) {
  return request<{ deleted: number }>(`/admin/api/client-keys/${id}`, { method: 'DELETE' })
}
