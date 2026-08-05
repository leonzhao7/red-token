import type { ClientKey, ProxyServer, Relay, RelayModel, UsageLog, ActivityEvent } from '../types'

export const MODELS = [
  'gpt-4o',
  'gpt-4o-mini',
  'gpt-4-turbo',
  'claude-3-5-sonnet',
  'claude-3-5-haiku',
  'gemini-1.5-pro',
  'gemini-1.5-flash',
  'deepseek-v3',
  'deepseek-r1',
  'qwen-max',
  'qwen-plus',
  'glm-4-plus',
  'moonshot-v1-32k',
  'llama-3-70b'
] as const

export const MODELS_INFO: Record<string, { color: string; group: string }> = {
  'gpt-4o': { color: 'cyan', group: 'OpenAI' },
  'gpt-4o-mini': { color: 'cyan', group: 'OpenAI' },
  'gpt-4-turbo': { color: 'cyan', group: 'OpenAI' },
  'claude-3-5-sonnet': { color: 'pink', group: 'Anthropic' },
  'claude-3-5-haiku': { color: 'pink', group: 'Anthropic' },
  'gemini-1.5-pro': { color: 'blue', group: 'Google' },
  'gemini-1.5-flash': { color: 'blue', group: 'Google' },
  'deepseek-v3': { color: 'violet', group: 'DeepSeek' },
  'deepseek-r1': { color: 'violet', group: 'DeepSeek' },
  'qwen-max': { color: 'amber', group: 'Alibaba' },
  'qwen-plus': { color: 'amber', group: 'Alibaba' },
  'glm-4-plus': { color: 'green', group: 'Zhipu' },
  'moonshot-v1-32k': { color: 'rose', group: 'Moonshot' },
  'llama-3-70b': { color: 'neutral', group: 'Meta' }
}

export const PLATFORMS = ['OpenAI', 'Anthropic', 'Gemini', 'Azure', 'Claude', 'DeepSeek', 'Custom'] as const

export const MODEL_CATALOG: RelayModel[] = [
  { id: 'm1', name: 'gpt-4o', group: 'OpenAI', priceIn: 2.5, priceOut: 10 },
  { id: 'm2', name: 'gpt-4o-mini', group: 'OpenAI', priceIn: 0.15, priceOut: 0.6 },
  { id: 'm3', name: 'gpt-4-turbo', group: 'OpenAI', priceIn: 10, priceOut: 30 },
  { id: 'm4', name: 'claude-3-5-sonnet', group: 'Anthropic', priceIn: 3, priceOut: 15 },
  { id: 'm5', name: 'claude-3-5-haiku', group: 'Anthropic', priceIn: 0.8, priceOut: 4 },
  { id: 'm6', name: 'gemini-1.5-pro', group: 'Google', priceIn: 1.25, priceOut: 5 },
  { id: 'm7', name: 'gemini-1.5-flash', group: 'Google', priceIn: 0.075, priceOut: 0.3 },
  { id: 'm8', name: 'deepseek-v3', group: 'DeepSeek', priceIn: 0.27, priceOut: 1.1 },
  { id: 'm9', name: 'deepseek-r1', group: 'DeepSeek', priceIn: 0.55, priceOut: 2.19 },
  { id: 'm10', name: 'qwen-max', group: 'Alibaba', priceIn: 1.6, priceOut: 6.4 },
  { id: 'm11', name: 'qwen-plus', group: 'Alibaba', priceIn: 0.8, priceOut: 2 },
  { id: 'm12', name: 'glm-4-plus', group: 'Zhipu', priceIn: 0.5, priceOut: 2 },
  { id: 'm13', name: 'moonshot-v1-32k', group: 'Moonshot', priceIn: 0.6, priceOut: 2.4 },
  { id: 'm14', name: 'llama-3-70b', group: 'Meta', priceIn: 0.9, priceOut: 0.9 },
  { id: 'm15', name: 'claude-opus-4.6', group: 'Anthropic', priceIn: 15, priceOut: 75 }
]

function pickModels(names: string[]): RelayModel[] {
  return MODEL_CATALOG.filter((m) => names.includes(m.name))
}

