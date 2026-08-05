import { ref, watch } from 'vue'

const stored = localStorage.getItem('nexus-theme')
const theme = ref<'dark' | 'light'>(stored === 'light' ? 'light' : 'dark')

watch(theme, (t) => {
  document.documentElement.setAttribute('data-theme', t)
  document.documentElement.classList.toggle('dark', t === 'dark')
  localStorage.setItem('nexus-theme', t)
})

if (stored === 'light') {
  document.documentElement.setAttribute('data-theme', 'light')
}

export function useTheme() {
  return {
    theme,
    toggle() {
      theme.value = theme.value === 'dark' ? 'light' : 'dark'
    }
  }
}
