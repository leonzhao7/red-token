<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import {
  Workflow,
  Play,
  Pencil,
  Trash2,
  Plus,
  Save,
  LoaderCircle,
  AlertTriangle,
  Sparkles,
  Braces,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  ChevronRight,
  ArrowUp,
  ArrowDown,
  X,
  ListPlus,
  Download,
  Upload,
  ChevronsDownUp,
  ChevronsUpDown
} from 'lucide-vue-next'
import { toast } from '../composables/toast'
import Modal from '../components/Modal.vue'
import {
  listWorkflows,
  createWorkflow,
  updateWorkflow,
  deleteWorkflow,
  executeWorkflow,
  type WorkflowRecord,
  type WorkflowDefinition,
  type WorkflowStep,
  type WorkflowExecuteResult,
  type WorkflowRequestLog,
  type WorkflowDebugLog
} from '../api/workflows'
import { listBackends, type BackendResponse } from '../api/backends'

const loading = ref(true)
const loadError = ref('')
const workflows = ref<WorkflowRecord[]>([])

interface KvRow {
  key: string
  value: string
}
interface ExtractRow {
  alias: string
  expression: string
}
interface ExpectRouteRow {
  statuses: string
  goto: string
}
interface StepForm {
  id: string
  name: string
  foreachAlias: string
  foreachAs: string
  foreachIndexAs: string
  method: string
  path: string
  body: string
  when: string
  whenGoto: string
  expectRoutes: ExpectRouteRow[]
  legacyExpect: string
  extract: ExtractRow[]
}

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']
const WORKFLOW_ID_RE = /^[a-z][a-z0-9_-]{0,63}$/
const ALIAS_RE = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/
const HEADER_NAME_RE = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/
const WORKFLOW_OUTPUT_FIELDS = ['api_keys', 'models', 'quota', 'quota_unit', 'today_reward', 'used_quota', 'user_id', 'username']

const showForm = ref(false)
const isEditing = ref(false)
const saving = ref(false)
const formError = ref('')
const stepOpen = ref<boolean[]>([])

const form = reactive<{ name: string; id: string; headers: KvRow[]; steps: StepForm[]; output: string }>(freshForm())

function freshForm() {
  return { name: '', id: '', headers: [], steps: [emptyStep()], output: '{}' }
}

function emptyStep(): StepForm {
  return {
    id: '',
    name: '',
    foreachAlias: '',
    foreachAs: '',
    foreachIndexAs: '',
    method: 'GET',
    path: '',
    body: '',
    when: '',
    whenGoto: '',
    expectRoutes: [],
    legacyExpect: '',
    extract: [emptyExtract()]
  }
}

function emptyKv(): KvRow {
  return { key: '', value: '' }
}

function emptyExtract(): ExtractRow {
  return { alias: '', expression: '' }
}

const SAMPLE_DEFINITION = `{
  "spec": "http-workflow/v4",
  "id": "sub2api-default-checkin-profile",
  "name": "sub2api 默认签到",
  "headers": {},
  "steps": [
    {
      "id": "get_me",
      "name": "获取用户信息",
      "request": {
        "method": "GET",
        "path": "/api/v1/auth/me",
        "query": {},
        "headers": {}
      },
      "extract": [
        { "alias": "user_id", "expression": ".data.id | tostring" },
        { "alias": "username", "expression": ".data.email // .data.username // \\"\\"" },
        { "alias": "quota", "expression": "(.data.quota // .data.free_balance // .data.balance // 0) | tonumber" },
        { "alias": "quota_unit", "expression": "(.data.quota_unit // .data.quota_display_type // \\"$\\") | tostring" },
        { "alias": "today_reward", "expression": "(.data.today_reward // .data.checkin_reward // 0) | tonumber" }
      ]
    },
    {
      "id": "get_keys",
      "name": "获取 API Key 列表",
      "request": {
        "method": "GET",
        "path": "/api/v1/keys",
        "query": { "page": 1, "page_size": 100, "scope": "personal" },
        "headers": {}
      },
      "extract": [
        {
          "alias": "api_keys_base",
          "expression": "[.data.items[] | {id: (.id | tostring), name: (.name // \\"\\"), key: (.key // \\"\\"), group: (.group.name // .group // \\"default\\"), used_quota: ((.used_quota // 0) | tonumber)}]"
        },
        {
          "alias": "key_ids",
          "expression": "$vars.api_keys_base | map(.id)"
        }
      ]
    },
    {
      "id": "get_key_usage",
      "name": "获取 API Key 用量",
      "request": {
        "method": "POST",
        "path": "/api/v1/usage/dashboard/api-keys-usage",
        "query": {},
        "headers": {},
        "body": { "api_key_ids": "{{key_ids}}" }
      },
      "extract": [
        {
          "alias": "api_keys",
          "expression": "$vars.api_keys_base | map(. as $key | $key + {used_quota: (($response.body.data.stats[($key.id | tostring)].used_quota // $response.body.data.stats[($key.id | tostring)].total_actual_cost // 0) | tonumber)})"
        },
        {
          "alias": "used_quota",
          "expression": "$vars.api_keys | map(.used_quota) | add // 0"
        }
      ]
    },
    {
      "id": "get_models",
      "name": "获取模型定价",
      "request": {
        "method": "GET",
        "path": "/api/v1/models",
        "query": {},
        "headers": {}
      },
      "extract": [
        {
          "alias": "group_ratios",
          "expression": ".group_ratio // {}"
        },
        {
          "alias": "model_rows",
          "expression": "[.data[] | ((.price_type // .quota_type // 0) | tonumber) as $price_type | {name: (.model_name // .name), groups: (.enable_groups // []), in_price: ((if $price_type == 1 then (.price // .model_price // 0) else (.input_price // 0) end) | tonumber), out_price: ((if $price_type == 1 then (.price // .model_price // 0) else (.output_price // 0) end) | tonumber), price_type: $price_type}]"
        },
        {
          "alias": "models",
          "expression": "$vars.model_rows | map(. as $model | [$model.groups[] | {name: ., ratio: ($vars.group_ratios[.] // 1)}] as $groups | ($groups | map(.ratio) | min // null) as $min_ratio | {name: $model.name, cheapest_groups: (if $min_ratio == null then [] else [$groups[] | select(.ratio == $min_ratio) | .name] end), in_price: $model.in_price, out_price: $model.out_price, price_type: $model.price_type})"
        }
      ]
    }
  ],
  "output": {
    "user_id": "{{user_id}}",
    "username": "{{username}}",
    "quota": "{{quota}}",
    "quota_unit": "{{quota_unit}}",
    "used_quota": "{{used_quota}}",
    "today_reward": "{{today_reward}}",
    "api_keys": "{{api_keys}}",
    "models": "{{models}}"
  }
}`

const activeEditor = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)
const availableAliases = computed(() => {
  const seen: string[] = [
    'runtime#/username',
    'runtime#/password',
    'runtime#/user_id',
    'runtime#/headers'
  ]
  for (const st of form.steps) {
    for (const ex of st.extract) {
      const alias = ex.alias.trim()
      if (alias && !seen.includes(alias)) seen.push(alias)
    }
  }
  return seen
})

