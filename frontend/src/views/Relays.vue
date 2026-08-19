<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import {
  Plus,
  Search,
  Server,
  CalendarCheck,
  Cookie,
  Pencil,
  Trash2,
  Globe,
  KeyRound,
  Wallet,
  Coins,
  User,
  CirclePlay,
  CirclePause,
  ChevronDown,
  ArrowRight,
  AlertTriangle,
  LoaderCircle,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Eye,
  EyeOff,
  X
} from 'lucide-vue-next'
import Modal from '../components/Modal.vue'
import { MODEL_CATALOG } from '../data/mock'
import { toast } from '../composables/toast'
import {
  ApiError,
  createBackend,
  deleteBackend,
  listBackends,
  listSocksProxies,
  recordBackendSyncSummary,
  syncBackendCookies,
  syncBackendStream,
  updateBackend,
  type BackendApiKey,
  type BackendResponse,
  type BackendSyncResponse,
  type SocksProxyResponse
} from '../api/backends'
import { listWorkflows, type WorkflowRecord } from '../api/workflows'
import type { Relay, RelayKey, RelayModel, PlatformType } from '../types'

const search = ref('')
type ModelFamily = 'gpt' | 'claude' | 'grok' | 'deepseek' | 'kimi'
const modelFilter = ref<'all' | ModelFamily>('all')
const manualCheckinFilter = ref(false)
const page = ref(1)
const pageSize = 10
const modelFamilies: Array<{ value: ModelFamily; keywords: string[] }> = [
  { value: 'gpt', keywords: ['gpt'] },
  { value: 'claude', keywords: ['claude'] },
  { value: 'grok', keywords: ['grok'] },
  { value: 'deepseek', keywords: ['deepseek'] },
  { value: 'kimi', keywords: ['kimi', 'moonshot'] }
]

type RelayStatusView = 'active' | 'disabled' | 'abnormal'
type RelayView = Omit<Relay, 'status'> & {
  status: RelayStatusView
  protocol: 'openai' | 'anthropic' | 'both'
  consoleUrl: string
  consoleSyncSupported: boolean
  pricingModels: RelayModel[]
  quotaUnit: string
  todayReward: number
  raw: BackendResponse
}

const relays = ref<RelayView[]>([])
const proxyOptions = ref<SocksProxyResponse[]>([])
const workflowOptions = ref<WorkflowRecord[]>([])
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const syncingAll = ref(false)
const busyIds = ref<Set<string>>(new Set())
const cookieBusyIds = ref<Set<string>>(new Set())

function setBusy(id: string, busy: boolean) {
  const next = new Set(busyIds.value)
  busy ? next.add(id) : next.delete(id)
  busyIds.value = next
}

function setCookieBusy(id: string, busy: boolean) {
  const next = new Set(cookieBusyIds.value)
  busy ? next.add(id) : next.delete(id)
  cookieBusyIds.value = next
}

/* ---- console request log (global) ---- */
import {
  openConsoleLog,
  appendLogRow,
  appendWorkflowLogRow,
  consoleLogRows,
  consoleLogTitle,
  consoleLogShowName,
  expandedLogRowIds,
  hideConsoleLog,
  showConsoleLog,
  type ConsoleLogRow
} from '../composables/consoleLog'

function errorMessage(error: unknown) {
  if (error instanceof ApiError) return error.message
  return error instanceof Error ? error.message : '请求失败，请稍后重试'
}

function parseObject(raw: string | undefined): Record<string, any> {
  if (!raw) return {}
  try {
    const value = JSON.parse(raw)
    return value && typeof value === 'object' ? value : {}
  } catch {
    return {}
  }
}

function parseArray(raw: string | undefined): Record<string, any>[] {
  if (!raw) return []
  try {
    const value = JSON.parse(raw)
    return Array.isArray(value) ? value.filter((item) => item && typeof item === 'object') : []
  } catch {
    return []
  }
}

function formatConsoleHeaders(headers: Record<string, string> | undefined) {
  if (!headers) return ''
  return Object.entries(headers)
    .map(([key, value]) => `${key.trim()}: ${value.trim()}`)
    .filter((line) => !line.startsWith(': ') && !line.endsWith(': '))
    .join('\n')
}

function parseConsoleHeaders(raw: string) {
  const headers: Record<string, string> = {}
  const names = new Set<string>()
  for (const sourceLine of raw.split(/\r?\n/)) {
    const line = sourceLine.trim()
    if (!line) continue
    const separator = line.indexOf(':')
    const key = separator >= 0 ? line.slice(0, separator).trim() : ''
    const value = separator >= 0 ? line.slice(separator + 1).trim() : ''
    if (!key || !value) throw new Error('Headers 每行必须使用 Key: Value 格式')
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(key)) throw new Error(`无效的 Header 名称：${key}`)
    const normalizedKey = key.toLowerCase()
    if (names.has(normalizedKey)) throw new Error(`Header 名称重复：${key}`)
    names.add(normalizedKey)
    headers[key] = value
  }
  return headers
}

function parseModelList(raw: string) {
  return [...new Set(raw.split(',').map((model) => model.trim()).filter(Boolean))]
}

function formatModelMapping(mapping: Record<string, string>) {
  return Object.keys(mapping).length ? JSON.stringify(mapping) : ''
}

function parseModelMapping(raw: string) {
  const value = raw.trim()
  if (!value) return {}
  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    throw new Error('Model Mapping 必须是有效的 JSON 对象')
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Model Mapping 必须是有效的 JSON 对象')
  }
  const mapping: Record<string, string> = {}
  for (const [source, target] of Object.entries(parsed)) {
    if (typeof target !== 'string') throw new Error('Model Mapping 的映射值必须是字符串')
    const model = source.trim()
    const upstream = target.trim()
    if (model && upstream) mapping[model] = upstream
  }
  return mapping
}

