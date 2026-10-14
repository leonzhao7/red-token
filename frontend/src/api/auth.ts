import { request } from './http'
import type { User } from './users'

export interface LoginPayload {
  username: string
  password: string
}

export interface UserResponse {
  user: User
}

export async function login(payload: LoginPayload) {
  return request<UserResponse>('/admin/api/auth/login', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export async function logout() {
  return request('/admin/api/auth/logout', { method: 'POST' })
}

export async function getCurrentUser() {
  return request<UserResponse>('/admin/api/auth/me')
}
