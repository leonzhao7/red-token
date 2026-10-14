<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Moon, Sun, ChevronDown, Menu, Terminal, LogOut } from 'lucide-vue-next'
import { useTheme } from '../composables/useTheme'
import { toggleConsoleLog, consoleLogRows } from '../composables/consoleLog'
import { useAuth } from '../composables/useAuth'

const { theme, toggle } = useTheme()
const route = useRoute()
const router = useRouter()
const auth = useAuth()
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

const showUserMenu = ref(false)
const userInitial = computed(() => auth.user.value?.username.charAt(0).toUpperCase() || 'U')

async function handleLogout() {
  showUserMenu.value = false
  await auth.logout()
}
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

      <div class="avatar-row" @click="showUserMenu = !showUserMenu">
        <div class="avatar">{{ userInitial }}</div>
        <ChevronDown :size="13" class="avatar-caret" />

        <div v-if="showUserMenu" class="user-menu" @click.stop>
          <div class="user-menu-header">
            <div class="user-menu-name">{{ auth.user.value?.username }}</div>
            <div v-if="auth.user.value?.is_default_password" class="user-menu-badge">使用默认密码</div>
          </div>
          <div class="user-menu-divider"></div>
          <button class="user-menu-item" @click="handleLogout">
            <LogOut :size="16" />
            退出登录
          </button>
        </div>
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

.avatar-row { display: flex; align-items: center; gap: 4px; cursor: pointer; position: relative; }
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

.user-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  min-width: 200px;
  background: var(--glass-strong);
  border: 1px solid var(--border-strong);
  border-radius: 12px;
  box-shadow: var(--shadow-xl);
  overflow: hidden;
  z-index: 50;
}

.user-menu-header {
  padding: 16px;
}

.user-menu-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
  margin-bottom: 4px;
}

.user-menu-badge {
  font-size: 12px;
  color: var(--warning);
  background: var(--warning-dim);
  padding: 2px 8px;
  border-radius: 4px;
  display: inline-block;
}

.user-menu-divider {
  height: 1px;
  background: var(--border);
}

.user-menu-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: none;
  border: none;
  color: var(--text);
  font-size: 14px;
  cursor: pointer;
  transition: background 0.2s var(--ease-out);
}

.user-menu-item:hover {
  background: var(--surface);
}

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
