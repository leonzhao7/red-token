<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import {
  Plus,
  Search,
  Server,
  CalendarCheck,
  Pencil,
  Trash2,
  Globe,
  KeyRound,
  Wallet,
  Coins,
  User,
  CircleStop,
  CirclePlay,
  ChevronDown,
  ArrowRight,
  AlertTriangle,
  LoaderCircle,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  ExternalLink
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
  syncBackend,
  updateBackend,
  type BackendApiKey,
  type BackendResponse,
  type BackendType,
  type SocksProxyResponse
} from '../api/backends'
import type { Relay, RelayKey, RelayModel, PlatformType } from '../types'

const search = ref('')
type ConsoleBackendType = Exclude<BackendType, ''>
const platformFilter = ref<'all' | ConsoleBackendType>('all')
type ModelFamily = 'gpt' | 'claude' | 'grok' | 'deepseek' | 'kimi'
const modelFilter = ref<'all' | ModelFamily>('all')
const page = ref(1)
const pageSize = 10
const backendTypes: ConsoleBackendType[] = ['new-api', 'sub2api']
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
  backendType: BackendType
  consoleUrl: string
  consoleSyncSupported: boolean
  raw: BackendResponse
}

const relays = ref<RelayView[]>([])
const proxyOptions = ref<SocksProxyResponse[]>([])
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const syncingAll = ref(false)
const busyIds = ref<Set<string>>(new Set())

function setBusy(id: string, busy: boolean) {
  const next = new Set(busyIds.value)
  busy ? next.add(id) : next.delete(id)
  busyIds.value = next
}

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

function consoleUserIdOf(backend: BackendResponse) {
  const account = parseObject(backend.console_account_json)
  if (typeof account.id === 'number' && Number.isFinite(account.id) && account.id > 0) return String(Math.trunc(account.id))
  return typeof account.id === 'string' ? account.id.trim() : ''
}

