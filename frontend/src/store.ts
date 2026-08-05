import { reactive } from 'vue'
import type { ClientKey, ProxyServer, Relay, UsageLog } from './types'
import { apiKeys, proxies, relays, usageLogs } from './data/mock'

export const store = reactive({
  apiKeys: [...apiKeys] as ClientKey[],
  proxies: [...proxies] as ProxyServer[],
  relays: [...relays] as Relay[],
  usageLogs: [...usageLogs] as UsageLog[],

  addKey(key: ClientKey) {
    this.apiKeys.unshift(key)
  },
  removeKey(id: string) {
    this.apiKeys = this.apiKeys.filter((k) => k.id !== id)
  },
  updateKey(id: string, patch: Partial<ClientKey>) {
    const idx = this.apiKeys.findIndex((k) => k.id === id)
    if (idx >= 0) Object.assign(this.apiKeys[idx], patch)
  },

  addProxy(p: ProxyServer) {
    this.proxies.unshift(p)
  },
  removeProxy(id: string) {
    this.proxies = this.proxies.filter((p) => p.id !== id)
  },
  updateProxy(id: string, patch: Partial<ProxyServer>) {
    const idx = this.proxies.findIndex((p) => p.id === id)
    if (idx >= 0) Object.assign(this.proxies[idx], patch)
  },

  addRelay(r: Relay) {
    this.relays.unshift(r)
  },
  removeRelay(id: string) {
    this.relays = this.relays.filter((r) => r.id !== id)
  },
  updateRelay(id: string, patch: Partial<Relay>) {
    const idx = this.relays.findIndex((r) => r.id === id)
    if (idx >= 0) Object.assign(this.relays[idx], patch)
  }
})

let nextLogId = usageLogs.length + 1
export function prependLog(log: Omit<UsageLog, 'id'>) {
  store.usageLogs.unshift({ id: `log-${String(nextLogId++).padStart(3, '0')}`, ...log })
}

export const proxyName = (id: string) => store.proxies.find((p) => p.id === id)?.name ?? '直连'
