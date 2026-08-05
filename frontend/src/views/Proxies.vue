<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  Plus,
  Network,
  Pencil,
  Trash2,
  Zap,
  CircleDot,
  Cable,
  Globe2,
  User,
  Lock,
  Eye,
  EyeOff
} from 'lucide-vue-next'
import Modal from '../components/Modal.vue'
import { store } from '../store'
import { toast } from '../composables/toast'
import type { ProxyServer, ProxyProtocol, ProxyAuth } from '../types'

const search = ref('')
const onlyActive = ref(false)

const filtered = computed(() =>
  store.proxies.filter((p) => {
    const okSearch = p.name.toLowerCase().includes(search.value.toLowerCase()) || p.host.includes(search.value)
    const okStatus = !onlyActive.value || p.status === 'active'
    return okSearch && okStatus
  })
)

const protocolColor: Record<ProxyProtocol, string> = { http: 'cyan', https: 'violet', socks5: 'pink' }

function testProxy(p: ProxyServer) {
  const latency = Math.round(60 + Math.random() * 120)
  const ok = Math.random() > 0.15
  store.updateProxy(p.id, { latency: ok ? latency : 0, successRate: ok ? 97 + Math.random() * 2.8 : 0 })
  toast(ok ? '连接测试通过' : '连接测试失败', `${p.name} · ${ok ? latency + 'ms' : '无法建立连接'}`, ok ? 'success' : 'danger')
}

function toggleProxy(p: ProxyServer) {
  const next = p.status === 'active' ? 'disabled' : 'active'
  store.updateProxy(p.id, { status: next })
  toast(next === 'active' ? '代理已启用' : '代理已停用', p.name, next === 'active' ? 'success' : 'warning')
}

function removeProxy(p: ProxyServer) {
  store.removeProxy(p.id)
  toast('代理节点已删除', p.name, 'danger')
}

const revealed = ref<Set<string>>(new Set())
function toggleReveal(id: string) {
  const s = new Set(revealed.value)
  s.has(id) ? s.delete(id) : s.add(id)
  revealed.value = s
}
function maskPassword(p: ProxyServer) {
  if (!p.password) return ''
  if (revealed.value.has(p.id)) return p.password
  return p.password.replace(/./g, '•')
}

/* ---- form ---- */
const showForm = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => editingId.value !== null)
const form = ref<{ name: string; protocol: ProxyProtocol; host: string; port: number; auth: ProxyAuth; username: string; password: string }>({
  name: '',
  protocol: 'http',
  host: '',
  port: 1080,
  auth: 'none',
  username: '',
  password: ''
})

function openCreate() {
  editingId.value = null
  form.value = { name: '', protocol: 'http', host: '', port: 1080, auth: 'none', username: '', password: '' }
  showForm.value = true
}

function openEditProxy(p: ProxyServer) {
  editingId.value = p.id
  form.value = {
    name: p.name,
    protocol: p.protocol,
    host: p.host,
    port: p.port,
    auth: p.auth,
    username: p.username,
    password: p.password
  }
  showForm.value = true
}

function saveProxy() {
  if (!form.value.name || !form.value.host) {
    toast('请填写名称与地址', '', 'warning')
    return
  }
  if (isEditing.value && editingId.value) {
    store.updateProxy(editingId.value, { ...form.value })
    toast('代理节点已更新', form.value.name, 'success')
  } else {
    store.addProxy({
      id: 'p' + Date.now(),
      ...form.value,
      location: '待探测',
      latency: 0,
      successRate: 0,
      status: 'active',
      usedBy: 0
    })
    toast('代理节点已添加', '等待首次连通性测试', 'success')
  }
  showForm.value = false
}
</script>

