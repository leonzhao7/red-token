import { request } from './http'

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
  cdp_address: string
  health_check_interval: string
  health_check_backends: string
  scheduler_user_isolation: string
  scheduler_user_affinity_ttl: string
}

export const getConfig = () => request<ConfigResponse>('/admin/api/config')

export const updateConfig = (payload: Partial<Omit<ConfigResponse, 'listen_addr' | 'db_path'>>) =>
  request<ConfigResponse>('/admin/api/config', { method: 'PUT', body: JSON.stringify(payload) })

export const reloadConfig = () =>
  request<ConfigResponse>('/admin/api/config/reload', { method: 'POST' })
