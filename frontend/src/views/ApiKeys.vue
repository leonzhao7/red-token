<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  Plus,
  KeyRound,
  Search,
  Copy,
  Trash2,
  Pencil,
  ShieldCheck,
  ShieldOff,
  Coins,
  Activity
} from 'lucide-vue-next'
import Modal from '../components/Modal.vue'
import { store } from '../store'
import { MODELS, MODELS_INFO } from '../data/mock'
import { toast } from '../composables/toast'
import type { ClientKey } from '../types'

const search = ref('')
const statusFilter = ref<'all' | 'active' | 'disabled' | 'limited' | 'expired'>('all')
const showCreate = ref(false)

const filtered = computed(() =>
  store.apiKeys.filter((k) => {
    const okSearch = k.name.toLowerCase().includes(search.value.toLowerCase()) || k.key.includes(search.value)
    const okStatus = statusFilter.value === 'all' || k.status === statusFilter.value
    return okSearch && okStatus
  })
)

function fmtTokens(n: number) {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(0) + 'k'
  return String(n)
}

function fmtReq(n: number) {
  return n.toLocaleString('zh-CN')
}

function copyKey(k: ClientKey) {
  navigator.clipboard?.writeText(k.key.replace('sk-', 'sk-').slice(0, 20) + '…')
  toast('密钥已复制', `${k.name} · 已复制到剪贴板`, 'info')
}

function toggleStatus(k: ClientKey) {
  const next = k.status === 'active' ? 'disabled' : 'active'
  store.updateKey(k.id, { status: next })
  toast(next === 'active' ? '密钥已启用' : '密钥已停用', k.name, next === 'active' ? 'success' : 'warning')
}

function removeKey(k: ClientKey) {
  store.removeKey(k.id)
  toast('密钥已删除', k.name, 'danger')
}

/* ---- create / edit form ---- */
const form = ref({
  name: '',
  models: [] as string[],
  rateLimit: 100,
  expiry: '1y'
})

const editingId = ref<string | null>(null)
const isEditing = computed(() => editingId.value !== null)

function resetForm() {
  form.value = { name: '', models: [], rateLimit: 100, expiry: '1y' }
}

function expiryOf(k: ClientKey) {
  return k.expiresAt === '永久' ? 'never' : k.expiresAt === '2027-08-02' ? '1y' : '6m'
}

function openCreate() {
  editingId.value = null
  resetForm()
  showCreate.value = true
}

function openEditKey(k: ClientKey) {
  editingId.value = k.id
  form.value = {
    name: k.name,
    models: [...k.models],
    rateLimit: k.rateLimit,
    expiry: expiryOf(k)
  }
  showCreate.value = true
}

function saveKey() {
  if (!form.value.name.trim()) {
    toast('请填写密钥名称', '', 'warning')
    return
  }
  const patch = {
    name: form.value.name.trim(),
    models: form.value.models.length ? form.value.models : ['gpt-4o'],
    rateLimit: form.value.rateLimit,
    expiresAt: form.value.expiry === '1y' ? '2027-08-02' : form.value.expiry === '6m' ? '2027-02-02' : '永久'
  }
  if (isEditing.value && editingId.value) {
    store.updateKey(editingId.value, patch)
    showCreate.value = false
    resetForm()
    editingId.value = null
    toast('密钥配置已更新', patch.name, 'success')
    return
  }
  const rnd = Math.random().toString(16).slice(2, 12)
  store.addKey({
    id: 'k' + Date.now(),
    key: `nx-sk-${form.value.name.slice(0, 3).toLowerCase()}-${rnd}`,
    status: 'active',
    totalTokens: 10_000_000,
    usedTokens: 0,
    tokenInput: 0,
    tokenCache: 0,
    tokenOutput: 0,
    reqSuccess: 0,
    reqFail: 0,
    createdAt: '2026-08-02',
    lastUsed: '从未',
    ...patch
  })
  showCreate.value = false
  resetForm()
  toast('API Key 已创建', '已生成本地演示密钥', 'success')
}
</script>

