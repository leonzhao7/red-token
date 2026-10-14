import { ref, readonly } from 'vue'
import { login as apiLogin, logout as apiLogout, getCurrentUser } from '../api/auth'
import { setUnauthorizedHandler } from '../api/http'
import type { User } from '../api/users'

// Global reactive auth state shared across all components
const _user = ref<User | null>(null)
const _checked = ref(false)

export function useAuth() {
  async function check() {
    try {
      const res = await getCurrentUser()
      _user.value = res.user as unknown as User
    } catch {
      _user.value = null
    } finally {
      _checked.value = true
    }
  }

  async function login(username: string, password: string): Promise<string | null> {
    try {
      const res = await apiLogin({ username, password })
      _user.value = res.user as unknown as User
      return null
    } catch (e: any) {
      return e?.message || '登录失败'
    }
  }

  async function logout() {
    try { await apiLogout() } catch {}
    _user.value = null
  }

  return {
    user: readonly(_user),
    checked: readonly(_checked),
    isLoggedIn: () => _user.value !== null,
    check,
    login,
    logout
  }
}

// Install the 401 handler once so any api module can trigger redirect
export function installUnauthorizedHandler(onUnauthorized: () => void) {
  setUnauthorizedHandler(onUnauthorized)
}