<template>
  <div class="page stagger">
    <!-- toolbar -->
    <section class="toolbar panel">
      <div class="search-box" style="width: 240px">
        <Cable :size="15" />
        <input v-model="search" class="input" placeholder="搜索代理名称 / 地址…" />
      </div>
      <label class="toggle-label">
        <span class="switch" :class="{ on: onlyActive }" @click="onlyActive = !onlyActive"></span>
        仅看在线
      </label>
      <div class="spacer"></div>
      <button class="btn btn-primary" @click="openCreate"><Plus :size="15" /> 添加节点</button>
    </section>

    <!-- proxy cards -->
    <section class="proxy-grid">
      <article v-for="p in filtered" :key="p.id" class="proxy-card" :class="{ off: p.status === 'disabled' }">
        <div class="pc-glow"></div>
        <header class="pc-head">
          <div class="pc-id">
            <span class="pc-ico" :class="p.status === 'active' ? 'on' : ''"><Network :size="15" /></span>
            <div>
              <div class="pc-name">{{ p.name }}</div>
            </div>
          </div>
          <span class="pill" :class="p.status === 'active' ? 'success' : 'neutral'"><span class="dot" />{{ p.status === 'active' ? '在线' : '停用' }}</span>
        </header>

        <div class="pc-badge-row">
          <span class="tag" :class="protocolColor[p.protocol]">{{ p.protocol.toUpperCase() }}</span>
          <span v-if="p.auth !== 'none'" class="pill info"><span class="dot" />{{ p.auth === 'username' ? '用户名认证' : 'Token' }}</span>
          <span v-else class="pill neutral"><span class="dot" />无认证</span>
        </div>

        <div class="pc-rows">
          <div class="pc-row">
            <Globe2 :size="13" />
            <span class="pc-row-label">地址</span>
            <span class="pc-row-val mono">{{ p.host }}:{{ p.port }}</span>
          </div>
          <div class="pc-row">
            <User :size="13" />
            <span class="pc-row-label">用户名</span>
            <span class="pc-row-val mono">{{ p.username || '—' }}</span>
          </div>
          <div class="pc-row">
            <Lock :size="13" />
            <span class="pc-row-label">密码</span>
            <span class="pc-row-val mono">{{ p.password ? maskPassword(p) : '—' }}</span>
            <button v-if="p.password" class="icon-btn mini" :title="revealed.has(p.id) ? '隐藏密码' : '显示密码'" @click="toggleReveal(p.id)">
              <EyeOff v-if="revealed.has(p.id)" :size="13" />
              <Eye v-else :size="13" />
            </button>
          </div>
        </div>

        <footer class="pc-foot">
          <button class="btn btn-ghost btn-sm" @click="testProxy(p)"><Zap :size="13" /> 测试</button>
          <div class="spacer"></div>
          <button class="icon-btn success" title="测试连接" @click="testProxy(p)"><Zap :size="14" /></button>
          <button class="icon-btn primary" title="编辑" @click="openEditProxy(p)"><Pencil :size="14" /></button>
          <button class="icon-btn" :title="p.status === 'active' ? '停用' : '启用'" @click="toggleProxy(p)"><CircleDot :size="14" /></button>
          <button class="icon-btn danger" title="删除" @click="removeProxy(p)"><Trash2 :size="14" /></button>
        </footer>
      </article>

      <article class="proxy-card add-tile" @click="openCreate">
        <Plus :size="22" />
        <span>添加代理节点</span>
      </article>
    </section>

    <!-- add / edit modal -->
    <Modal :open="showForm" :title="isEditing ? '编辑代理节点' : '添加代理节点'" subtitle="协议与认证信息将被安全存储" :icon="Network" @close="showForm = false">
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">节点名称 <span class="req">*</span></label>
          <input v-model="form.name" class="input" placeholder="例如：US · 主出口" />
        </div>
        <div class="field">
          <label class="field-label">协议</label>
          <div class="proto-picker">
            <button v-for="proto in ['http', 'https', 'socks5']" :key="proto" class="proto-chip" :class="{ on: form.protocol === proto }" @click="form.protocol = proto as ProxyProtocol">
              {{ proto.toUpperCase() }}
            </button>
          </div>
        </div>
      </div>
      <div class="form-grid-2">
        <div class="field">
          <label class="field-label">主机地址 <span class="req">*</span></label>
          <input v-model="form.host" class="input mono" placeholder="us-01.proxy.internal" />
        </div>
        <div class="field">
          <label class="field-label">端口</label>
          <input v-model.number="form.port" class="input mono" type="number" />
        </div>
      </div>
      <div class="field">
        <label class="field-label">认证方式</label>
        <div class="proto-picker">
          <button v-for="a in [
            { v: 'none', l: '无认证' },
            { v: 'username', l: '用户名密码' },
            { v: 'token', l: 'Token' }
          ]" :key="a.v" class="proto-chip" :class="{ on: form.auth === a.v }" @click="form.auth = a.v as ProxyAuth">
            {{ a.l }}
          </button>
        </div>
      </div>
      <div v-if="form.auth === 'username'" class="form-grid-2">
        <div class="field">
          <label class="field-label">用户名</label>
          <input v-model="form.username" class="input mono" placeholder="relay_user" />
        </div>
        <div class="field">
          <label class="field-label">密码</label>
          <input v-model="form.password" class="input mono" type="password" placeholder="••••••••" />
        </div>
      </div>
      <template #footer>
        <button class="btn btn-ghost" @click="showForm = false">取消</button>
        <button class="btn btn-primary" @click="saveProxy"><Plus :size="15" /> {{ isEditing ? '保存修改' : '添加节点' }}</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.toolbar { display: flex; align-items: center; gap: 16px; padding: 14px 16px; flex-wrap: wrap; }
