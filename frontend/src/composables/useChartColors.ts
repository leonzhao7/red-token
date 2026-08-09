import { computed } from 'vue'
import { useTheme } from './useTheme'

function readVar(name: string, fallback: string) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

export function useChartColors() {
  const { theme } = useTheme()
  const c = computed(() => {
    const isLight = theme.value === 'light'

    return {
      text: readVar('--text', isLight ? '#0b0d1a' : '#f4f6fb'),
      muted: readVar('--text-muted', isLight ? '#6b7188' : '#8a93a8'),
      faint: readVar('--text-faint', isLight ? '#9aa0b5' : '#5b6377'),
      border: readVar('--border', isLight ? 'rgba(10,12,30,0.09)' : 'rgba(255,255,255,0.08)'),
      c1: readVar('--c1', isLight ? '#0891b2' : '#22d3ee'),
      c2: readVar('--c2', isLight ? '#6366f1' : '#8b5cf6'),
      c3: readVar('--c3', isLight ? '#c026d3' : '#e879f9'),
      c4: readVar('--c4', isLight ? '#10b981' : '#34d399'),
      c5: readVar('--c5', isLight ? '#f59e0b' : '#fbbf24'),
      c6: readVar('--c6', isLight ? '#e11d48' : '#fb7185'),
      c7: readVar('--c7', isLight ? '#ea580c' : '#fb923c')
    }
  })

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
