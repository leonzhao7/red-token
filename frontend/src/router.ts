import { createRouter, createWebHashHistory } from 'vue-router'
import { useAuth } from './composables/useAuth'
import Login from './views/Login.vue'
import Dashboard from './views/Dashboard.vue'
import ApiKeys from './views/ApiKeys.vue'
import Proxies from './views/Proxies.vue'
import Relays from './views/Relays.vue'
import Logs from './views/Logs.vue'
import Settings from './views/Settings.vue'
import Workflows from './views/Workflows.vue'
import Users from './views/Users.vue'

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login, meta: { title: '登录', public: true } },
    { path: '/', name: 'dashboard', component: Dashboard, meta: { title: '总览', group: '运营' } },
    { path: '/keys', name: 'keys', component: ApiKeys, meta: { title: 'API Keys', group: '接入' } },
    { path: '/proxies', name: 'proxies', component: Proxies, meta: { title: '代理服务器', group: '接入' } },
    { path: '/relays', name: 'relays', component: Relays, meta: { title: '中转站', group: '接入' } },
    { path: '/logs', name: 'logs', component: Logs, meta: { title: '使用日志', group: '观测' } },
    { path: '/settings', name: 'settings', component: Settings, meta: { title: '系统配置', group: '系统' } },
    { path: '/settings/workflows', name: 'workflows', component: Workflows, meta: { title: '工作流', group: '系统' } },
    { path: '/settings/users', name: 'users', component: Users, meta: { title: '用户管理', group: '系统' } }
  ]
})

router.beforeEach(async (to) => {
  const auth = useAuth()

  // Wait for initial auth check
  if (!auth.checked.value) {
    await auth.check()
  }

  // Redirect to login if not authenticated and trying to access protected route
  if (!to.meta.public && !auth.isLoggedIn()) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }

  // Redirect to dashboard if authenticated and trying to access login
  if (to.name === 'login' && auth.isLoggedIn()) {
    return { name: 'dashboard' }
  }
})
