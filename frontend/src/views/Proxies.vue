<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  Plus, Network, Pencil, Trash2,
  Search, Globe2, User, Lock, Eye, EyeOff,
  LoaderCircle, AlertTriangle, Link2, Activity, X
} from 'lucide-vue-next'
import Modal from '../components/Modal.vue'
import { toast } from '../composables/toast'
import {
  listSocksProxies,
  createSocksProxy,
  updateSocksProxy,
  deleteSocksProxy
} from '../api/backends'
import type { SocksProxyListItem } from '../api/backends'

const proxies = ref<SocksProxyListItem[]>([])
const loading = ref(true)
const loadError = ref('')
const search = ref('')
const onlyEnabled = ref(false)

const filtered = computed(() =>
  proxies.value.filter(p => {
    const okSearch = p.name.toLowerCase().includes(search.value.toLowerCase()) || p.address.includes(search.value)
    const okStatus = !onlyEnabled.value || p.enabled
    return okSearch && okStatus
  })
)

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await listSocksProxies()
    proxies.value = res.items
  } catch (e: any) {
    loadError.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

const revealed = ref(new Set<number>())
function toggleReveal(id: number) {
  const s = new Set(revealed.value)
  s.has(id) ? s.delete(id) : s.add(id)
  revealed.value = s
}

const confirmTarget = ref<SocksProxyListItem | null>(null)

const showForm = ref(false)
const editingId = ref<number | null>(null)
const isEditing = computed(() => editingId.value !== null)
const saving = ref(false)
const form = ref({ name: '', address: '', username: '', password: '', enabled: true })

function openCreate() {
  editingId.value = null
  form.value = { name: '', address: '', username: '', password: '', enabled: true }
  showForm.value = true
}

function openEdit(p: SocksProxyListItem) {
  editingId.value = p.id
  form.value = { name: p.name, address: p.address, username: p.username, password: '', enabled: p.enabled }
  showForm.value = true
}

async function save() {
  if (!form.value.name.trim() || !form.value.address.trim()) {
    toast('请填写名称与地址', '', 'warning')
    return
  }
  saving.value = true
  try {
    if (isEditing.value && editingId.value !== null) {
      const updated = await updateSocksProxy(editingId.value, { ...form.value })
      const idx = proxies.value.findIndex(p => p.id === editingId.value)
      if (idx >= 0) Object.assign(proxies.value[idx], updated)
      toast('代理已更新', form.value.name, 'success')
    } else {
      const created = await createSocksProxy({ ...form.value })
      proxies.value.push({ ...created, bound_backend_count: 0 })
      toast('代理已添加', form.value.name, 'success')
    }
    showForm.value = false
  } catch (e: any) {
    toast(isEditing.value ? '更新失败' : '添加失败', e?.message || '', 'danger')
  } finally {
    saving.value = false
  }
}

async function confirmRemove() {
  const p = confirmTarget.value
  if (!p) return
  confirmTarget.value = null
  try {
    await deleteSocksProxy(p.id)
    proxies.value = proxies.value.filter(x => x.id !== p.id)
    toast('代理已删除', p.name, 'danger')
  } catch (e: any) {
    toast('删除失败', e?.message || '', 'danger')
  }
}

async function toggleEnabled(p: SocksProxyListItem) {
  const next = !p.enabled
  try {
    const updated = await updateSocksProxy(p.id, {
      name: p.name,
      address: p.address,
      username: p.username,
      password: p.password ?? '',
      enabled: next
    })
    Object.assign(p, updated)
    toast(next ? '代理已启用' : '代理已停用', p.name, next ? 'success' : 'warning')
  } catch (e: any) {
    toast('操作失败', e?.message || '', 'danger')
  }
}

function formatLatency(ms?: number) {
  if (ms == null) return '—'
  return ms < 1000 ? Math.round(ms) + ' ms' : (ms / 1000).toFixed(1) + ' s'
}

onMounted(loadData)
</script>

<template>
  <div class="page stagger">
    <section class="toolbar panel">
      <div class="search-box" style="width: 240px">
        <Search :size="15" />
        <input v-model="search" class="input" placeholder="搜索代理名称 / 地址…" />
        <button v-if="search" class="search-clear" aria-label="清除搜索" @click="search = ''"><X :size="14" /></button>
      </div>
      <label class="toggle-label">
        <span class="switch" :class="{ on: onlyEnabled }" @click="onlyEnabled = !onlyEnabled"></span>
        仅看已启用
      </label>
      <div class="spacer"></div>
      <button class="btn btn-primary" @click="openCreate"><Plus :size="15" /> 添加</button>
    </section>

    <div v-if="loading" class="load-center">
      <LoaderCircle :size="20" class="spin" /><span>正在加载…</span>
    </div>

    <div v-else-if="loadError" class="load-center err">
      <AlertTriangle :size="16" /><span>{{ loadError }}</span>
      <button class="btn btn-ghost btn-sm" @click="loadData">重试</button>
    </div>

    <section v-else class="proxy-grid">
      <article v-for="p in filtered" :key="p.id" class="proxy-card" :class="{ off: !p.enabled }">
        <div class="pc-glow"></div>
        <header class="pc-head">
          <div class="pc-id">
            <span class="pc-ico" :class="{ on: p.enabled }"><Network :size="15" /></span>
            <div class="pc-name">{{ p.name }}</div>
          </div>
          <span class="pill" :class="p.enabled ? 'success' : 'neutral'"><span class="dot" />{{ p.enabled ? '已启用' : '已停用' }}</span>
        </header>

        <div class="pc-badge-row">
          <span class="tag pink">SOCKS5</span>
          <span v-if="p.bound_backend_count" class="pill info">
            <Link2 :size="10" /> {{ p.bound_backend_count }} 个中转站
          </span>
        </div>

        <div class="pc-rows">
          <div class="pc-row">
            <Globe2 :size="13" />
            <span class="pc-row-label">地址</span>
            <span class="pc-row-val mono">{{ p.address }}</span>
          </div>
          <div class="pc-row">
            <User :size="13" />
            <span class="pc-row-label">用户名</span>
            <span class="pc-row-val mono">{{ p.username || '—' }}</span>
          </div>
          <div class="pc-row">
            <Lock :size="13" />
            <span class="pc-row-label">密码</span>
            <span class="pc-row-val mono">{{ p.password ? (revealed.has(p.id) ? p.password : '••••••••') : '—' }}</span>
            <button v-if="p.password" class="icon-btn mini" @click="toggleReveal(p.id)">
              <EyeOff v-if="revealed.has(p.id)" :size="13" />
              <Eye v-else :size="13" />
            </button>
          </div>
          <div class="pc-row">
            <Activity :size="13" />
            <span class="pc-row-label">延迟</span>
            <span class="pc-row-val mono">{{ formatLatency(p.avg_latency_ms) }}</span>
          </div>
        </div>

        <footer class="pc-foot">
          <div class="spacer"></div>
          <button class="icon-btn primary" title="修改" @click="openEdit(p)"><Pencil :size="14" /></button>
          <button class="icon-btn" :title="p.enabled ? '停用' : '启用'" @click="toggleEnabled(p)">
            <span class="toggle-dot" :class="{ on: p.enabled }"></span>
          </button>
          <button class="icon-btn danger" title="删除" @click="confirmTarget = p"><Trash2 :size="14" /></button>
        </footer>
      </article>

    </section>

    <Modal :open="showForm" :title="isEditing ? '修改代理' : '添加代理'" subtitle="SOCKS5 代理配置" :icon="Network" @close="showForm = false">
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">名称 <span class="req">*</span></label>
          <input v-model="form.name" class="input" placeholder="例如：HK · 节点 1" />
        </div>
        <div class="field">
          <label class="field-label">地址 <span class="req">*</span></label>
          <input v-model="form.address" class="input mono" placeholder="127.0.0.1:1080" />
        </div>
      </div>
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">用户名</label>
          <input v-model="form.username" class="input mono" placeholder="留空表示无认证" />
        </div>
        <div class="field">
          <label class="field-label">密码</label>
          <input v-model="form.password" class="input mono" type="password" placeholder="留空表示无密码" />
        </div>
      </div>
      <div class="field">
        <label class="toggle-label">
          <span class="switch" :class="{ on: form.enabled }" @click="form.enabled = !form.enabled"></span>
          创建后立即启用
        </label>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="showForm = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="save">
          <LoaderCircle v-if="saving" :size="14" class="spin" />
          <Plus v-else :size="15" />
          {{ isEditing ? '保存修改' : '添加代理' }}
        </button>
      </template>
    </Modal>

    <Modal :open="confirmTarget !== null" title="删除代理" :icon="Trash2" @close="confirmTarget = null">
      <p class="confirm-text">确定要删除代理 <strong>{{ confirmTarget?.name }}</strong> 吗？此操作不可撤销。</p>
      <template #footer>
        <button class="btn btn-ghost" @click="confirmTarget = null">取消</button>
        <button class="btn btn-danger" @click="confirmRemove"><Trash2 :size="14" /> 确认删除</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }
