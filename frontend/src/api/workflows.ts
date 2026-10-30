import { request } from './http'

export interface WorkflowRequest {
  method: string
  path: string
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
  turnstile?: 'enable'
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
