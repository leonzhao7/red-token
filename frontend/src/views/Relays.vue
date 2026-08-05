<script setup lang="ts">
import { ref, computed } from 'vue'
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
  ArrowRight
} from 'lucide-vue-next'
import Modal from '../components/Modal.vue'
import { store, proxyName } from '../store'
import { MODEL_CATALOG, PLATFORMS } from '../data/mock'
import { toast } from '../composables/toast'
import type { Relay, RelayKey, PlatformType } from '../types'

const search = ref('')
const platformFilter = ref<'all' | PlatformType>('all')
const statusFilter = ref<'all' | 'active' | 'disabled'>('all')

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
  store.relays.filter((r) => {
    const okSearch = r.name.toLowerCase().includes(search.value.toLowerCase()) || r.url.includes(search.value) || r.username.toLowerCase().includes(search.value.toLowerCase())
    const okPlat = platformFilter.value === 'all' || r.platform === platformFilter.value
    const okStatus = statusFilter.value === 'all' || r.status === statusFilter.value
    return okSearch && okPlat && okStatus
  })
)

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

function toggleStatus(r: Relay) {
  const next = r.status === 'active' ? 'disabled' : 'active'
  store.updateRelay(r.id, { status: next })
  toast(next === 'active' ? '中转站已启用' : '中转站已停用', r.name, next === 'active' ? 'success' : 'warning')
}

function checkin(r: Relay) {
  if (r.checkinAt) {
    toast('今日已签到', `${r.name} · ${r.checkinAt}`, 'info')
    return
  }
  const now = '2026-08-02 ' + new Date().toTimeString().slice(0, 5)
  store.updateRelay(r.id, { checkinAt: now })
  toast('签到成功', `${r.name} · ${now}`, 'success')
}

function checkinAll() {
  const now = '2026-08-02 ' + new Date().toTimeString().slice(0, 5)
  const todo = store.relays.filter((r) => !r.checkinAt)
  todo.forEach((r) => store.updateRelay(r.id, { checkinAt: now }))
  toast(todo.length ? '批量签到完成' : '今日均已签到', todo.length ? `已为 ${todo.length} 个账户完成今日签到` : '无需签到', todo.length ? 'success' : 'info')
}

const relayToDelete = ref<Relay | null>(null)

function askRemove(r: Relay) {
  relayToDelete.value = r
}

function confirmRemove() {
  const r = relayToDelete.value
  if (!r) return
  store.removeRelay(r.id)
  relayToDelete.value = null
  toast('中转站已删除', r.name, 'danger')
}

/* ---- form ---- */
interface KeyFormItem {
  name: string
  username: string
  key: string
  models: string[]
  modelMap: Record<string, string>
  usedTokens: number
}

const form = ref<{
  name: string
  url: string
  platform: PlatformType
  username: string
  balance: number
  used: number
  proxyId: string
  models: string[]
  keys: KeyFormItem[]
}>({
  name: '',
  url: '',
  platform: 'OpenAI',
  username: '',
  balance: 100,
  used: 0,
  proxyId: '',
  models: [],
  keys: []
})

const showForm = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => editingId.value !== null)

function resetForm() {
  form.value = { name: '', url: '', platform: 'OpenAI', username: '', balance: 100, used: 0, proxyId: '', models: [], keys: [] }
}

function openCreate() {
  editingId.value = null
  resetForm()
  showForm.value = true
}

function openEditRelay(r: Relay) {
  editingId.value = r.id
  form.value = {
    name: r.name,
    url: r.url,
    platform: r.platform,
    username: r.username,
    balance: r.balance,
    used: r.used,
    proxyId: r.proxyId,
    models: r.models.map((m) => m.name),
    keys: r.keys.map((k) => ({ ...k, models: [...k.models], modelMap: { ...k.modelMap }, usedTokens: k.usedTokens }))
  }
  showForm.value = true
}

