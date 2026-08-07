function expressionCoefficient(expression: string, variable: string): number | null {
  const escaped = variable.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const number = '(-?\\d+(?:\\.\\d+)?)'
  const after = expression.match(new RegExp(`\\b${escaped}\\b\\s*\\*\\s*${number}`, 'i'))
  if (after) return Number(after[1])
  const before = expression.match(new RegExp(`${number}\\s*\\*\\s*\\b${escaped}\\b`, 'i'))
  if (before) return Number(before[1])
  return new RegExp(`\\b${escaped}\\b`, 'i').test(expression) ? 1 : null
}

export function tieredExpressionPricing(
  record: Record<string, unknown>,
  groupRatio: number,
  exchangeRate: number
): { input: number; output: number } | null {
  if (String(record.billing_mode || '').trim() !== 'tiered_expr') return null
  const expression = String(record.billing_expr || '')
  const shortTier = expression.match(/tier\s*\(\s*["']short["']\s*,\s*([^)]*)\)/i)?.[1]
  const firstTier = expression.match(/tier\s*\(\s*["'][^"']+["']\s*,\s*([^)]*)\)/i)?.[1]
  const tier = shortTier || firstTier
  if (!tier) return null

  const inputCoefficient = expressionCoefficient(tier, 'p')
  const outputCoefficient = expressionCoefficient(tier, 'c')
  if (inputCoefficient === null || outputCoefficient === null) return null
  return {
    input: groupRatio * inputCoefficient * exchangeRate,
    output: groupRatio * outputCoefficient * exchangeRate
  }
}

export function fixedRequestPricing(
  modelPrice: number,
  groupRatio: number
): number {
  if (!Number.isFinite(modelPrice) || modelPrice < 0) return 0
  if (!Number.isFinite(groupRatio) || groupRatio < 0) return 0
  return modelPrice * groupRatio
}