const globalHeaderCount = computed(() => form.headers.filter((h) => h.key.trim()).length)

function onFocus(e: FocusEvent) {
  activeEditor.value = e.target as HTMLInputElement | HTMLTextAreaElement
}

function insertAlias(alias: string) {
  const el = activeEditor.value
  if (!el) return
  const start = el.selectionStart ?? el.value.length
  const end = el.selectionEnd ?? start
  const text = `{{${alias}}}`
  el.setRangeText(text, start, end, 'end')
  el.dispatchEvent(new Event('input'))
  el.focus()
}

function defToForm(def: WorkflowDefinition) {
  form.name = def.name
  form.id = def.id
  form.headers = objToKv(def.headers)
  form.steps = def.steps.map((step) => ({
    id: step.id,
    name: step.name,
    foreachAlias: step.foreach?.alias || '',
    foreachAs: step.foreach?.as || '',
    foreachIndexAs: step.foreach?.index_as || '',
    method: step.request.method,
    path: step.request.path,
    body: 'body' in step.request ? JSON.stringify(step.request.body, null, 2) : '',
    when: step.when?.expression || '',
    whenGoto: step.when?.goto || '',
    expectRoutes: typeof step.expect === 'object'
      ? (step.expect.routes || []).map((route) => ({ statuses: route.statuses.join(','), goto: route.goto }))
      : [],
    legacyExpect: typeof step.expect === 'string' ? step.expect : '',
    extract: (step.extract || []).map((e) => ({ alias: e.alias, expression: e.expression }))
  }))
  form.output = JSON.stringify(def.output, null, 2)
}

function objToKv(obj: Record<string, unknown> | undefined): KvRow[] {
  if (!obj || !Object.keys(obj).length) return []
  return Object.entries(obj).map(([key, value]) => ({ key, value: valueToStr(value) }))
}

function valueToStr(value: unknown): string {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : JSON.stringify(value)
}

function openCreate() {
  isEditing.value = false
  formError.value = ''
  Object.assign(form, freshForm())
  stepOpen.value = [true]
  showForm.value = true
}

function openEdit(record: WorkflowRecord) {
  isEditing.value = true
  formError.value = ''
  defToForm(record.definition)
  stepOpen.value = form.steps.map(() => false)
  showForm.value = true
}

function fillExample() {
  try {
    const def = JSON.parse(SAMPLE_DEFINITION) as WorkflowDefinition
    defToForm(def)
    stepOpen.value = form.steps.map(() => false)
    formError.value = ''
  } catch (e: any) {
    formError.value = '内置示例解析失败：' + (e?.message || '')
  }
}

