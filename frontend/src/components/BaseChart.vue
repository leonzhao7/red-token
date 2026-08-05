<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import * as echarts from 'echarts'

const props = withDefaults(
  defineProps<{
    option: echarts.EChartsOption
    height?: string
  }>(),
  { height: '260px' }
)

const el = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

function render() {
  if (!el.value) return
  if (!chart) {
    chart = echarts.init(el.value)
    const ro = new ResizeObserver(() => chart?.resize())
    ro.observe(el.value)
    ;(el.value as any)._ro = ro
  }
  chart.setOption(props.option, true)
}

function dispose() {
  if (el.value && (el.value as any)._ro) {
    ;(el.value as any)._ro.disconnect()
    delete (el.value as any)._ro
  }
  chart?.dispose()
  chart = null
}

onMounted(render)
watch(() => props.option, render, { deep: true })
onBeforeUnmount(dispose)
</script>

<template>
  <div ref="el" class="chart" :style="{ height }"></div>
</template>

<style scoped>
.chart {
  width: 100%;
}
</style>
