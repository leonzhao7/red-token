export interface WorkflowSpecFeatures {
  globalHeaders?: boolean
  foreach?: boolean
  controlFlow?: boolean
}

export function selectWorkflowSpec(currentSpec: string, features: WorkflowSpecFeatures): string {
  let requiredVersion = 1
  if (features.controlFlow) requiredVersion = 2
  if (features.foreach) requiredVersion = 3
  if (features.globalHeaders) requiredVersion = 4

  const normalized = currentSpec.trim()
  const match = /^http-workflow\/v([1-4])$/.exec(normalized)
  if (!match) return normalized
  return `http-workflow/v${Math.max(Number(match[1]), requiredVersion)}`
}