<template>
  <div class="page stagger">
    <!-- toolbar -->
    <section class="toolbar panel">
      <div class="search-box" style="width: 260px">
        <Search :size="15" />
        <input v-model="search" class="input" placeholder="搜索名称或密钥…" />
      </div>
      <div class="segmented">
        <button :class="{ active: statusFilter === 'all' }" @click="statusFilter = 'all'">全部</button>
        <button :class="{ active: statusFilter === 'active' }" @click="statusFilter = 'active'">启用</button>
        <button :class="{ active: statusFilter === 'disabled' }" @click="statusFilter = 'disabled'">停用</button>
      </div>
      <div class="spacer"></div>
      <button class="btn btn-primary" @click="openCreate"><Plus :size="15" /> 新建密钥</button>
    </section>

    <!-- key cards -->
    <section class="keys-grid">
      <TransitionGroup name="list">
        <article v-for="k in filtered" :key="k.id" class="key-card panel">
          <div class="key-head">
            <div class="key-avatar" :class="k.status">
              <KeyRound :size="17" />
            </div>
            <div class="key-title">
              <h3>{{ k.name }}</h3>
              <span class="mono key-val">{{ k.key }}</span>
            </div>
            <button class="icon-btn" title="复制" @click="copyKey(k)"><Copy :size="14" /></button>
          </div>

          <div class="key-models">
            <span v-for="m in k.models.slice(0, 4)" :key="m" class="tag" :class="MODELS_INFO[m]?.color || 'neutral'">{{ m }}</span>
            <span v-if="k.models.length > 4" class="tag">+{{ k.models.length - 4 }}</span>
          </div>

          <div class="key-meta">
            <div class="km-row">
              <span class="km-ico cyan"><Coins :size="13" /></span>
              <span class="km-label">Token</span>
              <span class="km-value mono">
                <em>输入 {{ fmtTokens(k.tokenInput) }}</em>
                <em>缓存 {{ fmtTokens(k.tokenCache) }}</em>
                <em>输出 {{ fmtTokens(k.tokenOutput) }}</em>
              </span>
            </div>
            <div class="km-row">
              <span class="km-ico violet"><Activity :size="13" /></span>
              <span class="km-label">请求</span>
              <span class="km-value mono">
                <em class="ok">成功 {{ fmtReq(k.reqSuccess) }}</em>
                <em class="fail">失败 {{ fmtReq(k.reqFail) }}</em>
              </span>
            </div>
          </div>

          <div class="key-foot">
            <span class="pill" :class="k.status === 'active' ? 'success' : k.status === 'limited' ? 'warning' : k.status === 'expired' ? 'danger' : 'neutral'">
              <span class="dot" />
              {{ k.status === 'active' ? '启用' : k.status === 'limited' ? '额度受限' : k.status === 'expired' ? '已过期' : '已停用' }}
            </span>
            <div class="key-actions">
              <button class="icon-btn primary" title="编辑" @click="openEditKey(k)"><Pencil :size="14" /></button>
              <button class="icon-btn" :title="k.status === 'active' ? '停用' : '启用'" @click="toggleStatus(k)">
                <ShieldOff v-if="k.status === 'active'" :size="14" />
                <ShieldCheck v-else :size="14" />
              </button>
              <button class="icon-btn danger" title="删除" @click="removeKey(k)"><Trash2 :size="14" /></button>
            </div>
          </div>
        </article>
      </TransitionGroup>
    </section>

    <!-- create / edit modal -->
    <Modal :open="showCreate" :title="isEditing ? '编辑 API Key' : '新建 API Key'" :subtitle="isEditing ? '修改密钥名称、模型权限与速率配置' : '为客户端生成访问密钥，并限制可用模型'" :icon="Plus" @close="showCreate = false">
      <div class="field">
        <label class="field-label">密钥名称 <span class="req">*</span></label>
        <input v-model="form.name" class="input" placeholder="例如：生产环境 · Web App" />
      </div>
      <div class="field">
        <label class="field-label">模型权限 <span class="req">*</span></label>
        <div class="model-picker">
          <label v-for="m in MODELS" :key="m" class="model-chip" :class="{ on: form.models.includes(m) }">
            <input v-model="form.models" type="checkbox" :value="m" />
            <span class="tag" :class="MODELS_INFO[m]?.color || 'neutral'">{{ m }}</span>
          </label>
        </div>
        <span class="field-hint">未选择时默认允许全部模型。</span>
      </div>
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">速率限制（次/分钟）</label>
          <input v-model.number="form.rateLimit" class="input" type="number" min="1" />
        </div>
        <div class="field">
          <label class="field-label">有效期</label>
          <select v-model="form.expiry" class="select">
            <option value="1y">1 年</option>
            <option value="6m">6 个月</option>
            <option value="never">永久</option>
          </select>
        </div>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="showCreate = false">取消</button>
        <button class="btn btn-primary" @click="saveKey">
          <Pencil v-if="isEditing" :size="15" />
          <Plus v-else :size="15" />
          {{ isEditing ? '保存修改' : '创建密钥' }}
        </button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.toolbar { display: flex; align-items: center; gap: 14px; padding: 14px 16px; flex-wrap: wrap; }
