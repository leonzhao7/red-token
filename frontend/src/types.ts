export type Status = 'active' | 'disabled' | 'expired' | 'limited'
export type RelayStatus = 'active' | 'disabled'

export interface ClientKey {
  id: string
  name: string
  key: string
  models: string[]
  status: Status
  totalTokens: number
  usedTokens: number
  tokenInput: number
  tokenCache: number
  tokenOutput: number
  reqSuccess: number
  reqFail: number
  rateLimit: number
  createdAt: string
  lastUsed: string
  expiresAt: string
}

export type ProxyProtocol = 'http' | 'socks5' | 'https'
export type ProxyAuth = 'none' | 'username' | 'token'

export interface ProxyServer {
  id: string
  name: string
  protocol: ProxyProtocol
  host: string
  port: number
  auth: ProxyAuth
  username: string
  password: string
  location: string
  latency: number
  successRate: number
  status: Status
  usedBy: number
}

export type PlatformType = 'OpenAI' | 'Anthropic' | 'Gemini' | 'Azure' | 'Claude' | 'DeepSeek' | 'Custom'

export interface RelayModel {
  id: string
  name: string
  group: string
  priceIn: number
  priceOut: number
  billingType?: 'token' | 'fixed'
}

export interface RelayKey {
  id: string
  name: string
  group?: string
  username: string
  key: string
  models: string[]
  modelMap: Record<string, string>
  usedTokens: number
}

export interface Relay {
  id: string
  name: string
  url: string
  platform: PlatformType
  status: RelayStatus
  balance: number
  used: number
  username: string
  checkinAt: string
  proxyId: string
  models: RelayModel[]
  keys: RelayKey[]
}

export type LogStatus = 'success' | 'error' | 'timeout' | 'ratelimit'

export interface UsageLog {
  id: string
  time: string
  keyName: string
  key: string
  model: string
  relay: string
  promptTokens: number
  completionTokens: number
  totalTokens: number
  latency: number
  status: LogStatus
  cost: number
}

export interface ActivityEvent {
  id: number
  type: 'key' | 'relay' | 'proxy' | 'system'
  title: string
  detail: string
  time: string
  tone: 'success' | 'warning' | 'danger' | 'info'
}
