import assert from 'node:assert/strict'
import test from 'node:test'

import { selectWorkflowSpec } from '../src/utils/workflowSpec.ts'

test('preserves existing workflow specs when no newer feature is used', () => {
  for (const version of [1, 2, 3, 4, 5]) {
    const spec = `http-workflow/v${version}`
    assert.equal(selectWorkflowSpec(spec, {}), spec)
  }
})

test('selects the minimum spec required by newly used features', () => {
  assert.equal(selectWorkflowSpec('http-workflow/v1', { controlFlow: true }), 'http-workflow/v2')
  assert.equal(selectWorkflowSpec('http-workflow/v1', { foreach: true }), 'http-workflow/v3')
  assert.equal(selectWorkflowSpec('http-workflow/v3', { globalHeaders: true }), 'http-workflow/v4')
})

test('selects v5 when step headers are used', () => {
  for (const version of [1, 2, 3, 4]) {
    assert.equal(selectWorkflowSpec(`http-workflow/v${version}`, { stepHeaders: true }), 'http-workflow/v5')
  }
  assert.equal(selectWorkflowSpec('http-workflow/v5', { stepHeaders: true }), 'http-workflow/v5')
  assert.equal(selectWorkflowSpec('http-workflow/v4', { stepHeaders: true, globalHeaders: true, foreach: true, controlFlow: true }), 'http-workflow/v5')
})

test('does not silently rewrite an unknown imported spec', () => {
  assert.equal(selectWorkflowSpec('vendor-workflow/v1', { globalHeaders: true }), 'vendor-workflow/v1')
})