export const apiKeys: ClientKey[] = [
  {
    id: 'k1',
    name: '生产环境 · Web App',
    key: 'nx-sk-prod-9f2c4a7d1e8b',
    models: ['gpt-4o', 'gpt-4o-mini', 'claude-3-5-sonnet', 'deepseek-v3'],
    status: 'active',
    totalTokens: 50_000_000,
    usedTokens: 31_240_500,
    tokenInput: 19_500_000,
    tokenCache: 3_800_000,
    tokenOutput: 7_940_500,
    reqSuccess: 182_003,
    reqFail: 2_200,
    rateLimit: 100,
    createdAt: '2026-03-12',
    lastUsed: '12s 前',
    expiresAt: '2027-03-12'
  },
  {
    id: 'k2',
    name: '测试环境 · Staging',
    key: 'nx-sk-test-3b8d1c5f9a20',
    models: ['gpt-4o-mini', 'gemini-1.5-flash'],
    status: 'active',
    totalTokens: 5_000_000,
    usedTokens: 1_210_800,
    tokenInput: 720_000,
    tokenCache: 120_000,
    tokenOutput: 370_800,
    reqSuccess: 31_400,
    reqFail: 709,
    rateLimit: 30,
    createdAt: '2026-04-02',
    lastUsed: '3m 前',
    expiresAt: '2026-12-02'
  },
  {
    id: 'k3',
    name: '数据分析 Pipeline',
    key: 'nx-sk-data-7e2a9c0b4f61',
    models: ['deepseek-r1', 'qwen-max'],
    status: 'limited',
    totalTokens: 20_000_000,
    usedTokens: 19_872_100,
    tokenInput: 12_300_000,
    tokenCache: 1_900_000,
    tokenOutput: 5_672_100,
    reqSuccess: 95_100,
    reqFail: 1_350,
    rateLimit: 60,
    createdAt: '2026-01-20',
    lastUsed: '2h 前',
    expiresAt: '2026-09-20'
  },
  {
    id: 'k4',
    name: '移动端 App',
    key: 'nx-sk-mob-5c7f3d9a2e14',
    models: ['gpt-4o-mini', 'gemini-1.5-flash', 'glm-4-plus'],
    status: 'active',
    totalTokens: 100_000_000,
    usedTokens: 44_552_300,
    tokenInput: 27_800_000,
    tokenCache: 5_100_000,
    tokenOutput: 11_652_300,
    reqSuccess: 506_300,
    reqFail: 6_584,
    rateLimit: 200,
    createdAt: '2025-11-08',
    lastUsed: '45s 前',
    expiresAt: '2026-11-08'
  },
  {
    id: 'k5',
    name: '内部工具 · 运维',
    key: 'nx-sk-ops-1d4e8b2f7c09',
    models: ['claude-3-5-sonnet', 'gpt-4o'],
    status: 'disabled',
    totalTokens: 10_000_000,
    usedTokens: 2_104_900,
    tokenInput: 1_300_000,
    tokenCache: 210_000,
    tokenOutput: 594_900,
    reqSuccess: 14_900,
    reqFail: 332,
    rateLimit: 50,
    createdAt: '2026-02-14',
    lastUsed: '6d 前',
    expiresAt: '2026-08-14'
  },
  {
    id: 'k6',
    name: '第三方客户 · A公司',
    key: 'nx-sk-3rd-6f0e9c3a8b27',
    models: ['gpt-4o', 'deepseek-v3', 'qwen-max'],
    status: 'expired',
    totalTokens: 30_000_000,
    usedTokens: 30_000_000,
    tokenInput: 18_400_000,
    tokenCache: 3_200_000,
    tokenOutput: 8_400_000,
    reqSuccess: 146_800,
    reqFail: 1_932,
    rateLimit: 80,
    createdAt: '2025-08-01',
    lastUsed: '21d 前',
    expiresAt: '2026-05-01'
  }
]

