<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import {
  Search,
  RefreshCw,
  Trash2,
  Filter,
  ChevronLeft,
  ChevronRight,
  Timer,
  LoaderCircle,
  SlidersHorizontal
} from 'lucide-vue-next'
import { toast } from '../composables/toast'
import { listLogs, getLogOptions, clearLogs } from '../api/logs'
import type { UsageLog, LogOptions } from '../api/logs'

const PAGE_SIZE = 20

const logs = ref<UsageLog[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(true)

const options = ref<LogOptions>({ backends: [], models: [], client_keys: [], proxies: [] })

const search = ref('')
const modelFilter = ref('')
const keyFilter = ref('')
const statusFilter = ref('')

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))

async function loadOptions() {
  try {
    options.value = await getLogOptions()
  } catch {}
}

async function loadData() {
  loading.value = true
  try {
    const res = await listLogs({
      q: search.value || undefined,
      model: modelFilter.value || undefined,
      client_key: keyFilter.value || undefined,
      status: statusFilter.value || undefined,
      page: page.value,
      limit: PAGE_SIZE
    })
    logs.value = res.items
    total.value = res.total
  } catch (e: any) {
    toast('加载失败', e?.message || '', 'danger')
  } finally {
    loading.value = false
  }
}

function resetPage() {
  page.value = 1
}

function goPage(p: number) {
  page.value = Math.min(Math.max(1, p), totalPages.value)
}

async function refresh() {
  await loadData()
  toast('已刷新', '日志数据已更新', 'info')
}

async function doClear() {
  try {
    const res = await clearLogs()
    logs.value = []
    total.value = 0
    page.value = 1
    toast('日志已清空', `共删除 ${res.deleted} 条`, 'warning')
  } catch (e: any) {
    toast('清空失败', e?.message || '', 'danger')
  }
}

// debounce search
let searchTimer: ReturnType<typeof setTimeout>
watch(search, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { resetPage(); loadData() }, 350)
})

watch([modelFilter, keyFilter, statusFilter], () => {
  resetPage()
  loadData()
})

watch(page, loadData)

function statusColor(log: UsageLog): string {
  const fam = log.status_family || (log.status_code ? Math.floor(log.status_code / 100) + 'xx' : '')
  if (fam === '2xx') return 'ok'
  if (fam === '3xx') return 'info'
  if (fam === '4xx') return 'warn'
  if (fam === '5xx') return 'err'
  return 'neutral'
}

