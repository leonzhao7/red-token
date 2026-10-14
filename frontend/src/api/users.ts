import { request } from './http'

export interface User {
  id: number
  username: string
  enabled: boolean
  is_default_password: boolean
  last_login_at?: string
  created_at: string
  updated_at: string
}

export interface CreateUserPayload {
  username: string
  password: string
  enabled: boolean
}

export interface UpdateUserPayload {
  username: string
  enabled: boolean
}

export interface UpdatePasswordPayload {
  current_password: string
  new_password: string
}

export function listUsers() {
  return request<{ items: User[]; total: number }>('/admin/api/users')
}

export function createUser(payload: CreateUserPayload) {
  return request<{ user: User }>('/admin/api/users', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function updateUser(id: number, payload: UpdateUserPayload) {
  return request<{ user: User }>(`/admin/api/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function updateUserPassword(id: number, payload: UpdatePasswordPayload) {
  return request<{ user: User }>(`/admin/api/users/${id}/password`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function deleteUser(id: number) {
  return request<{ deleted: number }>(`/admin/api/users/${id}`, {
    method: 'DELETE'
  })
}
