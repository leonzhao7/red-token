import { ref } from 'vue'

export interface ConsoleLogRow {
  id: string
  backendName: string
  time: string
  kind: 'request' | 'workflow'
  method?: string
  path: string
  statusCode: number | null
  body: string
  level?: string
  phase?: string
  stepId?: string
  message?: string
  durationMs?: number
}

/* ---- module-level reactive state ---- */
export const consoleLogVisible = ref(false)
export const consoleLogTitle = ref('')
export const consoleLogRows = ref<ConsoleLogRow[]>([])
export const consoleLogShowName = ref(false)
export const expandedLogRowIds = ref<Set<string>>(new Set())

let nextLogRowId = 0

/* ---- actions ---- */

/** Clear rows and open the panel (used by Relays sync/checkin) */
export function openConsoleLog(title: string) {
  consoleLogTitle.value = title
  consoleLogRows.value = []
  expandedLogRowIds.value = new Set()
  consoleLogShowName.value = false
  consoleLogVisible.value = true
}

/** Show without clearing (used by header icon) */
export function showConsoleLog() {
  consoleLogVisible.value = true
}

/** Hide without clearing */
export function hideConsoleLog() {
  consoleLogVisible.value = false
}

/** Toggle visibility (header icon) */
export function toggleConsoleLog() {
  consoleLogVisible.value = !consoleLogVisible.value
}

/** Append a log row */
export function appendLogRow(
  req: { time: string; method?: string; path: string; status_code: number; body: string },
  backendName: string
) {
  consoleLogRows.value.push({
    id: `log-${nextLogRowId++}`,
    backendName,
    time: formatLogTime(req.time),
    kind: 'request',
    method: req.method,
    path: req.path,
    statusCode: Number.isFinite(req.status_code) ? req.status_code : null,
    body: req.body || ''
  })
}

/** Append a workflow execution event to the same console activity stream. */
export function appendWorkflowLogRow(
  log: {
    time: string
    level: string
    step_id?: string
    phase: string
    message: string
    duration_ms?: number
    details?: Record<string, unknown>
  },
  backendName: string
) {
  consoleLogRows.value.push({
    id: `log-${nextLogRowId++}`,
    backendName,
    time: formatLogTime(log.time),
    kind: 'workflow',
    path: '',
    statusCode: null,
    body: log.details ? JSON.stringify(log.details) : '',
    level: log.level || 'info',
    phase: log.phase,
    stepId: log.step_id,
    message: log.message,
    durationMs: log.duration_ms
  })
}

/** Replace all rows */
export function setConsoleLogRows(rows: ConsoleLogRow[]) {
  consoleLogRows.value = rows
  expandedLogRowIds.value = new Set()
}

/** Clear all rows */
export function clearConsoleLogRows() {
  consoleLogRows.value = []
  expandedLogRowIds.value = new Set()
}

/** Toggle row expansion */
export function toggleLogRow(id: string) {
  const next = new Set(expandedLogRowIds.value)
  next.has(id) ? next.delete(id) : next.add(id)
  expandedLogRowIds.value = next
}

/* ---- formatting helpers ---- */

export function formatLogTime(value: string) {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value || new Date().toLocaleString() : parsed.toLocaleString()
}

export function formatLogBody(body: string) {
  try { return JSON.stringify(JSON.parse(body), null, 2) } catch { return body }
}

export function formatLogPreview(body: string) {
  const text = body ? body.replace(/\s+/g, ' ').trim() : ''
  return text.length <= 140 ? text : `${text.slice(0, 140)}...`
}

export function formatLogStatus(statusCode: number | null) {
  return statusCode && statusCode > 0 ? String(statusCode) : 'No response'
}

export function logStatusClass(statusCode: number | null) {
  if (!statusCode) return 'status-none'
  if (statusCode >= 200 && statusCode < 300) return 'status-success'
  if (statusCode >= 400) return 'status-error'
  return 'status-other'
}
