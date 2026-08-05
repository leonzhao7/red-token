<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  Search,
  RefreshCw,
  Trash2,
  Filter,
  ChevronLeft,
  ChevronRight,
  Timer,
  Coins,
  Ban,
  AlertCircle
} from 'lucide-vue-next'
import { store } from '../store'
import { toast } from '../composables/toast'
import type { LogStatus } from '../types'

const search = ref('')
const modelFilter = ref('all')
const statusFilter = ref<'all' | 'success' | 'client' | 'server'>('all')
const page = ref(1)
const pageSize = 8

const models = computed(() => Array.from(new Set(store.usageLogs.map((l) => l.model))))

const statusMatch = (s: LogStatus, f: string) => {
  if (f === 'all') return true
  if (f === 'success') return s === 'success'
  if (f === 'client') return s === 'error'
  return s === 'timeout' || s === 'ratelimit'
}

const filtered = computed(() =>
  store.usageLogs.filter((l) => {
    const okSearch =
      l.keyName.toLowerCase().includes(search.value.toLowerCase()) ||
      l.key.includes(search.value.toLowerCase()) ||
      l.relay.toLowerCase().includes(search.value.toLowerCase())
    const okModel = modelFilter.value === 'all' || l.model === modelFilter.value
    const okStatus = statusMatch(l.status, statusFilter.value)
    return okSearch && okModel && okStatus
  })
)

const totalPages = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)))
const pageItems = computed(() => {
  const start = (page.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function resetPage() {
  page.value = 1
}

const statusMeta: Record<LogStatus, { label: string; cls: string }> = {
  success: { label: '成功', cls: 'success' },
  error: { label: '失败', cls: 'danger' },
  timeout: { label: '超时', cls: 'warning' },
  ratelimit: { label: '限流', cls: 'info' }
}

function clearLogs() {
  store.usageLogs = []
  resetPage()
  toast('日志已清空', '', 'warning')
}

function goPage(p: number) {
  page.value = Math.min(Math.max(1, p), totalPages.value)
}
</script>

<template>
  <div class="page stagger">
    <!-- toolbar -->
    <section class="toolbar panel">
      <div class="search-box" style="width: 230px">
        <Search :size="15" />
        <input v-model="search" class="input" placeholder="搜索密钥 / 中转站…" @input="resetPage" />
      </div>
      <div class="toolbar-filters">
        <select v-model="modelFilter" class="select" style="width: 170px" @change="resetPage">
          <option value="all">全部模型</option>
          <option v-for="m in models" :key="m" :value="m">{{ m }}</option>
        </select>
        <select v-model="statusFilter" class="select" style="width: 130px" @change="resetPage">
          <option value="all">全部</option>
          <option value="success">成功</option>
          <option value="client">客户端异常</option>
          <option value="server">服务端异常</option>
        </select>
      </div>
      <div class="spacer"></div>
      <button class="btn btn-ghost" @click="toast('已刷新', '日志数据已更新', 'info')"><RefreshCw :size="15" /> 刷新</button>
      <button class="btn btn-danger" @click="clearLogs"><Trash2 :size="15" /> 清空</button>
    </section>

    <!-- table -->
    <section class="panel table-panel">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>时间</th>
              <th>调用方</th>
              <th>模型</th>
              <th>中转站</th>
              <th>Tokens</th>
              <th>延迟</th>
              <th>状态</th>
              <th style="text-align: right">费用</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="l in pageItems" :key="l.id">
              <td class="mono" style="color: var(--text-muted); font-size: 12px">{{ l.time }}</td>
              <td>
                <div class="caller">
                  <span class="caller-avatar">{{ l.keyName.slice(0, 1) }}</span>
                  <div>
                    <strong>{{ l.keyName }}</strong>
                    <span class="mono caller-key">{{ l.key }}</span>
                  </div>
                </div>
              </td>
              <td><span class="tag">{{ l.model }}</span></td>
              <td style="color: var(--text-soft)">{{ l.relay }}</td>
              <td class="mono">{{ l.totalTokens.toLocaleString() }}</td>
              <td>
                <span v-if="l.latency" class="mono latency" :class="l.latency > 600 ? 'slow' : ''">
                  <Timer :size="12" /> {{ l.latency }}ms
                </span>
                <span v-else class="text-faint">--</span>
              </td>
              <td>
                <span class="pill" :class="statusMeta[l.status].cls">
                  <span class="dot" /> {{ statusMeta[l.status].label }}
                </span>
              </td>
              <td class="mono" style="text-align: right" :class="l.cost ? 'cost' : 'text-faint'">
                {{ l.cost ? '$' + l.cost.toFixed(4) : '--' }}
              </td>
            </tr>
            <tr v-if="!pageItems.length">
              <td colspan="8">
                <div class="empty">
                  <Filter :size="20" />
                  <span>没有匹配的日志记录，调整筛选条件试试。</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="pagination">
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

    <!-- hints -->
    <section class="hint-row">
      <div class="hint-card">
        <AlertCircle :size="15" class="hint-ico warning" />
        <div><strong>失败诊断</strong><span>点击日志行的状态徽章可查看失败堆栈与重试建议。</span></div>
      </div>
      <div class="hint-card">
        <Ban :size="15" class="hint-ico info" />
        <div><strong>限流处理</strong><span>限流请求自动排队重试，最多 3 次，间隔指数退避。</span></div>
      </div>
      <div class="hint-card">
        <Coins :size="15" class="hint-ico success" />
        <div><strong>费用统计</strong><span>费用按各供应商实时单价核算，可导出对账。</span></div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--space-4); }

.toolbar { display: flex; align-items: center; gap: 12px; padding: 14px 16px; flex-wrap: wrap; }
.toolbar-filters { display: flex; gap: 10px; flex-wrap: wrap; }
.spacer { flex: 1; }

.caller { display: flex; align-items: center; gap: 9px; }
.caller-avatar {
  width: 30px; height: 30px; border-radius: 9px;
  display: flex; align-items: center; justify-content: center;
  background: var(--grad-soft); color: var(--primary);
  font-size: 12px; font-weight: 700;
  flex: none;
}
.caller > div { display: flex; flex-direction: column; }
.caller strong { font-size: 12.5px; font-weight: 600; color: var(--text); }
.caller-key { font-size: 10.5px; color: var(--text-faint); }

.latency { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; color: var(--success); }
.latency.slow { color: var(--warning); }
.latency svg { width: 12px; height: 12px; }
.cost { color: var(--text-soft); font-weight: 600; }

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

.hint-row { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--space-4); }
.hint-card {
  display: flex; gap: 11px; align-items: flex-start;
  background: var(--surface); border: 1px solid var(--border-soft);
  border-radius: var(--radius-md); padding: 14px 16px;
}
.hint-ico { flex: none; margin-top: 2px; }
.hint-ico.warning { color: var(--warning); }
.hint-ico.info { color: var(--info); }
.hint-ico.success { color: var(--success); }
.hint-card div { display: flex; flex-direction: column; }
.hint-card strong { font-size: 12.5px; font-weight: 600; }
.hint-card span { font-size: 11.5px; color: var(--text-faint); line-height: 1.5; }

@media (max-width: 900px) { .hint-row { grid-template-columns: 1fr; } }
</style>