function exportWorkflow(record: WorkflowRecord) {
  const json = JSON.stringify(record.definition, null, 2)
  const blob = new Blob([json], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${record.id}.json`
  a.click()
  URL.revokeObjectURL(url)
}

const importFileInput = ref<HTMLInputElement | null>(null)

function triggerImport() {
  importFileInput.value?.click()
}

function handleImportFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const def = JSON.parse(e.target?.result as string) as WorkflowDefinition
      if (!def.spec || !def.id || !def.steps) throw new Error('不是合法的工作流 JSON')
      isEditing.value = false
      formError.value = ''
      defToForm(def)
      stepOpen.value = form.steps.map(() => false)
      showForm.value = true
    } catch (err: any) {
      toast('导入失败', err?.message || '解析 JSON 失败', 'danger')
    }
  }
  reader.readAsText(file)
  ;(event.target as HTMLInputElement).value = ''
}

function formatJson() {
  try {
    form.output = JSON.stringify(JSON.parse(form.output), null, 2)
    formError.value = ''
  } catch (e: any) {
    formError.value = 'Output JSON 格式错误'
  }
}

function addStep() {
  form.steps.push(emptyStep())
  stepOpen.value.push(true)
}

function expandAll() {
  stepOpen.value = form.steps.map(() => true)
}

function collapseAll() {
  stepOpen.value = form.steps.map(() => false)
}

function methodTone(method: string): string {
  switch (method.trim().toUpperCase()) {
    case 'GET':
      return 'cyan'
    case 'POST':
      return 'violet'
    case 'PUT':
    case 'PATCH':
      return 'amber'
    case 'DELETE':
      return 'rose'
    default:
      return 'blue'
  }
}

function removeStep(index: number) {
  form.steps.splice(index, 1)
  stepOpen.value.splice(index, 1)
}

function moveStep(index: number, delta: number) {
  const target = index + delta
  if (target < 0 || target >= form.steps.length) return
  const [step] = form.steps.splice(index, 1)
  form.steps.splice(target, 0, step)
  const [open] = stepOpen.value.splice(index, 1)
  stepOpen.value.splice(target, 0, open)
}

function addKv(rows: KvRow[]) {
  rows.push(emptyKv())
}

function removeKv(rows: KvRow[], index: number) {
  rows.splice(index, 1)
}

function addExtract(step: StepForm) {
  step.extract.push(emptyExtract())
}

function addExpectRoute(step: StepForm) {
  step.expectRoutes.push({ statuses: '', goto: '' })
}

function removeExpectRoute(step: StepForm, index: number) {
  step.expectRoutes.splice(index, 1)
}

function removeExtract(step: StepForm, index: number) {
  step.extract.splice(index, 1)
}

function methodOptions() {
  const set = [...HTTP_METHODS]
  for (const st of form.steps) {
    const m = st.method.trim().toUpperCase()
    if (m && !set.includes(m)) set.push(m)
  }
  return set
}

function kvToObj(rows: KvRow[]): Record<string, unknown> {
  const obj: Record<string, unknown> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (!key) continue
    obj[key] = parseKvValue(row.value)
  }
  return obj
}

function parseKvValue(text: string): unknown {
  const trimmed = text.trim()
  if (trimmed === '') return ''
  try {
    return JSON.parse(trimmed)
  } catch {
    return text
  }
}

function validateHeaderRows(rows: KvRow[], label: string, errors: string[]) {
  const names = new Set<string>()
  for (const row of rows) {
    const name = row.key.trim()
    if (!name) continue
    if (!HEADER_NAME_RE.test(name)) errors.push(`${label}：Header 名称「${name}」格式非法`)
    const normalized = name.toLowerCase()
    if (names.has(normalized)) errors.push(`${label}：Header 名称「${name}」重复`)
    names.add(normalized)
  }
}

function validateForm(): string[] {
  const errors: string[] = []
  if (!form.id.trim()) errors.push('工作流 ID 不能为空')
  else if (!WORKFLOW_ID_RE.test(form.id.trim())) errors.push('ID 必须匹配 ^[a-z][a-z0-9_-]{0,63}$')
  if (!form.name.trim()) errors.push('名称不能为空')

  validateHeaderRows(form.headers, '全局 Headers', errors)

  const stepIds = new Set<string>()
  form.steps.forEach((st, i) => {
    const label = `第 ${i + 1} 步` + (st.name.trim() ? `（${st.name.trim()}）` : '')
    if (!st.id.trim()) errors.push(`${label}：步骤 ID 不能为空`)
    else if (!WORKFLOW_ID_RE.test(st.id.trim())) errors.push(`${label}：步骤 ID 必须匹配 ^[a-z][a-z0-9_-]{0,63}$`)
    else if (stepIds.has(st.id.trim())) errors.push(`${label}：步骤 ID「${st.id.trim()}」重复`)
    else stepIds.add(st.id.trim())
    if (!st.name.trim()) errors.push(`${label}：步骤名称不能为空`)
    const foreachAlias = st.foreachAlias.trim()
    const foreachAs = st.foreachAs.trim()
    const foreachIndexAs = st.foreachIndexAs.trim()
    if (foreachAlias || foreachAs || foreachIndexAs) {
      if (!foreachAlias) errors.push(`${label}：Foreach 缺少来源 Alias`)
      else if (!ALIAS_RE.test(foreachAlias)) errors.push(`${label}：Foreach 来源 Alias「${foreachAlias}」格式非法`)
      if (!foreachAs) errors.push(`${label}：Foreach 缺少元素 Alias`)
      else if (!ALIAS_RE.test(foreachAs)) errors.push(`${label}：Foreach 元素 Alias「${foreachAs}」格式非法`)
      if (foreachIndexAs && !ALIAS_RE.test(foreachIndexAs)) errors.push(`${label}：Foreach 下标 Alias「${foreachIndexAs}」格式非法`)
      if (foreachAlias && (foreachAlias === foreachAs || foreachAlias === foreachIndexAs)) errors.push(`${label}：Foreach 来源 Alias 不能与迭代 Alias 相同`)
      if (foreachIndexAs && foreachAs === foreachIndexAs) errors.push(`${label}：Foreach 元素 Alias 和下标 Alias 不能相同`)
    }
    if (!st.path.trim()) errors.push(`${label}：path 不能为空`)
    else if (!st.path.trim().startsWith('/')) errors.push(`${label}：path 必须以 / 开头`)
    if (st.body.trim()) {
      try {
        JSON.parse(st.body)
      } catch {
        errors.push(`${label}：Body 不是合法 JSON`)
      }
    }
    const when = st.when.trim()
    const whenGoto = st.whenGoto.trim()
    if ((when === '') !== (whenGoto === '')) errors.push(`${label}：When 条件和跳转目标必须同时配置`)
    if (whenGoto && !form.steps.some((candidate) => candidate.id.trim() === whenGoto)) {
      errors.push(`${label}：When 跳转目标步骤「${whenGoto}」不存在`)
    }
    st.expectRoutes.forEach((route, routeIndex) => {
      const routeLabel = `${label}：Expect 路由 ${routeIndex + 1}`
      const statuses = route.statuses.split(',').map((value) => value.trim()).filter(Boolean)
      if (!statuses.length) errors.push(`${routeLabel} 缺少状态码`)
      else if (statuses.some((value) => !/^\d+$/.test(value) || Number(value) < 100 || Number(value) > 599)) errors.push(`${routeLabel} 包含非法 HTTP 状态码`)
      if (!route.goto.trim()) errors.push(`${routeLabel} 缺少跳转目标`)
      else if (!form.steps.some((candidate) => candidate.id.trim() === route.goto.trim())) errors.push(`${routeLabel} 目标步骤「${route.goto.trim()}」不存在`)
    })
    if (st.legacyExpect) errors.push(`${label}：该步骤仍使用 v1 字符串 Expect，请删除旧表达式并迁移为 v3 状态码路由后保存`)
    const extractAliases = new Set<string>()
    st.extract.forEach((ex, j) => {
      if (!ex.alias.trim()) {
        errors.push(`${label}：第 ${j + 1} 条提取缺少别名`)
        return
      }
      if (!ALIAS_RE.test(ex.alias.trim())) errors.push(`${label}：提取别名「${ex.alias.trim()}」格式非法，需匹配 ^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
      if ((foreachAs && ex.alias.trim() === foreachAs) || (foreachIndexAs && ex.alias.trim() === foreachIndexAs)) {
        errors.push(`${label}：提取别名「${ex.alias.trim()}」与 Foreach 迭代 Alias 冲突`)
      }
      if (foreachAlias && extractAliases.has(ex.alias.trim())) errors.push(`${label}：Foreach 的提取别名「${ex.alias.trim()}」重复`)
      extractAliases.add(ex.alias.trim())
      if (!ex.expression.trim()) errors.push(`${label}：别名「${ex.alias.trim()}」缺少 jq 表达式`)
    })
  })
  try {
    const output = JSON.parse(form.output)
    if (!output || typeof output !== 'object' || Array.isArray(output)) {
      errors.push('Output 必须是对象')
    } else {
      const fields = Object.keys(output).sort()
      const missing = WORKFLOW_OUTPUT_FIELDS.filter((field) => !fields.includes(field))
      const unknown = fields.filter((field) => !WORKFLOW_OUTPUT_FIELDS.includes(field))
      if (missing.length) errors.push(`Output 缺少固定字段：${missing.join(', ')}`)
      if (unknown.length) errors.push(`Output 包含不支持的字段：${unknown.join(', ')}`)
    }
  } catch {
    errors.push('Output 不是合法 JSON')
  }
  return errors
}

function formToDef(): WorkflowDefinition {
  const steps: WorkflowStep[] = form.steps.map((st) => {
    const request: WorkflowStep['request'] = {
      method: st.method.trim().toUpperCase() || 'GET',
      path: st.path.trim()
    }
    if (st.body.trim()) {
      request.body = JSON.parse(st.body)
    }
    const step: WorkflowStep = {
      id: st.id.trim(),
      name: st.name.trim(),
      request: request as WorkflowStep['request']
    }
    const foreachAlias = st.foreachAlias.trim()
    const foreachAs = st.foreachAs.trim()
    const foreachIndexAs = st.foreachIndexAs.trim()
    if (foreachAlias && foreachAs) {
      step.foreach = { alias: foreachAlias, as: foreachAs }
      if (foreachIndexAs) step.foreach.index_as = foreachIndexAs
    }
    const when = st.when.trim()
    const whenGoto = st.whenGoto.trim()
    if (when && whenGoto) {
      step.when = { expression: when, goto: whenGoto }
    }
    const routes = st.expectRoutes
      .filter((route) => route.statuses.trim() && route.goto.trim())
      .map((route) => ({
        statuses: route.statuses.split(',').map((value) => Number(value.trim())).filter((value) => Number.isInteger(value)),
        goto: route.goto.trim()
      }))
    if (routes.length) {
      step.expect = { routes }
    }
    const extract = st.extract.filter((e) => e.alias.trim()).map((e) => ({ alias: e.alias.trim(), expression: e.expression.trim() }))
    if (extract.length) step.extract = extract
    return step
  })
  const definition: WorkflowDefinition = {
    spec: 'http-workflow/v4',
    id: form.id.trim(),
    name: form.name.trim(),
    steps,
    output: JSON.parse(form.output)
  }
  const globalHeaders = kvToObj(form.headers)
  if (Object.keys(globalHeaders).length) definition.headers = globalHeaders
  return definition
}