.toolbar { display: flex; align-items: center; gap: 16px; padding: 14px 16px; flex-wrap: wrap; }
.spacer { flex: 1; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--text-muted); cursor: pointer; user-select: none; }

.load-center { display: flex; align-items: center; gap: 10px; padding: 48px; color: var(--text-faint); font-size: 13px; justify-content: center; }
.load-center.err { color: var(--danger); }

.proxy-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.proxy-card {
  position: relative; overflow: hidden;
  display: flex; flex-direction: column; gap: 12px;
  background: var(--glass); border: 1px solid var(--border);
  border-radius: var(--radius-lg); padding: 16px;
  backdrop-filter: blur(14px);
  transition: transform 0.25s var(--ease-out), border-color 0.25s ease, box-shadow 0.25s ease;
}
.proxy-card:hover {
  transform: translateY(-3px);
  border-color: var(--border-strong);
  box-shadow: 0 18px 40px -18px rgba(0,0,0,0.5), 0 0 0 1px rgba(34,211,238,0.06);
}
.proxy-card.off { opacity: 0.72; }
.proxy-card.off:hover { opacity: 1; }

.pc-glow {
  position: absolute; top: -60px; right: -60px;
  width: 150px; height: 150px; border-radius: 50%;
  background: radial-gradient(circle, rgba(34,211,238,0.14), transparent 70%);
  pointer-events: none;
}

