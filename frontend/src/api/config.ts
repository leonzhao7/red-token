const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export interface ConfigResponse {
  listen_addr: string
  db_path: string
  log_level: string
  backend_cooldown: string
  backend_fails: string
  backend_console_user_agent: string
  focus_models: string
  connect_timeout: string
  request_timeout: string
  shutdown_timeout: string
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

export const getConfig = () => request<ConfigResponse>('/admin/api/config')

export const updateConfig = (payload: Partial<Omit<ConfigResponse, 'listen_addr' | 'db_path'>>) =>
  request<ConfigResponse>('/admin/api/config', { method: 'PUT', body: JSON.stringify(payload) })

export const reloadConfig = () =>
  request<ConfigResponse>('/admin/api/config/reload', { method: 'POST' })
