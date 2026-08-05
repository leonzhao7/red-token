<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { Moon, Sun, Search, Command, ChevronDown, Menu } from 'lucide-vue-next'
import { useTheme } from '../composables/useTheme'

const { theme, toggle } = useTheme()
const route = useRoute()
const emit = defineEmits<{ toggleSidebar: [] }>()

const title = computed(() => route.meta.title as string)
const group = computed(() => route.meta.group as string)
const subtitle = computed(() => route.meta.subtitle as string)
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
      <div class="crumb-line">
        <span class="crumb-group">{{ group }}</span>
        <h1 class="crumb-title">{{ title }}</h1>
      </div>
      <span v-if="subtitle" class="crumb-sub">{{ subtitle }}</span>
    </div>

    <div class="header-right">
      <div class="search-box header-search">
        <Search :size="15" />
        <input class="input" placeholder="搜索模型、中转站、密钥…" />
        <kbd class="kbd"><Command :size="10" /> K</kbd>
      </div>

      <div class="clock mono">
        <span class="clock-time">{{ clock }}</span>
        <span class="clock-date">{{ date }}</span>
      </div>

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

.crumb { display: flex; flex-direction: column; gap: 3px; min-width: 0; justify-content: center; }
.crumb-line { display: flex; align-items: baseline; gap: 9px; min-width: 0; }
.crumb-group { font-size: 11px; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.1em; font-weight: 600; }
.crumb-title { font-size: 17px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.crumb-sub { font-size: 11px; color: var(--text-faint); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 520px; }

.header-right { display: flex; align-items: center; gap: 12px; margin-left: auto; }
.header-search { width: 280px; }
.header-search .input { padding: 8px 13px; font-size: 12.5px; }
.header-search .kbd {
  position: absolute;
  right: 9px;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  gap: 2px;
  pointer-events: none;
  font-size: 10px;
}

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

@media (max-width: 1100px) {
  .clock { display: none; }
  .header-search { width: 200px; }
}
@media (max-width: 860px) {
  .header-search { display: none; }
  .burger { display: inline-flex; }
}
</style>