.pc-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.pc-id { display: flex; align-items: center; gap: 10px; min-width: 0; }
.pc-ico {
  width: 34px; height: 34px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  background: var(--surface-2); color: var(--text-faint); flex: none;
}
.pc-ico.on { background: rgba(34,211,238,0.12); color: var(--c1); box-shadow: 0 0 14px rgba(34,211,238,0.2); }
.pc-name { font-size: 14.5px; font-weight: 650; color: var(--text); letter-spacing: -0.01em; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.pc-badge-row { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
.pc-badge-row .pill { display: inline-flex; align-items: center; gap: 4px; }

.pc-rows {
  display: flex; flex-direction: column; gap: 8px;
  padding: 11px 0;
  border-top: 1px solid var(--border-soft);
  border-bottom: 1px solid var(--border-soft);
}
.pc-row { display: flex; align-items: center; gap: 9px; font-size: 12.5px; color: var(--text-muted); }
.pc-row > svg { width: 13px; height: 13px; color: var(--text-faint); flex: none; }
.pc-row-label { width: 44px; font-size: 11px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.04em; flex: none; }
.pc-row-val { flex: 1; color: var(--text-soft); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.icon-btn.mini { width: 22px; height: 22px; border-radius: 6px; }

.pc-foot { display: flex; align-items: center; gap: 4px; }

.toggle-dot {
  display: block; width: 10px; height: 10px; border-radius: 50%;
  background: var(--text-faint); transition: background 0.2s;
}
.toggle-dot.on { background: var(--success); box-shadow: 0 0 6px var(--success); }

.add-tile {
  border-style: dashed; border-color: var(--border-strong);
  background: transparent; align-items: center; justify-content: center;
  min-height: 170px; gap: 10px;
  color: var(--text-faint); font-size: 13.5px; font-weight: 600; cursor: pointer;
}
.add-tile:hover { color: var(--primary); border-color: var(--primary); background: rgba(34,211,238,0.04); }

.confirm-text { font-size: 13.5px; color: var(--text-soft); line-height: 1.6; }

.form-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-bottom: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
.field:last-child { margin-bottom: 0; }
.field-label { font-size: 12.5px; font-weight: 600; color: var(--text-soft); }
.field-hint { font-size: 11.5px; color: var(--text-faint); }
.req { color: var(--danger); }
</style>
