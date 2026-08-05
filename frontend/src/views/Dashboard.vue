<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  Sparkles,
  Plus,
  KeyRound,
  Activity,
  Server,
  Bot,
  Coins,
  TrendingUp,
  Wifi,
  ArrowUpRight,
  Cpu,
  ShieldCheck,
  Zap,
  CalendarCheck,
  ChevronRight
} from 'lucide-vue-next'
import StatCard from '../components/StatCard.vue'
import BaseChart from '../components/BaseChart.vue'
import { useChartColors } from '../composables/useChartColors'
import { store } from '../store'
import { toast } from '../composables/toast'
import type { Relay } from '../types'

const router = useRouter()
const { c, axisStyle, tooltipStyle } = useChartColors()

const range = ref<'24h' | '7d' | '30d'>('24h')

/* ---------------- mock series ---------------- */
const hours = Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, '0')}:00`)
const days7 = ['07-27', '07-28', '07-29', '07-30', '07-31', '08-01', '08-02']
const days30 = Array.from({ length: 30 }, (_, i) => `0${Math.floor(i / 10) || ''}${i % 10 === 0 ? i / 10 : ''}`.slice(-2))

function genSeries(base: number, n: number, vol = 0.3, seed = 0) {
  const out: number[] = []
  let v = base
  for (let i = 0; i < n; i++) {
    const wave = Math.sin(i / 3 + seed) * base * vol
    const noise = (Math.random() - 0.5) * base * vol * 2
    v = Math.max(base * 0.2, v + wave * 0.25 + noise)
    out.push(Math.round(v))
  }
  return out
}

const tokenData = computed(() => {
  if (range.value === '24h') return genSeries(8_200_000, 24, 0.35, 1)
  if (range.value === '7d') return genSeries(11_500_000, 7, 0.4, 2)
  return genSeries(9_800_000, 30, 0.45, 3)
})

const tokenAxis = computed(() => (range.value === '24h' ? hours : range.value === '7d' ? days7 : days30))

const requestData = genSeries(420, 24, 0.4, 5)

/* ---------------- chart options ---------------- */
const trendOption = computed<echarts.EChartsOption>(() => {
  const data = tokenData.value
  return {
    grid: { left: 8, right: 8, top: 30, bottom: 4, containLabel: true },
    tooltip: {
      ...tooltipStyle([]),
      trigger: 'axis' as const,
      valueFormatter: (v: any) => (v / 1e6).toFixed(2) + ' M'
    },
    xAxis: { type: 'category', data: tokenAxis.value, boundaryGap: false, ...axisStyle(), axisLine: { show: false } },
    yAxis: {
      type: 'value',
      ...axisStyle(),
      splitNumber: 3,
      axisLabel: { ...axisStyle().axisLabel, formatter: (v: number) => (v / 1e6).toFixed(0) + 'M' }
    },
    series: [
      {
        name: 'Token 用量',
        type: 'line',
        data,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2.5, color: c.value.c1 },
        itemStyle: { color: c.value.c1 },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(34,211,238,0.32)' },
              { offset: 0.55, color: 'rgba(99,102,241,0.12)' },
              { offset: 1, color: 'rgba(139,92,246,0)' }
            ]
          }
        },
        emphasis: { focus: 'series' as const }
      },
      {
        name: '输入 Tokens',
        type: 'line',
        data: data.map((v) => Math.round(v * 0.62)),
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 1.5, color: c.value.c2, opacity: 0.7, type: 'dashed' as const },
        itemStyle: { color: c.value.c2 }
      }
    ]
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

const trafficOption = computed<echarts.EChartsOption>(() => {
  return {
    grid: { left: 8, right: 8, top: 30, bottom: 4, containLabel: true },
    tooltip: { ...tooltipStyle([]), trigger: 'axis' as const },
    xAxis: { type: 'category', data: hours, boundaryGap: false, ...axisStyle(), axisLine: { show: false }, axisLabel: { ...axisStyle().axisLabel, interval: 3 } },
    yAxis: { type: 'value', ...axisStyle(), splitNumber: 3 },
    series: [
      {
        name: '请求量',
        type: 'bar',
        data: requestData,
        barWidth: '55%',
        itemStyle: {
          borderRadius: [4, 4, 0, 0],
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(139,92,246,0.85)' },
              { offset: 1, color: 'rgba(139,92,246,0.15)' }
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
        radius: '95%',
        center: ['50%', '58%'],
        progress: { show: true, width: 12, roundCap: true, itemStyle: { color: c.value.c4 } },
        pointer: { show: false },
        axisLine: { lineStyle: { width: 12, color: [[1, c.value.border]] } },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: { show: false },
        anchor: { show: false },
        title: { show: true, offsetCenter: [0, '46%'], color: c.value.muted, fontSize: 11, fontFamily: 'Inter' },
        detail: {
          valueAnimation: true,
          offsetCenter: [0, '22%'],
          formatter: '{value}%',
          color: c.value.text,
          fontSize: 30,
          fontFamily: 'JetBrains Mono',
          fontWeight: 700
        },
        data: [{ value: 98.4, name: '请求成功率' }]
      }
    ]
  }
})

/* ---------------- aggregates ---------------- */
const activeCount = computed(() => store.relays.filter((r) => r.status === 'active').length)

const relayStatusIcon = (s: Relay['status']) => (s === 'active' ? 'green' : 'rose')

const fmtUsd = (n: number) => '$' + n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const checkinAll = () => {
  const todo = store.relays.filter((r) => !r.checkinAt)
  todo.forEach((r) => store.updateRelay(r.id, { checkinAt: '2026-08-02 ' + new Date().toTimeString().slice(0, 5) }))
  toast('批量签到完成', `已为 ${todo.length} 个账户完成今日签到`, 'success')
}
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
      <button class="btn btn-ghost btn-sm" @click="checkinAll"><CalendarCheck :size="14" /> 批量签到</button>
      <button class="btn btn-primary btn-sm" @click="router.push('/keys')"><Plus :size="14" /> 创建密钥</button>
    </section>

    <!-- stats -->
    <section class="stats-grid">
      <StatCard
        label="Token 总消耗"
        :value="'48.2M'"
        :delta="12.6"
        :icon="Coins"
        tone="cyan"
        sub="较上周 · 输入 29.8M / 输出 18.4M"
        :spark="genSeries(30, 20, 0.3, 1)"
      />
      <StatCard
        label="今日请求"
        :value="'1,024,839'"
        :delta="8.2"
        :icon="Activity"
        tone="violet"
        sub="峰值 4,912 次/分 · 均值 712 次/分"
        :spark="genSeries(400, 20, 0.4, 2)"
      />
      <StatCard
        label="启用中转站"
        :value="`${activeCount}/${store.relays.length}`"
        :delta="4"
        :icon="Server"
        tone="green"
        sub="接入供应商账户 · 余额自动巡检"
        :spark="genSeries(5, 20, 0.12, 3)"
      />
      <StatCard
        label="活跃模型"
        :value="'14'"
        :delta="-2.4"
        :icon="Bot"
        tone="pink"
        sub="跨 6 家平台 · 今日调用 9 个"
        :spark="genSeries(12, 20, 0.2, 4)"
      />
    </section>

    <!-- charts row 1 -->
    <section class="charts-1">
      <div class="panel token-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Sparkles :size="16" /> Token 用量趋势</div>
            <div class="panel-sub">输入 / 输出 token 随时间消耗曲线</div>
          </div>
        </div>
        <div class="panel-body">
          <div class="token-summary">
            <div class="token-big mono">48.24M</div>
            <div class="token-delta"><TrendingUp :size="13" /> <span class="mono">+12.6%</span> 较上一周期</div>
            <div class="token-legend">
              <span class="legend-item"><i class="lg lg-1"></i>总量</span>
              <span class="legend-item"><i class="lg lg-2"></i>输入</span>
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

      <div class="panel traffic-panel">
        <div class="panel-header">
          <div>
            <div class="panel-title"><Activity :size="16" /> 请求流量</div>
            <div class="panel-sub">24 小时请求量（次/时）</div>
          </div>
          <span class="pill success"><span class="dot" /> 正常</span>
        </div>
        <div class="panel-body">
          <BaseChart :option="trafficOption" height="140px" />
          <div class="traffic-meta">
            <div class="tm-cell">
              <div class="tm-label">请求成功率</div>
              <BaseChart :option="gaugeOption" height="118px" />
            </div>
            <div class="tm-list">
              <div class="tm-row">
                <span>错误请求</span>
                <strong class="mono" style="color: var(--danger)">1,382</strong>
              </div>
              <div class="tm-row">
                <span>超时请求</span>
                <strong class="mono" style="color: var(--warning)">2,104</strong>
              </div>
              <div class="tm-row">
                <span>限流拦截</span>
                <strong class="mono" style="color: var(--info)">847</strong>
              </div>
              <div class="tm-row">
                <span>平均延迟</span>
                <strong class="mono" style="color: var(--text)">132ms</strong>
              </div>
            </div>
          </div>
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
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-4);
}

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
  grid-template-columns: 1fr 1.2fr;
  gap: var(--space-4);
}
.traffic-meta { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 14px; }
.tm-cell { display: flex; flex-direction: column; align-items: center; }
.tm-label { font-size: 11.5px; color: var(--text-faint); font-weight: 600; letter-spacing: 0.05em; }
.tm-list { display: flex; flex-direction: column; gap: 10px; justify-content: center; padding-left: 12px; border-left: 1px solid var(--border); }
.tm-row { display: flex; justify-content: space-between; font-size: 12.5px; color: var(--text-muted); }
.tm-row strong { font-weight: 700; }

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
  .traffic-meta { grid-template-columns: 1fr; }
}
</style>
