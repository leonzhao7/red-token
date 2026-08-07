import assert from 'node:assert/strict'
import test from 'node:test'
import { tieredExpressionPricing } from '../src/utils/tieredPricing.ts'

function assertPrice(actual: { input: number; output: number } | null, input: number, output: number) {
  assert.ok(actual)
  assert.ok(Math.abs(actual.input - input) < 1e-12, `expected input ${input}, got ${actual.input}`)
  assert.ok(Math.abs(actual.output - output) < 1e-12, `expected output ${output}, got ${actual.output}`)
}

test('uses the short tier when it is available', () => {
  const pricing = tieredExpressionPricing({
    billing_mode: 'tiered_expr',
    billing_expr: 'len <= 272000 ? tier("short", p * 1 + c * 6) : tier("long", p * 2 + c * 9)'
  }, 0.04, 1)

  assertPrice(pricing, 0.04, 0.24)
})

test('uses the first tier when no short tier exists', () => {
  const pricing = tieredExpressionPricing({
    billing_mode: 'tiered_expr',
    billing_expr: 'len <= 200000 ? tier("<=200k", p * 2 + cr * 0.5 + c * 6) : tier(">200k", p * 4 + c * 12)'
  }, 0.8, 1)

  assertPrice(pricing, 1.6, 4.8)
})
