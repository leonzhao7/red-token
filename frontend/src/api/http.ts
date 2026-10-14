export const apiBase = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')

export class ApiError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

type UnauthorizedHandler = () => void

let onUnauthorized: UnauthorizedHandler | null = null

/**
 * Registers the callback used when the backend reports an expired or missing
 * session. Set by the auth composable so this module stays dependency-free.
 */
export function setUnauthorizedHandler(handler: UnauthorizedHandler) {
  onUnauthorized = handler
}

/** Paths that legitimately return 401 and must not trigger a redirect. */
const authPaths = ['/admin/api/auth/login', '/admin/api/auth/me']

export function handleUnauthorized(path: string) {
  if (authPaths.some(authPath => path.startsWith(authPath))) return
  onUnauthorized?.()
}

export async function parseErrorMessage(response: Response, payload: unknown): Promise<string> {
  if (payload && typeof payload === 'object' && 'error' in payload) {
    return String((payload as { error?: { message?: unknown } }).error?.message || response.statusText)
  }
  if (typeof payload === 'string' && payload.trim()) return payload.trim()
  return response.statusText || `请求失败（${response.status}）`
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body != null) headers.set('Content-Type', 'application/json')

  // credentials: 'include' keeps the session cookie attached even when the API
  // is served from a different origin than the console.
  const response = await fetch(apiBase + path, { ...init, headers, credentials: 'include' })
  const contentType = response.headers.get('Content-Type') || ''
  const payload = contentType.includes('application/json')
    ? await response.json().catch(() => null)
    : await response.text().catch(() => '')

  if (!response.ok) {
    if (response.status === 401) handleUnauthorized(path)
    throw new ApiError(await parseErrorMessage(response, payload), response.status)
  }

  return payload as T
}
