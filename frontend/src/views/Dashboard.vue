<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Sparkles,
  Activity,
  Coins,
  Wifi,
  ArrowUpRight,
  Cpu
} from 'lucide-vue-next'
import StatCard from '../components/StatCard.vue'
import BaseChart from '../components/BaseChart.vue'
import { useChartColors } from '../composables/useChartColors'
import { store } from '../store'
import { toast } from '../composables/toast'
import { getHourlyModelStats } from '../api/dashboard'
import type { HourlyModelStatsResponse } from '../api/dashboard'

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
const modelColors = computed(() => [
  c.value.c1,
  c.value.c2,
  c.value.c3,
  c.value.c4,
  c.value.c5,
  c.value.c7,
  c.value.c6
])

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
    color: modelColors.value[i % modelColors.value.length]
  }))
})

interface ModelUsageSlice {
  name: string
  value: number
  percent: number
  color: string
}

const modelUsageDistribution = computed<ModelUsageSlice[]>(() => {
  const items = modelStats.value?.items ?? []
  const totals = new Map<string, number>()

  for (const item of items) {
    const total = (item.input_tokens || 0) + (item.output_tokens || 0)
    if (total <= 0) continue
    const name = item.model.trim() || '未知模型'
    totals.set(name, (totals.get(name) || 0) + total)
  }

  const ranked = [...totals.entries()]
    .map(([name, value]) => ({ name, value }))
    .sort((a, b) => b.value - a.value)
  const top = ranked.slice(0, 5)
  const otherValue = ranked.slice(5).reduce((sum, item) => sum + item.value, 0)
  if (otherValue > 0) top.push({ name: '其他', value: otherValue })

  const total = top.reduce((sum, item) => sum + item.value, 0)
  return top.map((item, index) => ({
    ...item,
    percent: total > 0 ? (item.value / total) * 100 : 0,
    color: item.name === '其他'
      ? c.value.faint
      : modelColors.value[index % modelColors.value.length]
  }))
})

interface RelayUsageSlice {
  name: string
  input: number
  output: number
  total: number
}

const relayUsageTop = computed<RelayUsageSlice[]>(() => {
  const items = modelStats.value?.items ?? []
  const map = new Map<string, { input: number; output: number }>()

  for (const it of items) {
    const name = it.backend?.trim() || '未知中转站'
    const cur = map.get(name) || { input: 0, output: 0 }
    cur.input += it.input_tokens || 0
    cur.output += it.output_tokens || 0
    map.set(name, cur)
  }

  const ranked = [...map.entries()]
    .map(([name, v]) => ({ name, input: v.input, output: v.output, total: v.input + v.output }))
    .sort((a, b) => b.total - a.total)

  const top = ranked.slice(0, 5)
  const rest = ranked.slice(5)
  if (rest.length > 0) {
    const merged = rest.reduce((acc, r) => ({ input: acc.input + r.input, output: acc.output + r.output }), { input: 0, output: 0 })
    top.push({ name: '其他', input: merged.input, output: merged.output, total: merged.input + merged.output })
  }
  return top
})

const rangeLabel = computed(() => {
  if (range.value === '24h') return '近 24 小时'
  if (range.value === '7d') return '近 7 天'
  return '近 30 天'
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
const activeTokenSeries = ref<string | null>(null)
const activeRequestSeries = ref<string | null>(null)

function isTokenSeriesSelected(name: string) {
  return activeTokenSeries.value === null || activeTokenSeries.value === name
}

function selectTokenSeries(name: string) {
  activeTokenSeries.value = activeTokenSeries.value === name ? null : name
}

function isRequestSeriesSelected(name: string) {
  return activeRequestSeries.value === null || activeRequestSeries.value === name
}

function selectRequestSeries(name: string) {
  activeRequestSeries.value = activeRequestSeries.value === name ? null : name
}

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
    legend: {
      show: false,
      selected: Object.fromEntries(
        ['总 Token', ...models.map((model) => model.name)]
          .map((name) => [name, isTokenSeriesSelected(name)])
      )
    },
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
  const models = modelUsageDistribution.value
  return {
    tooltip: {
      ...tooltipStyle([]),
      trigger: 'item' as const,
      formatter: (params: any) => `${params.name}<br/> ${fmtNum(Number(params.value))} tokens (${Number(params.percent || 0).toFixed(1)}%)`
    },
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
          text: fmtNum(totalTokens.value),
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
          text: '总消耗',
          fill: c.value.muted,
          font: '12px "Inter", sans-serif',
          textAlign: 'center'
        }
      }
    ]
  }
})

const relayBarOption = computed<echarts.EChartsOption>(() => {
  const top = relayUsageTop.value
  const names = top.map(t => t.name)
  return {
    grid: { left: 8, right: 40, top: 8, bottom: 4, containLabel: true },
    tooltip: {
      ...tooltipStyle([]),
      trigger: 'item' as const,
      formatter: (p: any) => `${p.name}<br/>总用量: ${fmtNum(p.value)}`
    },
    xAxis: { type: 'value', ...axisStyle(), splitNumber: 3, axisLabel: { ...axisStyle().axisLabel, formatter: (v: number) => fmtNum(v) } },
    yAxis: { type: 'category', ...axisStyle(), data: names, inverse: true, axisLine: { show: false }, axisLabel: { color: c.value.text, fontSize: 11.5, fontFamily: 'Inter', width: 92, overflow: 'truncate' } },
    series: [
      {
        type: 'bar',
        data: top.map((t, i) => ({ value: t.total, itemStyle: { color: t.name === '其他' ? c.value.faint : c.value.c2, borderRadius: [0, 6, 6, 0] } })),
        barWidth: 14,
        showBackground: true,
        backgroundStyle: { color: c.value.border, borderRadius: 6 },
        label: { show: true, position: 'right', color: c.value.muted, fontSize: 11, fontFamily: 'JetBrains Mono', formatter: (p: any) => fmtNum(p.value) }
      }
    ]
  }
})

