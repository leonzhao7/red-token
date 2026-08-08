<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Sparkles,
  Activity,
  Coins,
  Wifi,
  ArrowUpRight,
  Cpu,
  ShieldCheck,
  Zap,
  ChevronRight
} from 'lucide-vue-next'
import StatCard from '../components/StatCard.vue'
import BaseChart from '../components/BaseChart.vue'
import { useChartColors } from '../composables/useChartColors'
import { store } from '../store'
import { toast } from '../composables/toast'
import { getHourlyModelStats } from '../api/dashboard'
import type { HourlyModelStatsResponse } from '../api/dashboard'
import type { Relay } from '../types'

const router = useRouter()
const { c, axisStyle, tooltipStyle } = useChartColors()

const range = ref<'24h' | '7d' | '30d'>('24h')

/* ---------------- API data ---------------- */
const modelStats = ref<HourlyModelStatsResponse | null>(null)
const loading = ref(false)

function getTimeRange(r: '24h' | '7d' | '30d') {
  const now = new Date()
  now.setMinutes(0, 0, 0)
  const end = now.toISOString()
  const start = new Date(now)
  if (r === '24h') start.setHours(start.getHours() - 24)
  else if (r === '7d') start.setDate(start.getDate() - 7)
  else start.setDate(start.getDate() - 30)
  return { start_hour: start.toISOString(), end_hour: end }
}

async function fetchDashboard() {
  loading.value = true
  try {
    const tr = getTimeRange(range.value)
    modelStats.value = await getHourlyModelStats(tr.start_hour, tr.end_hour)
  } catch (e: any) {
    toast('数据加载失败', e.message || '无法获取仪表盘数据', 'danger')
  } finally {
    loading.value = false
  }
}

onMounted(fetchDashboard)
watch(range, fetchDashboard)

/* ---------------- computed stats ---------------- */
// Token stats from model-level hourly data (real token counts)
const totalInputTokens = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.input_tokens || 0), 0)
})
const totalOutputTokens = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.output_tokens || 0), 0)
})
const totalTokens = computed(() => totalInputTokens.value + totalOutputTokens.value)
const totalCacheTokens = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.input_cache_tokens || 0), 0)
})

// Request stats from model stats
const totalRequests = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.requests || 0), 0)
})
const successRequests = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.successes || 0), 0)
})
const failedRequests = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.failures || 0), 0)
})

// Latency (weighted average of success_avg_duration_ms by successes)
const avgLatency = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return 0
  let totalDuration = 0
  let totalSucc = 0
  for (const it of items) {
    const s = it.successes || 0
    totalDuration += (it.success_avg_duration_ms || 0) * s
    totalSucc += s
  }
  return totalSucc > 0 ? Math.round(totalDuration / totalSucc) : 0
})

// Traffic bytes (request + response)
const totalRequestBytes = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.success_request_bytes || 0), 0)
})
const totalResponseBytes = computed(() => {
  const items = modelStats.value?.items
  if (!items) return 0
  return items.reduce((sum, it) => sum + (it.success_response_bytes || 0), 0)
})
const totalTrafficBytes = computed(() => totalRequestBytes.value + totalResponseBytes.value)

const successRate = computed(() =>
  totalRequests.value > 0 ? +((successRequests.value / totalRequests.value) * 100).toFixed(1) : 0
)

const cacheRate = computed(() =>
  totalInputTokens.value > 0 ? +((totalCacheTokens.value / totalInputTokens.value) * 100).toFixed(1) : 0
)

// Token sparkline from model stats (per-hour total tokens)
const tokenSpark = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []
  const hourMap = new Map<string, number>()
  for (const it of items) {
    const total = (it.input_tokens || 0) + (it.output_tokens || 0)
    const key = it.hour
    hourMap.set(key, (hourMap.get(key) || 0) + total)
  }
  return [...hourMap.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, v]) => v)
    .slice(-20)
})


/* ---------------- format helpers ---------------- */
function fmtNum(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}

function fmtBytes(b: number): string {
  if (b >= 1e9) return (b / 1e9).toFixed(2) + 'GB'
  if (b >= 1e6) return (b / 1e6).toFixed(2) + 'MB'
  if (b >= 1e3) return (b / 1e3).toFixed(1) + 'KB'
  return b + 'B'
}

function fmtMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

