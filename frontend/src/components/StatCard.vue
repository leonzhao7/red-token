<script setup lang="ts">
import { computed } from 'vue'
import { ArrowDownRight, ArrowUpRight, type LucideIcon } from 'lucide-vue-next'
import BaseChart from './BaseChart.vue'

const props = withDefaults(
  defineProps<{
    label: string
    value: string
    sub?: string
    delta?: number
    icon?: LucideIcon
    tone?: 'cyan' | 'violet' | 'pink' | 'green' | 'amber' | 'blue'
    spark?: number[]
    index?: number
  }>(),
  { tone: 'cyan', index: 0 }
)

const up = computed(() => (props.delta ?? 0) >= 0)

const sparkOption = computed<echarts.EChartsOption>(() => {
  const colors = {
    cyan: '#22d3ee',
    violet: '#8b5cf6',
    pink: '#e879f9',
    green: '#34d399',
    amber: '#fbbf24',
    blue: '#38bdf8'
  }
  const c = colors[props.tone]
  const data = props.spark ?? []
  return {
    grid: { left: 0, right: 0, top: 6, bottom: 0 },
    xAxis: { type: 'category', show: false, data: data.map((_, i) => i) },
    yAxis: { type: 'value', show: false, min: 0 },
    tooltip: { show: false },
    series: [
      {
        type: 'line',
        data,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2, color: c },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: c + '55' },
              { offset: 1, color: c + '00' }
            ]
          }
        }
      }
    ]
  }
})
</script>

<template>
  <div class="stat-card panel hoverable" :class="tone">
    <div class="stat-top">
      <span class="stat-ico"><component :is="icon" :size="18" /></span>
      <span v-if="delta !== undefined" class="stat-delta" :class="up ? 'up' : 'down'">
        <ArrowUpRight v-if="up" :size="12" />
        <ArrowDownRight v-else :size="12" />
        {{ Math.abs(delta) }}%
      </span>
    </div>
    <div class="stat-label">{{ label }}</div>
    <div class="stat-value mono">{{ value }}</div>
    <div v-if="sub && !$slots.default" class="stat-sub">{{ sub }}</div>
    <div v-if="$slots.default" class="stat-slot-area">
      <slot />
    </div>
    <BaseChart v-if="spark && spark.length" class="stat-spark" :option="sparkOption" height="44px" />
  </div>
</template>

<style scoped>
.stat-card { padding: 20px; overflow: hidden; position: relative; }
.stat-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.stat-ico {
  width: 38px;
  height: 38px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
}
.stat-card.cyan .stat-ico { background: rgba(34,211,238,0.12); color: var(--c1); box-shadow: 0 0 18px rgba(34,211,238,0.25); }
.stat-card.violet .stat-ico { background: rgba(139,92,246,0.12); color: var(--c2); box-shadow: 0 0 18px rgba(139,92,246,0.25); }
.stat-card.pink .stat-ico { background: rgba(232,121,249,0.12); color: var(--c3); box-shadow: 0 0 18px rgba(232,121,249,0.25); }
.stat-card.green .stat-ico { background: rgba(52,211,153,0.12); color: var(--c4); box-shadow: 0 0 18px rgba(52,211,153,0.25); }
.stat-card.amber .stat-ico { background: rgba(251,191,36,0.12); color: var(--c5); box-shadow: 0 0 18px rgba(251,191,36,0.25); }
.stat-card.blue .stat-ico { background: rgba(56,189,248,0.12); color: var(--c7); box-shadow: 0 0 18px rgba(56,189,248,0.25); }

.stat-delta {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  font-size: 12px;
  font-weight: 700;
  font-family: var(--font-mono);
  padding: 3px 8px;
  border-radius: 8px;
}
.stat-delta.up { color: var(--success); background: var(--success-soft); }
.stat-delta.down { color: var(--danger); background: var(--danger-soft); }

.stat-label { font-size: 12.5px; color: var(--text-muted); font-weight: 500; }
.stat-value {
  font-size: 30px;
  font-weight: 700;
  letter-spacing: -0.03em;
  line-height: 1.2;
  margin-top: 3px;
  color: var(--text);
}
.stat-sub { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.stat-slot-area {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px dashed var(--border-soft, rgba(255,255,255,0.06));
}
.stat-spark { margin-top: 12px; }
</style>
