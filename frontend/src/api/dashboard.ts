import { request } from './http'

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