/* request sparkline from model stats (per-hour total requests) */
const requestSpark = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []
  const hourMap = new Map<string, number>()
  for (const it of items) {
    const key = it.hour
    hourMap.set(key, (hourMap.get(key) || 0) + (it.requests || 0))
  }
  return [...hourMap.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, v]) => v)
    .slice(-20)
})

/* ---------------- per-model token series ---------------- */
const MODEL_COLORS = ['#22d3ee', '#8b5cf6', '#e879f9', '#34d399', '#fbbf24', '#38bdf8', '#f87171']

interface ModelSeries {
  name: string
  data: number[]
  color: string
}

const modelTokenSeries = computed<ModelSeries[]>(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []

  // collect all hours from the axis for alignment
  const axis = tokenAxis.value
  if (axis.length === 0) return []

  // group items by model
  const modelMap = new Map<string, Map<string, number>>()
  for (const it of items) {
    const total = (it.input_tokens || 0) + (it.output_tokens || 0)
    if (total === 0) continue
    if (!modelMap.has(it.model)) modelMap.set(it.model, new Map())
    // Normalize hour label to match axis format
    const hourLabel = formatHourLabel(it.hour, range.value)
    const m = modelMap.get(it.model)!
    m.set(hourLabel, (m.get(hourLabel) || 0) + total)
  }

  // Sort models by total tokens descending, take top 6
  const ranked = [...modelMap.entries()]
    .map(([name, hourMap]) => ({
      name,
      hourMap,
      total: [...hourMap.values()].reduce((a, b) => a + b, 0)
    }))
    .sort((a, b) => b.total - a.total)
    .slice(0, 6)

  return ranked.map((m, i) => ({
    name: m.name,
    data: axis.map(label => m.hourMap.get(label) || 0),
    color: MODEL_COLORS[i % MODEL_COLORS.length]
  }))
})

