<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
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
  Activity,
  LoaderCircle,
  X
} from 'lucide-vue-next'
import Modal from '../components/Modal.vue'
import { toast } from '../composables/toast'
import {
  listClientKeys,
  createClientKey,
  updateClientKey,
  deleteClientKey
} from '../api/clientKeys'
import type { ClientKeyListItem } from '../api/clientKeys'

const loading = ref(true)
const loadError = ref('')
const keys = ref<ClientKeyListItem[]>([])
const deleteTarget = ref<ClientKeyListItem | null>(null)

const search = ref('')
const statusFilter = ref<'all' | 'enabled' | 'disabled'>('all')

const filtered = computed(() =>
  keys.value.filter(k => {
    const okSearch = k.name.toLowerCase().includes(search.value.toLowerCase()) ||
      k.token.includes(search.value)
    const okStatus = statusFilter.value === 'all' ||
      (statusFilter.value === 'enabled' && k.enabled) ||
      (statusFilter.value === 'disabled' && !k.enabled)
    return okSearch && okStatus
  })
)

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await listClientKeys()
    keys.value = res.items
  } catch (e: any) {
    loadError.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function fmtTokens(n: number) {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(0) + 'k'
  return String(n)
}

function fmtReq(n: number) {
  return n.toLocaleString('zh-CN')
}

function copyKey(k: ClientKeyListItem) {
  navigator.clipboard?.writeText(k.token)
  toast('密钥已复制', `${k.name} · 已复制到剪贴板`, 'info')
}

async function toggleStatus(k: ClientKeyListItem) {
  try {
    const res = await updateClientKey(k.id, {
      name: k.name,
      token: '',
      allowed_models: k.allowed_models,
      enabled: !k.enabled
    })
    Object.assign(k, res.client)
    toast(k.enabled ? '密钥已启用' : '密钥已停用', k.name, k.enabled ? 'success' : 'warning')
  } catch (e: any) {
    toast('操作失败', e?.message || '', 'danger')
  }
}

async function confirmRemoveKey() {
  const k = deleteTarget.value
  if (!k) return
  deleteTarget.value = null
  try {
    await deleteClientKey(k.id)
    keys.value = keys.value.filter(x => x.id !== k.id)
    toast('密钥已删除', k.name, 'danger')
  } catch (e: any) {
    toast('删除失败', e?.message || '', 'danger')
  }
}

/* ---- form ---- */
const showForm = ref(false)
const editingId = ref<number | null>(null)
const isEditing = computed(() => editingId.value !== null)
const saving = ref(false)

const form = ref({ name: '', allowed_models: '', enabled: true })
const modelInput = ref('')

const modelTags = computed(() =>
  form.value.allowed_models
    ? form.value.allowed_models.split(/[,\s]+/).map(s => s.trim()).filter(Boolean)
    : []
)

function addModel() {
  const name = modelInput.value.trim()
  if (!name) return
  const tags = modelTags.value
  if (!tags.includes(name)) {
    form.value.allowed_models = [...tags, name].join(',')
  }
  modelInput.value = ''
}

function removeModel(name: string) {
  form.value.allowed_models = modelTags.value.filter(t => t !== name).join(',')
}

function parseModels(k: ClientKeyListItem): string[] {
  return k.allowed_models ? k.allowed_models.split(/[,\s]+/).map(s => s.trim()).filter(Boolean) : []
}

function openCreate() {
  editingId.value = null
  form.value = { name: '', allowed_models: '', enabled: true }
  modelInput.value = ''
  showForm.value = true
}

function openEdit(k: ClientKeyListItem) {
  editingId.value = k.id
  form.value = { name: k.name, allowed_models: k.allowed_models, enabled: k.enabled }
  modelInput.value = ''
  showForm.value = true
}

async function save() {
  if (!form.value.name.trim()) {
    toast('请填写密钥名称', '', 'warning')
    return
  }
  saving.value = true
  try {
    if (isEditing.value && editingId.value !== null) {
      const res = await updateClientKey(editingId.value, {
        name: form.value.name.trim(),
        token: '',
        allowed_models: form.value.allowed_models,
        enabled: form.value.enabled
      })
      const idx = keys.value.findIndex(k => k.id === editingId.value)
      if (idx >= 0) Object.assign(keys.value[idx], res.client)
      toast('密钥已更新', form.value.name, 'success')
    } else {
      const res = await createClientKey({
        name: form.value.name.trim(),
        token: '',
        allowed_models: form.value.allowed_models,
        enabled: form.value.enabled
      })
      keys.value.unshift(res.client as ClientKeyListItem)
      if (res.issued_token) {
        navigator.clipboard?.writeText(res.issued_token)
        toast('密钥已创建并复制', `Token: ${res.issued_token.slice(0, 16)}…`, 'success')
      } else {
        toast('密钥已创建', form.value.name, 'success')
      }
    }
    showForm.value = false
  } catch (e: any) {
    toast(isEditing.value ? '更新失败' : '创建失败', e?.message || '', 'danger')
  } finally {
    saving.value = false
  }
}

onMounted(loadData)
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
        <button :class="{ active: statusFilter === 'enabled' }" @click="statusFilter = 'enabled'">启用</button>
        <button :class="{ active: statusFilter === 'disabled' }" @click="statusFilter = 'disabled'">停用</button>
      </div>
      <div class="spacer"></div>
      <button class="btn btn-primary" @click="openCreate"><Plus :size="15" /> 新建密钥</button>
    </section>

    <!-- loading -->
    <div v-if="loading" class="cfg-loading">
      <LoaderCircle :size="20" class="spin" /><span>正在加载密钥…</span>
    </div>

    <!-- error -->
    <div v-else-if="loadError" class="cfg-loading" style="color: var(--danger)">{{ loadError }}</div>

    <!-- key cards -->
    <section v-else class="keys-grid">
      <TransitionGroup name="list">
        <article v-for="k in filtered" :key="k.id" class="key-card panel">
          <div class="key-head">
            <div class="key-avatar" :class="k.enabled ? 'active' : 'disabled'">
              <KeyRound :size="17" />
            </div>
            <div class="key-title">
              <h3>{{ k.name }}</h3>
              <span class="mono key-val">{{ k.token }}</span>
            </div>
            <button class="icon-btn" title="复制" @click="copyKey(k)"><Copy :size="14" /></button>
          </div>

          <div class="key-models">
            <span v-for="m in parseModels(k).slice(0, 4)" :key="m" class="tag">{{ m }}</span>
            <span v-if="parseModels(k).length > 4" class="tag">+{{ parseModels(k).length - 4 }}</span>
            <span v-if="!parseModels(k).length" class="tag neutral">全部模型</span>
          </div>

          <div class="key-meta">
            <div class="km-row">
              <span class="km-ico cyan"><Coins :size="13" /></span>
              <span class="km-label">Token</span>
              <span class="km-value mono">
                <em>输入 {{ fmtTokens(k.token_input || 0) }}</em>
                <em>输出 {{ fmtTokens(k.token_output || 0) }}</em>
              </span>
            </div>
            <div class="km-row">
              <span class="km-ico violet"><Activity :size="13" /></span>
              <span class="km-label">请求</span>
              <span class="km-value mono">
                <em class="ok">成功 {{ fmtReq(k.req_success || 0) }}</em>
                <em class="fail">失败 {{ fmtReq(k.req_fail || 0) }}</em>
              </span>
            </div>
          </div>

          <div class="key-foot">
            <span class="pill" :class="k.enabled ? 'success' : 'neutral'">
              <span class="dot" />
              {{ k.enabled ? '启用' : '已停用' }}
            </span>
            <div class="key-actions">
              <button class="icon-btn primary" title="编辑" @click="openEdit(k)"><Pencil :size="14" /></button>
              <button class="icon-btn" :title="k.enabled ? '停用' : '启用'" @click="toggleStatus(k)">
                <ShieldOff v-if="k.enabled" :size="14" />
                <ShieldCheck v-else :size="14" />
              </button>
              <button class="icon-btn danger" title="删除" @click="deleteTarget = k"><Trash2 :size="14" /></button>
            </div>
          </div>
        </article>
      </TransitionGroup>
    </section>

    <!-- create / edit modal -->
    <Modal :open="showForm" :title="isEditing ? '编辑 API Key' : '新建 API Key'" :subtitle="isEditing ? '修改密钥名称与模型权限' : '为客户端生成访问密钥'" :icon="Plus" @close="showForm = false">
      <div class="field">
        <label class="field-label">密钥名称 <span class="req">*</span></label>
        <input v-model="form.name" class="input" placeholder="例如：生产环境 · Web App" />
      </div>
      <div class="field">
        <label class="field-label">允许模型</label>
        <div class="model-input-row">
          <input
            v-model="modelInput"
            class="input mono"
            placeholder="输入模型名称，回车添加"
            @keydown.enter.prevent="addModel"
          />
          <button class="btn btn-ghost" title="添加" @click="addModel"><Plus :size="15" /></button>
        </div>
        <div class="model-tags">
          <span v-for="m in modelTags" :key="m" class="tag">
            {{ m }}
            <button class="model-x" @click="removeModel(m)"><X :size="11" /></button>
          </span>
          <span v-if="!modelTags.length" class="model-empty">未设置时允许全部模型</span>
        </div>
      </div>
      <div class="field">
        <label class="field-label">状态</label>
        <label class="toggle-label">
          <span class="switch" :class="{ on: form.enabled }" @click="form.enabled = !form.enabled"></span>
          {{ form.enabled ? '启用' : '停用' }}
        </label>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="showForm = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="save">
          <LoaderCircle v-if="saving" :size="15" class="spin" />
          <Pencil v-else-if="isEditing" :size="15" />
          <Plus v-else :size="15" />
          {{ isEditing ? '保存修改' : '创建密钥' }}
        </button>
      </template>
    </Modal>

    <Modal :open="deleteTarget !== null" title="删除 API Key" :icon="Trash2" @close="deleteTarget = null">
      <p class="confirm-text">确定要删除 API Key <strong>{{ deleteTarget?.name }}</strong> 吗？此操作不可撤销。</p>
      <template #footer>
        <button class="btn btn-ghost" @click="deleteTarget = null">取消</button>
        <button class="btn btn-danger" @click="confirmRemoveKey"><Trash2 :size="14" /> 确认删除</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.toolbar { display: flex; align-items: center; gap: 14px; padding: 14px 16px; flex-wrap: wrap; }
.spacer { flex: 1; }

.cfg-loading { display: flex; align-items: center; gap: 10px; padding: 48px; color: var(--text-faint); font-size: 13px; justify-content: center; }

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

.model-input-row { display: flex; gap: 8px; }
.model-input-row .input { flex: 1; }
.model-tags { display: flex; flex-wrap: wrap; gap: 7px; margin-top: 6px; }
.model-tags .tag { display: inline-flex; align-items: center; gap: 5px; }
.model-x {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; padding: 0;
  border: none; border-radius: 4px;
  background: transparent; color: var(--text-faint); cursor: pointer;
}
.model-x:hover { color: var(--danger); background: var(--danger-soft); }
.model-empty { font-size: 12px; color: var(--text-faint); }

.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--text-muted); cursor: pointer; user-select: none; }
.confirm-text { font-size: 13.5px; color: var(--text-soft); line-height: 1.6; }

.list-enter-active, .list-leave-active { transition: all 0.35s var(--ease-out); }
.list-enter-from, .list-leave-to { opacity: 0; transform: translateY(14px) scale(0.98); }
</style>