const requestTrendOption = computed<echarts.EChartsOption>(() => {
  const axis = tokenAxis.value
  return {
    grid: { left: 8, right: 16, top: 16, bottom: 4, containLabel: true },
    tooltip: { ...tooltipStyle([]), trigger: 'axis' as const, valueFormatter: (v: any) => fmtNum(Number(v)) },
    legend: {
      show: false,
      selected: Object.fromEntries(
        ['总请求', '成功', '失败'].map((name) => [name, isRequestSeriesSelected(name)])
      )
    },
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
            <div class="panel-sub">{{ rangeLabel }}各模型 token 消耗趋势</div>
          </div>
        </div>
        <div class="panel-body">
          <div class="token-summary">
            <div class="token-legend">
              <button
                type="button"
                class="legend-item"
                :class="{ inactive: !isTokenSeriesSelected('总 Token') }"
                :aria-pressed="isTokenSeriesSelected('总 Token')"
                @click="selectTokenSeries('总 Token')"
              ><i class="lg lg-1"></i>总量</button>
              <button
                v-for="ms in modelTokenSeries"
                :key="ms.name"
                type="button"
                class="legend-item"
                :class="{ inactive: !isTokenSeriesSelected(ms.name) }"
                :aria-pressed="isTokenSeriesSelected(ms.name)"
                @click="selectTokenSeries(ms.name)"
              >
                <i class="lg" :style="{ background: ms.color }"></i>{{ ms.name }}
              </button>
            </div>
          </div>
          <BaseChart :option="trendOption" height="250px" />
        </div>
      </div>

      <div class="panel model-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Cpu :size="16" /> 模型用量分布</div>
            <div class="panel-sub">{{ rangeLabel }} token 消耗占比</div>
          </div>
          <button class="icon-btn primary" @click="router.push('/logs')" aria-label="查看日志">
            <ArrowUpRight :size="15" />
          </button>
        </div>
        <div class="panel-body">
          <BaseChart :option="modelDonutOption" height="210px" />
          <div class="donut-legend">
            <span v-for="m in modelUsageDistribution" :key="m.name" class="donut-row">
              <span class="d-dot" :style="{ background: m.color }"></span>
              <span class="d-name">{{ m.name }}</span>
              <span class="d-tokens mono">{{ fmtNum(m.value) }}</span>
              <span class="d-val mono">{{ m.percent.toFixed(1) }}%</span>
            </span>
            <span v-if="!modelUsageDistribution.length" class="donut-empty">暂无数据</span>
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
            <div class="panel-sub">{{ rangeLabel }}请求次数统计</div>
          </div>
        </div>
        <div class="panel-body">
          <div class="token-summary">
            <div class="token-legend">
              <button
                v-for="item in [
                  { name: '总请求', className: 'lg-2', color: '' },
                  { name: '成功', className: '', color: '#34d399' },
                  { name: '失败', className: '', color: '#f87171' }
                ]"
                :key="item.name"
                type="button"
                class="legend-item"
                :class="{ inactive: !isRequestSeriesSelected(item.name) }"
                :aria-pressed="isRequestSeriesSelected(item.name)"
                @click="selectRequestSeries(item.name)"
              ><i class="lg" :class="item.className" :style="item.color ? { background: item.color } : undefined"></i>{{ item.name }}</button>
            </div>
          </div>
          <BaseChart :option="requestTrendOption" height="250px" />
        </div>
      </div>

      <div class="panel relay-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Wifi :size="16" /> 中转站用量排行</div>
            <div class="panel-sub">{{ rangeLabel }} token 消耗排行</div>
          </div>
          <button class="btn btn-outline btn-sm" @click="router.push('/relays')">管理中转站</button>
        </div>
        <div class="panel-body">
          <BaseChart :option="relayBarOption" height="280px" />
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
.token-legend { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-left: auto; }
.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px;
  border: 0;
  border-radius: 4px;
  color: var(--text-muted);
  background: transparent;
  font: inherit;
  font-size: 11.5px;
  cursor: pointer;
  transition: color 0.15s ease, opacity 0.15s ease;
}
.legend-item:hover { color: var(--text); }
.legend-item.inactive { opacity: 0.38; }
.lg { width: 16px; height: 3px; border-radius: 3px; display: inline-block; }
.lg-1 { background: var(--c1); box-shadow: 0 0 8px rgba(34,211,238,0.5); }
.lg-2 { background: var(--c2); opacity: 0.8; }

/* donut */
.donut-legend { display: flex; flex-direction: column; gap: 8px; padding: 0 6px; }
.donut-row { display: flex; align-items: center; gap: 9px; font-size: 12px; }
.d-dot { width: 8px; height: 8px; border-radius: 3px; flex: none; }
.d-name { color: var(--text-soft); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.d-tokens { width: 64px; color: var(--text-soft); font-weight: 600; text-align: right; }
.d-val { width: 52px; color: var(--text-muted); font-weight: 600; text-align: right; }
.donut-empty { padding: 4px 0; color: var(--text-faint); font-size: 12px; }

/* charts 2 */
.charts-2 {
  display: grid;
  grid-template-columns: 1.9fr 1fr;
  gap: var(--space-4);
}

@media (max-width: 1200px) {
  .stats-grid { grid-template-columns: repeat(2, 1fr); }
  .charts-1, .charts-2 { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .stats-grid { grid-template-columns: 1fr; }
}
</style>
