const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export interface WorkflowRequest {
  method: string
  path: string
  query?: Record<string, unknown>
  headers?: Record<string, unknown>
  body?: unknown
}

export interface WorkflowExtraction {
  alias: string
  expression: string
}

export interface WorkflowStatusRoute {
  statuses: number[]
  goto: string
}

export interface WorkflowExpect {
  routes?: WorkflowStatusRoute[]
  accepted_statuses?: number[]
}

export interface WorkflowWhen {
  expression: string
  goto: string
}

export interface WorkflowForeach {
  alias: string
  as: string
  index_as?: string
}

export interface WorkflowStep {
  id: string
  name: string
  foreach?: WorkflowForeach
  request: WorkflowRequest
  expect?: string | WorkflowExpect
  when?: WorkflowWhen
  extract?: WorkflowExtraction[]
}

export interface WorkflowDefinition {
  spec: string
  id: string
  name: string
  headers?: Record<string, unknown>
  steps: WorkflowStep[]
  output: unknown
}

export interface WorkflowRecord {
  id: string
  name: string
  definition: WorkflowDefinition
  created_at: string
  updated_at: string
}

export interface WorkflowRequestLog {
  time: string
  method: string
  path: string
  status_code: number
  body: string
}

export interface WorkflowDebugLog {
  time: string
  level: 'debug' | 'info' | 'warn' | 'error' | string
  step_id?: string
  step_name?: string
  phase: string
  message: string
  duration_ms?: number
  details?: Record<string, unknown>
}

export interface WorkflowExecuteResult {
  workflow_id: string
  backend: { id: number; name: string }
  output: unknown
  aliases: Record<string, unknown>
  executed_at: string
  requests: WorkflowRequestLog[]
  debug_logs: WorkflowDebugLog[]
}

export interface WorkflowResultSnapshot {
  workflow_id: string
  backend_id: number
  output: unknown
  executed_at: string
}

export interface WorkflowExecuteError {
  error?: { message?: string; type?: string }
  requests?: WorkflowRequestLog[]
  debug_logs?: WorkflowDebugLog[]
}

interface PagedResponse<T> {
  items: T[]
  total: number
  page: number
  limit: number
}

class ApiError extends Error {
  status: number
  requests?: WorkflowRequestLog[]
  debugLogs?: WorkflowDebugLog[]

  constructor(message: string, status: number, requests?: WorkflowRequestLog[], debugLogs?: WorkflowDebugLog[]) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.requests = requests
    this.debugLogs = debugLogs
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
    const requests =
      payload && typeof payload === 'object' && Array.isArray((payload as WorkflowExecuteError).requests)
        ? (payload as WorkflowExecuteError).requests
        : undefined
    const debugLogs =
      payload && typeof payload === 'object' && Array.isArray((payload as WorkflowExecuteError).debug_logs)
        ? (payload as WorkflowExecuteError).debug_logs
        : undefined
    const message =
      payload && typeof payload === 'object' && (payload as WorkflowExecuteError).error?.message
        ? (payload as WorkflowExecuteError).error!.message!
        : typeof payload === 'string' && payload.trim()
          ? payload.trim()
          : response.statusText || `请求失败（${response.status}）`
    throw new ApiError(message, response.status, requests, debugLogs)
  }
  return payload as T
}

export function listWorkflows() {
  return request<PagedResponse<WorkflowRecord>>('/admin/api/workflows?page=1&limit=10000')
}

export function getWorkflow(id: string) {
  return request<WorkflowRecord>(`/admin/api/workflows/${encodeURIComponent(id)}`)
}

export function createWorkflow(definition: WorkflowDefinition) {
  return request<WorkflowRecord>('/admin/api/workflows', {
    method: 'POST',
    body: JSON.stringify(definition)
  })
}

export function updateWorkflow(id: string, definition: WorkflowDefinition) {
  return request<WorkflowRecord>(`/admin/api/workflows/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(definition)
  })
}

export function deleteWorkflow(id: string) {
  return request<{ deleted: string }>(`/admin/api/workflows/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function executeWorkflow(id: string, payload: { backend_id: number; aliases?: Record<string, unknown> }) {
  return request<WorkflowExecuteResult>(`/admin/api/workflows/${encodeURIComponent(id)}/execute`, {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function getWorkflowResult(id: string, backendId: number) {
  return request<WorkflowResultSnapshot>(`/admin/api/workflows/${encodeURIComponent(id)}/results/${backendId}`)
}