function numberValue(value: unknown) {
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function quotaToCurrency(value: unknown, account: Record<string, any>) {
  const amount = numberValue(value)
  const unit = numberValue(account.quota_per_unit) || 500000
  const exchangeRate = numberValue(account.custom_currency_exchange_rate) || 1
  return (amount / unit) * exchangeRate
}

function accountValues(backend: BackendResponse, keys: BackendApiKey[]) {
  const account = parseObject(backend.console_account_json)
  const directBalance = account.balance
  const balance = directBalance !== undefined
    ? numberValue(directBalance)
    : quotaToCurrency(account.quota, account)
  const directUsed = account.total_actual_cost
  const used = directUsed !== undefined
    ? numberValue(directUsed)
    : account.used_quota !== undefined
      ? quotaToCurrency(account.used_quota, account)
      : 0
  const keyUsed = keys.reduce((sum, key) => sum + numberValue(key.used_quota), 0)
  return {
    balance,
    used: directUsed !== undefined || account.used_quota !== undefined ? used : quotaToCurrency(keyUsed, account),
    username: String(account.username || account.email || account.id || backend.console_username || ''),
    checkinAt: String(account.last_checkin_at || '')
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

function pricingByModel(backend: BackendResponse) {
  const result = new Map<string, { input: number; output: number }>()
  const pricing = parseObject(backend.console_pricing_json)
  const records = Array.isArray(pricing.data) ? pricing.data : []
  for (const item of records) {
    if (!item || typeof item !== 'object') continue
    const record = item as Record<string, any>
    const name = String(record.model_name || record.model || '').trim()
    if (!name) continue
    const input = numberValue(record.input_price ?? record.prompt_price ?? record.model_price ?? record.input_cost)
    const output = numberValue(record.output_price ?? record.completion_price ?? record.output_cost)
    result.set(name, { input, output })
  }
  return result
}

function modelInfo(name: string, group: string, pricing: Map<string, { input: number; output: number }>): RelayModel {
  const known = MODEL_CATALOG.find((model) => model.name === name)
  const price = pricing.get(name)
  return {
    id: `model-${name}`,
    name,
    group: known?.group || group,
    priceIn: price?.input || known?.priceIn || 0,
    priceOut: price?.output || known?.priceOut || 0
  }
}

function mapBackend(backend: BackendResponse): RelayView {
  const keys = Array.isArray(backend.api_keys) ? backend.api_keys : []
  const pricing = pricingByModel(backend)
  const modelNames = new Set<string>()
  const relayKeys: RelayKey[] = keys.map((key, index) => {
    const models = Array.isArray(key.models) ? key.models.filter(Boolean) : []
    models.forEach((model) => modelNames.add(model))
    Object.keys(key.model_mapping || {}).forEach((model) => modelNames.add(model))
    return {
      id: `${backend.id}-key-${index}`,
      name: key.group || `Key ${index + 1}`,
      username: '',
      key: key.api_key,
      models,
      modelMap: key.model_mapping || {},
      usedTokens: numberValue(key.used_quota)
    }
  })
  const account = accountValues(backend, keys)
  const group = platformOf(backend)
  const models = [...modelNames].map((name) => modelInfo(name, group, pricing))
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
    proxyId: backend.proxy_id ? String(backend.proxy_id) : '',
    models,
    keys: relayKeys,
    protocol: protocolOf(backend.protocol),
    backendType: backend.backend_type === 'sub2api' || backend.backend_type === 'new-api' ? backend.backend_type : '',
    consoleUrl: backend.console_url || '',
    consoleSyncSupported: backend.backend_type === 'new-api' || backend.backend_type === 'sub2api',
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
      const okPlat = platformFilter.value === 'all' || r.backendType === platformFilter.value
      const selectedFamily = modelFamilies.find((family) => family.value === modelFilter.value)
      const okModel = !selectedFamily || r.models.some((model) => {
        const name = model.name.toLowerCase()
        return selectedFamily.keywords.some((keyword) => name.includes(keyword))
      })
      return okSearch && okPlat && okModel
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

watch([search, platformFilter, modelFilter], resetPage)
watch(totalPages, (pages) => {
  if (page.value > pages) page.value = pages
})

const fmtUsd = (n: number) => '$' + n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtPrice = (n: number) => '$' + n.toFixed(2)
const fmtTokens = (n: number) => (n >= 1e9 ? (n / 1e9).toFixed(2) + 'B' : (n / 1e6).toFixed(1) + 'M')
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

async function syncRelay(r: RelayView, quiet = false, audit = true) {
  if (!r.consoleUrl) {
    if (!quiet) toast('缺少控制台地址', '请先在编辑中转站中填写 Console URL', 'warning')
    return false
  }
  if (!r.consoleSyncSupported) {
    if (!quiet) toast('暂不支持控制台同步', '该后端未配置 backend_type，请先编辑并选择 new-api 或 sub2api', 'warning')
    return false
  }
  setBusy(r.id, true)
  try {
    const response = await syncBackend(Number(r.id), audit)
    upsertRelay(response.backend)
    if (!quiet) toast('中转站同步完成', `${r.name} · 账户与 API Key 已刷新`, 'success')
    return true
  } catch (error) {
    if (!quiet) toast('中转站同步失败', errorMessage(error), 'danger')
    return false
  } finally {
    setBusy(r.id, false)
  }
}

async function checkin(r: RelayView) {
  if (isToday(r.checkinAt)) {
    toast('今日已同步', `${r.name} · ${formatDate(r.checkinAt)}`, 'info')
  }
  await syncRelay(r)
}

async function checkinAll() {
  const todo = relays.value.filter((relay) => relay.consoleUrl && relay.consoleSyncSupported && !isToday(relay.checkinAt))
  if (!todo.length) {
    toast('今日无需同步', '没有待签到且配置了控制台地址的中转站', 'info')
    return
  }
  syncingAll.value = true
  const results = await Promise.all(todo.map((relay) => syncRelay(relay, true, false)))
  const successCount = results.filter(Boolean).length
  try {
    await recordBackendSyncSummary(todo.length, successCount, todo.length - successCount)
  } catch {
    // The individual sync results are already persisted; summary auditing is best effort.
  }
  syncingAll.value = false
  toast(successCount ? '批量同步完成' : '批量同步失败', `成功 ${successCount} 个，失败 ${todo.length - successCount} 个`, successCount === todo.length ? 'success' : successCount ? 'warning' : 'danger')
}

const relayToDelete = ref<RelayView | null>(null)

function askRemove(r: RelayView) {
  relayToDelete.value = r
}

/* ---- form ---- */
interface KeyFormItem {
  name: string
  key: string
  modelsInput: string
  modelMappingInput: string
  usedTokens: number
}

function defaultKeyFormItem(): KeyFormItem {
  return { name: 'default', key: '', modelsInput: '', modelMappingInput: '', usedTokens: 0 }
}

const form = ref<{
  name: string
  url: string
  protocol: 'openai' | 'anthropic' | 'both'
  backendType: BackendType
  consoleUrl: string
  username: string
  password: string
  newApiRefresh: string
  consoleHeaders: string
  consoleUserId: string
  consoleAuthorization: string
  consoleCheckinPath: string
  channelUrl: string
  proxyId: string
  weight: number
  keys: KeyFormItem[]
}>({
  name: '',
  url: '',
  protocol: 'openai',
  backendType: 'new-api',
  consoleUrl: '',
  username: '',
  password: '',
  newApiRefresh: '',
  consoleHeaders: '',
  consoleUserId: '',
  consoleAuthorization: '',
  consoleCheckinPath: '',
  channelUrl: '',
  proxyId: '',
  weight: 10,
  keys: []
})

const showForm = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => editingId.value !== null)
const isNewAPIBackendType = computed(() => form.value.backendType === 'new-api')
const isSub2APIBackendType = computed(() => form.value.backendType === 'sub2api')
const consoleHeadersError = computed(() => {
  if (!isNewAPIBackendType.value) return ''
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
    name: '', url: '', protocol: 'openai', backendType: 'new-api', consoleUrl: '', username: '', password: '',
    newApiRefresh: '', consoleHeaders: '', consoleUserId: '', consoleAuthorization: '', consoleCheckinPath: '',
    channelUrl: '', proxyId: '', weight: 10, keys: []
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
    backendType: r.backendType,
    consoleUrl: r.raw.console_url || '',
    username: r.raw.console_username || '',
    password: r.raw.console_password || '',
    newApiRefresh: r.raw.new_api_refresh || '',
    consoleHeaders: formatConsoleHeaders(r.raw.console_headers),
    consoleUserId: consoleUserIdOf(r.raw),
    consoleAuthorization: r.raw.console_authorization || '',
    consoleCheckinPath: r.raw.console_checkin_path || '',
    channelUrl: r.raw.channel_url || '',
    proxyId: r.proxyId,
    weight: numberValue(r.raw.weight) || 10,
    keys: r.keys.length
      ? r.keys.map((key) => ({
          name: key.name,
          key: key.key,
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
  const apiKeys: BackendApiKey[] = form.value.keys.map((key, index) => {
    return {
      api_key: key.key.trim(),
      group: key.name.trim() || `key-${index + 1}`,
      models: parseModelList(key.modelsInput),
      model_mapping: parseModelMapping(key.modelMappingInput),
      used_quota: Math.max(0, Math.round(key.usedTokens || 0))
    }
  })
  const backendType = form.value.backendType
  return {
    name: form.value.name.trim(),
    protocol: form.value.protocol,
    backend_type: backendType,
    base_url: form.value.url.trim(),
    api_keys: apiKeys,
    console_url: form.value.consoleUrl.trim(),
    console_username: form.value.username.trim(),
    console_password: form.value.password,
    new_api_refresh: backendType === 'new-api' ? form.value.newApiRefresh.trim() : '',
    console_headers: backendType === 'new-api' ? parseConsoleHeaders(form.value.consoleHeaders) : {},
    console_user_id: backendType === 'new-api' ? form.value.consoleUserId.trim() : '',
    console_authorization: backendType === 'sub2api' ? form.value.consoleAuthorization.trim() : '',
    console_checkin_path: backendType === 'sub2api' ? form.value.consoleCheckinPath.trim() : '',
    channel_url: backendType === 'sub2api' ? form.value.channelUrl.trim() : '',
    proxy_id: form.value.proxyId ? Number(form.value.proxyId) : 0,
    weight: Math.min(100, Math.max(1, Math.round(numberValue(form.value.weight) || 10)))
  }
}

async function saveRelay() {
  if (!form.value.name || !form.value.url) {
    toast('请填写名称与 URL', '', 'warning')
    return
  }
  if (form.value.keys.some((key) => !key.key.trim() || !key.name.trim() || !parseModelList(key.modelsInput).length)) {
    toast('API Key 配置不完整', '已添加的每个 Key 都需要密钥、Group 和至少一个模型', 'warning')
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
    const [backendPage, proxyPage] = await Promise.all([listBackends(), listSocksProxies()])
    relays.value = backendPage.items.map(mapBackend)
    proxyOptions.value = proxyPage.items
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
      </div>
      <div class="filter-group">
        <button class="filter-chip" :class="{ on: platformFilter === 'all' }" @click="platformFilter = 'all'">全部平台</button>
        <button v-for="type in backendTypes" :key="type" class="filter-chip" :class="{ on: platformFilter === type }" @click="platformFilter = type">{{ type }}</button>
      </div>
      <div class="spacer"></div>
      <div class="filter-group">
        <button class="filter-chip" :class="{ on: modelFilter === 'all' }" @click="modelFilter = 'all'">全部模型</button>
        <button v-for="family in modelFamilies" :key="family.value" class="filter-chip" :class="{ on: modelFilter === family.value }" @click="modelFilter = family.value">{{ family.value }}</button>
      </div>
      <button class="btn btn-ghost btn-sm" :disabled="syncingAll" @click="checkinAll">
        <LoaderCircle v-if="syncingAll" :size="14" class="spin" />
        <CalendarCheck v-else :size="14" /> 批量签到
      </button>
      <button class="btn btn-primary btn-sm" :disabled="loading" @click="openCreate"><Plus :size="14" /> 添加中转站</button>
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

      <TransitionGroup v-else name="rl">
        <div v-for="r in pageItems" :key="r.id" class="rl-row" :class="{ open: expandedId === r.id, off: r.status !== 'active', abnormal: r.status === 'abnormal' }">
          <div class="rl-main" @click="toggleExpand(r.id)">
            <div class="rl-cell name">
              <span class="rl-avatar" :class="r.platform.toLowerCase()"><Server :size="14" /></span>
              <div class="rl-names">
                <div class="rl-name-row">
                  <strong :class="{ struck: r.status !== 'active' }">{{ r.name }}</strong>
                  <span v-if="r.status === 'disabled'" class="tag neutral">已停用</span>
                  <span v-else-if="r.status === 'abnormal'" class="tag danger">异常</span>
                </div>
              </div>
            </div>
            <div class="rl-cell">
              <a :href="r.url" target="_blank" rel="noopener noreferrer" class="mono rl-host" :title="r.url" @click.stop>
                <span>{{ hostOf(r.url) }}</span>
                <ExternalLink :size="12" />
              </a>
            </div>
            <div class="rl-cell models">
              <span v-for="m in r.models.slice(0, 3)" :key="m.id" class="tag">{{ m.name }}</span>
              <span v-if="r.models.length > 3" class="tag neutral">+{{ r.models.length - 3 }}</span>
            </div>
            <div class="rl-cell money">
              <span class="mono rl-bal" :class="{ low: r.balance < 20 }">{{ fmtUsd(r.balance) }}</span>
              <span class="mono rl-used">{{ fmtUsd(r.used) }}</span>
            </div>
            <div class="rl-cell weight"><span class="mono rl-weight">{{ r.raw.weight }}</span></div>
            <div class="rl-cell op">
              <button class="icon-btn op-toggle" :disabled="busyIds.has(r.id)" :title="r.status === 'active' ? '停用' : '启用'" @click.stop="toggleStatus(r)">
                <CircleStop v-if="r.status === 'active'" :size="14" class="ico-on" />
                <CirclePlay v-else :size="14" class="ico-off" />
              </button>
              <button class="icon-btn checkin-btn" :class="{ done: isToday(r.checkinAt) }" :disabled="busyIds.has(r.id)" :title="r.checkinAt ? '已同步 ' + formatDate(r.checkinAt) + ' · 点击重新同步' : '签到并同步'" @click.stop="checkin(r)">
                <LoaderCircle v-if="busyIds.has(r.id)" :size="14" class="spin" />
                <CalendarCheck v-else :size="14" />
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
                      <div class="ra-ico cyan"><Wallet :size="14" /></div>
                      <div class="ra-body">
                        <div class="ra-label">账户余额</div>
                        <div class="ra-val mono" :class="{ low: r.balance < 20 }">{{ fmtUsd(r.balance) }}</div>
                      </div>
                    </div>
                    <div class="ra-cell">
                      <div class="ra-ico violet"><Coins :size="14" /></div>
                      <div class="ra-body">
                        <div class="ra-label">累计用额</div>
                        <div class="ra-val mono">{{ fmtUsd(r.used) }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="r-info">
                    <div class="ri-row">
                      <Server :size="13" />
                      <span class="ri-label">平台类型</span>
                      <span class="ri-val mono">{{ r.backendType || '-' }}</span>
                    </div>
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
                        <span class="rk-name"><KeyRound :size="12" />{{ k.name }}</span>
                        <span class="rk-flag" title="已用额度"><Coins :size="11" />{{ fmtTokens(k.usedTokens) }} tokens</span>
                      </div>
                      <div class="rk-key mono">{{ k.key }}</div>
                      <div class="rk-bottom">
                        <span class="rk-user mono">{{ k.name }}</span>
                        <span class="spacer"></span>
                        <span v-for="m in k.models" :key="m" class="rk-model" :class="{ mapped: k.modelMap[m] }">
                          {{ m }}<template v-if="k.modelMap[m]"><ArrowRight :size="11" /><em class="mono">{{ k.modelMap[m] }}</em></template>
                        </span>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- models -->
                <div class="rld-col">
                  <div class="rld-title">可用模型 · {{ r.models.length }}</div>
                  <div class="rm-list">
                    <div v-for="m in r.models" :key="m.id" class="rm-item">
                      <span class="tag rm-tag">{{ m.name }}</span>
                      <span class="rm-group">{{ m.group }}</span>
                      <span class="rm-price mono">in {{ fmtPrice(m.priceIn) }} / out {{ fmtPrice(m.priceOut) }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </Transition>
        </div>
      </TransitionGroup>

      <div v-if="!loading && !loadError" class="pagination">
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
    <Modal :open="showForm" :title="isEditing ? '编辑中转站' : '添加中转站'" :subtitle="isEditing ? '修改后端账户配置，保存后立即生效' : '接入后端账户并配置 API Key 与控制台同步'" :icon="Server" width="760px" @close="showForm = false">
      <div class="relay-form">
        <section class="relay-form-section">
          <div class="ke-head">
            <span class="field-label">后端服务</span>
            <em class="ke-hint">上游 API 服务的连接信息</em>
          </div>

          <div class="form-grid-2">
            <div class="field">
              <label class="field-label">名称 <span class="req">*</span></label>
              <input v-model="form.name" class="input" placeholder="例如：OpenAI Primary" />
            </div>
            <div class="field">
              <label class="field-label">后端类型</label>
              <select v-model="form.backendType" class="select">
                <option value="">通用</option>
                <option value="new-api">new-api</option>
                <option value="sub2api">sub2api</option>
              </select>
            </div>
          </div>

          <div class="field">
            <label class="field-label">Server URL</label>
            <input v-model="form.consoleUrl" class="input mono" placeholder="https://console.example.com" />
          </div>

          <div class="form-grid-2">
            <div class="field">
              <label class="field-label">权重</label>
              <input v-model.number="form.weight" class="input mono" type="number" min="1" max="100" />
              <em class="ke-hint">负载均衡权重 1-100</em>
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

          <div v-if="isSub2APIBackendType" class="field">
            <label class="field-label">Authorization</label>
            <textarea v-model="form.consoleAuthorization" class="textarea mono" rows="2" placeholder="Bearer sk-..."></textarea>
          </div>
          <div v-if="isSub2APIBackendType" class="field">
            <label class="field-label">签到 Path</label>
            <input v-model="form.consoleCheckinPath" class="input mono" placeholder="/api/v1/checkin" />
          </div>
          <div v-if="isSub2APIBackendType" class="field">
            <label class="field-label">渠道 URL</label>
            <input v-model="form.channelUrl" class="input mono" placeholder="/api/v1/channels" />
          </div>

          <div v-if="isNewAPIBackendType" class="field">
            <label class="field-label">Headers</label>
            <textarea v-model="form.consoleHeaders" class="textarea mono" rows="3" placeholder="Authorization: Bearer token&#10;Cookie: session=abc123"></textarea>
            <em v-if="consoleHeadersError" class="ke-hint">{{ consoleHeadersError }}</em>
          </div>
          <div v-if="isNewAPIBackendType" class="form-grid-2">
            <div class="field">
              <label class="field-label">new_api_refresh</label>
              <input v-model="form.newApiRefresh" class="input mono" type="password" placeholder="Refresh token" />
            </div>
            <div class="field">
              <label class="field-label">用户 ID</label>
              <input v-model="form.consoleUserId" class="input mono" placeholder="1929" />
            </div>
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
              <button class="btn btn-ghost btn-sm" @click="addKeyRow"><Plus :size="13" /> 添加 Key</button>
            </div>
            <div v-for="(k, i) in form.keys" :key="i" class="ke-card">
              <div class="ke-head">
                <span class="field-label">Key {{ i + 1 }}</span>
                <button class="icon-btn danger" title="移除" @click="removeKeyRow(i)"><Trash2 :size="14" /></button>
              </div>
              <div class="form-grid-2">
                <div class="field">
                  <label class="field-label">API Key <span class="req">*</span></label>
                  <input v-model="k.key" class="input mono" type="password" placeholder="sk-..." />
                </div>
                <div class="field">
                  <label class="field-label">Group <span class="req">*</span></label>
                  <input v-model="k.name" class="input mono" placeholder="default" />
                </div>
              </div>
              <div class="field">
                <label class="field-label">Models <span class="req">*</span></label>
                <input v-model="k.modelsInput" class="input mono" placeholder="gpt-4o, claude-3-5-sonnet, deepseek-chat" />
                <em class="ke-hint">多个模型使用英文逗号分隔</em>
              </div>
              <div class="field">
                <label class="field-label">Model Mapping <em class="ke-hint">客户端模型名 → 上游模型名</em></label>
                <input v-model="k.modelMappingInput" class="input mono" placeholder='{ "gpt-4o": "azure-gpt-4o" }' />
                <em v-if="modelMappingErrors[i]" class="ke-hint">{{ modelMappingErrors[i] }}</em>
                <em v-else class="ke-hint">JSON 对象，留空表示不转换</em>
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
          {{ isEditing ? '保存修改' : '添加中转站' }}
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
.checkin-btn.done { color: var(--success); }
.checkin-btn.done:hover { color: color-mix(in srgb, var(--success) 75%, #000); background: color-mix(in srgb, var(--success) 10%, transparent); }

.rl-cell.op .checkin-btn { color: #8b5cf6; }
.rl-cell.op .checkin-btn:hover { color: #8b5cf6; background: rgba(139, 92, 246, 0.12); }
.rl-cell.op .checkin-btn.done { color: #8b5cf6; }
.rl-cell.op .checkin-btn.done:hover { color: #8b5cf6; background: rgba(139, 92, 246, 0.12); }

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
.relay-list { display: flex; flex-direction: column; overflow: hidden; padding: 0; }
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
.rl-row.off strong { color: var(--text-faint); text-decoration: line-through; }
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
  flex: none;
}
.rl-avatar.openai { background: rgba(34,211,238,0.12); color: var(--c1); }
.rl-avatar.anthropic { background: rgba(232,121,249,0.12); color: var(--c3); }
.rl-avatar.gemini { background: rgba(56,189,248,0.12); color: var(--c7); }
.rl-avatar.deepseek { background: rgba(139,92,246,0.12); color: var(--c2); }
.rl-avatar.custom { background: rgba(251,191,36,0.12); color: var(--c5); }

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
.rl-cell.op .ico-off { color: var(--info); }
.rl-cell.op .op-toggle { color: var(--info); }
.rl-cell.op .op-toggle:hover { color: var(--info); background: rgba(56, 189, 248, 0.12); }
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

.r-account { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.ra-cell {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border-soft);
}
.ra-ico { width: 30px; height: 30px; border-radius: 9px; display: flex; align-items: center; justify-content: center; flex: none; }
.ra-ico.cyan { background: rgba(34,211,238,0.12); color: var(--c1); }
.ra-ico.violet { background: rgba(139,92,246,0.12); color: var(--c2); }
.ra-body { display: flex; flex-direction: column; }
.ra-label { font-size: 10.5px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.04em; }
.ra-val { font-size: 16px; font-weight: 700; color: var(--text); }
.ra-val.low { color: var(--danger); }

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
.rk-name { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; font-weight: 600; color: var(--text-soft); }
.rk-name svg { color: var(--primary); }
.rk-flag { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; font-size: 10.5px; font-weight: 600; color: var(--text-faint); }
.rk-flag svg { color: var(--text-faint); }
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
.rk-bottom { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
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

.rl-enter-active, .rl-leave-active { transition: all 0.3s var(--ease-out); }
.rl-enter-from, .rl-leave-to { opacity: 0; transform: translateY(-8px); }
.detail-enter-active, .detail-leave-active { transition: all 0.3s var(--ease-out); }
.detail-enter-from, .detail-leave-to { opacity: 0; transform: translateY(-6px); }

@media (max-width: 1200px) { .rld-grid { grid-template-columns: 1fr; } }
@media (max-width: 640px) { .form-grid-2 { grid-template-columns: 1fr; } }
</style>