.spacer { flex: 1; }

.keys-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: var(--space-4);
}
.key-card { padding: 20px; display: flex; flex-direction: column; gap: 15px; }
.key-head { display: flex; align-items: center; gap: 12px; }
.key-avatar {
  width: 40px;
  height: 40px;
  border-radius: 13px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  border: 1px solid var(--border);
}
.key-avatar.active { background: var(--success-soft); color: var(--success); box-shadow: 0 0 18px rgba(52,211,153,0.25); }
.key-avatar.disabled { background: var(--surface-2); color: var(--text-faint); }
.key-avatar.limited { background: var(--warning-soft); color: var(--warning); box-shadow: 0 0 18px rgba(251,191,36,0.2); }
.key-avatar.expired { background: var(--danger-soft); color: var(--danger); }
.key-title { flex: 1; min-width: 0; }
.key-title h3 { font-size: 15px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.key-val { font-size: 12px; color: var(--text-faint); display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.key-models { display: flex; gap: 6px; flex-wrap: wrap; }

.key-meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 0;
  border-top: 1px solid var(--border-soft);
  border-bottom: 1px solid var(--border-soft);
}
.km-row {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 12px;
  color: var(--text-muted);
}
.km-ico {
  width: 26px;
  height: 26px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}
.km-ico.cyan { background: rgba(34,211,238,0.12); color: var(--c1); }
.km-ico.violet { background: rgba(139,92,246,0.12); color: var(--c2); }
.km-label {
  width: 36px;
  font-weight: 600;
  color: var(--text-faint);
  letter-spacing: 0.02em;
}
.km-value {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--text-soft);
  white-space: nowrap;
  overflow: hidden;
}
.km-value em { font-style: normal; }
.km-value em.ok { color: var(--success); }
.km-value em.fail { color: var(--danger); }

.key-foot { display: flex; align-items: center; justify-content: space-between; }
.key-actions { display: flex; gap: 2px; }

.form-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.model-picker { display: flex; flex-wrap: wrap; gap: 7px; max-height: 160px; overflow-y: auto; padding: 4px; }
.model-chip { position: relative; cursor: pointer; }
.model-chip input { position: absolute; opacity: 0; pointer-events: none; }
.model-chip .tag { transition: all 0.2s ease; }
.model-chip:hover .tag { transform: translateY(-1px); border-color: var(--border-strong); }
.model-chip.on .tag {
  outline: 2px solid var(--primary);
  outline-offset: 1px;
  color: var(--text);
  background: var(--surface-3);
}

.list-enter-active, .list-leave-active { transition: all 0.35s var(--ease-out); }
.list-enter-from, .list-leave-to { opacity: 0; transform: translateY(14px) scale(0.98); }
</style>