async function save() {
  const errors = validateForm()
  if (errors.length) {
    formError.value = errors.join('；')
    return
  }
  formError.value = ''
  saving.value = true
  try {
    const definition = formToDef()
    if (isEditing.value) {
      const record = await updateWorkflow(form.id, definition)
      const idx = workflows.value.findIndex((w) => w.id === record.id)
      if (idx >= 0) workflows.value.splice(idx, 1, record)
      toast('工作流已更新', record.name, 'success')
    } else {
      const record = await createWorkflow(definition)
      workflows.value.unshift(record)
      toast('工作流已创建', record.name, 'success')
    }
    showForm.value = false
  } catch (e: any) {
    toast('保存失败', e?.message || '', 'danger')
  } finally {
    saving.value = false
  }
}

const showExecute = ref(false)
const executingWorkflow = ref<WorkflowRecord | null>(null)
const backends = ref<BackendResponse[]>([])
const selectedBackendId = ref<number | 0>(0)
const aliasesText = ref('')
const executing = ref(false)
const executeResult = ref<WorkflowExecuteResult | null>(null)
const executeError = ref('')
const executeRequests = ref<WorkflowRequestLog[]>([])
const executeDebugLogs = ref<WorkflowDebugLog[]>([])

async function openExecute(record: WorkflowRecord) {
  executingWorkflow.value = record
  selectedBackendId.value = 0
  aliasesText.value = ''
  executeResult.value = null
  executeError.value = ''
  executeRequests.value = []
  executeDebugLogs.value = []
  showExecute.value = true
  try {
    if (!backends.value.length) {
      const page = await listBackends()
      backends.value = page.items.filter((b) => b.console_url && b.console_url.trim())
    }
  } catch {}
}

const selectedBackend = computed(() => backends.value.find((b) => b.id === selectedBackendId.value))

async function runExecute() {
  const wf = executingWorkflow.value
  if (!wf || !selectedBackendId.value) return
  executeResult.value = null
  executeError.value = ''
  executeRequests.value = []
  executeDebugLogs.value = []
  executing.value = true
  let aliases: Record<string, unknown> | undefined
  if (aliasesText.value.trim()) {
    try {
      aliases = JSON.parse(aliasesText.value)
    } catch (e: any) {
      toast('输入别名解析失败', e?.message || '', 'danger')
      executing.value = false
      return
    }
  }
  try {
    const result = await executeWorkflow(wf.id, {
      backend_id: selectedBackendId.value,
      aliases
    })
    executeResult.value = result
    executeRequests.value = result.requests || []
    executeDebugLogs.value = result.debug_logs || []
    toast('执行成功', `${result.backend.name} · ${new Date(result.executed_at).toLocaleString('zh-CN')}`, 'success')
  } catch (e: any) {
    executeError.value = e?.message || '执行失败'
    executeRequests.value = e?.requests || []
    executeDebugLogs.value = e?.debugLogs || []
    toast('执行失败', executeError.value, 'danger')
  } finally {
    executing.value = false
  }
}

const workflowToDelete = ref<WorkflowRecord | null>(null)

async function doDelete() {
  const wf = workflowToDelete.value
  if (!wf) return
  try {
    await deleteWorkflow(wf.id)
    workflows.value = workflows.value.filter((w) => w.id !== wf.id)
    workflowToDelete.value = null
    toast('工作流已删除', wf.name, 'success')
  } catch (e: any) {
    toast('删除失败', e?.message || '', 'danger')
  }
}

function fmtTime(s: string) {
  return new Date(s).toLocaleString('zh-CN', { hour12: false })
}

