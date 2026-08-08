const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export interface DashboardSummary {
  cards: {
    backends: { count: number; enabled: number; failures: number }
    client_keys: { count: number; enabled: number }
    proxies: { count: number }
  }
  counts: { backends: number; client_keys: number; socks_proxies: number }
  growth: { requests: number; errors: number }
  status: { healthy_backends: number; recent_errors: number; active_clients: number }
  sparkline: Array<{ label: string; requests: number }>
}

export interface UsageBucket {
  label: string
  requests: number
  successes: number
  failures: number
  latency_ms: number
  traffic_bytes: number
  error_rate: number
}

export interface DashboardUsage {
  range: string
  series: UsageBucket[]
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

export interface HourlyModelStatsItem {
  backend_id: number
  backend: string
  model: string
  hour: string
  requests: number
  successes: number
  failures: number
  input_tokens: number
  output_tokens: number
  input_cache_tokens: number
  success_avg_duration_ms: number
  success_request_bytes: number
  success_response_bytes: number
}

export interface HourlyModelStatsResponse {
  query: {
    backend: string
    model: string
    start_hour: string
    end_hour: string
  }
  scope: {
    backends: Array<{ id: number; name: string }>
    models: string[]
    time_range: { start_hour: string; end_hour: string; timezone: string }
  }
  items: HourlyModelStatsItem[]
}

export const getDashboardSummary = () =>
  request<DashboardSummary>('/admin/api/dashboard/summary')

export const getDashboardUsage = (range: '24h' | '7d' | '30d' = '7d') =>
  request<DashboardUsage>(`/admin/api/dashboard/usage?range=${range}`)

export const getHourlyModelStats = (startHour: string, endHour: string, model?: string) => {
  const params = new URLSearchParams({ start_hour: startHour, end_hour: endHour })
  if (model) params.set('model', model)
  return request<HourlyModelStatsResponse>(`/admin/api/backend-hourly-model-stats?${params}`)
}