function fmtBytes(n?: number) {
  if (!n) return '—'
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

function fmtTime(s: string) {
  if (!s) return '—'
  const d = new Date(s)
  return d.toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

// visible page buttons (max 7 around current)
const visiblePages = computed(() => {
  const t = totalPages.value
  if (t <= 7) return Array.from({ length: t }, (_, i) => i + 1)
  const cur = page.value
  const pages: (number | null)[] = []
  pages.push(1)
  if (cur > 3) pages.push(null)
  for (let i = Math.max(2, cur - 1); i <= Math.min(t - 1, cur + 1); i++) pages.push(i)
  if (cur < t - 2) pages.push(null)
  pages.push(t)
  return pages
})

// optional column visibility
const colKeys = ['path', 'client_addr', 'request_id', 'ua'] as const
type ColKey = typeof colKeys[number]
const colLabels: Record<ColKey, string> = {
  path: '路径',
  client_addr: '客户端地址',
  request_id: '请求 ID',
  ua: 'User-Agent'
}
const visibleCols = ref<Record<ColKey, boolean>>({
  path: true,
  client_addr: false,
  request_id: false,
  ua: false
})
const colMenuOpen = ref(false)

function openColMenu() {
  colMenuOpen.value = true
  setTimeout(() => {
    document.addEventListener('click', () => { colMenuOpen.value = false }, { once: true })
  }, 0)
}

const totalCols = computed(() => 8 + colKeys.filter(k => visibleCols.value[k]).length)

onMounted(() => {
  loadOptions()
  loadData()
})
</script>

<template>
  <div class="page stagger">
    <!-- toolbar -->
    <section class="toolbar panel">
      <div class="search-box" style="width: 230px">
        <Search :size="15" />
        <input v-model="search" class="input" placeholder="搜索请求 ID / 模型 / 中转站…" />
      </div>
      <div class="toolbar-filters">
        <select v-model="modelFilter" class="select" style="width: 150px">
          <option value="">全部模型</option>
          <option v-for="m in options.models" :key="m" :value="m">{{ m }}</option>
        </select>
        <select v-model="keyFilter" class="select" style="width: 150px">
          <option value="">全部 API Key</option>
          <option v-for="k in options.client_keys" :key="k" :value="k">{{ k }}</option>
        </select>
        <select v-model="statusFilter" class="select" style="width: 130px">
          <option value="">全部状态</option>
          <option value="2xx">成功 2xx</option>
          <option value="4xx">客户端错误 4xx</option>
          <option value="5xx">服务端错误 5xx</option>
        </select>
      </div>
      <div class="spacer"></div>
      <div class="col-menu-wrap" @click.stop>
        <button class="btn btn-ghost" @click="openColMenu">
          <SlidersHorizontal :size="15" /> 列
        </button>
        <div v-if="colMenuOpen" class="col-menu">
          <label v-for="k in colKeys" :key="k" class="col-menu-item">
            <input type="checkbox" v-model="visibleCols[k]" />
            {{ colLabels[k] }}
          </label>
        </div>
      </div>
      <button class="btn btn-ghost" :disabled="loading" @click="refresh">
        <LoaderCircle v-if="loading" :size="15" class="spin" />
        <RefreshCw v-else :size="15" />
        刷新
      </button>
      <button class="btn btn-danger" @click="doClear"><Trash2 :size="15" /> 清空</button>
    </section>

    <!-- table -->
    <section class="panel table-panel">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>时间</th>
              <th>Key</th>
              <th>模型</th>
              <th>中转站</th>
              <th>状态</th>
              <th>延迟</th>
              <th>Tokens</th>
              <th>Bytes</th>
              <th v-if="visibleCols.path">路径</th>
              <th v-if="visibleCols.client_addr">客户端</th>
              <th v-if="visibleCols.request_id">请求 ID</th>
              <th v-if="visibleCols.ua">User-Agent</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading && !logs.length">
              <td :colspan="totalCols">
                <div class="empty"><LoaderCircle :size="18" class="spin" /><span>加载中…</span></div>
              </td>
            </tr>
            <tr v-for="l in logs" :key="l.id">
              <td class="mono td-time">{{ fmtTime(l.created_at) }}</td>
              <td class="td-key">{{ l.client_name || '—' }}</td>
              <td><span class="tag">{{ l.model }}</span></td>
              <td class="td-relay">{{ l.backend_name || '—' }}</td>
              <td>
                <span class="status-code" :class="statusColor(l)">{{ l.status_code || '—' }}</span>
              </td>
              <td>
                <span v-if="l.duration_ms != null" class="mono latency" :class="l.duration_ms > 60000 ? 'slow' : ''">
                  <Timer :size="12" /> {{ (l.duration_ms / 1000).toFixed(2) }}s
                </span>
                <span v-else class="text-faint">—</span>
              </td>
              <td class="mono">
                <div class="cell-l1">输入 {{ (l.input_tokens || 0).toLocaleString() }}</div>
                <div class="cell-l2">缓存 {{ (l.input_cache_tokens || 0).toLocaleString() }} · 输出 {{ (l.output_tokens || 0).toLocaleString() }}</div>
              </td>
              <td class="mono">
                <div class="cell-l1">{{ fmtBytes((l.request_bytes || 0) + (l.response_bytes || 0)) }}</div>
                <div class="cell-l2">请求 {{ fmtBytes(l.request_bytes) }} · 响应 {{ fmtBytes(l.response_bytes) }}</div>
              </td>
              <td v-if="visibleCols.path" class="td-path" :title="l.path || ''">{{ l.path || '—' }}</td>
              <td v-if="visibleCols.client_addr" class="td-addr">{{ l.client_ip || '—' }}</td>
              <td v-if="visibleCols.request_id" class="mono td-reqid">{{ l.request_id || '—' }}</td>
              <td v-if="visibleCols.ua" class="td-ua" :title="l.user_agent || ''">{{ l.user_agent || '—' }}</td>
            </tr>
            <tr v-if="!loading && !logs.length">
              <td :colspan="totalCols">
                <div class="empty">
                  <Filter :size="20" />
                  <span>没有匹配的日志记录，调整筛选条件试试。</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-show="total > 0" class="pagination">
        <span class="page-info mono">{{ total.toLocaleString() }} 条记录 · 第 {{ page }}/{{ totalPages }} 页</span>
        <div class="page-btns">
          <button class="icon-btn" :disabled="page <= 1" @click="goPage(page - 1)" aria-label="上一页"><ChevronLeft :size="15" /></button>
          <template v-for="(p, i) in visiblePages" :key="i">
            <span v-if="p === null" class="page-ellipsis">…</span>
            <button v-else class="page-num mono" :class="{ on: p === page }" @click="goPage(p)">{{ p }}</button>
          </template>
          <button class="icon-btn" :disabled="page >= totalPages" @click="goPage(page + 1)" aria-label="下一页"><ChevronRight :size="15" /></button>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.toolbar { display: flex; align-items: center; gap: 12px; padding: 14px 16px; flex-wrap: wrap; overflow: visible; }
.toolbar.panel { overflow: visible; z-index: 10; position: relative; }
.toolbar-filters { display: flex; gap: 10px; flex-wrap: wrap; }
.spacer { flex: 1; }

.table-panel { position: relative; z-index: 1; }

.col-menu-wrap {
  position: relative;
}
.col-menu {
  position: absolute; top: 100%; right: 0; margin-top: 6px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 12px; padding: 8px; min-width: 150px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.15);
  z-index: 1000;
}
.col-menu-item {
  display: flex; align-items: center; gap: 8px; padding: 6px 10px;
  font-size: 12.5px; color: var(--text); cursor: pointer;
  border-radius: 6px; transition: background 0.15s;
}
.col-menu-item:hover { background: var(--surface-2); }
.col-menu-item input[type="checkbox"] {
  width: 15px; height: 15px; cursor: pointer;
}

.td-time { color: var(--text-muted); font-size: 12px; white-space: nowrap; }
.td-key { font-size: 12.5px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 120px; }
.td-relay { color: var(--text-soft); font-size: 12.5px; }
.td-path { font-size: 12px; color: var(--text-muted); max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.td-addr { font-size: 12px; color: var(--text-soft); white-space: nowrap; }
.td-reqid { font-size: 11.5px; color: var(--text-faint); max-width: 100px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.td-ua { font-size: 11.5px; color: var(--text-faint); max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.status-code {
  font-size: 12.5px; font-weight: 700; font-variant-numeric: tabular-nums;
  padding: 2px 7px; border-radius: 6px;
}
.status-code.ok     { color: var(--success); background: var(--success-soft); }
.status-code.info   { color: var(--c1);      background: rgba(34,211,238,0.10); }
.status-code.warn   { color: var(--warning); background: rgba(245,158,11,0.12); }
.status-code.err    { color: var(--danger);  background: var(--danger-soft); }
.status-code.neutral{ color: var(--text-faint); background: var(--surface-2); }

.cell-l1 { font-size: 12.5px; color: var(--text-soft); line-height: 1.4; }
.cell-l2 { font-size: 11px; color: var(--text-faint); line-height: 1.4; margin-top: 1px; }

.latency { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; color: var(--text-soft); }
.latency.slow { color: var(--danger); }
.latency svg { width: 12px; height: 12px; }

.empty {
  display: flex; align-items: center; justify-content: center; gap: 10px;
  padding: 44px 0; color: var(--text-faint); font-size: 13px;
}

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
.page-ellipsis { padding: 0 4px; color: var(--text-faint); font-size: 13px; }
</style>