function statusClass(code: number) {
  if (code >= 200 && code < 300) return 'ok'
  if (code >= 300 && code < 400) return 'info'
  if (code >= 400 && code < 500) return 'warn'
  return 'err'
}

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    const page = await listWorkflows()
    workflows.value = page.items
  } catch (e: any) {
    loadError.value = e?.message || '加载工作流失败'
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
</script>

<template>
  <div class="page stagger">
    <section class="toolbar panel">
      <div class="wf-title-row">
        <div class="wf-title-icon"><Workflow :size="16" /></div>
        <div>
          <div class="wf-title">HTTP 工作流配置</div>
          <div class="wf-sub">按 docs/http_workflow.md 编排控制台请求，生成 profile 业务快照；可对任意中转站执行并持久化结果</div>
        </div>
      </div>
      <div class="spacer"></div>
      <button class="btn btn-ghost" @click="triggerImport"><Upload :size="15" /> 导入</button>
      <button class="btn btn-primary" @click="openCreate"><Plus :size="15" /> 新建 Profile</button>
      <input ref="importFileInput" type="file" accept=".json,application/json" style="display:none" @change="handleImportFile" />
    </section>

    <section class="panel wf-list">
      <div v-if="loading" class="wf-state"><LoaderCircle :size="20" class="spin" /><span>正在加载工作流…</span></div>
      <div v-else-if="loadError" class="wf-state error">
        <AlertTriangle :size="20" />
        <span>{{ loadError }}</span>
        <button class="btn btn-ghost btn-sm" @click="loadData">重试</button>
      </div>
      <div v-else-if="!workflows.length" class="wf-state">
        <Workflow :size="20" />
        <span>还没有配置工作流，点击右上角「新建 Profile」开始</span>
      </div>
      <template v-else>
        <div v-for="w in workflows" :key="w.id" class="wf-row">
          <div class="wf-row-ico"><Workflow :size="16" /></div>
          <div class="wf-row-main">
            <div class="wf-row-name">{{ w.name }}</div>
            <div class="wf-row-meta">
              <span class="mono">{{ w.id }}</span>
              <span class="sep">·</span>
              <span>{{ w.definition.steps.length }} 步</span>
              <span class="sep">·</span>
              <span>更新于 {{ fmtTime(w.updated_at) }}</span>
            </div>
          </div>
          <div class="wf-row-actions">
            <button class="btn btn-ghost btn-sm btn-purple" @click="openExecute(w)">
              <Play :size="13" /> 执行
            </button>
            <button class="icon-btn" title="导出" @click="exportWorkflow(w)"><Download :size="15" /></button>
            <button class="icon-btn" title="编辑" @click="openEdit(w)"><Pencil :size="15" /></button>
            <button class="icon-btn wf-del" title="删除" @click="workflowToDelete = w"><Trash2 :size="15" /></button>
          </div>
        </div>
      </template>
    </section>

    <!-- create / edit -->
    <Modal
      :open="showForm"
      :title="isEditing ? '编辑工作流' : '新建工作流'"
      :subtitle="isEditing ? '修改配置后保存，立即生效' : '创建结构化 HTTP 工作流 profile'"
      :icon="Workflow"
      width="960px"
      @close="showForm = false"
    >
      <div class="wf-form">
        <div class="wf-basic">
          <div class="field">
            <label class="field-label">名称</label>
            <input v-model="form.name" class="input" placeholder="例如：sub2api 默认签到" />
          </div>
          <div class="field">
            <label class="field-label">ID <em class="wf-hint">稳定标识，创建后不可修改</em></label>
            <input v-model="form.id" class="input mono" :disabled="isEditing" placeholder="sub2api-default-checkin-profile" spellcheck="false" />
          </div>
        </div>

        <div v-if="formError" class="wf-form-error"><AlertTriangle :size="14" /><span>{{ formError }}</span></div>

        <div class="wf-section wf-sec-headers">
          <div class="wf-sec-head wf-section-head">
            <span class="wf-sec-title">全局 Headers <em class="wf-hint">附加到每个步骤请求</em></span>
            <span v-if="globalHeaderCount" class="wf-adv-count">{{ globalHeaderCount }}</span>
            <div class="spacer"></div>
            <button class="btn btn-ghost btn-sm" @click="addKv(form.headers)"><Plus :size="12" /> 添加 Header</button>
          </div>
          <div class="wf-section-body">
            <div v-if="form.headers.length" class="wf-kv">
              <div v-for="(header, headerIndex) in form.headers" :key="headerIndex" class="wf-kv-row">
                <input v-model="header.key" class="input mono wf-kv-key" placeholder="Header 名称" spellcheck="false" />
                <input v-model="header.value" class="input mono" placeholder="值，可引用" spellcheck="false" @focus="onFocus" />
                <button class="icon-btn wf-del" title="移除" @click="removeKv(form.headers, headerIndex)"><X :size="13" /></button>
              </div>
            </div>
          </div>
        </div>

        <div class="wf-section wf-sec-steps">
          <div class="wf-sec-head wf-section-head">
            <span class="wf-sec-title">步骤编排</span>
            <em v-if="form.steps.length" class="wf-hint">{{ form.steps.length }} 个</em>
            <div class="spacer"></div>
            <button class="btn btn-ghost btn-sm" @click="expandAll"><ChevronsDownUp :size="13" /> 展开全部</button>
            <button class="btn btn-ghost btn-sm" @click="collapseAll"><ChevronsUpDown :size="13" /> 收起全部</button>
            <button class="btn btn-ghost btn-sm" @click="fillExample"><Sparkles :size="13" /> 填充示例</button>
            <button class="btn btn-primary btn-sm" @click="addStep"><Plus :size="13" /> 添加步骤</button>
          </div>
          <div class="wf-section-body">
            <div v-if="availableAliases.length" class="wf-alias-bar">
              <span class="wf-alias-label">引用</span>
              <button v-for="alias in availableAliases" :key="alias" class="wf-alias" @click="insertAlias(alias)">
                {{ alias }}
              </button>
              <span class="wf-alias-tip">点击插入</span>
            </div>

            <div v-for="(st, i) in form.steps" :key="i" class="wf-step" :class="{ open: stepOpen[i] }">
          <div class="wf-step-head" @click="stepOpen[i] = !stepOpen[i]">
            <span class="wf-step-idx mono">{{ String(i + 1).padStart(2, '0') }}</span>
            <span class="tag mono wf-method-tag" :class="methodTone(st.method)">{{ st.method || 'GET' }}</span>
            <span class="mono wf-step-path">{{ st.path || '未设置路径' }}</span>
            <div class="wf-extract-chips">
              <template v-if="st.extract.length">
                <span v-for="(ex, ei) in st.extract" :key="ei" class="wf-chip">{{ ex.alias.trim() || '未命名' }}</span>
              </template>
              <span v-else class="wf-chip empty">未配置提取</span>
            </div>
            <button class="icon-btn" title="上移" @click.stop="moveStep(i, -1)"><ArrowUp :size="14" /></button>
            <button class="icon-btn" title="下移" @click.stop="moveStep(i, 1)"><ArrowDown :size="14" /></button>
            <button class="icon-btn wf-del" title="删除步骤" @click.stop="removeStep(i)"><Trash2 :size="14" /></button>
            <button class="icon-btn" :title="stepOpen[i] ? '折叠' : '展开'">
              <ChevronUp v-if="stepOpen[i]" :size="15" />
              <ChevronDown v-else :size="15" />
            </button>
          </div>

          <div v-if="stepOpen[i]" class="wf-step-body">
            <div class="wf-grid-2">
              <div class="field">
                <label class="field-label">步骤 ID</label>
                <input v-model="st.id" class="input mono" placeholder="get_me" spellcheck="false" />
              </div>
              <div class="field">
                <label class="field-label">请求 <em class="wf-hint">相对路径，支持引用</em></label>
                <div class="wf-req-line">
                  <select v-model="st.method" class="select wf-method-select">
                    <option v-for="m in methodOptions()" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-model="st.path" class="input mono" placeholder="/api/v1/auth/me" spellcheck="false" @focus="onFocus" />
                </div>
              </div>
            </div>

            <div class="wf-extract">
              <div class="wf-sec-head sub">
                <span class="wf-sec-title">Extract 提取 <em class="wf-hint">响应值保存为 alias</em></span>
                <div class="spacer"></div>
                <button class="btn btn-ghost btn-sm" @click="addExtract(st)"><ListPlus :size="13" /> 添加提取</button>
              </div>
              <div v-if="!st.extract.length" class="wf-extract-empty">未配置提取</div>
              <div v-for="(ex, ei) in st.extract" :key="ei" class="wf-extract-row">
                <input v-model="ex.alias" class="input mono wf-extract-alias" placeholder="alias" spellcheck="false" />
                <input
                  v-model="ex.expression"
                  class="input mono wf-extract-expr"
                  placeholder=".data.id | tostring"
                  spellcheck="false"
                  @focus="onFocus"
                />
                <button class="icon-btn wf-del" title="移除" @click="removeExtract(st, ei)"><X :size="13" /></button>
              </div>
            </div>

            <details class="wf-adv">
              <summary class="wf-adv-summary"><ChevronRight :size="14" class="wf-adv-arrow" /> 高级设置</summary>
              <div class="wf-adv-body">
                <div class="field">
                  <label class="field-label">Foreach <em class="wf-hint">对数组 alias 逐项执行</em></label>
                  <div class="wf-foreach-line">
                    <input v-model="st.foreachAlias" class="input mono" placeholder="来源 Alias，如 items" spellcheck="false" />
                    <span class="wf-goto-arrow mono">→</span>
                    <input v-model="st.foreachAs" class="input mono" placeholder="元素 Alias，如 item" spellcheck="false" />
                    <input v-model="st.foreachIndexAs" class="input mono" placeholder="下标 Alias（可选）" spellcheck="false" />
                  </div>
                </div>

                <div class="field">
                  <label class="field-label">Body <em class="wf-hint">留空则不发送</em></label>
                  <textarea
                    v-model="st.body"
                    class="textarea mono wf-body"
                    placeholder='如 { "api_key_ids": "{{key_ids}}" }'
                    spellcheck="false"
                    @focus="onFocus"
                  ></textarea>
                </div>

                <div class="field">
                  <div class="wf-sec-head sub">
                    <span class="wf-sec-title">Expect 状态码路由</span>
                    <div class="spacer"></div>
                    <button class="btn btn-ghost btn-sm" @click="addExpectRoute(st)"><ListPlus :size="13" /> 添加路由</button>
                  </div>
                  <div v-if="st.legacyExpect" class="wf-extract-empty wf-legacy-expect">
                    v1 Expect：<span class="mono">{{ st.legacyExpect }}</span>
                    <button class="btn btn-ghost btn-sm" @click="st.legacyExpect = ''">确认删除并迁移</button>
                  </div>
                  <div v-if="!st.expectRoutes.length" class="wf-extract-empty">未配置路由；2xx 继续，其他失败</div>
                  <div v-for="(route, routeIndex) in st.expectRoutes" :key="routeIndex" class="wf-extract-row">
                    <input v-model="route.statuses" class="input mono" placeholder="状态码，如 401,403" spellcheck="false" />
                    <span class="wf-goto-arrow mono">→</span>
                    <input v-model="route.goto" class="input mono wf-goto" placeholder="目标步骤 ID" spellcheck="false" />
                    <button class="icon-btn wf-del" title="移除" @click="removeExpectRoute(st, routeIndex)"><X :size="13" /></button>
                  </div>
                </div>

                <div class="field">
                  <label class="field-label">When Alias 条件 <em class="wf-hint">提交后判断，可跳转</em></label>
                  <div class="wf-req-line">
                    <input
                      v-model="st.when"
                      class="input mono"
                      placeholder='$vars.a == true'
                      spellcheck="false"
                      @focus="onFocus"
                    />
                    <span class="wf-goto-arrow mono">→</span>
                    <input
                      v-model="st.whenGoto"
                      class="input mono wf-goto"
                      placeholder="目标步骤 ID"
                      spellcheck="false"
                    />
                  </div>
                </div>
              </div>
            </details>
          </div>
        </div>
        </div>
        </div>

        <div class="wf-section wf-sec-output">
          <div class="wf-sec-head wf-section-head">
            <span class="wf-sec-title">Output <em class="wf-hint">递归 alias 模板，字段集合由后端固定约束</em></span>
            <div class="spacer"></div>
            <button class="btn btn-ghost btn-sm" @click="formatJson"><Braces :size="13" /> 格式化</button>
          </div>
          <div class="wf-section-body">
            <textarea v-model="form.output" class="textarea mono wf-output" spellcheck="false" @focus="onFocus"></textarea>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="showForm = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="save">
          <LoaderCircle v-if="saving" :size="15" class="spin" />
          <Save v-else :size="15" />
          保存
        </button>
      </template>
    </Modal>

    <!-- execute -->
    <Modal
      :open="showExecute"
      title="执行工作流"
      :subtitle="executingWorkflow ? executingWorkflow.name : ''"
      :icon="Play"
      width="900px"
      @close="showExecute = false"
    >
      <div class="wf-form">
        <div class="wf-form-grid">
          <div class="field">
            <label class="field-label">目标中转站</label>
            <select v-model="selectedBackendId" class="select">
              <option :value="0" disabled>选择要执行的中转站…</option>
              <option v-for="b in backends" :key="b.id" :value="b.id">{{ b.name }}（{{ b.console_url }}）</option>
            </select>
            <span v-if="selectedBackend" class="field-hint">基础 URL 与认证信息取自中转站控制台配置</span>
          </div>
          <div class="field">
            <label class="field-label">输入 Alias <em class="wf-hint">可选</em></label>
            <textarea v-model="aliasesText" class="textarea mono wf-aliases" placeholder='{ "some_key": "value" }' spellcheck="false"></textarea>
          </div>
        </div>

        <div v-if="executing" class="wf-run-state"><LoaderCircle :size="18" class="spin" /><span>正在执行…</span></div>

        <div v-else-if="executeResult" class="wf-result">
          <div class="wf-result-head ok">
            <CheckCircle2 :size="15" />
            <span>执行成功 · {{ executeResult.backend.name }} · {{ fmtTime(executeResult.executed_at) }}</span>
          </div>
          <div class="wf-result-block">
            <div class="wf-result-label">Output</div>
            <pre class="wf-pre">{{ JSON.stringify(executeResult.output, null, 2) }}</pre>
          </div>
          <div v-if="executeRequests.length" class="wf-result-block">
            <div class="wf-result-label">请求记录</div>
            <div class="wf-req-list">
              <details v-for="(r, i) in executeRequests" :key="i" class="wf-req" :open="r.status_code === 0 || r.status_code >= 400">
                <summary class="wf-req-summary">
                  <span class="wf-req-seq mono">{{ i + 1 }}</span>
                  <span class="tag mono wf-method">{{ r.method }}</span>
                  <span class="mono wf-req-path">{{ r.path }}</span>
                  <span class="wf-status mono" :class="statusClass(r.status_code)">{{ r.status_code || '未收到响应' }}</span>
                </summary>
                <pre v-if="r.body" class="wf-req-body">{{ r.body }}</pre>
              </details>
            </div>
          </div>
        </div>

        <div v-else-if="executeError" class="wf-result">
          <div class="wf-result-head err"><AlertTriangle :size="15" /><span>执行失败：{{ executeError }}</span></div>
          <div v-if="executeRequests.length" class="wf-result-block">
            <div class="wf-result-label">请求记录</div>
            <div class="wf-req-list">
              <details v-for="(r, i) in executeRequests" :key="i" class="wf-req" :open="r.status_code === 0 || r.status_code >= 400">
                <summary class="wf-req-summary">
                  <span class="wf-req-seq mono">{{ i + 1 }}</span>
                  <span class="tag mono wf-method">{{ r.method }}</span>
                  <span class="mono wf-req-path">{{ r.path }}</span>
                  <span class="wf-status mono" :class="statusClass(r.status_code)">{{ r.status_code || '未收到响应' }}</span>
                </summary>
                <pre v-if="r.body" class="wf-req-body">{{ r.body }}</pre>
              </details>
            </div>
          </div>
        </div>

        <div v-if="!executing && executeDebugLogs.length" class="wf-result-block">
          <div class="wf-result-label">调试日志</div>
          <div class="wf-debug-list">
            <details
              v-for="(entry, i) in executeDebugLogs"
              :key="`${entry.time}-${i}`"
              class="wf-debug-entry"
              :class="`level-${entry.level}`"
              :open="entry.level === 'error'"
            >
              <summary class="wf-debug-summary">
                <span class="wf-debug-time mono">{{ fmtTime(entry.time) }}</span>
                <span class="wf-debug-level mono">{{ entry.level }}</span>
                <span v-if="entry.step_id" class="tag mono">{{ entry.step_id }}</span>
                <span class="wf-debug-phase mono">{{ entry.phase }}</span>
                <span class="wf-debug-message">{{ entry.message }}</span>
                <span v-if="entry.duration_ms != null" class="wf-debug-duration mono">{{ entry.duration_ms }} ms</span>
              </summary>
              <pre v-if="entry.details" class="wf-debug-details">{{ JSON.stringify(entry.details, null, 2) }}</pre>
            </details>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="showExecute = false">关闭</button>
        <button class="btn btn-primary" :disabled="executing || !selectedBackendId" @click="runExecute">
          <LoaderCircle v-if="executing" :size="15" class="spin" />
          <Play v-else :size="15" />
          执行
        </button>
      </template>
    </Modal>

    <!-- delete confirm -->
    <Modal
      :open="workflowToDelete !== null"
      title="删除工作流"
      :subtitle="workflowToDelete?.name"
      :icon="Trash2"
      @close="workflowToDelete = null"
    >
      <div class="wf-confirm">
        <AlertTriangle :size="20" />
        <p>删除后该 profile 配置将无法恢复，已保存的执行结果仍保留。</p>
        <p class="mono">{{ workflowToDelete?.id }}</p>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="workflowToDelete = null">取消</button>
        <button class="btn btn-danger" @click="doDelete"><Trash2 :size="15" /> 删除</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.toolbar { display: flex; align-items: center; gap: var(--space-4); padding: 14px 18px; flex-wrap: wrap; }
.wf-title-row { display: flex; align-items: center; gap: 12px; min-width: 0; }
.wf-title-icon {
  width: 38px; height: 38px; border-radius: 12px; flex: none;
  display: flex; align-items: center; justify-content: center;
  background: var(--grad-soft); color: var(--primary);
  border: 1px solid var(--border-strong);
}
.wf-title { font-size: 15px; font-weight: 600; color: var(--text); }
.wf-sub { font-size: 11.5px; color: var(--text-faint); margin-top: 2px; }

.wf-list { padding: 6px; }
.wf-state {
  display: flex; align-items: center; justify-content: center; gap: 10px;
  padding: 44px 16px; color: var(--text-faint); font-size: 13px;
}
.wf-state.error { color: var(--danger); }
.wf-row {
  display: flex; align-items: center; gap: 14px;
  padding: 14px 14px;
  border-bottom: 1px solid var(--border-soft);
  transition: background 0.2s ease;
}
.wf-row:last-child { border-bottom: none; }
.wf-row:hover { background: var(--surface-2); }
.wf-row-ico {
  width: 36px; height: 36px; border-radius: 11px; flex: none;
  display: flex; align-items: center; justify-content: center;
  background: var(--surface-3); color: var(--primary);
  border: 1px solid var(--border);
}
.wf-row-main { min-width: 0; flex: 1; }
.wf-row-name { font-size: 14.5px; font-weight: 600; color: var(--text); }
.wf-row-meta { display: flex; align-items: center; gap: 8px; font-size: 11.5px; color: var(--text-faint); margin-top: 3px; flex-wrap: wrap; }
.wf-row-meta .sep { color: var(--text-faint); }
.wf-row-actions { display: flex; align-items: center; gap: 6px; flex: none; }
.wf-del:hover { color: var(--danger); background: var(--danger-soft); }

.wf-form { display: flex; flex-direction: column; gap: 14px; }
.wf-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.wf-basic { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.wf-form-error {
  display: flex; align-items: flex-start; gap: 8px;
  padding: 10px 13px;
  font-size: 12.5px; line-height: 1.5; color: var(--danger);
  background: var(--danger-soft); border: 1px solid rgba(251, 113, 133, 0.3);
  border-radius: var(--radius-sm);
}
.wf-form-error svg { flex: none; margin-top: 1px; }
.wf-hint { font-style: normal; font-size: 11px; color: var(--text-faint); font-weight: 500; margin-left: 6px; }
.wf-foreach-line { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) minmax(0, 1fr); align-items: center; gap: 8px; }

.wf-sec-head { display: flex; align-items: center; gap: 10px; }
.wf-sec-head.sub { margin: 2px 0 4px; }
.wf-sec-title { font-size: 12px; font-weight: 700; letter-spacing: 0.06em; color: var(--text-soft); }
.wf-sec-title em { text-transform: none; letter-spacing: 0; }

.wf-alias-bar {
  display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
  padding: 8px 12px;
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm);
}
.wf-alias-label { font-size: 11px; font-weight: 700; color: var(--text-faint); margin-right: 2px; }
.wf-alias {
  padding: 3px 9px; border-radius: 999px; cursor: pointer;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--primary); background: var(--grad-soft);
  border: 1px solid var(--border-strong);
  transition: filter 0.15s ease;
}
.wf-alias:hover { filter: brightness(1.15); }
.wf-alias-tip { font-size: 11px; color: var(--text-faint); }