.spacer { flex: 1; }
.toggle-label { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--text-muted); cursor: pointer; user-select: none; }

.proxy-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.proxy-card {
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: var(--glass);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 16px;
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
  position: absolute;
  top: -60px; right: -60px;
  width: 150px; height: 150px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(34,211,238,0.14), transparent 70%);
  pointer-events: none;
}

.pc-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.pc-id { display: flex; align-items: center; gap: 10px; min-width: 0; }
.pc-ico {
  width: 34px; height: 34px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  background: var(--surface-2); color: var(--text-faint);
  flex: none;
}
.pc-ico.on { background: rgba(34,211,238,0.12); color: var(--c1); box-shadow: 0 0 14px rgba(34,211,238,0.2); }
.pc-name { font-size: 14.5px; font-weight: 650; color: var(--text); letter-spacing: -0.01em; }

.pc-badge-row { display: flex; gap: 6px; flex-wrap: wrap; }

.pc-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 11px 0;
  border-top: 1px solid var(--border-soft);
  border-bottom: 1px solid var(--border-soft);
}
.pc-row { display: flex; align-items: center; gap: 9px; font-size: 12.5px; color: var(--text-muted); }
.pc-row > svg { width: 13px; height: 13px; color: var(--text-faint); flex: none; }
.pc-row-label { width: 44px; font-size: 11px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.04em; flex: none; }
.pc-row-val { flex: 1; color: var(--text-soft); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.icon-btn.mini { width: 22px; height: 22px; border-radius: 6px; }

.pc-foot { display: flex; align-items: center; gap: 2px; }
.text-danger { color: var(--danger); }
.text-ok { color: var(--success); }

.add-tile {
  border-style: dashed;
  border-color: var(--border-strong);
  background: transparent;
  align-items: center;
  justify-content: center;
  min-height: 170px;
  gap: 10px;
  color: var(--text-faint);
  font-size: 13.5px;
  font-weight: 600;
  cursor: pointer;
}
.add-tile:hover { color: var(--primary); border-color: var(--primary); background: rgba(34,211,238,0.04); }

.form-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.proto-picker { display: flex; gap: 7px; flex-wrap: wrap; }
.proto-chip {
  flex: 1;
  padding: 9px 10px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12.5px;
  font-weight: 600;
  font-family: var(--font-mono);
  cursor: pointer;
  transition: all 0.2s ease;
}
.proto-chip:hover { border-color: var(--border-strong); color: var(--text); }
.proto-chip.on { border-color: var(--primary); color: var(--primary); background: rgba(34,211,238,0.1); box-shadow: 0 0 0 3px rgba(34,211,238,0.1); }
</style>