export const proxies: ProxyServer[] = [
  {
    id: 'p1',
    name: 'US · 主出口',
    protocol: 'socks5',
    host: 'us-east-01.proxy.internal',
    port: 1080,
    auth: 'username',
    username: 'relay_us',
    password: 'Us@east#2026',
    location: '美国 · 弗吉尼亚',
    latency: 128,
    successRate: 99.2,
    status: 'active',
    usedBy: 4
  },
  {
    id: 'p2',
    name: 'SG · 东南亚',
    protocol: 'http',
    host: 'sg-01.proxy.internal',
    port: 8080,
    auth: 'none',
    username: '',
    password: '',
    location: '新加坡',
    latency: 96,
    successRate: 98.7,
    status: 'active',
    usedBy: 2
  },
  {
    id: 'p3',
    name: 'JP · 东京',
    protocol: 'http',
    host: 'jp-02.proxy.internal',
    port: 3128,
    auth: 'username',
    username: 'relay_jp',
    password: 'Jp@Tokyo#26',
    location: '日本 · 东京',
    latency: 84,
    successRate: 99.5,
    status: 'active',
    usedBy: 3
  },
  {
    id: 'p4',
    name: 'EU · 法兰克福',
    protocol: 'https',
    host: 'eu-01.proxy.internal',
    port: 443,
    auth: 'token',
    username: '',
    password: '',
    location: '德国 · 法兰克福',
    latency: 0,
    successRate: 0,
    status: 'disabled',
    usedBy: 0
  },
  {
    id: 'p5',
    name: 'HK · 备用出口',
    protocol: 'socks5',
    host: 'hk-01.proxy.internal',
    port: 1080,
    auth: 'none',
    username: '',
    password: '',
    location: '中国香港',
    latency: 152,
    successRate: 96.8,
    status: 'active',
    usedBy: 1
  }
]