function formatHourLabel(isoHour: string, r: '24h' | '7d' | '30d'): string {
  const d = new Date(isoHour)
  if (r === '24h') {
    return `${String(d.getHours()).padStart(2, '0')}:00`
  }
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${mm}-${dd}`
}

/* ---------------- time axis & request series from model stats ---------------- */
const tokenAxis = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []
  const labels = new Set<string>()
  for (const it of items) labels.add(formatHourLabel(it.hour, range.value))
  return [...labels].sort()
})

const requestData = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []
  const axis = tokenAxis.value
  const hourMap = new Map<string, number>()
  for (const it of items) {
    const label = formatHourLabel(it.hour, range.value)
    hourMap.set(label, (hourMap.get(label) || 0) + (it.requests || 0))
  }
  return axis.map(label => hourMap.get(label) || 0)
})

const successData = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []
  const axis = tokenAxis.value
  const hourMap = new Map<string, number>()
  for (const it of items) {
    const label = formatHourLabel(it.hour, range.value)
    hourMap.set(label, (hourMap.get(label) || 0) + (it.successes || 0))
  }
  return axis.map(label => hourMap.get(label) || 0)
})

const failureData = computed(() => {
  const items = modelStats.value?.items
  if (!items || items.length === 0) return []
  const axis = tokenAxis.value
  const hourMap = new Map<string, number>()
  for (const it of items) {
    const label = formatHourLabel(it.hour, range.value)
    hourMap.set(label, (hourMap.get(label) || 0) + (it.failures || 0))
  }
  return axis.map(label => hourMap.get(label) || 0)
})

const hours = tokenAxis

/* ---------------- chart options ---------------- */
const trendOption = computed<echarts.EChartsOption>(() => {
  const models = modelTokenSeries.value
  const axis = tokenAxis.value

  /* total line (sum of all models per time bucket) */
  const totalLine: number[] = axis.map((_: string, i: number) =>
    models.reduce((sum: number, m: ModelSeries) => sum + (m.data[i] || 0), 0)
  )

  const series: any[] = [
    {
      name: '总 Token',
      type: 'line',
      data: totalLine,
      smooth: true,
      symbol: 'none',
      lineStyle: { width: 2.5, color: c.value.c1 },
      itemStyle: { color: c.value.c1 },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(34,211,238,0.25)' },
            { offset: 1, color: 'rgba(34,211,238,0)' }
          ]
        }
      },
      emphasis: { focus: 'series' as const }
    },
    /* per-model lines */
    ...models.map((m: ModelSeries) => ({
      name: m.name,
      type: 'line',
      data: m.data,
      smooth: true,
      symbol: 'none',
      lineStyle: { width: 1.5, color: m.color },
      itemStyle: { color: m.color },
      emphasis: { focus: 'series' as const }
    }))
  ]

  return {
    grid: { left: 8, right: 8, top: 30, bottom: 4, containLabel: true },
    tooltip: {
      ...tooltipStyle([]),
      trigger: 'axis' as const,
      valueFormatter: (v: any) => fmtNum(Number(v))
    },
    legend: { show: false },
    xAxis: { type: 'category', data: axis, boundaryGap: false, ...axisStyle(), axisLine: { show: false } },
    yAxis: {
      type: 'value',
      ...axisStyle(),
      splitNumber: 3,
      axisLabel: { ...axisStyle().axisLabel, formatter: (v: number) => fmtNum(v) }
    },
    series
  }
})

const modelDonutOption = computed<echarts.EChartsOption>(() => {
  const models = [
    { name: 'gpt-4o', value: 34.8, color: c.value.c1 },
    { name: 'gpt-4o-mini', value: 22.4, color: c.value.c7 },
    { name: 'claude-3-5-sonnet', value: 18.2, color: c.value.c3 },
    { name: 'deepseek-r1', value: 11.5, color: c.value.c2 },
    { name: 'gemini-1.5-flash', value: 7.1, color: c.value.c4 },
    { name: '其他', value: 6.0, color: c.value.faint }
  ]
  return {
    tooltip: { ...tooltipStyle([]), trigger: 'item' as const, formatter: '{b}<br/> {c}%' },
    series: [
      {
        type: 'pie',
        radius: ['58%', '80%'],
        center: ['50%', '46%'],
        avoidLabelOverlap: true,
        itemStyle: { borderRadius: 8, borderWidth: 3, borderColor: 'transparent' },
        label: { show: false },
        emphasis: { scaleSize: 8, label: { show: false } },
        data: models,
        padAngle: 3
      }
    ],
    graphic: [
      {
        type: 'text',
        left: 'center',
        top: '38%',
        style: {
          text: '16.2M',
          fill: c.value.text,
          font: '700 24px "JetBrains Mono", monospace',
          textAlign: 'center'
        }
      },
      {
        type: 'text',
        left: 'center',
        top: '52%',
        style: {
          text: '今日总消耗',
          fill: c.value.muted,
          font: '12px "Inter", sans-serif',
          textAlign: 'center'
        }
      }
    ]
  }
})

const relayBarOption = computed<echarts.EChartsOption>(() => {
  const top = [...store.relays]
    .sort((a, b) => b.used - a.used)
    .slice(0, 6)
    .map((r) => ({ name: r.name, used: r.used, color: r.status === 'disabled' ? c.value.faint : c.value.c2 }))
  return {
    grid: { left: 8, right: 40, top: 8, bottom: 4, containLabel: true },
    tooltip: {
      ...tooltipStyle([]),
      trigger: 'item' as const,
      formatter: (p: any) => `${p.name}<br/> ${(p.value / 1e6).toFixed(1)} M tokens`
    },
    xAxis: { type: 'value', ...axisStyle(), splitNumber: 3, axisLabel: { ...axisStyle().axisLabel, formatter: (v: number) => (v / 1e6).toFixed(0) + 'M' } },
    yAxis: { type: 'category', ...axisStyle(), data: top.map((t) => t.name), axisLine: { show: false }, axisLabel: { color: c.value.text, fontSize: 11.5, fontFamily: 'Inter', width: 92, overflow: 'truncate' } },
    series: [
      {
        type: 'bar',
        data: top.map((t) => ({ value: t.used, itemStyle: { color: t.color, borderRadius: [0, 6, 6, 0] } })),
        barWidth: 12,
        showBackground: true,
        backgroundStyle: { color: c.value.border, borderRadius: 6 },
        label: { show: true, position: 'right', color: c.value.muted, fontSize: 11, fontFamily: 'JetBrains Mono', formatter: (p: any) => (p.value / 1e6).toFixed(0) + 'M' }
      }
    ]
  }
})

const requestTrendOption = computed<echarts.EChartsOption>(() => {
  const axis = tokenAxis.value
  return {
    grid: { left: 8, right: 16, top: 16, bottom: 4, containLabel: true },
    tooltip: { ...tooltipStyle([]), trigger: 'axis' as const, valueFormatter: (v: any) => fmtNum(Number(v)) },
    legend: { show: false },
    xAxis: { type: 'category', data: axis, boundaryGap: false, ...axisStyle(), axisLine: { show: false } },
    yAxis: { type: 'value', ...axisStyle(), splitNumber: 4, axisLabel: { ...axisStyle().axisLabel, formatter: (v: number) => fmtNum(v) } },
    series: [
      {
        name: '总请求',
        type: 'line',
        data: requestData.value,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2.5, color: c.value.c2 },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(139,92,246,0.2)' },
              { offset: 1, color: 'rgba(139,92,246,0)' }
            ]
          }
        }
      },
      {
        name: '成功',
        type: 'line',
        data: successData.value,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2, color: '#34d399' },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(52,211,153,0.2)' },
              { offset: 1, color: 'rgba(52,211,153,0)' }
            ]
          }
        }
      },
      {
        name: '失败',
        type: 'line',
        data: failureData.value,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2, color: '#f87171' },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(248,113,113,0.15)' },
              { offset: 1, color: 'rgba(248,113,113,0)' }
            ]
          }
        }
      }
    ]
  }
})

const gaugeOption = computed<echarts.EChartsOption>(() => {
  return {
    series: [
      {
        type: 'gauge',
        startAngle: 210,
        endAngle: -30,
        min: 0,
        max: 100,
        radius: '88%',
        center: ['50%', '56%'],
        progress: { show: true, width: 10, roundCap: true, itemStyle: { color: c.value.c4 } },
        pointer: { show: false },
        axisLine: { lineStyle: { width: 10, color: [[1, c.value.border]] } },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: { show: false },
        anchor: { show: false },
        title: { show: true, offsetCenter: [0, '52%'], color: c.value.muted, fontSize: 10, fontFamily: 'Inter' },
        detail: {
          valueAnimation: true,
          offsetCenter: [0, '18%'],
          formatter: '{value}%',
          color: c.value.text,
          fontSize: 22,
          fontFamily: 'JetBrains Mono',
          fontWeight: 700
        },
        data: [{ value: successRate.value, name: '成功率' }]
      }
    ]
  }
})

const cacheGaugeOption = computed<echarts.EChartsOption>(() => {
  return {
    series: [
      {
        type: 'gauge',
        startAngle: 210,
        endAngle: -30,
        min: 0,
        max: 100,
        radius: '88%',
        center: ['50%', '56%'],
        progress: { show: true, width: 10, roundCap: true, itemStyle: { color: c.value.c1 } },
        pointer: { show: false },
        axisLine: { lineStyle: { width: 10, color: [[1, c.value.border]] } },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: { show: false },
        anchor: { show: false },
        title: { show: true, offsetCenter: [0, '52%'], color: c.value.muted, fontSize: 10, fontFamily: 'Inter' },
        detail: {
          valueAnimation: true,
          offsetCenter: [0, '18%'],
          formatter: '{value}%',
          color: c.value.text,
          fontSize: 22,
          fontFamily: 'JetBrains Mono',
          fontWeight: 700
        },
        data: [{ value: cacheRate.value, name: '缓存率' }]
      }
    ]
  }
})

/* ---------------- aggregates ---------------- */
const activeCount = computed(() => store.relays.filter((r) => r.status === 'active').length)

const relayStatusIcon = (s: Relay['status']) => (s === 'active' ? 'green' : 'rose')

const fmtUsd = (n: number) => '$' + n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

</script>

<template>
  <div class="dashboard stagger">
    <!-- toolbar -->
    <section class="toolbar panel">
      <span class="toolbar-label">统计范围</span>
      <div class="segmented">
        <button :class="{ active: range === '24h' }" @click="range = '24h'">24H</button>
        <button :class="{ active: range === '7d' }" @click="range = '7d'">7D</button>
        <button :class="{ active: range === '30d' }" @click="range = '30d'">30D</button>
      </div>
      <div class="spacer"></div>
    </section>

    <!-- stats -->
    <section class="stats-grid">
      <StatCard
        label="Token 消耗"
        :value="fmtNum(totalTokens)"
        :icon="Coins"
        tone="cyan"
        :spark="tokenSpark"
      >
        <div class="req-gauge-float">
          <BaseChart :option="cacheGaugeOption" height="120px" />
        </div>
        <div class="sm-row">
          <span class="sm-chip cyan"><span class="sm-dot"></span>输入 <b class="mono">{{ fmtNum(totalInputTokens) }}</b></span>
          <span class="sm-chip violet"><span class="sm-dot"></span>输出 <b class="mono">{{ fmtNum(totalOutputTokens) }}</b></span>
          <span class="sm-chip green"><span class="sm-dot"></span>缓存 <b class="mono">{{ fmtNum(totalCacheTokens) }}</b></span>
        </div>
      </StatCard>
      <StatCard
        label="请求统计"
        :value="fmtNum(totalRequests)"
        :icon="Activity"
        tone="violet"
        :spark="requestSpark"
      >
        <div class="req-gauge-float">
          <BaseChart :option="gaugeOption" height="120px" />
        </div>
        <div class="sm-row">
          <span class="sm-chip success"><span class="sm-dot"></span>成功 <b class="mono">{{ fmtNum(successRequests) }}</b></span>
          <span class="sm-chip danger"><span class="sm-dot"></span>失败 <b class="mono">{{ fmtNum(failedRequests) }}</b></span>
          <span class="sm-chip muted"><span class="sm-dot"></span>延迟 <b class="mono">{{ fmtMs(avgLatency) }}</b></span>
          <span class="sm-chip muted"><span class="sm-dot"></span>请求 <b class="mono">{{ fmtBytes(totalRequestBytes) }}</b></span>
          <span class="sm-chip muted"><span class="sm-dot"></span>响应 <b class="mono">{{ fmtBytes(totalResponseBytes) }}</b></span>
        </div>
      </StatCard>
    </section>

    <!-- charts row 1 -->
    <section class="charts-1">
      <div class="panel token-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Sparkles :size="16" /> Token 用量趋势</div>
            <div class="panel-sub">各模型 token 消耗趋势 · 按用量排名前 6</div>
          </div>
        </div>
        <div class="panel-body">
          <div class="token-summary">
            <div class="token-big mono">{{ fmtNum(totalTokens) }}</div>
            <div class="token-meta">输入 {{ fmtNum(totalInputTokens) }} · 输出 {{ fmtNum(totalOutputTokens) }}</div>
            <div class="token-legend">
              <span class="legend-item"><i class="lg lg-1"></i>总量</span>
              <span v-for="ms in modelTokenSeries" :key="ms.name" class="legend-item">
                <i class="lg" :style="{ background: ms.color }"></i>{{ ms.name }}
              </span>
            </div>
          </div>
          <BaseChart :option="trendOption" height="250px" />
        </div>
      </div>

      <div class="panel model-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Cpu :size="16" /> 模型用量分布</div>
            <div class="panel-sub">今日 token 消耗占比</div>
          </div>
          <button class="icon-btn primary" @click="router.push('/logs')" aria-label="查看日志">
            <ArrowUpRight :size="15" />
          </button>
        </div>
        <div class="panel-body">
          <BaseChart :option="modelDonutOption" height="210px" />
          <div class="donut-legend">
            <span v-for="m in [
              { n: 'gpt-4o', v: '34.8%', col: 'var(--c1)' },
              { n: 'gpt-4o-mini', v: '22.4%', col: 'var(--c7)' },
              { n: 'claude-3-5-sonnet', v: '18.2%', col: 'var(--c3)' },
              { n: 'deepseek-r1', v: '11.5%', col: 'var(--c2)' }
            ]" :key="m.n" class="donut-row">
              <span class="d-dot" :style="{ background: m.col }"></span>
              <span class="d-name">{{ m.n }}</span>
              <span class="d-val mono">{{ m.v }}</span>
            </span>
          </div>
        </div>
      </div>
    </section>

    <!-- charts row 2 -->
    <section class="charts-2">
      <div class="panel request-trend-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Activity :size="16" /> 请求趋势</div>
            <div class="panel-sub">按时间统计请求次数</div>
          </div>
        </div>
        <div class="panel-body">
          <div class="token-summary">
            <div class="token-big mono">{{ fmtNum(totalRequests) }}</div>
            <div class="token-meta">成功 {{ fmtNum(successRequests) }} · 失败 {{ fmtNum(failedRequests) }}</div>
            <div class="token-legend">
              <span class="legend-item"><i class="lg lg-2"></i>总请求</span>
              <span class="legend-item"><i class="lg" style="background:#34d399"></i>成功</span>
              <span class="legend-item"><i class="lg" style="background:#f87171"></i>失败</span>
            </div>
          </div>
          <BaseChart :option="requestTrendOption" height="250px" />
        </div>
      </div>

      <div class="panel relay-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Wifi :size="16" /> 中转站用量排行</div>
            <div class="panel-sub">按累计消耗 token 排序</div>
          </div>
          <button class="btn btn-outline btn-sm" @click="router.push('/relays')">管理中转站</button>
        </div>
        <div class="panel-body">
          <BaseChart :option="relayBarOption" height="280px" />
        </div>
      </div>
    </section>

    <!-- bottom row -->
    <section class="bottom-grid">
      <div class="panel activity-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Zap :size="16" /> 实时动态</div>
            <div class="panel-sub">网关事件流</div>
          </div>
          <span class="pill info"><span class="dot pulse-dot" /> LIVE</span>
        </div>
        <div class="panel-body">
          <div class="feed">
            <div v-for="ev in [
              { t: 'relay', title: '中转站签到成功', detail: 'Claude 代理池 · 每日额度刷新 +50M', time: '08:00', tone: 'success' },
              { t: 'key', title: '新建 API Key', detail: '第三方客户 · A公司 创建密钥', time: '09:42', tone: 'info' },
              { t: 'relay', title: '额度告警', detail: '智谱 GLM 剩余额度不足 0.1%', time: '10:15', tone: 'warning' },
              { t: 'system', title: '自动故障转移', detail: 'Claude 池降级至 Gemini 官方', time: '11:47', tone: 'info' },
              { t: 'relay', title: '同步完成', detail: 'DeepSeek 模型列表已更新', time: '12:20', tone: 'success' },
              { t: 'key', title: '额度用尽预警', detail: '数据分析 Pipeline 达 99.4%', time: '13:05', tone: 'warning' }
            ]" :key="ev.title" class="feed-item">
              <span class="feed-dot" :class="ev.tone"></span>
              <div class="feed-body">
                <strong>{{ ev.title }}</strong>
                <span class="feed-detail">{{ ev.detail }}</span>
              </div>
              <span class="feed-time mono">{{ ev.time }}</span>
            </div>
          </div>
        </div>
      </div>

      <div class="panel relay-status-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><ShieldCheck :size="16" /> 中转站链路状态</div>
            <div class="panel-sub">实时健康监测</div>
          </div>
        </div>
        <div class="panel-body">
          <div v-for="r in store.relays.slice(0, 6)" :key="r.id" class="relay-row" @click="router.push('/relays')">
            <div class="rr-left">
              <span class="rr-dot" :class="relayStatusIcon(r.status)"></span>
              <div class="rr-name">
                <strong>{{ r.name }}</strong>
                <span class="rr-model">{{ r.models[0]?.name }} 等 {{ r.models.length }} 个模型</span>
              </div>
            </div>
            <div class="rr-right">
              <span class="mono rr-latency">{{ fmtUsd(r.balance) }}</span>
              <ChevronRight :size="14" class="rr-chevron" />
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.dashboard { display: flex; flex-direction: column; gap: var(--space-5); }

.toolbar { display: flex; align-items: center; gap: 16px; padding: 14px 16px; flex-wrap: wrap; }
.toolbar-label { font-size: 11px; font-weight: 600; color: var(--text-faint); letter-spacing: 0.06em; text-transform: uppercase; }
.spacer { flex: 1; }

/* stats */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-4);
}
.req-gauge-float {
  position: absolute;
  top: 0;
  left: 20%;
  bottom: 70px;
  height: 60%;
  width: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* stat card sub-metrics */
.sm-row {
  display: flex;
  flex-wrap: nowrap;
  gap: 8px;
}
.sm-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  border-radius: 20px;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-soft);
  background: rgba(255,255,255,0.04);
  border: 1px solid rgba(255,255,255,0.06);
  transition: background 0.15s, border-color 0.15s;
}
.sm-chip:hover {
  background: rgba(255,255,255,0.07);
  border-color: rgba(255,255,255,0.1);
}
.sm-chip b {
  font-weight: 700;
  color: var(--text);
}
.sm-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.sm-chip.cyan .sm-dot { background: #22d3ee; box-shadow: 0 0 6px rgba(34,211,238,0.5); }
.sm-chip.violet .sm-dot { background: #8b5cf6; box-shadow: 0 0 6px rgba(139,92,246,0.5); }
.sm-chip.green .sm-dot { background: #34d399; box-shadow: 0 0 6px rgba(52,211,153,0.5); }
.sm-chip.success .sm-dot { background: var(--success); box-shadow: 0 0 6px rgba(52,211,153,0.5); }
.sm-chip.danger .sm-dot { background: var(--danger); box-shadow: 0 0 6px rgba(239,68,68,0.5); }
.sm-chip.muted .sm-dot { background: var(--text-faint); }

/* charts 1 */
.charts-1 {
  display: grid;
  grid-template-columns: 1.9fr 1fr;
  gap: var(--space-4);
}
.token-summary { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; margin-bottom: 6px; }
.token-big { font-size: 24px; font-weight: 700; color: var(--text); letter-spacing: -0.02em; }
.token-delta {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 600;
  color: var(--success);
  background: var(--success-soft);
  padding: 4px 10px;
  border-radius: 99px;
}
.token-delta svg { color: var(--success); }
.token-legend { display: flex; gap: 14px; margin-left: auto; }
.legend-item { display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--text-muted); }
.lg { width: 16px; height: 3px; border-radius: 3px; display: inline-block; }
.lg-1 { background: var(--c1); box-shadow: 0 0 8px rgba(34,211,238,0.5); }
.lg-2 { background: var(--c2); opacity: 0.8; }

/* donut */
.donut-legend { display: flex; flex-direction: column; gap: 8px; padding: 0 6px; }
.donut-row { display: flex; align-items: center; gap: 9px; font-size: 12px; }
.d-dot { width: 8px; height: 8px; border-radius: 3px; flex: none; }
.d-name { color: var(--text-soft); flex: 1; }
.d-val { color: var(--text-muted); font-weight: 600; }

/* charts 2 */
.charts-2 {
  display: grid;
  grid-template-columns: 1.9fr 1fr;
  gap: var(--space-4);
}

/* bottom */
.bottom-grid { display: grid; grid-template-columns: 1.1fr 1fr; gap: var(--space-4); }
.feed { display: flex; flex-direction: column; }
.feed-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 11px 0;
  border-bottom: 1px solid var(--border-soft);
}
.feed-item:last-child { border-bottom: none; }
.feed-dot { width: 9px; height: 9px; border-radius: 50%; flex: none; }
.feed-dot.success { background: var(--success); box-shadow: 0 0 10px var(--success); }
.feed-dot.warning { background: var(--warning); box-shadow: 0 0 10px var(--warning); }
.feed-dot.danger { background: var(--danger); box-shadow: 0 0 10px var(--danger); }
.feed-dot.info { background: var(--info); box-shadow: 0 0 10px var(--info); }
.feed-body { flex: 1; display: flex; flex-direction: column; }
.feed-body strong { font-size: 13px; font-weight: 600; }
.feed-detail { font-size: 11.5px; color: var(--text-faint); }
.feed-time { font-size: 11px; color: var(--text-faint); }

.relay-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 11px 12px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background 0.2s ease;
}
.relay-row:hover { background: var(--surface); }
.rr-left { display: flex; align-items: center; gap: 11px; min-width: 0; }
.rr-dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.rr-dot.green { background: var(--success); box-shadow: 0 0 10px var(--success); }
.rr-dot.amber { background: var(--warning); box-shadow: 0 0 10px var(--warning); }
.rr-dot.info { background: var(--info); box-shadow: 0 0 10px var(--info); }
.rr-dot.rose { background: var(--danger); box-shadow: 0 0 10px var(--danger); }
.rr-name { display: flex; flex-direction: column; min-width: 0; }
.rr-name strong { font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.rr-model { font-size: 11px; color: var(--text-faint); }
.rr-right { display: flex; align-items: center; gap: 8px; }
.rr-latency { font-size: 11.5px; color: var(--text-muted); }
.rr-chevron { color: var(--text-faint); transition: transform 0.2s ease; }
.relay-row:hover .rr-chevron { transform: translateX(3px); color: var(--text); }

@media (max-width: 1200px) {
  .stats-grid { grid-template-columns: repeat(2, 1fr); }
  .charts-1, .charts-2, .bottom-grid { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .stats-grid { grid-template-columns: 1fr; }
}
</style>