.wf-step {
  border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface); overflow: hidden;
}
.wf-step.open { border-color: var(--border-strong); }
.wf-step-head {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 12px; cursor: pointer; user-select: none;
  min-width: 0;
}
.wf-step-idx {
  width: 28px; text-align: center; flex: none;
  font-size: 11.5px; font-weight: 700; color: var(--primary);
  background: var(--grad-soft); border: 1px solid var(--border-strong);
  border-radius: 8px; padding: 3px 0;
}
.wf-method-tag { flex: none; font-size: 10.5px; }
.wf-step-path {
  flex: 0 1 220px; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: 12px; color: var(--text-soft);
}
.wf-extract-chips {
  flex: 1; min-width: 0;
  display: flex; align-items: center; justify-content: flex-end; gap: 6px; flex-wrap: wrap;
}
.wf-chip {
  display: inline-flex; align-items: center;
  padding: 2px 9px; border-radius: 999px;
  font-family: var(--font-mono); font-size: 10.5px;
  color: var(--primary); background: var(--grad-soft); border: 1px solid var(--border-strong);
}
.wf-chip.empty { color: var(--text-faint); background: transparent; border: 1px dashed var(--border-strong); }
.wf-step-body {
  display: flex; flex-direction: column; gap: 12px;
  padding: 12px 14px 14px;
  border-top: 1px solid var(--border-soft);
}

