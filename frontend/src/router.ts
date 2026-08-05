import { createRouter, createWebHashHistory } from 'vue-router'
import Dashboard from './views/Dashboard.vue'
import ApiKeys from './views/ApiKeys.vue'
import Proxies from './views/Proxies.vue'
import Relays from './views/Relays.vue'
import Logs from './views/Logs.vue'
import Settings from './views/Settings.vue'

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: Dashboard, meta: { title: '总览', group: '运营', subtitle: '网关实时总览 · 今日已完成 1,024,839 次请求路由，系统运转平稳' } },
    { path: '/keys', name: 'keys', component: ApiKeys, meta: { title: 'API Keys', group: '接入', subtitle: '管理客户端访问密钥，按密钥维度限制可用模型与速率' } },
    { path: '/proxies', name: 'proxies', component: Proxies, meta: { title: '代理服务器', group: '接入', subtitle: '统一管理链路出口代理，支持 HTTP / HTTPS / SOCKS5 协议' } },
    { path: '/relays', name: 'relays', component: Relays, meta: { title: '中转站', group: '接入', subtitle: '统一管理供应商账户、余额、签到与 API Key' } },
    { path: '/logs', name: 'logs', component: Logs, meta: { title: '使用日志', group: '观测', subtitle: '实时记录每条模型调用，支持多维筛选与导出' } },
    { path: '/settings', name: 'settings', component: Settings, meta: { title: '系统配置', group: '系统', subtitle: '管理网关运行参数、安全策略与通知渠道' } }
  ]
})