export const relays: Relay[] = [
  {
    id: 'r1',
    name: '官方中转 · OpenAI',
    url: 'https://api.openai.com/v1',
    platform: 'OpenAI',
    status: 'active',
    balance: 285.4,
    used: 1208.6,
    username: 'openai-master@nexus',
    checkinAt: '2026-08-02 08:00',
    proxyId: 'p1',
    models: pickModels(['gpt-4o', 'gpt-4o-mini', 'gpt-4-turbo', 'claude-3-5-sonnet', 'claude-3-5-haiku', 'gemini-1.5-pro', 'gemini-1.5-flash', 'deepseek-v3', 'deepseek-r1', 'qwen-max', 'qwen-plus', 'glm-4-plus']),
    keys: [
      { id: 'rk1', name: '主用 Key', username: 'sk-proj-1a2b3c4d', key: 'sk-proj-1a2b3c4d5e6f7g8h9i0j', models: ['gpt-4o', 'gpt-4o-mini'], modelMap: { 'gpt-4o': 'gpt-4o-2024-11-20' }, usedTokens: 126_500_000 },
      { id: 'rk2', name: '备用 Key', username: 'sk-proj-9z8y7x6w', key: 'sk-proj-9z8y7x6w5v4u3t2s1r0q', models: ['gpt-4-turbo'], modelMap: {}, usedTokens: 3_200_000 }
    ]
  },
  {
    id: 'r2',
    name: 'Claude 代理池',
    url: 'https://claude-proxy.agg.io/v1',
    platform: 'Anthropic',
    status: 'active',
    balance: 96.2,
    used: 1542.1,
    username: 'claude-pool-user',
    checkinAt: '2026-08-02 08:00',
    proxyId: 'p2',
    models: pickModels(['claude-3-5-sonnet', 'claude-3-5-haiku', 'claude-opus-4.6', 'gpt-4o', 'gpt-4o-mini', 'gpt-4-turbo', 'deepseek-v3', 'deepseek-r1', 'qwen-max', 'gemini-1.5-pro', 'gemini-1.5-flash', 'glm-4-plus', 'llama-3-70b']),
    keys: [
      { id: 'rk3', name: '主用 Key', username: 'sk-ant-5c5d5e5f', key: 'sk-ant-5c5d5e5f6g6h6i6j6k6l', models: ['claude-opus-4.6', 'claude-3-5-sonnet'], modelMap: { 'claude-opus-4.6': 'claude-opus-4-8' }, usedTokens: 94_800_000 },
      { id: 'rk4', name: '降级 Key', username: 'sk-ant-1a1b1c1d', key: 'sk-ant-1a1b1c1d2e2f2g2h2i2j', models: ['gpt-4o'], modelMap: {}, usedTokens: 12_400_000 }
    ]
  },
  {
    id: 'r3',
    name: 'Gemini 官方',
    url: 'https://generativelanguage.googleapis.com',
    platform: 'Gemini',
    status: 'active',
    balance: 320.0,
    used: 623.5,
    username: 'gemini-admin@gcp',
    checkinAt: '2026-08-02 08:00',
    proxyId: 'p3',
    models: pickModels(['gemini-1.5-pro', 'gemini-1.5-flash', 'gpt-4o-mini', 'claude-3-5-haiku', 'deepseek-v3', 'qwen-plus', 'glm-4-plus', 'moonshot-v1-32k', 'llama-3-70b', 'qwen-max', 'deepseek-r1']),
    keys: [
      { id: 'rk5', name: '唯一 Key', username: 'AIzaSy-9f8e7d6c', key: 'AIzaSy9f8e7d6c5b4a3z2y1x0wv', models: ['gemini-1.5-pro', 'gemini-1.5-flash'], modelMap: { 'gemini-1.5-flash': 'gemini-1.5-flash-001' }, usedTokens: 45_600_000 }
    ]
  },
  {
    id: 'r4',
    name: '深度求索 · DeepSeek',
    url: 'https://api.deepseek.com/v1',
    platform: 'DeepSeek',
    status: 'active',
    balance: 18.4,
    used: 715.0,
    username: 'ds-user-8842',
    checkinAt: '2026-08-02 08:00',
    proxyId: 'p5',
    models: pickModels(['deepseek-v3', 'deepseek-r1', 'gpt-4o-mini', 'qwen-max', 'qwen-plus', 'glm-4-plus', 'claude-3-5-haiku', 'gemini-1.5-flash', 'moonshot-v1-32k', 'llama-3-70b']),
    keys: [
      { id: 'rk6', name: '主用 Key', username: 'sk-8f2d...', key: 'sk-8f2d5a1c4e7b9f0d3a2c8e1b', models: ['deepseek-v3', 'deepseek-r1'], modelMap: { 'deepseek-r1': 'deepseek-reasoner' }, usedTokens: 78_300_000 }
    ]
  },
  {
    id: 'r5',
    name: '通义千问 · 阿里云',
    url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    platform: 'Custom',
    status: 'active',
    balance: 150.8,
    used: 289.0,
    username: 'aliyun-ram:relay-svc',
    checkinAt: '2026-08-02 07:55',
    proxyId: 'p2',
    models: pickModels(['qwen-max', 'qwen-plus', 'deepseek-v3', 'deepseek-r1', 'glm-4-plus', 'moonshot-v1-32k', 'gpt-4o-mini', 'claude-3-5-haiku', 'gemini-1.5-flash', 'llama-3-70b']),
    keys: [
      { id: 'rk7', name: '主用 Key', username: 'sk-3c7e...', key: 'sk-3c7e9a2f5d1b8e4c0a6f3d2b', models: ['qwen-max', 'qwen-plus'], modelMap: { 'qwen-plus': 'qwen-plus-latest' }, usedTokens: 23_900_000 },
      { id: 'rk8', name: '备用 Key', username: 'sk-5a5b5c5d', key: 'sk-5a5b5c5d6e6f7a7b8c8d9e', models: ['qwen-max'], modelMap: {}, usedTokens: 5_100_000 }
    ]
  },
  {
    id: 'r6',
    name: '智谱 · GLM',
    url: 'https://open.bigmodel.cn/api/paas/v4',
    platform: 'Custom',
    status: 'disabled',
    balance: 0.3,
    used: 299.8,
    username: 'zhipu-acct-5521',
    checkinAt: '',
    proxyId: 'p3',
    models: pickModels(['glm-4-plus', 'qwen-plus', 'deepseek-v3', 'moonshot-v1-32k', 'llama-3-70b', 'gpt-4o-mini', 'claude-3-5-haiku']),
    keys: [
      { id: 'rk9', name: '唯一 Key', username: 'sk-9a4e...', key: 'sk-9a4e7c2f8b1d5e9a0c3f6d8b', models: ['glm-4-plus'], modelMap: {}, usedTokens: 10_200_000 }
    ]
  },
  {
    id: 'r7',
    name: 'Moonshot Kimi',
    url: 'https://api.moonshot.cn/v1',
    platform: 'Custom',
    status: 'active',
    balance: 200.0,
    used: 124.0,
    username: 'moonshot-user-7719',
    checkinAt: '2026-08-02 08:00',
    proxyId: 'p1',
    models: pickModels(['moonshot-v1-32k', 'glm-4-plus', 'qwen-max', 'deepseek-v3', 'claude-3-5-haiku', 'gemini-1.5-flash', 'gpt-4o-mini', 'llama-3-70b', 'qwen-plus', 'deepseek-r1']),
    keys: [
      { id: 'rk10', name: '主用 Key', username: 'sk-e5b2...', key: 'sk-e5b2f8d1a4c7b9e0d3f6a2c', models: ['moonshot-v1-32k', 'qwen-plus'], modelMap: {}, usedTokens: 8_700_000 }
    ]
  }
]

