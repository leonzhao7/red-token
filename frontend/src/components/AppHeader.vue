<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { Moon, Sun, ChevronDown, Menu, Terminal } from 'lucide-vue-next'
import { useTheme } from '../composables/useTheme'
import { toggleConsoleLog, consoleLogRows } from '../composables/consoleLog'

const { theme, toggle } = useTheme()
const route = useRoute()
const emit = defineEmits<{ toggleSidebar: [] }>()

const title = computed(() => route.meta.title as string)
const group = computed(() => route.meta.group as string)
const now = ref(new Date())
setInterval(() => (now.value = new Date()), 1000 * 60)

const clock = computed(() =>
  now.value.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
)
const date = computed(() =>
  now.value.toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })
)
</script>

<template>
  <header class="header">
    <button class="icon-btn burger" @click="emit('toggleSidebar')" aria-label="打开导航">
      <Menu :size="18" />
    </button>

    <div class="crumb">
      <span class="crumb-group">{{ group }}</span>
      <h1 class="crumb-title">{{ title }}</h1>
    </div>

    <div class="header-right">
      <div class="clock mono">
        <span class="clock-time">{{ clock }}</span>
        <span class="clock-date">{{ date }}</span>
      </div>

      <button class="icon-btn terminal-btn" @click="toggleConsoleLog" aria-label="控制台日志">
        <Terminal :size="16" />
        <span v-if="consoleLogRows.length > 0" class="terminal-dot"></span>
      </button>

      <button class="icon-btn" @click="toggle" aria-label="切换主题">
        <Moon v-if="theme === 'dark'" :size="16" />
        <Sun v-else :size="16" />
      </button>

      <div class="avatar-row">
        <div class="avatar">NX</div>
        <ChevronDown :size="13" class="avatar-caret" />
      </div>
    </div>
  </header>
</template>

<style scoped>
.header {
  position: sticky;
  top: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  gap: 16px;
  height: var(--header-h);
  padding: 0 26px;
  background: color-mix(in srgb, var(--bg) 72%, transparent);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-bottom: 1px solid var(--border-soft);
}
.burger { display: none; }

.crumb { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
.crumb-group { font-size: 11px; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.1em; font-weight: 600; flex-shrink: 0; }
.crumb-title { font-size: 20px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.header-right { display: flex; align-items: center; gap: 12px; margin-left: auto; }

.clock { display: flex; flex-direction: column; align-items: flex-end; line-height: 1.25; padding: 0 6px; }
.clock-time { font-size: 15px; font-weight: 700; color: var(--text); }
.clock-date { font-size: 10.5px; color: var(--text-faint); }

.avatar-row { display: flex; align-items: center; gap: 4px; cursor: pointer; }
.avatar {
  width: 34px;
  height: 34px;
  border-radius: 11px;
  background: var(--grad);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: var(--glow-violet);
}
.avatar-caret { color: var(--text-faint); }

.terminal-btn { position: relative; }
.terminal-dot {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--c2);
  box-shadow: 0 0 6px var(--c2);
}

@media (max-width: 1100px) {
  .clock { display: none; }
}
@media (max-width: 860px) {
  .burger { display: inline-flex; }
}
</style>