.wf-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.wf-req-line { display: flex; gap: 8px; }
.wf-method-select { width: 116px; flex: none; }

.wf-kv { display: flex; flex-direction: column; gap: 6px; }
.wf-kv-row { display: flex; gap: 6px; }
.wf-kv-row > .input:not(.wf-kv-key) { min-width: 0; }
.wf-kv-key { width: 130px; flex: none; }

.wf-section { border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); overflow: hidden; }
.wf-section-head {
  position: relative;
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  padding: 10px 14px;
  background: var(--sec-tint, var(--surface-2));
  border-bottom: 1px solid var(--border-soft);
}
.wf-section-head::before {
  content: '';
  position: absolute; left: 0; top: 0; bottom: 0;
  width: 3px;
  background: var(--sec-accent, transparent);
}
.wf-sec-headers { --sec-tint: rgba(34, 211, 238, 0.07); --sec-accent: #22d3ee; --sec-deep: #0891b2; --sec-deep-bg: rgba(8, 145, 178, 0.16); }
.wf-sec-steps { --sec-tint: rgba(139, 92, 246, 0.08); --sec-accent: #8b5cf6; --sec-deep: #7c3aed; --sec-deep-bg: rgba(124, 58, 237, 0.16); }
.wf-sec-output { --sec-tint: rgba(52, 211, 153, 0.07); --sec-accent: #34d399; --sec-deep: #059669; --sec-deep-bg: rgba(5, 150, 105, 0.16); }

.wf-section-head .btn {
  background: var(--sec-deep-bg);
  color: var(--sec-deep);
  border-color: var(--border-strong);
  box-shadow: none;
}
.wf-section-head .btn:hover {
  color: var(--text);
  box-shadow: none;
}
.wf-section-body { display: flex; flex-direction: column; gap: 12px; padding: 14px; }

.wf-adv { border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); overflow: hidden; }
.wf-adv-summary {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px;
  font-size: 12.5px; font-weight: 700; color: var(--text-soft);
  cursor: pointer; user-select: none; list-style: none;
  background: var(--surface-2);
  transition: color 0.15s ease;
}
.wf-adv-summary::-webkit-details-marker { display: none; }
.wf-adv-summary:hover { color: var(--text); }
.wf-adv[open] .wf-adv-summary { color: var(--text); border-bottom: 1px solid var(--border-soft); }
.wf-adv-arrow { color: var(--text-faint); transition: transform 0.2s var(--ease-out); }
.wf-adv[open] .wf-adv-arrow { transform: rotate(90deg); }
.wf-adv-body { display: flex; flex-direction: column; gap: 12px; padding: 12px 14px; }
.wf-adv-body .field { margin-bottom: 0; }
.wf-adv-body .wf-sec-head.sub { margin: 2px 0 7px; }
.wf-adv-count {
  display: inline-flex; align-items: center;
  padding: 1px 8px; border-radius: 999px;
  font-size: 10.5px; font-weight: 700; font-family: var(--font-mono);
  color: var(--primary); background: var(--grad-soft); border: 1px solid var(--border-strong);
}

.wf-body, .wf-output { min-height: 90px; font-size: 12px; line-height: 1.55; background: var(--bg-soft); tab-size: 2; }

.wf-extract { display: flex; flex-direction: column; gap: 8px; }
.wf-extract-empty { font-size: 12px; color: var(--text-faint); padding: 6px 2px; }
.wf-extract-row { display: flex; gap: 6px; }
.wf-extract-alias { width: 160px; flex: none; }
.wf-goto-arrow { color: var(--text-faint); padding: 0 4px; }
.wf-goto { width: 180px; flex: none; }
.wf-legacy-expect { display: flex; align-items: center; gap: 8px; color: var(--warning); }

.wf-aliases { min-height: 84px; font-size: 12px; line-height: 1.5; background: var(--bg-soft); }

.wf-run-state {
  display: flex; align-items: center; gap: 10px;
  padding: 18px; color: var(--text-soft); font-size: 13px;
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm);
}
.wf-result { display: flex; flex-direction: column; gap: 12px; }
.wf-result-head {
  display: flex; align-items: center; gap: 8px;
  font-size: 13px; font-weight: 600;
  padding: 10px 13px; border-radius: var(--radius-sm);
}
.wf-result-head.ok { color: var(--success); background: var(--success-soft); }
.wf-result-head.err { color: var(--danger); background: var(--danger-soft); }
.wf-result-block { display: flex; flex-direction: column; gap: 7px; }
.wf-result-label { font-size: 11px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-faint); }
.wf-pre {
  margin: 0;
  padding: 12px 14px;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.55;
  color: var(--text-soft);
  background: var(--bg-soft);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  overflow: auto;
  max-height: 320px;
}
.wf-req-list { display: flex; flex-direction: column; gap: 6px; }
.wf-req {
  padding: 0 12px;
  background: var(--surface);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  font-size: 12px;
}
.wf-req-summary { display: flex; align-items: center; gap: 10px; min-height: 36px; cursor: pointer; list-style: none; }
.wf-req-summary::-webkit-details-marker { display: none; }
.wf-req-body { margin: 0 0 10px 28px; padding: 9px 11px; max-height: 220px; overflow: auto; white-space: pre-wrap; word-break: break-word; color: var(--text-soft); background: var(--bg-soft); border: 1px solid var(--border-soft); border-radius: var(--radius-sm); font: 11px/1.5 var(--font-mono); }
.wf-req-seq { color: var(--text-faint); width: 18px; text-align: right; }
.wf-method { font-size: 10.5px; flex: none; }
.wf-req-path { color: var(--text-soft); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.wf-status { flex: none; font-weight: 700; }
.wf-status.ok { color: var(--success); }
.wf-status.info { color: var(--info); }
.wf-status.warn { color: var(--warning); }
.wf-status.err { color: var(--danger); }
.wf-debug-list { display: flex; flex-direction: column; gap: 5px; }
.wf-debug-entry { border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--surface); }
.wf-debug-entry.level-error { border-color: color-mix(in srgb, var(--danger) 45%, var(--border)); }
.wf-debug-summary { display: flex; align-items: center; gap: 8px; min-height: 34px; padding: 5px 10px; cursor: pointer; list-style: none; font-size: 11.5px; }
.wf-debug-summary::-webkit-details-marker { display: none; }
.wf-debug-time { color: var(--text-faint); flex: none; }
.wf-debug-level { color: var(--info); text-transform: uppercase; width: 42px; flex: none; }
.wf-debug-entry.level-error .wf-debug-level { color: var(--danger); }
.wf-debug-phase { color: var(--primary); flex: none; }
.wf-debug-message { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-soft); flex: 1; }
.wf-debug-duration { color: var(--text-faint); flex: none; }
.wf-debug-details { margin: 0 10px 10px; padding: 9px 11px; max-height: 300px; overflow: auto; white-space: pre-wrap; word-break: break-word; color: var(--text-soft); background: var(--bg-soft); border: 1px solid var(--border-soft); border-radius: var(--radius-sm); font: 11px/1.5 var(--font-mono); }

.wf-confirm { display: flex; flex-direction: column; align-items: center; gap: 10px; text-align: center; padding: 8px 0; color: var(--text-soft); font-size: 13.5px; }
.wf-confirm svg { color: var(--warning); }
.wf-confirm p.mono { color: var(--text-faint); font-size: 12px; }

@media (max-width: 720px) {
  .wf-form-grid, .wf-basic, .wf-grid-2 { grid-template-columns: 1fr; }
  .wf-kv-key { width: min(112px, 34vw); }
  .wf-sec-head { flex-wrap: wrap; }
  .wf-sec-title { white-space: nowrap; }
  .wf-foreach-line { grid-template-columns: 1fr; }
  .wf-foreach-line .wf-goto-arrow { display: none; }
  .wf-row { flex-wrap: wrap; }
}
</style>
