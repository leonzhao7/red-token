import { computed } from 'vue'
import { useTheme } from './useTheme'

function readVar(name: string, fallback: string) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

export function useChartColors() {
  const { theme } = useTheme()
  const c = computed(() => ({
    text: readVar('--text', '#f4f6fb'),
    muted: readVar('--text-muted', '#8a93a8'),
    faint: readVar('--text-faint', '#5b6377'),
    border: readVar('--border', 'rgba(255,255,255,0.08)'),
    c1: readVar('--c1', '#22d3ee'),
    c2: readVar('--c2', '#8b5cf6'),
    c3: readVar('--c3', '#e879f9'),
    c4: readVar('--c4', '#34d399'),
    c5: readVar('--c5', '#fbbf24'),
    c6: readVar('--c6', '#fb7185'),
    c7: readVar('--c7', '#38bdf8')
  }))

  function axisStyle() {
    return {
      axisLine: { lineStyle: { color: c.value.border } },
      axisTick: { show: false },
      axisLabel: { color: c.value.muted, fontFamily: 'JetBrains Mono', fontSize: 11 },
      splitLine: { lineStyle: { color: c.value.border, type: 'dashed' as const } }
    }
  }

  const tooltipStyle = (color: string[]) => ({
    backgroundColor: 'rgba(10,10,18,0.92)',
    borderColor: 'rgba(255,255,255,0.12)',
    borderWidth: 1,
    padding: [10, 14],
    textStyle: { color: '#e6e9f2', fontSize: 12, fontFamily: 'Inter' },
    extraCssText: 'border-radius:12px;box-shadow:0 12px 40px rgba(0,0,0,0.5);backdrop-filter:blur(8px);',
    axisPointer: {
      type: 'line' as const,
      lineStyle: { color: 'rgba(255,255,255,0.15)', type: 'dashed' as const }
    }
  })

  return { c, axisStyle, tooltipStyle, theme }
}