export const usageLogs: UsageLog[] = [
  { id: 'log-001', time: '2026-08-02 14:32:08', keyName: '移动端 App', key: 'nx-sk-mob-5c7f', model: 'gpt-4o-mini', relay: '官方中转 · OpenAI', promptTokens: 842, completionTokens: 512, totalTokens: 1354, latency: 186, status: 'success', cost: 0.0042 },
  { id: 'log-002', time: '2026-08-02 14:31:55', keyName: '生产环境 · Web App', key: 'nx-sk-prod-9f2c', model: 'claude-3-5-sonnet', relay: 'Claude 代理池', promptTokens: 2210, completionTokens: 1840, totalTokens: 4050, latency: 431, status: 'success', cost: 0.0481 },
  { id: 'log-003', time: '2026-08-02 14:31:12', keyName: '第三方客户 · A公司', key: 'nx-sk-3rd-6f0e', model: 'gpt-4o', relay: '官方中转 · OpenAI', promptTokens: 1204, completionTokens: 890, totalTokens: 2094, latency: 201, status: 'ratelimit', cost: 0 },
  { id: 'log-004', time: '2026-08-02 14:30:47', keyName: '数据分析 Pipeline', key: 'nx-sk-data-7e2a', model: 'deepseek-r1', relay: '深度求索 · DeepSeek', promptTokens: 5120, completionTokens: 3208, totalTokens: 8328, latency: 1240, status: 'success', cost: 0.0210 },
  { id: 'log-005', time: '2026-08-02 14:30:02', keyName: '移动端 App', key: 'nx-sk-mob-5c7f', model: 'gemini-1.5-flash', relay: 'Gemini 官方', promptTokens: 640, completionTokens: 220, totalTokens: 860, latency: 94, status: 'success', cost: 0.0011 },
  { id: 'log-006', time: '2026-08-02 14:29:31', keyName: '内部工具 · 运维', key: 'nx-sk-ops-1d4e', model: 'gpt-4o', relay: '官方中转 · OpenAI', promptTokens: 880, completionTokens: 76, totalTokens: 956, latency: 168, status: 'success', cost: 0.0054 },
  { id: 'log-007', time: '2026-08-02 14:28:58', keyName: '生产环境 · Web App', key: 'nx-sk-prod-9f2c', model: 'deepseek-v3', relay: '深度求索 · DeepSeek', promptTokens: 3120, completionTokens: 2456, totalTokens: 5576, latency: 342, status: 'timeout', cost: 0 },
  { id: 'log-008', time: '2026-08-02 14:28:11', keyName: '第三方客户 · A公司', key: 'nx-sk-3rd-6f0e', model: 'qwen-max', relay: '通义千问 · 阿里云', promptTokens: 980, completionTokens: 410, totalTokens: 1390, latency: 152, status: 'success', cost: 0.0096 },
  { id: 'log-009', time: '2026-08-02 14:27:43', keyName: '移动端 App', key: 'nx-sk-mob-5c7f', model: 'glm-4-plus', relay: '智谱 · GLM', promptTokens: 420, completionTokens: 180, totalTokens: 600, latency: 0, status: 'error', cost: 0 },
  { id: 'log-010', time: '2026-08-02 14:27:02', keyName: '测试环境 · Staging', key: 'nx-sk-test-3b8d', model: 'gpt-4o-mini', relay: '官方中转 · OpenAI', promptTokens: 340, completionTokens: 120, totalTokens: 460, latency: 132, status: 'success', cost: 0.0004 },
  { id: 'log-011', time: '2026-08-02 14:26:36', keyName: '数据分析 Pipeline', key: 'nx-sk-data-7e2a', model: 'qwen-max', relay: '通义千问 · 阿里云', promptTokens: 2040, completionTokens: 1510, totalTokens: 3550, latency: 288, status: 'success', cost: 0.0204 },
  { id: 'log-012', time: '2026-08-02 14:25:59', keyName: '生产环境 · Web App', key: 'nx-sk-prod-9f2c', model: 'claude-3-5-haiku', relay: 'Claude 代理池', promptTokens: 560, completionTokens: 320, totalTokens: 880, latency: 389, status: 'success', cost: 0.0028 },
  { id: 'log-013', time: '2026-08-02 14:25:21', keyName: '移动端 App', key: 'nx-sk-mob-5c7f', model: 'gpt-4o-mini', relay: '官方中转 · OpenAI', promptTokens: 1100, completionTokens: 780, totalTokens: 1880, latency: 158, status: 'success', cost: 0.0058 },
  { id: 'log-014', time: '2026-08-02 14:24:40', keyName: '第三方客户 · A公司', key: 'nx-sk-3rd-6f0e', model: 'deepseek-v3', relay: '深度求索 · DeepSeek', promptTokens: 1560, completionTokens: 900, totalTokens: 2460, latency: 265, status: 'success', cost: 0.0042 },
  { id: 'log-015', time: '2026-08-02 14:24:03', keyName: '测试环境 · Staging', key: 'nx-sk-test-3b8d', model: 'gemini-1.5-flash', relay: 'Gemini 官方', promptTokens: 230, completionTokens: 90, totalTokens: 320, latency: 88, status: 'success', cost: 0.0003 },
  { id: 'log-016', time: '2026-08-02 14:23:30', keyName: '移动端 App', key: 'nx-sk-mob-5c7f', model: 'gpt-4o-mini', relay: '官方中转 · OpenAI', promptTokens: 1900, completionTokens: 1400, totalTokens: 3300, latency: 172, status: 'success', cost: 0.0102 },
  { id: 'log-017', time: '2026-08-02 14:22:55', keyName: '数据分析 Pipeline', key: 'nx-sk-data-7e2a', model: 'deepseek-r1', relay: '深度求索 · DeepSeek', promptTokens: 4430, completionTokens: 2890, totalTokens: 7320, latency: 1120, status: 'success', cost: 0.0183 },
  { id: 'log-018', time: '2026-08-02 14:22:18', keyName: '生产环境 · Web App', key: 'nx-sk-prod-9f2c', model: 'gpt-4o', relay: '官方中转 · OpenAI', promptTokens: 2600, completionTokens: 150, totalTokens: 2750, latency: 234, status: 'success', cost: 0.0210 },
  { id: 'log-019', time: '2026-08-02 14:21:44', keyName: '内部工具 · 运维', key: 'nx-sk-ops-1d4e', model: 'claude-3-5-sonnet', relay: 'Claude 代理池', promptTokens: 720, completionTokens: 480, totalTokens: 1200, latency: 452, status: 'error', cost: 0 },
  { id: 'log-020', time: '2026-08-02 14:21:02', keyName: '第三方客户 · A公司', key: 'nx-sk-3rd-6f0e', model: 'qwen-max', relay: '通义千问 · 阿里云', promptTokens: 890, completionTokens: 360, totalTokens: 1250, latency: 148, status: 'success', cost: 0.0086 }
]

export const activityFeed: ActivityEvent[] = [
  { id: 1, type: 'relay', title: '中转站签到成功', detail: 'Claude 代理池 · 每日额度已刷新 +50M tokens', time: '08:00', tone: 'success' },
  { id: 2, type: 'key', title: '新建 API Key', detail: '第三方客户 · A公司 创建了访问密钥', time: '09:42', tone: 'info' },
  { id: 3, type: 'relay', title: '中转站额度告警', detail: '智谱 · GLM 额度剩余不足 0.1%', time: '10:15', tone: 'warning' },
  { id: 4, type: 'proxy', title: '代理节点延迟告警', detail: 'HK · 备用出口 延迟升高至 152ms', time: '11:03', tone: 'warning' },
  { id: 5, type: 'system', title: '系统自动故障转移', detail: 'Claude 代理池 请求降级至 Gemini 官方', time: '11:47', tone: 'info' },
  { id: 6, type: 'relay', title: '中转站同步完成', detail: '深度求索 · DeepSeek 模型列表已更新', time: '12:20', tone: 'success' },
  { id: 7, type: 'key', title: 'API Key 额度用尽', detail: '数据分析 Pipeline 已达 99.4% 使用率', time: '13:05', tone: 'warning' }
]
