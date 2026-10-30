export interface WorkflowVariableRow {
  key: string
  value: string
}

export function parseWorkflowValue(text: string): unknown {
  if (!text.trim()) return ''
  try {
    return JSON.parse(text.trim())
  } catch {
    return text
  }
}

export function workflowVariablesToRows(variables?: Record<string, unknown>): WorkflowVariableRow[] {
  return Object.entries(variables || {}).map(([key, value]) => ({ key, value: JSON.stringify(value) }))
}

export function workflowVariablesFromRows(rows: WorkflowVariableRow[]): Record<string, unknown> {
  return Object.fromEntries(rows.filter((row) => row.key.trim()).map((row) => [row.key.trim(), parseWorkflowValue(row.value)]))
}