function addKeyRow() {
  if (form.value.keys.length >= 2) return
  form.value.keys.push({ name: '', username: '', key: '', models: [], modelMap: {}, usedTokens: 0 })
}
function removeKeyRow(i: number) {
  form.value.keys.splice(i, 1)
}
function toggleKeyModel(i: number, m: string) {
  const item = form.value.keys[i]
  const idx = item.models.indexOf(m)
  if (idx >= 0) {
    item.models.splice(idx, 1)
    delete item.modelMap[m]
  } else {
    if (item.models.length >= 2) return
    item.models.push(m)
    item.modelMap[m] = m
  }
}

function saveRelay() {
  if (!form.value.name || !form.value.url) {
    toast('请填写名称与 URL', '', 'warning')
    return
  }
  const keys: RelayKey[] = form.value.keys.map((k) => {
    const modelMap: Record<string, string> = {}
    for (const m of k.models) {
      const to = (k.modelMap[m] || '').trim()
      if (to && to !== m) modelMap[m] = to
    }
    return {
      id: k.name || 'rk' + Date.now(),
      name: k.name || '未命名 Key',
      username: k.username,
      key: k.key,
      models: k.models,
      modelMap,
      usedTokens: k.usedTokens || 0
    }
  })
  const patch = {
    name: form.value.name,
    url: form.value.url,
    platform: form.value.platform,
    username: form.value.username,
    balance: form.value.balance,
    used: form.value.used,
    proxyId: form.value.proxyId || store.proxies[0]?.id || '',
    models: MODEL_CATALOG.filter((m) => form.value.models.includes(m.name)),
    keys
  }
  if (isEditing.value && editingId.value) {
    store.updateRelay(editingId.value, patch)
    toast('中转站已更新', patch.name, 'success')
  } else {
    store.addRelay({
      id: 'r' + Date.now(),
      ...patch,
      status: 'active',
      checkinAt: ''
    })
    toast('中转站已创建', '账户信息已接入路由池', 'success')
  }
  showForm.value = false
  resetForm()
  editingId.value = null
}
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
        <button v-for="p in PLATFORMS" :key="p" class="filter-chip" :class="{ on: platformFilter === p }" @click="platformFilter = p">{{ p }}</button>
      </div>
      <div class="spacer"></div>
      <div class="filter-group">
        <button class="filter-chip" :class="{ on: statusFilter === 'all' }" @click="statusFilter = 'all'">全部</button>
        <button class="filter-chip" :class="{ on: statusFilter === 'active' }" @click="statusFilter = 'active'">启用</button>
        <button class="filter-chip" :class="{ on: statusFilter === 'disabled' }" @click="statusFilter = 'disabled'">停用</button>
      </div>
      <button class="btn btn-ghost btn-sm" @click="checkinAll"><CalendarCheck :size="14" /> 批量签到</button>
      <button class="btn btn-primary btn-sm" @click="openCreate"><Plus :size="14" /> 添加中转站</button>
    </section>

    <!-- relay list -->
    <section class="panel relay-list">
      <div class="rl-head">
        <span class="rl-th name">名称</span>
        <span class="rl-th">域名</span>
        <span class="rl-th">使用模型</span>
        <span class="rl-th">平台</span>
        <span class="rl-th">余额 / 用额</span>
        <span class="rl-th">用户名 / ID</span>
        <span class="rl-th op"></span>
      </div>

      <TransitionGroup name="rl">
        <div v-for="r in filtered" :key="r.id" class="rl-row" :class="{ open: expandedId === r.id, off: r.status === 'disabled' }">
          <div class="rl-main" @click="toggleExpand(r.id)">
            <div class="rl-cell name">
              <span class="rl-avatar" :class="r.platform.toLowerCase()"><Server :size="14" /></span>
              <div class="rl-names">
                <div class="rl-name-row">
                  <strong :class="{ struck: r.status === 'disabled' }">{{ r.name }}</strong>
                  <span v-if="r.status === 'disabled'" class="tag neutral">已停用</span>
                </div>
                <span class="rl-sub mono">{{ r.keys.length }} Keys · {{ r.models.length }} 模型</span>
              </div>
            </div>
            <div class="rl-cell"><span class="mono rl-host">{{ hostOf(r.url) }}</span></div>
            <div class="rl-cell models">
              <span v-for="m in r.models.slice(0, 3)" :key="m.id" class="tag">{{ m.name }}</span>
              <span v-if="r.models.length > 3" class="tag neutral">+{{ r.models.length - 3 }}</span>
            </div>
            <div class="rl-cell"><span class="tag" :class="platformColor[r.platform] || 'neutral'">{{ r.platform }}</span></div>
            <div class="rl-cell money">
              <span class="mono rl-bal" :class="{ low: r.balance < 20 }">{{ fmtUsd(r.balance) }}</span>
              <span class="mono rl-used">{{ fmtUsd(r.used) }}</span>
            </div>
            <div class="rl-cell"><span class="mono rl-user" :title="r.username">{{ r.username || '—' }}</span></div>
            <div class="rl-cell op">
              <button class="icon-btn checkin-btn" :class="{ done: !!r.checkinAt }" :title="r.checkinAt ? '已签到 ' + r.checkinAt.slice(11) + ' · 点击重新签到' : '签到'" @click.stop="checkin(r)">
                <CalendarCheck :size="14" />
              </button>
              <button class="icon-btn primary" title="编辑" @click.stop="openEditRelay(r)"><Pencil :size="14" /></button>
              <button class="icon-btn" :title="r.status === 'active' ? '停用' : '启用'" @click.stop="toggleStatus(r)">
                <CircleStop v-if="r.status === 'active'" :size="14" class="ico-on" />
                <CirclePlay v-else :size="14" class="ico-off" />
              </button>
              <button class="icon-btn danger" title="删除" @click.stop="askRemove(r)"><Trash2 :size="14" /></button>
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
                      <User :size="13" />
                      <span class="ri-label">用户名 / ID</span>
                      <span class="ri-val mono">{{ r.username || '—' }}</span>
                    </div>
                    <div class="ri-row">
                      <CalendarCheck :size="13" />
                      <span class="ri-label">签到时间</span>
                      <span class="ri-val mono" :class="{ faint: !r.checkinAt }">{{ r.checkinAt || '未签到' }}</span>
                    </div>
                    <div class="ri-row">
                      <Globe :size="13" />
                      <span class="ri-label">代理服务器</span>
                      <span class="ri-val">{{ r.proxyId ? proxyName(r.proxyId) : '直连' }}</span>
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
                        <span class="rk-user mono">{{ k.username || '—' }}</span>
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
    </section>

    <!-- add / edit modal -->
    <Modal :open="showForm" :title="isEditing ? '编辑中转站' : '添加中转站'" :subtitle="isEditing ? '修改供应商账户配置，保存后立即生效' : '接入模型供应商账户，配置余额、签到与 API Key'" :icon="Server" @close="showForm = false">
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">中转站名称 <span class="req">*</span></label>
          <input v-model="form.name" class="input" placeholder="例如：官方中转 · OpenAI" />
        </div>
        <div class="field">
          <label class="field-label">平台类型</label>
          <select v-model="form.platform" class="select">
            <option v-for="p in PLATFORMS" :key="p" :value="p">{{ p }}</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label class="field-label">Base URL <span class="req">*</span></label>
        <input v-model="form.url" class="input mono" placeholder="https://api.openai.com/v1" />
      </div>
      <div class="field">
        <label class="field-label">用户名 / ID</label>
        <input v-model="form.username" class="input mono" placeholder="openai-master@nexus" />
      </div>
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">账户余额（USD）</label>
          <input v-model.number="form.balance" class="input mono" type="number" min="0" step="0.1" />
        </div>
        <div class="field">
          <label class="field-label">累计用额（USD）</label>
          <input v-model.number="form.used" class="input mono" type="number" min="0" step="0.1" />
        </div>
      </div>
      <div class="field">
        <label class="field-label">连接代理</label>
        <select v-model="form.proxyId" class="select">
          <option value="">直连</option>
          <option v-for="p in store.proxies.filter((x) => x.status === 'active')" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </div>
      <div class="field">
        <label class="field-label">可用模型</label>
        <div class="model-picker">
          <label v-for="m in MODEL_CATALOG" :key="m.id" class="model-chip" :class="{ on: form.models.includes(m.name) }">
            <input v-model="form.models" type="checkbox" :value="m.name" />
            <span class="tag">{{ m.name }}</span>
          </label>
        </div>
      </div>

      <div class="keys-editor">
        <div class="ke-head">
          <span class="field-label">已配置 API Key <em class="ke-hint">1–2 个，每个绑定 1–2 个模型</em></span>
          <button class="btn btn-ghost btn-sm" :disabled="form.keys.length >= 2" @click="addKeyRow"><Plus :size="13" /> 添加 Key</button>
        </div>
        <div v-for="(k, i) in form.keys" :key="i" class="ke-card">
          <div class="ke-top">
            <div class="field">
              <label class="field-label">Key 名称</label>
              <input v-model="k.name" class="input mono" placeholder="主用 Key" />
            </div>
            <button class="icon-btn danger" title="移除" @click="removeKeyRow(i)"><Trash2 :size="14" /></button>
          </div>
          <div class="form-grid-2">
            <div class="field">
              <label class="field-label">用户名</label>
              <input v-model="k.username" class="input mono" placeholder="sk-proj-****" />
            </div>
            <div class="field">
              <label class="field-label">API Key <em class="ke-hint">将完整显示，长度 10–30 位</em></label>
              <input v-model="k.key" class="input mono" placeholder="sk-proj-xxxx…" />
            </div>
          </div>
          <div class="form-grid-2">
            <div class="field">
              <label class="field-label">已用额度（M tokens）</label>
              <input v-model.number="k.usedTokens" class="input mono" type="number" min="0" step="0.1" />
            </div>
            <div class="field">
              <label class="field-label">绑定模型（1–2 个）</label>
              <div class="model-picker compact">
                <label v-for="m in MODEL_CATALOG" :key="m.id" class="model-chip" :class="{ on: k.models.includes(m.name) }" @click.prevent="toggleKeyModel(i, m.name)">
                  <span class="tag">{{ m.name }}</span>
                </label>
              </div>
            </div>
          </div>
          <div v-if="k.models.length" class="field">
            <label class="field-label">模型转换 <em class="ke-hint">上游模型名，留空则不转换</em></label>
            <div class="map-list">
              <div v-for="m in k.models" :key="m" class="map-row">
                <span class="tag rm-tag">{{ m }}</span>
                <ArrowRight :size="13" />
                <input v-model="k.modelMap[m]" class="input mono" placeholder="例如 claude-opus-4-8" />
              </div>
            </div>
          </div>
        </div>
        <div v-if="!form.keys.length" class="ke-empty">尚未配置 API Key，点击右上角添加。</div>
      </div>

      <template #footer>
        <button class="btn btn-ghost" @click="showForm = false">取消</button>
        <button class="btn btn-primary" @click="saveRelay">
          <Pencil v-if="isEditing" :size="15" />
          <Plus v-else :size="15" />
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
.checkin-btn.done { color: var(--success); }
.checkin-btn.done:hover { color: color-mix(in srgb, var(--success) 75%, #000); background: color-mix(in srgb, var(--success) 10%, transparent); }
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
  grid-template-columns: minmax(210px, 1.5fr) minmax(120px, 1fr) minmax(170px, 1.4fr) 84px 130px minmax(110px, 0.9fr) 104px 96px;
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
  grid-template-columns: minmax(210px, 1.5fr) minmax(120px, 1fr) minmax(170px, 1.4fr) 84px 130px minmax(110px, 0.9fr) 104px 96px;
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
.rl-names strong { font-size: 13.5px; font-weight: 600; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.rl-sub { font-size: 10.5px; color: var(--text-faint); }
.rl-host { font-size: 12px; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.rl-cell.models { display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }
.rl-cell.models .tag { font-size: 10.5px; }

.rl-cell.money { display: flex; flex-direction: column; }
.rl-bal { font-size: 13px; font-weight: 700; color: var(--text); }
.rl-bal.low { color: var(--danger); }
.rl-used { font-size: 11px; color: var(--text-faint); }
.rl-user { font-size: 12px; color: var(--text-soft); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.rl-cell.checkin { display: flex; }
.rl-cell.checkin .pill { font-size: 11px; }
.rl-cell.checkin .btn { padding: 4px 9px; }

.rl-cell.op { display: flex; align-items: center; justify-content: flex-end; gap: 2px; }
.rl-cell.op .ico-on { color: var(--success); }
.rl-cell.op .ico-off { color: var(--info); }
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

.rm-list { display: flex; flex-direction: column; gap: 5px; max-height: 200px; overflow-y: auto; padding-right: 2px; }
.rm-item { display: flex; align-items: center; gap: 8px; font-size: 11.5px; }
.rm-tag { font-size: 11px; }
.rm-group { font-size: 11px; color: var(--text-faint); width: 72px; flex: none; }
.rm-price { font-size: 11px; color: var(--text-muted); margin-left: auto; flex: none; }

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

.form-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.model-picker { display: flex; flex-wrap: wrap; gap: 7px; max-height: 150px; overflow-y: auto; padding: 4px; }
.model-picker.compact { max-height: 96px; }
.model-chip { position: relative; cursor: pointer; }
.model-chip input { position: absolute; opacity: 0; pointer-events: none; }
.model-chip .tag { transition: all 0.2s ease; cursor: pointer; }
.model-chip:hover .tag { transform: translateY(-1px); border-color: var(--border-strong); }
.model-chip.on .tag { outline: 2px solid var(--primary); outline-offset: 1px; color: var(--text); background: var(--surface-3); }

.keys-editor { display: flex; flex-direction: column; gap: 10px; }
.ke-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.ke-hint { font-style: normal; font-size: 11px; color: var(--text-faint); font-weight: 500; margin-left: 6px; }
.ke-card {
  display: flex; flex-direction: column; gap: 12px;
  padding: 14px;
  border-radius: var(--radius-sm);
  background: var(--surface);
  border: 1px solid var(--border);
}
.ke-top { display: flex; align-items: flex-end; gap: 10px; }
.ke-top .field { flex: 1; }
.ke-empty { font-size: 12px; color: var(--text-faint); text-align: center; padding: 14px 0; border: 1px dashed var(--border-strong); border-radius: var(--radius-sm); }

.map-list { display: flex; flex-direction: column; gap: 7px; }
.map-row { display: flex; align-items: center; gap: 8px; }
.map-row svg { color: var(--text-faint); flex: none; }
.map-row .input { flex: 1; }

.rl-enter-active, .rl-leave-active { transition: all 0.3s var(--ease-out); }
.rl-enter-from, .rl-leave-to { opacity: 0; transform: translateY(-8px); }
.detail-enter-active, .detail-leave-active { transition: all 0.3s var(--ease-out); }
.detail-enter-from, .detail-leave-to { opacity: 0; transform: translateY(-6px); }

@media (max-width: 1200px) { .rld-grid { grid-template-columns: 1fr; } }
@media (max-width: 640px) { .form-grid-2 { grid-template-columns: 1fr; } }
</style>
