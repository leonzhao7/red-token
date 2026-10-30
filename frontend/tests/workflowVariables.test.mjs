import assert from 'node:assert/strict'
import test from 'node:test'

import { parseWorkflowValue, workflowVariablesFromRows, workflowVariablesToRows } from '../src/utils/workflowVariables.ts'

test('preserves global variable JSON types through an editor round trip', () => {
  const variables = {
    month: '{{runtime#/year_month}}',
    date: '{{runtime#/date}}',
    numericString: '123',
    nullString: 'null',
    quotedString: '"quoted"',
    whitespace: '  text  ',
    empty: '',
    amount: 10,
    enabled: true,
    nothing: null,
    items: [1, '2', false],
    nested: { value: '{{other#/value}}' }
  }
  assert.deepEqual(workflowVariablesFromRows(workflowVariablesToRows(variables)), variables)
})

test('accepts text templates and typed JSON values entered in the editor', () => {
  assert.equal(parseWorkflowValue('{{runtime#/date}}'), '{{runtime#/date}}')
  assert.equal(parseWorkflowValue('"001"'), '001')
  assert.equal(parseWorkflowValue(' 12 '), 12)
  assert.equal(parseWorkflowValue('null'), null)
  assert.deepEqual(parseWorkflowValue('[1,true]'), [1, true])
  assert.equal(parseWorkflowValue(''), '')
  assert.deepEqual(workflowVariablesFromRows([{ key: ' month ', value: '{{runtime#/year_month}}' }, { key: '', value: '' }]), {
    month: '{{runtime#/year_month}}'
  })
})

test('keeps prototype-like names as ordinary variable keys', () => {
  const result = workflowVariablesFromRows([{ key: '__proto__', value: '{"value":1}' }])
  assert.equal(Object.hasOwn(result, '__proto__'), true)
  assert.deepEqual(result.__proto__, { value: 1 })
  assert.equal(Object.getPrototypeOf(result), Object.prototype)
})
