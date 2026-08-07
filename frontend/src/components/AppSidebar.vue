<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  LayoutDashboard,
  KeyRound,
  Network,
  ServerCog,
  ScrollText,
  Settings,
  PanelLeftClose,
  PanelLeftOpen,
  Activity
} from 'lucide-vue-next'
import { store } from '../store'

const route = useRoute()
const router = useRouter()

const groups = [
  {
    label: '运营',
    items: [
      { to: '/', label: '总览', icon: LayoutDashboard },
      { to: '/relays', label: '中转站', icon: ServerCog },
      { to: '/keys', label: 'API Keys', icon: KeyRound }
    ]
  },
  {
    label: '接入',
    items: [
      { to: '/proxies', label: '代理服务器', icon: Network },
      { to: '/logs', label: '使用日志', icon: ScrollText }
    ]
  },
  {
    label: '系统',
    items: [{ to: '/settings', label: '系统配置', icon: Settings }]
  }
]

const props = defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ toggle: [] }>()
const healthPct = computed(() => {
  const active = store.relays.filter((r) => r.status === 'active').length
  return Math.round((active / store.relays.length) * 100)
})
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="brand" @click="router.push('/')" :title="collapsed ? 'NEXUS RELAY' : ''">
      <div class="brand-mark">
        <svg viewBox="0 0 32 32" width="26" height="26">
          <rect width="32" height="32" rx="8" fill="url(#brandGrad)" />
          <path
            d="M9 20 L14 12 L17 17 L21 10 L24 16"
            stroke="#0b0d1a"
            stroke-width="2.4"
            fill="none"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
          <defs>
            <linearGradient id="brandGrad" x1="0" y1="0" x2="32" y2="32">
              <stop stop-color="#22d3ee" />
              <stop offset="0.5" stop-color="#6366f1" />
              <stop offset="1" stop-color="#e879f9" />
            </linearGradient>
          </defs>
        </svg>
      </div>
      <div class="brand-text">
        <span class="brand-name">NEXUS<span class="accent">RELAY</span></span>
        <span class="brand-sub">AI Gateway Console</span>
      </div>
    </div>

    <nav class="nav">
      <div v-for="g in groups" :key="g.label" class="nav-group">
        <div class="nav-group-label">{{ g.label }}</div>
        <RouterLink
          v-for="item in g.items"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          :class="{ active: route.path === item.to }"
          :title="collapsed ? item.label : ''"
        >
          <component :is="item.icon" :size="17" />
          <span class="nav-label">{{ item.label }}</span>
          <span v-if="item.badge" class="nav-badge mono">{{ item.badge() }}</span>
        </RouterLink>
      </div>
    </nav>

    <div class="sidebar-foot">
      <div class="health-card">
        <div class="health-head">
          <span class="health-title"><Activity :size="13" /> 网关健康度</span>
          <span class="health-pct mono">{{ healthPct }}%</span>
        </div>
        <div class="progress green"><span :style="{ width: healthPct + '%' }"></span></div>
        <div class="health-meta">
          <span class="pulse-dot dot" />
          <span class="text-faint" style="font-size: 11px">{{ store.relays.filter((r) => r.status === 'active').length }} 个账户已启用</span>
        </div>
      </div>
      <div class="version-row">
        <span class="mono text-faint" style="font-size: 11px">v2.4.1-core</span>
        <button class="icon-btn" :title="collapsed ? '展开侧栏' : '仅显示图标'" @click="emit('toggle')">
          <PanelLeftClose v-if="!collapsed" :size="14" />
          <PanelLeftOpen v-else :size="14" />
        </button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  position: fixed;
  top: 0;
  left: 0;
  bottom: 0;
  width: var(--nav-w);
  display: flex;
  flex-direction: column;
  background: var(--glass-strong);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border-right: 1px solid var(--border);
  z-index: 50;
  transition: width 0.4s var(--ease-out), transform 0.4s var(--ease-out);
}

/* rail: icons only */
.sidebar.collapsed { width: var(--rail-w); transform: translateX(0); }
.sidebar.collapsed .brand { justify-content: center; padding: 20px 0 18px; }
.sidebar.collapsed .brand-text { display: none; }
.sidebar.collapsed .nav { padding: 14px 10px; }
.sidebar.collapsed .nav-group { margin-bottom: 10px; }
.sidebar.collapsed .nav-group-label { display: none; }
.sidebar.collapsed .nav-item { justify-content: center; padding: 11px 0; }
.sidebar.collapsed .nav-label, .sidebar.collapsed .nav-badge { display: none; }
.sidebar.collapsed .nav-item::before { left: -10px; }
.sidebar.collapsed .health-card { display: none; }
.sidebar.collapsed .version-row { justify-content: center; }
.sidebar.collapsed .version-row .mono { display: none; }

@media (max-width: 1024px) {
  .sidebar.collapsed { width: var(--nav-w); transform: translateX(-100%); }
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 20px 22px 18px;
  cursor: pointer;
  border-bottom: 1px solid var(--border-soft);
}
.brand-mark { filter: drop-shadow(0 0 14px rgba(139, 92, 246, 0.45)); }
.brand-text { display: flex; flex-direction: column; line-height: 1.2; }
.brand-name {
  font-family: var(--font-display);
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.04em;
}
.brand-name .accent {
  background: var(--grad);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  margin-left: 5px;
}
.brand-sub { font-size: 10.5px; color: var(--text-faint); letter-spacing: 0.14em; text-transform: uppercase; }

.nav { flex: 1; overflow-y: auto; padding: 14px 14px; }
.nav-group { margin-bottom: 18px; }
.nav-group-label {
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--text-faint);
  padding: 0 12px 8px;
}
.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  margin-bottom: 3px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  text-decoration: none;
  font-size: 13.5px;
  font-weight: 500;
  transition: all 0.2s var(--ease-out);
  overflow: hidden;
}
.nav-item::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%) scaleY(0);
  width: 3px;
  height: 55%;
  border-radius: 3px;
  background: var(--grad);
  transition: transform 0.25s var(--ease-out);
}
.nav-item:hover {
  color: var(--text);
  background: var(--surface);
}
.nav-item.active {
  color: var(--text);
  background: var(--grad-soft);
}
.nav-item.active::before { transform: translateY(-50%) scaleY(1); }
.nav-item.active svg { color: var(--primary); }
.nav-label { flex: 1; }
.nav-badge {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 99px;
  background: var(--success-soft);
  color: var(--success);
}

.sidebar-foot { padding: 14px; border-top: 1px solid var(--border-soft); }
.health-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: 13px;
  margin-bottom: 12px;
}
.health-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 9px; }
.health-title { display: flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 600; color: var(--text-soft); }
.health-title svg { color: var(--success); }
.health-pct { font-size: 12px; font-weight: 700; color: var(--success); }
.health-meta { display: flex; align-items: center; gap: 7px; margin-top: 9px; }
.health-meta .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--success); }
.version-row { display: flex; justify-content: space-between; align-items: center; }
</style>