function numberValue(value: unknown) {
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function accountValues(backend: BackendResponse, keys: BackendApiKey[]) {
  const account = parseObject(backend.console_account)
  const keyUsed = keys.reduce((sum, key) => sum + numberValue(key.used_quota), 0)
  return {
    balance: numberValue(account.quota),
    used: account.used_quota !== undefined ? numberValue(account.used_quota) : keyUsed,
    quotaUnit: String(account.quota_unit || 'USD').trim() || 'USD',
    username: String(account.username || account.email || account.id || backend.console_username || ''),
    checkinAt: String(account.last_checkin_at || ''),
    todayReward: numberValue(account.today_reward)
  }
}

function protocolOf(value: string) {
  const normalized = value.trim().toLowerCase()
  if (normalized === 'anthropic') return 'anthropic' as const
  if (normalized === 'both') return 'both' as const
  return 'openai' as const
}

function platformOf(backend: BackendResponse): PlatformType {
  const value = `${backend.name} ${backend.base_url}`.toLowerCase()
  if (backend.protocol.toLowerCase() === 'anthropic' || value.includes('claude') || value.includes('anthropic')) return 'Anthropic'
  if (value.includes('gemini') || value.includes('google')) return 'Gemini'
  if (value.includes('azure')) return 'Azure'
  if (value.includes('deepseek')) return 'DeepSeek'
  if (value.includes('openai')) return 'OpenAI'
  return 'Custom'
}

type ModelPricing = { input: number; output: number; group: string; billingType: 'token' | 'fixed' }

function pricingByModel(backend: BackendResponse) {
  const result = new Map<string, ModelPricing>()
  for (const record of parseArray(backend.console_models)) {
    const name = String(record.name || '').trim()
    if (!name) continue
    const groups = Array.isArray(record.cheapest_groups)
      ? record.cheapest_groups.map((group: unknown) => String(group).trim()).filter(Boolean)
      : []
    const group = groups.join(', ')
    if (numberValue(record.price_type) === 1) {
      const price = numberValue(record.price)
      result.set(name, {
        input: price,
        output: price,
        group,
        billingType: 'fixed'
      })
      continue
    }
    const input = numberValue(record.in_price)
    const output = numberValue(record.out_price)
    result.set(name, { input, output, group, billingType: 'token' })
  }
  return result
}

function modelInfo(name: string, group: string, pricing: Map<string, ModelPricing>): RelayModel {
  const known = MODEL_CATALOG.find((model) => model.name === name)
  const price = pricing.get(name)
  return {
    id: `model-${name}`,
    name,
    group: price?.group || known?.group || group,
    priceIn: price ? price.input : known?.priceIn || 0,
    priceOut: price ? price.output : known?.priceOut || 0,
    billingType: price?.billingType || 'token'
  }
}

function mapBackend(backend: BackendResponse): RelayView {
  const keys = Array.isArray(backend.api_keys) ? backend.api_keys : []
  const pricing = pricingByModel(backend)
  const relayKeys: RelayKey[] = keys.map((key, index) => {
    const models = Array.isArray(key.models) ? key.models.filter(Boolean) : []
    return {
      id: `${backend.id}-key-${index}`,
      name: key.name || '',
      group: key.group || 'default',
      username: '',
      key: key.key,
      models,
      modelMap: key.model_mapping || {},
      usedTokens: numberValue(key.used_quota)
    }
  })
  const account = accountValues(backend, keys)
  const group = platformOf(backend)
  // models: from API Key configured models (for card display)
  const keyModelNames = new Set<string>()
  relayKeys.forEach((k) => {
    k.models.forEach((m) => keyModelNames.add(m))
    Object.keys(k.modelMap).forEach((m) => keyModelNames.add(m))
  })
  const models = [...keyModelNames].map((name) => modelInfo(name, group, pricing))
  // pricingModels: from pricing data (for expanded available models list)
  const pricingModels = [...pricing.keys()].map((name) => modelInfo(name, group, pricing))
  const status: RelayStatusView = backend.status === 'normal' ? 'active' : backend.status
  return {
    id: String(backend.id),
    name: backend.name,
    url: backend.base_url,
    platform: platformOf(backend),
    status,
    balance: account.balance,
    used: account.used,
    username: account.username,
    checkinAt: account.checkinAt,
    todayReward: account.todayReward,
    proxyId: backend.proxy_id ? String(backend.proxy_id) : '',
    models,
    pricingModels,
    quotaUnit: account.quotaUnit,
    keys: relayKeys,
    protocol: protocolOf(backend.protocol),
    consoleUrl: backend.console_url || '',
    consoleSyncSupported: Boolean(backend.console_checkin_workflow_id?.trim()),
    raw: backend
  }
}

function proxyName(id: string) {
  return proxyOptions.value.find((proxy) => String(proxy.id) === id)?.name || `代理 #${id}`
}

function formatDate(value: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function isToday(value: string) {
  if (!value) return false
  const date = new Date(value)
  const now = new Date()
  return !Number.isNaN(date.getTime()) && date.toDateString() === now.toDateString()
}

function signinText(r: RelayView): string {
  if (!r.consoleSyncSupported) return r.todayReward > 0 ? fmtMoney(r.todayReward, r.quotaUnit) : '未配置'
  if (isToday(r.checkinAt)) return fmtMoney(r.todayReward, r.quotaUnit)
  return '未签到'
}

function signinState(r: RelayView): string {
  if (!r.consoleSyncSupported) return r.todayReward > 0 ? 'ok' : 'faint'
  return isToday(r.checkinAt) ? 'ok' : 'warn'
}

const platformColor: Record<string, string> = {
  OpenAI: 'cyan',
  Anthropic: 'pink',
  Gemini: 'blue',
  Azure: 'violet',
  Claude: 'pink',
  DeepSeek: 'violet',
  Custom: 'amber'
}

const filtered = computed(() =>
  relays.value
    .filter((r) => {
      const okSearch = r.name.toLowerCase().includes(search.value.toLowerCase()) || r.url.includes(search.value) || r.username.toLowerCase().includes(search.value.toLowerCase())
      const selectedFamily = modelFamilies.find((family) => family.value === modelFilter.value)
      const okModel = !selectedFamily || r.models.some((model) => {
        const name = model.name.toLowerCase()
        return selectedFamily.keywords.some((keyword) => name.includes(keyword))
      })
      const okManualCheckin = !manualCheckinFilter.value || r.raw.manual_checkin
      return okSearch && okModel && okManualCheckin
    })
    .sort((a, b) => numberValue(b.raw.weight) - numberValue(a.raw.weight))
)

const totalPages = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)))
const pageItems = computed(() => {
  const start = (page.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function resetPage() {
  page.value = 1
}

function goPage(nextPage: number) {
  page.value = Math.min(Math.max(1, nextPage), totalPages.value)
  expandedId.value = null
}

watch([search, modelFilter, manualCheckinFilter], resetPage)
watch(totalPages, (pages) => {
  if (page.value > pages) page.value = pages
})

function quotaUnitPrefix(unit: string) {
  const normalized = unit.trim()
  const symbols: Record<string, string> = {
    USD: '$',
    CNY: '¥',
    RMB: '¥',
    EUR: '€',
    GBP: '£',
    JPY: '¥',
    KRW: '₩'
  }
  return symbols[normalized.toUpperCase()] || normalized || '$'
}

const fmtMoney = (n: number, unit: string) =>
  quotaUnitPrefix(unit) + n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtPrice = (n: number, unit: string) => {
  const prefix = quotaUnitPrefix(unit)
  if (n === 0) return prefix + '0'
  if (n >= 1) return prefix + Number(n.toFixed(2))
  if (n >= 0.01) return prefix + Number(n.toFixed(3))
  return prefix + n.toPrecision(3)
}

const hostOf = (url: string) => {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

const expandedId = ref<string | null>(null)
function toggleExpand(id: string) {
  expandedId.value = expandedId.value === id ? null : id
}

async function toggleStatus(r: RelayView) {
  const next = r.status === 'active' ? 'disabled' : 'normal'
  setBusy(r.id, true)
  try {
    const backend = await updateBackend(Number(r.id), { status: next })
    upsertRelay(backend)
    toast(next === 'normal' ? '中转站已启用' : '中转站已停用', r.name, next === 'normal' ? 'success' : 'warning')
  } catch (error) {
    toast('状态更新失败', errorMessage(error), 'danger')
  } finally {
    setBusy(r.id, false)
  }
}

async function syncRelay(
  r: RelayView,
  options: { quiet?: boolean; audit?: boolean } = {}
) {
  const { quiet = false, audit = true } = options
  if (!r.consoleUrl) {
    if (!quiet) toast('缺少控制台地址', '请先在编辑中转站中填写 Console URL', 'warning')
    return false
  }
  if (!r.consoleSyncSupported) {
    if (!quiet) toast('缺少签到工作流', '请先编辑中转站并绑定签到工作流', 'warning')
    return false
  }
  setBusy(r.id, true)
  if (!quiet) openConsoleLog(r.name)
  try {
    const response = await syncBackendStream(Number(r.id), (req) => {
      appendLogRow(req, r.name)
    }, {
      audit,
      onWorkflowLog: (log) => appendWorkflowLogRow(log, r.name)
    })
    upsertRelay(response.backend)
    if (!quiet) toast('签到完成', `${r.name} · 账户与 API Key 已刷新`, 'success')
    return true
  } catch (error) {
    if (!quiet) toast('签到失败', errorMessage(error), 'danger')
    return false
  } finally {
    setBusy(r.id, false)
  }
}

async function checkin(r: RelayView) {
  if (isToday(r.checkinAt)) {
    toast('今日已签到', `${r.name} · ${formatDate(r.checkinAt)}`, 'info')
  }
  await syncRelay(r)
}

async function syncCookies(r: RelayView) {
  if (!r.consoleUrl) {
    toast('缺少控制台地址', '请先在编辑中转站中填写 Console URL', 'warning')
    return
  }
  setCookieBusy(r.id, true)
  setBusy(r.id, true)
  try {
    const response = await syncBackendCookies(Number(r.id))
    upsertRelay(response.backend)
    const imported = []
    if (response.cookie_count > 0) imported.push(`${response.cookie_count} 项 Cookie`)
    if (response.authorization_updated) imported.push('Authorization')
    if (imported.length) {
      toast('Chrome 登录凭据已更新', `${r.name} · ${imported.join(' · ')}`, 'success')
    } else {
      toast('未发现 Chrome 登录凭据', `${r.name} · 未找到 Cookie 或有效访问令牌`, 'warning')
    }
  } catch (error) {
    toast('Chrome 登录凭据同步失败', errorMessage(error), 'danger')
  } finally {
    setBusy(r.id, false)
    setCookieBusy(r.id, false)
  }
}

async function checkinAll() {
  const todo = relays.value.filter((relay) => relay.consoleUrl && relay.consoleSyncSupported && !isToday(relay.checkinAt))
  if (!todo.length) {
    toast('今日无需签到', '没有待签到且配置了控制台地址的中转站', 'info')
    return
  }
  syncingAll.value = true
  consoleLogShowName.value = false
  consoleLogRows.value = []
  expandedLogRowIds.value = new Set()
  showConsoleLog()

  const failedRows: ConsoleLogRow[] = []
  let successCount = 0

  for (const relay of todo) {
    consoleLogTitle.value = `批量签到 · ${relay.name}`
    consoleLogRows.value = []
    expandedLogRowIds.value = new Set()

    const ok = await syncRelay(relay, { quiet: true, audit: false })
    if (ok) {
      successCount++
    } else {
      // Collect failed requests (status >= 400 or no response) from this relay
      for (const row of consoleLogRows.value) {
        if (
          (row.kind === 'request' && (!row.statusCode || row.statusCode >= 400)) ||
          (row.kind === 'workflow' && row.level === 'error')
        ) {
          failedRows.push(row)
        }
      }
    }
  }

  // Show failed requests summary
  if (failedRows.length) {
    consoleLogTitle.value = '批量签到 · 失败请求'
    consoleLogShowName.value = true
    consoleLogRows.value = failedRows
    expandedLogRowIds.value = new Set()
  } else {
    hideConsoleLog()
  }

  try {
    await recordBackendSyncSummary(todo.length, successCount, todo.length - successCount)
  } catch {
    // The individual sync results are already persisted; summary auditing is best effort.
  }
  syncingAll.value = false
  toast(successCount ? '批量签到完成' : '批量签到失败', `成功 ${successCount} 个，失败 ${todo.length - successCount} 个`, successCount === todo.length ? 'success' : successCount ? 'warning' : 'danger')
}

const relayToDelete = ref<RelayView | null>(null)

function askRemove(r: RelayView) {
  relayToDelete.value = r
}

/* ---- form ---- */
interface KeyFormItem {
  readonly id: string
  readonly name: string
  readonly serverGroup: string
  key: string
  keyVisible: boolean
  modelsInput: string
  modelMappingInput: string
  usedTokens: number
}

function defaultKeyFormItem(): KeyFormItem {
  return { id: '', name: '', serverGroup: 'default', key: '', keyVisible: false, modelsInput: '', modelMappingInput: '', usedTokens: 0 }
}

const form = ref<{
  name: string
  url: string
  protocol: 'openai' | 'anthropic' | 'both'
  consoleUrl: string
  username: string
  password: string
  consoleHeaders: string
  consoleCheckinWorkflowId: string
  manualCheckin: boolean
  proxyId: string
  weight: number
  keys: KeyFormItem[]
}>({
  name: '',
  url: '',
  protocol: 'openai',
  consoleUrl: '',
  username: '',
  password: '',
  consoleHeaders: '',
  consoleCheckinWorkflowId: '',
  manualCheckin: false,
  proxyId: '',
  weight: 10,
  keys: []
})

const showForm = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => editingId.value !== null)
const consoleHeadersError = computed(() => {
  try {
    parseConsoleHeaders(form.value.consoleHeaders)
    return ''
  } catch (error) {
    return errorMessage(error)
  }
})
const modelMappingErrors = computed(() => form.value.keys.map((key) => {
  try {
    parseModelMapping(key.modelMappingInput)
    return ''
  } catch (error) {
    return errorMessage(error)
  }
}))

function resetForm() {
  form.value = {
    name: '', url: '', protocol: 'openai', consoleUrl: '', username: '', password: '',
    consoleHeaders: '',
    consoleCheckinWorkflowId: '', manualCheckin: false,
    proxyId: '', weight: 10, keys: []
  }
}

function openCreate() {
  editingId.value = null
  resetForm()
  showForm.value = true
}

function openEditRelay(r: RelayView) {
  editingId.value = r.id
  form.value = {
    name: r.name,
    url: r.url,
    protocol: r.protocol,
    consoleUrl: r.raw.console_url || '',
    username: r.raw.console_username || '',
    password: r.raw.console_password || '',
    consoleHeaders: formatConsoleHeaders(r.raw.console_headers),
    consoleCheckinWorkflowId: r.raw.console_checkin_workflow_id || '',
    manualCheckin: Boolean(r.raw.manual_checkin),
    proxyId: r.proxyId,
    weight: numberValue(r.raw.weight) || 10,
    keys: r.keys.length
      ? r.keys.map((key) => ({
          id: r.raw.api_keys.find((item) => item.key === key.key)?.id || '',
          name: key.name,
          serverGroup: key.group || 'default',
          key: key.key,
          keyVisible: false,
          modelsInput: key.models.join(', '),
          modelMappingInput: formatModelMapping(key.modelMap),
          usedTokens: key.usedTokens
        }))
      : []
  }
  showForm.value = true
}

function addKeyRow() {
  form.value.keys.push(defaultKeyFormItem())
}
function removeKeyRow(i: number) {
  form.value.keys.splice(i, 1)
}

function buildPayload() {
  const apiKeys: BackendApiKey[] = form.value.keys.map((key) => {
    return {
      id: key.id,
      key: key.key.trim(),
      name: key.name,
      group: key.serverGroup,
      models: parseModelList(key.modelsInput),
      model_mapping: parseModelMapping(key.modelMappingInput),
      used_quota: Math.max(0, key.usedTokens || 0)
    }
  })
  return {
    name: form.value.name.trim(),
    protocol: form.value.protocol,
    base_url: form.value.url.trim(),
    api_keys: apiKeys,
    console_url: form.value.consoleUrl.trim(),
    console_username: form.value.username.trim(),
    console_password: form.value.password,
    console_headers: parseConsoleHeaders(form.value.consoleHeaders),
    console_checkin_workflow_id: form.value.consoleCheckinWorkflowId.trim(),
    manual_checkin: form.value.manualCheckin,
    proxy_id: form.value.proxyId ? Number(form.value.proxyId) : 0,
    weight: Math.min(100, Math.max(1, Math.round(numberValue(form.value.weight) || 10)))
  }
}

async function saveRelay() {
  if (!form.value.name || !form.value.url) {
    toast('请填写名称与 URL', '', 'warning')
    return
  }
  if (form.value.consoleCheckinWorkflowId && !form.value.consoleUrl.trim()) {
    toast('请填写 Server URL', '签到工作流需要控制台地址', 'warning')
    return
  }
  if (form.value.keys.some((key) => !key.key.trim() || !parseModelList(key.modelsInput).length)) {
    toast('API Key 配置不完整', '已添加的每个 Key 都需要密钥和至少一个模型', 'warning')
    return
  }
  const modelMappingError = modelMappingErrors.value.find(Boolean)
  if (modelMappingError) {
    toast('Model Mapping 格式错误', modelMappingError, 'warning')
    return
  }
  if (consoleHeadersError.value) {
    toast('Headers 格式错误', consoleHeadersError.value, 'warning')
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    const backend = isEditing.value && editingId.value
      ? await updateBackend(Number(editingId.value), payload)
      : await createBackend(payload)
    upsertRelay(backend)
    toast(isEditing.value ? '中转站已更新' : '中转站已创建', backend.name, 'success')
    showForm.value = false
    resetForm()
    editingId.value = null
  } catch (error) {
    toast(isEditing.value ? '中转站更新失败' : '中转站创建失败', errorMessage(error), 'danger')
  } finally {
    saving.value = false
  }
}

async function confirmRemove() {
  const relay = relayToDelete.value
  if (!relay) return
  setBusy(relay.id, true)
  try {
    await deleteBackend(Number(relay.id))
    relays.value = relays.value.filter((item) => item.id !== relay.id)
    relayToDelete.value = null
    toast('中转站已删除', relay.name, 'danger')
  } catch (error) {
    toast('中转站删除失败', errorMessage(error), 'danger')
  } finally {
    setBusy(relay.id, false)
  }
}

function upsertRelay(backend: BackendResponse) {
  const relay = mapBackend(backend)
  const index = relays.value.findIndex((item) => item.id === relay.id)
  if (index < 0) relays.value = [relay, ...relays.value]
  else relays.value.splice(index, 1, relay)
}

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    const [backendPage, proxyPage, workflowPage] = await Promise.all([listBackends(), listSocksProxies(), listWorkflows()])
    relays.value = backendPage.items.map(mapBackend)
    proxyOptions.value = proxyPage.items
    workflowOptions.value = workflowPage.items
  } catch (error) {
    loadError.value = errorMessage(error)
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
</script>

<template>
  <div class="page stagger">
    <!-- toolbar -->
    <section class="toolbar panel">
      <div class="search-box" style="width: 250px">
        <Search :size="15" />
        <input v-model="search" class="input" placeholder="搜索名称 / 域名 / 用户名…" />
        <button v-if="search" class="search-clear" aria-label="清除搜索" @click="search = ''"><X :size="14" /></button>
      </div>
      <button class="filter-chip" :class="{ on: manualCheckinFilter }" @click="manualCheckinFilter = !manualCheckinFilter">手工签到</button>
      <div class="spacer"></div>
      <div class="filter-group">
        <button class="filter-chip" :class="{ on: modelFilter === 'all' }" @click="modelFilter = 'all'">全部模型</button>
        <button v-for="family in modelFamilies" :key="family.value" class="filter-chip" :class="{ on: modelFilter === family.value }" @click="modelFilter = family.value">{{ family.value }}</button>
      </div>
      <button class="btn btn-ghost btn-sm btn-purple" :disabled="syncingAll" @click="checkinAll">
        <LoaderCircle v-if="syncingAll" :size="14" class="spin" />
        <CalendarCheck v-else :size="14" /> 签到
      </button>
      <button class="btn btn-primary btn-sm" :disabled="loading" @click="openCreate"><Plus :size="14" /> 添加</button>
    </section>

    <!-- relay list -->
    <section class="panel relay-list">
      <div class="rl-head">
        <span class="rl-th name">名称</span>
        <span class="rl-th">域名</span>
        <span class="rl-th">模型</span>
        <span class="rl-th">额度</span>
        <span class="rl-th">权重</span>
        <span class="rl-th op"></span>
      </div>

      <div v-if="loading" class="relay-state"><LoaderCircle :size="20" class="spin" /><span>正在从后端加载中转站…</span></div>
      <div v-else-if="loadError" class="relay-state error">
        <AlertTriangle :size="20" />
        <span>{{ loadError }}</span>
        <button class="btn btn-ghost btn-sm" @click="loadData"><RefreshCw :size="14" /> 重试</button>
      </div>
      <div v-else-if="!filtered.length" class="relay-state"><Server :size="20" /><span>{{ relays.length ? '没有匹配的中转站' : '还没有配置中转站' }}</span></div>

      <div v-else>
        <div v-for="r in pageItems" :key="r.id" class="rl-row" :class="{ open: expandedId === r.id, off: r.status !== 'active', abnormal: r.status === 'abnormal' }">
          <div class="rl-main" @click="toggleExpand(r.id)">
            <div class="rl-cell name">
              <span class="rl-avatar"><Server :size="14" /></span>
              <div class="rl-names">
                <div class="rl-name-row">
                  <strong>{{ r.name }}</strong>
                </div>
              </div>
            </div>
            <div class="rl-cell">
              <a v-if="r.consoleUrl" :href="r.consoleUrl" target="_blank" rel="noopener noreferrer" class="mono rl-host" :title="r.consoleUrl" @click.stop>
                <span>{{ r.consoleUrl }}</span>
                <ExternalLink :size="12" />
              </a>
              <span v-else class="mono rl-host faint">{{ r.url || '-' }}</span>
            </div>
            <div class="rl-cell models">
              <span v-for="m in r.models.slice(0, 3)" :key="m.id" class="tag">{{ m.name }}</span>
              <span v-if="r.models.length > 3" class="tag neutral">+{{ r.models.length - 3 }}</span>
            </div>
            <div class="rl-cell money">
              <span class="mono rl-bal" :class="{ low: r.balance < 20 }">{{ fmtMoney(r.balance, r.quotaUnit) }}</span>
              <span class="mono rl-used">{{ fmtMoney(r.used, r.quotaUnit) }}</span>
            </div>
            <div class="rl-cell weight"><span class="mono rl-weight">{{ r.raw.weight }}</span></div>
            <div class="rl-cell op">
              <button class="icon-btn op-toggle" :disabled="busyIds.has(r.id)" :title="r.status === 'active' ? '停用' : '启用'" @click.stop="toggleStatus(r)">
                <CirclePlay v-if="r.status === 'active'" :size="14" class="ico-on" />
                <AlertTriangle v-else-if="r.status === 'abnormal'" :size="14" class="ico-abnormal" />
                <CirclePause v-else :size="14" class="ico-off" />
              </button>
              <button
                class="icon-btn checkin-btn"
                :class="{ done: r.consoleSyncSupported && isToday(r.checkinAt), pending: r.consoleSyncSupported && !isToday(r.checkinAt) }"
                :disabled="!r.consoleSyncSupported || busyIds.has(r.id)"
                :title="!r.consoleSyncSupported ? '未绑定签到工作流' : r.checkinAt ? '已签到 ' + formatDate(r.checkinAt) + ' · 点击重新签到' : '执行签到工作流'"
                @click.stop="checkin(r)"
              >
                <LoaderCircle v-if="busyIds.has(r.id) && !cookieBusyIds.has(r.id)" :size="14" class="spin" />
                <CalendarCheck v-else :size="14" />
              </button>
              <button
                class="icon-btn"
                :disabled="!r.consoleUrl || busyIds.has(r.id)"
                title="从 Chrome 同步登录凭据"
                @click.stop="syncCookies(r)"
              >
                <LoaderCircle v-if="cookieBusyIds.has(r.id)" :size="14" class="spin" />
                <Cookie v-else :size="14" />
              </button>
              <button class="icon-btn op-edit" title="编辑" @click.stop="openEditRelay(r)"><Pencil :size="14" /></button>
              <button class="icon-btn op-del" title="删除" @click.stop="askRemove(r)"><Trash2 :size="14" /></button>
              <ChevronDown :size="16" class="rl-chev" :class="{ on: expandedId === r.id }" />
            </div>
          </div>

          <Transition name="detail">
            <div v-if="expandedId === r.id" class="rl-detail">
              <div class="rld-grid">
                <!-- account -->
                <div class="rld-col">
                  <div class="rld-title">账户信息</div>
                  <div class="r-account">
                    <div class="ra-cell">
                      <div class="ra-head">
                        <div class="ra-ico cyan"><Wallet :size="12" /></div>
                        <div class="ra-label">账户余额</div>
                      </div>
                      <div class="ra-val mono" :class="{ low: r.balance < 20 }">{{ fmtMoney(r.balance, r.quotaUnit) }}</div>
                    </div>
                    <div class="ra-cell">
                      <div class="ra-head">
                        <div class="ra-ico violet"><Coins :size="12" /></div>
                        <div class="ra-label">累计用额</div>
                      </div>
                      <div class="ra-val mono">{{ fmtMoney(r.used, r.quotaUnit) }}</div>
                    </div>
                    <div
                      class="ra-cell"
                      :class="{ clickable: r.consoleSyncSupported && !isToday(r.checkinAt) }"
                      @click="r.consoleSyncSupported && !isToday(r.checkinAt) && checkin(r)"
                    >
                      <div class="ra-head">
                        <div class="ra-ico emerald"><CalendarCheck :size="12" /></div>
                        <div class="ra-label">今日签到</div>
                      </div>
                      <div class="ra-val mono" :class="signinState(r)">{{ signinText(r) }}</div>
                    </div>
                  </div>
                  <div class="r-info">
                    <div class="ri-row">
                      <User :size="13" />
                      <span class="ri-label">用户信息</span>
                      <span class="ri-val mono">{{ r.username || '-' }}</span>
                    </div>
                    <div class="ri-row">
                      <CalendarCheck :size="13" />
                      <span class="ri-label">签到时间</span>
                      <span class="ri-val mono" :class="{ faint: !r.checkinAt }">{{ r.checkinAt ? formatDate(r.checkinAt) : '-' }}</span>
                    </div>
                    <div class="ri-row">
                      <Globe :size="13" />
                      <span class="ri-label">代理服务</span>
                      <span class="ri-val">{{ r.proxyId ? proxyName(r.proxyId) : '-' }}</span>
                    </div>
                  </div>
                </div>

                <!-- keys -->
                <div class="rld-col">
                  <div class="rld-title">已配置 API Key · {{ r.keys.length }}</div>
                  <div class="rld-keys">
                    <div v-for="k in r.keys" :key="k.id" class="rk-item">
                      <div class="rk-top">
                        <span class="rk-name"><KeyRound :size="12" />{{ k.name || '未知' }}</span>
                        <span class="rk-group">{{ k.group || 'default' }}</span>
                        <span class="rk-used mono"><Coins :size="11" />{{ fmtMoney(k.usedTokens, r.quotaUnit) }}</span>
                      </div>
                      <div class="rk-key mono">{{ k.key }}</div>
                      <div class="rk-bottom">
                        <span v-for="m in k.models" :key="m" class="rk-model" :class="{ mapped: k.modelMap[m] }">
                          {{ m }}<template v-if="k.modelMap[m]"><ArrowRight :size="11" /><em class="mono">{{ k.modelMap[m] }}</em></template>
                        </span>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- models -->
                <div v-if="r.pricingModels.length" class="rld-col">
                  <div class="rld-title">可用模型 · {{ r.pricingModels.length }}</div>
                  <div class="rm-list">
                    <div v-for="m in r.pricingModels" :key="m.id" class="rm-item">
                      <span class="tag rm-tag">{{ m.name }}</span>
                      <span class="rm-group">{{ m.group || '-' }}</span>
                      <span v-if="m.billingType === 'fixed'" class="rm-price mono">{{ fmtPrice(m.priceIn, r.quotaUnit) }} / 次</span>
                      <span v-else class="rm-price mono">in {{ fmtPrice(m.priceIn, r.quotaUnit) }} / out {{ fmtPrice(m.priceOut, r.quotaUnit) }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </Transition>
        </div>
      </div>

      <div v-show="!loading && !loadError" class="pagination">
        <span class="page-info mono">{{ filtered.length }} 条记录 · 第 {{ page }}/{{ totalPages }} 页</span>
        <div class="page-btns">
          <button class="icon-btn" :disabled="page <= 1" @click="goPage(page - 1)" aria-label="上一页"><ChevronLeft :size="15" /></button>
          <button
            v-for="i in totalPages"
            :key="i"
            class="page-num mono"
            :class="{ on: i === page }"
            @click="goPage(i)"
          >{{ i }}</button>
          <button class="icon-btn" :disabled="page >= totalPages" @click="goPage(page + 1)" aria-label="下一页"><ChevronRight :size="15" /></button>
        </div>
      </div>
    </section>

    <!-- add / edit modal -->
    <Modal :open="showForm" :title="isEditing ? '编辑中转站' : '添加中转站'" :subtitle="isEditing ? '修改后端账户配置，保存后立即生效' : '接入后端账户并配置 API Key 与签到工作流'" :icon="Server" width="760px" @close="showForm = false">
      <div class="relay-form">
        <section class="relay-form-section">
          <div class="ke-head">
            <span class="field-label">后端服务</span>
            <em class="ke-hint">上游 API 服务的连接信息</em>
          </div>

          <div class="field">
            <label class="field-label">名称 <span class="req">*</span></label>
            <input v-model="form.name" class="input" placeholder="例如：OpenAI Primary" />
          </div>

          <div class="field">
            <label class="field-label">Server URL</label>
            <input v-model="form.consoleUrl" class="input mono" placeholder="https://console.example.com" />
          </div>

          <div class="form-grid-2 checkin-config-row">
            <div class="field">
              <label class="field-label">签到工作流</label>
              <select v-model="form.consoleCheckinWorkflowId" class="select">
                <option value="">不启用签到</option>
                <option v-for="wf in workflowOptions" :key="wf.id" :value="wf.id">{{ wf.id }} · {{ wf.name }}</option>
              </select>
            </div>
            <div class="field manual-checkin-field">
              <label class="field-label">手工签到</label>
              <div class="manual-checkin-control">
                <button
                  type="button"
                  class="switch"
                  :class="{ on: form.manualCheckin }"
                  role="switch"
                  :aria-checked="form.manualCheckin"
                  title="手工签到"
                  @click="form.manualCheckin = !form.manualCheckin"
                ></button>
                <span>{{ form.manualCheckin ? '启用' : '关闭' }}</span>
              </div>
            </div>
          </div>

          <div class="form-grid-2">
            <div class="field">
              <label class="field-label">权重 <em class="ke-hint">调度优先级 1-100</em></label>
              <input v-model.number="form.weight" class="input mono" type="number" min="1" max="100" />
            </div>
            <div class="field">
              <label class="field-label">代理</label>
              <select v-model="form.proxyId" class="select">
                <option value="">无代理</option>
                <option v-for="p in proxyOptions.filter((proxy) => proxy.enabled)" :key="p.id" :value="p.id">{{ p.name }} · {{ p.address }}</option>
              </select>
            </div>
          </div>

          <div class="form-grid-2">
            <div class="field">
              <label class="field-label">用户名</label>
              <input v-model="form.username" class="input mono" placeholder="admin" />
            </div>
            <div class="field">
              <label class="field-label">密码</label>
              <input v-model="form.password" class="input mono" type="password" placeholder="••••••" />
            </div>
          </div>

          <div class="field">
            <label class="field-label">Headers</label>
            <textarea v-model="form.consoleHeaders" class="textarea mono" rows="3" placeholder="Authorization: Bearer token&#10;Cookie: session=abc123"></textarea>
            <em v-if="consoleHeadersError" class="ke-hint">{{ consoleHeadersError }}</em>
          </div>
        </section>

        <section class="relay-form-section">
          <div class="ke-head">
            <span class="field-label">转发配置</span>
            <em class="ke-hint">模型路由与负载均衡</em>
          </div>

          <div class="field">
            <label class="field-label">Base URL <span class="req">*</span></label>
            <input v-model="form.url" class="input mono" placeholder="https://api.openai.com/v1" />
          </div>

          <div class="keys-editor">
            <div class="ke-head">
              <span class="field-label">API Keys <em class="ke-hint">可选；添加后每个 Key 至少绑定一个模型</em></span>
              <button class="btn btn-ghost btn-sm" @click="addKeyRow"><Plus :size="13" /> Key</button>
            </div>
            <div v-for="(k, i) in form.keys" :key="i" class="ke-card">
              <div class="ke-head">
                <span class="field-label">Key {{ i + 1 }}</span>
                <button class="icon-btn danger" title="移除" @click="removeKeyRow(i)"><Trash2 :size="14" /></button>
              </div>
              <div class="field">
                <label class="field-label">API Key <span class="req">*</span></label>
                <div class="secret-input">
                  <input v-model="k.key" class="input mono" :type="k.keyVisible ? 'text' : 'password'" placeholder="sk-..." />
                  <button
                    type="button"
                    class="secret-toggle"
                    :title="k.keyVisible ? '隐藏 API Key' : '显示 API Key'"
                    :aria-label="k.keyVisible ? '隐藏 API Key' : '显示 API Key'"
                    @click="k.keyVisible = !k.keyVisible"
                  >
                    <EyeOff v-if="k.keyVisible" :size="15" />
                    <Eye v-else :size="15" />
                  </button>
                </div>
              </div>
              <div class="field">
                <label class="field-label">Model <span class="req">*</span><em class="ke-hint">多个模型使用英文逗号分隔</em></label>
                <input v-model="k.modelsInput" class="input mono" placeholder="gpt-4o, claude-3-5-sonnet, deepseek-chat" />
              </div>
              <div class="field">
                <label class="field-label">Model Mapping <em class="ke-hint">客户端模型名 → 上游模型名</em></label>
                <input v-model="k.modelMappingInput" class="input mono" placeholder='{ "gpt-4o": "azure-gpt-4o" }' />
                <em v-if="modelMappingErrors[i]" class="ke-hint">{{ modelMappingErrors[i] }}</em>
              </div>
            </div>
            <div v-if="!form.keys.length" class="ke-empty">无需 API Key 时可直接保存，也可以点击右上角添加。</div>
          </div>

          <div class="field">
            <label class="field-label">上游协议</label>
            <select v-model="form.protocol" class="select">
              <option value="openai">OpenAI</option>
              <option value="anthropic">Anthropic</option>
              <option value="both">OpenAI + Anthropic</option>
            </select>
          </div>
        </section>
      </div>

      <template #footer>
        <button class="btn btn-ghost" @click="showForm = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="saveRelay">
          <LoaderCircle v-if="saving" :size="15" class="spin" />
          <Pencil v-if="!saving && isEditing" :size="15" />
          <Plus v-else-if="!saving" :size="15" />
          {{ isEditing ? '保存' : '添加' }}
        </button>
      </template>
    </Modal>

    <!-- delete confirm -->
    <Modal
      :open="!!relayToDelete"
      title="删除中转站"
      :subtitle="relayToDelete ? `确定删除「${relayToDelete.name}」吗？该操作会移除账户、API Key 与模型配置，不可恢复。` : ''"
      :icon="Trash2"
      width="440px"
      @close="relayToDelete = null"
    >
      <div v-if="relayToDelete" class="confirm-body">
        <div class="cf-row">
          <span class="cf-label">平台</span>
          <span class="tag" :class="platformColor[relayToDelete.platform] || 'neutral'">{{ relayToDelete.platform }}</span>
        </div>
        <div class="cf-row">
          <span class="cf-label">域名</span>
          <span class="mono">{{ hostOf(relayToDelete.url) }}</span>
        </div>
        <div class="cf-row">
          <span class="cf-label">API Key</span>
          <span class="mono">{{ relayToDelete.keys.length }} 个</span>
        </div>
        <div class="cf-row">
          <span class="cf-label">可用模型</span>
          <span class="mono">{{ relayToDelete.models.length }} 个</span>
        </div>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="relayToDelete = null">取消</button>
        <button class="btn btn-danger" @click="confirmRemove"><Trash2 :size="15" /> 确认删除</button>
      </template>
    </Modal>

  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.btn-danger { background: var(--danger); color: #fff; }
.btn-danger:hover { background: color-mix(in srgb, var(--danger) 82%, #000); }
.btn-danger:active { transform: translateY(0) scale(0.98); }
.btn-purple { color: #8b5cf6; }
.btn-purple:hover { color: #8b5cf6; background: rgba(139, 92, 246, 0.12); }
.spin { animation: relay-spin 0.9s linear infinite; }
@keyframes relay-spin { to { transform: rotate(360deg); } }
.relay-state {
  min-height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-faint);
  font-size: 13px;
}
.relay-state.error { color: var(--danger); flex-wrap: wrap; }
.relay-state.error .btn { color: var(--text-soft); }
.pagination {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-top: 1px solid var(--border-soft);
  flex-wrap: wrap; gap: 10px;
}
.page-info { font-size: 12px; color: var(--text-muted); }
.page-btns { display: flex; align-items: center; gap: 4px; }
.page-num {
  min-width: 32px; height: 32px; padding: 0 8px;
  border-radius: 9px; border: 1px solid transparent;
  background: transparent; color: var(--text-muted);
  font-size: 12.5px; font-weight: 600; cursor: pointer;
  transition: all 0.2s ease;
}
.page-num:hover { background: var(--surface-2); color: var(--text); }
.page-num.on { background: var(--grad); color: #fff; box-shadow: 0 2px 14px rgba(139,92,246,0.4); }
.checkin-btn.done { color: #8b5cf6; }
.checkin-btn.done:hover { color: #8b5cf6; background: rgba(139, 92, 246, 0.12); }
.checkin-btn.pending { position: relative; }
.checkin-btn.pending::after {
  content: '!';
  position: absolute;
  top: 2px; right: 2px;
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--danger);
  color: #fff;
  font-size: 6px;
  font-weight: 700;
  line-height: 8px;
  text-align: center;
  pointer-events: none;
}

.rl-cell.op .checkin-btn { color: var(--info); }
.rl-cell.op .checkin-btn:hover { color: var(--info); background: color-mix(in srgb, var(--info) 10%, transparent); }
.rl-cell.op .checkin-btn.done { color: #8b5cf6; }
.rl-cell.op .checkin-btn.done:hover { color: #8b5cf6; background: rgba(139, 92, 246, 0.12); }
.rl-cell.op .checkin-btn:disabled { color: var(--text-faint); opacity: 0.5; cursor: not-allowed; }

.confirm-body { display: flex; flex-direction: column; gap: 8px; }
.cf-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 9px 12px;
  background: var(--surface);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  font-size: 13px;
}
.cf-label { font-size: 11px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.05em; }

.toolbar { display: flex; align-items: center; gap: 14px; padding: 14px 16px; flex-wrap: wrap; }
.spacer { flex: 1; }
.filter-group { display: flex; gap: 6px; overflow-x: auto; max-width: 100%; padding-bottom: 2px; }
.filter-chip {
  flex: none;
  padding: 6px 12px;
  border-radius: 99px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
  white-space: nowrap;
}
.filter-chip:hover { border-color: var(--border-strong); color: var(--text); }
.filter-chip.on { border-color: var(--primary); color: var(--primary); background: rgba(34,211,238,0.1); box-shadow: 0 0 0 3px rgba(34,211,238,0.08); }

/* ---------- list ---------- */
.relay-list { display: flex; flex-direction: column; overflow: hidden; padding: 0; position: relative; }
.rl-head {
  display: grid;
  grid-template-columns: minmax(160px, 0.9fr) minmax(180px, 1.15fr) minmax(240px, 1.9fr) 96px 48px 170px;
  gap: 14px;
  align-items: center;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.rl-th { font-size: 10.5px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase; color: var(--text-faint); }
.rl-th.name { display: flex; align-items: center; gap: 8px; }
.rl-th.op { text-align: right; }

.rl-row { border-bottom: 1px solid var(--border-soft); transition: background 0.2s ease; }
.rl-row:last-child { border-bottom: none; }
.rl-row:hover { background: var(--surface-2); }
.rl-row.off { opacity: 0.6; }
.rl-row.off:hover { opacity: 1; }
.rl-row.off .rl-avatar { filter: grayscale(0.9); opacity: 0.6; }
.rl-row.off strong { color: var(--text-faint); }
.rl-row.open { background: var(--surface-2); }

.rl-main {
  display: grid;
  grid-template-columns: minmax(160px, 0.9fr) minmax(180px, 1.15fr) minmax(240px, 1.9fr) 96px 48px 170px;
  gap: 14px;
  align-items: center;
  padding: 13px 18px;
  cursor: pointer;
  user-select: none;
}
.rl-cell { min-width: 0; }

.rl-avatar {
  width: 34px; height: 34px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border);
  background: rgba(34,211,238,0.12); color: var(--c1);
  flex: none;
}

.rl-cell.name { display: flex; align-items: center; gap: 10px; }
.rl-names { min-width: 0; display: flex; flex-direction: column; }
.rl-name-row { display: flex; align-items: center; gap: 6px; min-width: 0; }
.rl-names strong { font-size: 15.5px; font-weight: 600; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.rl-host {
  display: inline-flex; align-items: center; gap: 5px; max-width: 100%;
  font-size: 12px; color: var(--text-muted); text-decoration: none;
}
.rl-host span { min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.rl-host svg { flex: none; color: var(--text-faint); }
.rl-host:hover { color: var(--primary); }
.rl-host:hover svg { color: currentColor; }

.rl-cell.models { display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }
.rl-cell.models .tag { font-size: 10.5px; }

.rl-cell.money { display: flex; flex-direction: column; }
.rl-bal { font-size: 13px; font-weight: 700; color: var(--text); }
.rl-bal.low { color: var(--danger); }
.rl-used { font-size: 11px; color: var(--text-faint); }
.rl-cell.weight { display: flex; align-items: center; }
.rl-weight { font-size: 13px; font-weight: 700; color: var(--text-soft); }

.rl-cell.checkin { display: flex; }
.rl-cell.checkin .pill { font-size: 11px; }
.rl-cell.checkin .btn { padding: 4px 9px; }

.rl-cell.op { display: flex; align-items: center; justify-content: flex-end; gap: 2px; }
.rl-cell.op .ico-on { color: var(--success); }
.rl-cell.op .ico-off { color: var(--text-faint); }
.rl-cell.op .ico-abnormal { color: var(--warning); }
.rl-cell.op .op-toggle { color: var(--text-muted); }
.rl-cell.op .op-toggle:hover { background: var(--surface-2); }
.rl-cell.op .op-edit { color: #fbbf24; }
.rl-cell.op .op-edit:hover { color: #fbbf24; background: rgba(251, 191, 36, 0.14); }
.rl-cell.op .op-del { color: var(--danger); }
.rl-cell.op .op-del:hover { color: var(--danger); background: var(--danger-soft); }
.rl-chev { color: var(--text-faint); transition: transform 0.25s var(--ease-out); }
.rl-chev.on { transform: rotate(180deg); color: var(--primary); }

/* detail */
.rl-detail {
  padding: 16px 18px 18px;
  border-top: 1px solid var(--border-soft);
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.rld-grid { display: grid; grid-template-columns: minmax(250px, 0.85fr) 1fr 1.05fr; gap: 16px; align-items: start; }
.rld-col { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.rld-title { font-size: 11px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-faint); }

.r-account { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
.ra-cell {
  display: flex; flex-direction: column; gap: 6px;
  padding: 10px 10px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border-soft);
  min-width: 0;
}
.ra-head { display: flex; align-items: center; gap: 6px; min-width: 0; }
.ra-ico { width: 20px; height: 20px; border-radius: 6px; display: flex; align-items: center; justify-content: center; flex: none; }
.ra-ico.cyan { background: rgba(34,211,238,0.12); color: var(--c1); }
.ra-ico.violet { background: rgba(139,92,246,0.12); color: var(--c2); }
.ra-ico.emerald { background: rgba(52,211,153,0.12); color: var(--c4); }
.ra-label { font-size: 10.5px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.04em; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ra-val { font-size: 15px; font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ra-val.low { color: var(--danger); }
.ra-val.ok { color: var(--success); }
.ra-val.warn { color: var(--warning); }
.ra-val.faint { color: var(--text-faint); font-size: 14px; }
.ra-cell.clickable { cursor: pointer; transition: border-color 0.2s ease, background 0.2s ease; }
.ra-cell.clickable:hover { border-color: var(--border-strong); background: var(--surface-2); }

.r-info {
  display: flex; flex-direction: column; gap: 7px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border-soft);
}
.ri-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); }
.ri-row svg { width: 13px; height: 13px; color: var(--text-faint); flex: none; }
.ri-label { width: 76px; color: var(--text-faint); font-size: 11px; font-weight: 600; letter-spacing: 0.03em; flex: none; }
.ri-val { flex: 1; color: var(--text-soft); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ri-val.faint { color: var(--text-faint); }

.rm-list { display: flex; flex-direction: column; gap: 6px; max-height: 200px; overflow-y: auto; padding-right: 2px; }
.rm-item {
  display: grid;
  grid-template-columns: 1.2fr 1fr 1.5fr;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  background: var(--surface);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  font-size: 11.5px;
}
.rm-tag { font-size: 11px; justify-self: start; }
.rm-group { font-size: 11px; color: var(--text-muted); }
.rm-price { font-size: 11px; color: var(--text-muted); text-align: right; justify-self: end; }

.rld-keys { display: flex; flex-direction: column; gap: 10px; }
.rk-item {
  display: flex; flex-direction: column; gap: 7px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border-soft);
}
.rk-top { display: flex; align-items: center; gap: 8px; }
.rk-name { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; font-weight: 600; color: var(--text-soft); min-width: 0; }
.rk-name svg { color: var(--primary); flex: none; }
.rk-group { font-size: 11px; color: var(--text-muted); background: var(--surface-3); padding: 2px 7px; border-radius: 5px; white-space: nowrap; }
.rk-used { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; font-size: 11px; font-weight: 600; color: var(--text-soft); white-space: nowrap; }
.rk-used svg { color: var(--text-faint); }
.rk-key {
  font-size: 12px;
  color: var(--text-soft);
  font-family: var(--font-mono);
  word-break: break-all;
  padding: 6px 9px;
  background: var(--surface-2);
  border-radius: 6px;
  border: 1px solid var(--border-soft);
}
.rk-bottom { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
.rk-user { font-size: 11px; color: var(--text-muted); }
.rk-model {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 6px;
  background: var(--surface-3);
  color: var(--text-soft);
  border: 1px solid var(--border-soft);
}
.rk-model svg { color: var(--text-faint); }
.rk-model em { font-style: normal; color: var(--primary); }
.rk-model.mapped { border-color: rgba(34,211,238,0.35); }

.relay-form { display: flex; flex-direction: column; gap: 10px; }
.relay-form-section { display: flex; flex-direction: column; gap: 7px; }
.relay-form .field { gap: 4px; margin-bottom: 0; }
.form-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.checkin-config-row { align-items: end; }
.manual-checkin-field { min-width: 0; }
.manual-checkin-control { min-height: 34px; display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--text-muted); }
.manual-checkin-control .switch { padding: 0; }
.secret-input { position: relative; }
.secret-input .input { width: 100%; padding-right: 38px; }
.secret-toggle {
  position: absolute; top: 50%; right: 7px; transform: translateY(-50%);
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; padding: 0;
  border: 0; border-radius: 7px; background: transparent;
  color: var(--text-faint); cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
}
.secret-toggle:hover { color: var(--text); background: var(--surface-3); }
.secret-toggle:focus-visible { outline: 2px solid var(--primary); outline-offset: 1px; }
.keys-editor { display: flex; flex-direction: column; gap: 6px; }
.ke-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.ke-hint { font-style: normal; font-size: 11px; color: var(--text-faint); font-weight: 500; margin-left: 6px; }
.ke-card {
  display: flex; flex-direction: column; gap: 6px;
  padding: 14px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border);
}
.ke-top { display: flex; align-items: flex-end; gap: 10px; }
.ke-top .field { flex: 1; }
.ke-empty { font-size: 12px; color: var(--text-faint); text-align: center; padding: 14px 0; border: 1px dashed var(--border-strong); border-radius: var(--radius-sm); }

.detail-enter-active, .detail-leave-active { transition: all 0.3s var(--ease-out); }
.detail-enter-from, .detail-leave-to { opacity: 0; transform: translateY(-6px); }

@media (max-width: 1200px) { .rld-grid { grid-template-columns: 1fr; } }
@media (max-width: 640px) { .form-grid-2 { grid-template-columns: 1fr; } }
</style>
