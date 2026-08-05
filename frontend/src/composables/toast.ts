import { reactive } from 'vue'
import type { Component } from 'vue'

export interface Toast {
  id: number
  title: string
  detail?: string
  tone: 'success' | 'danger' | 'warning' | 'info'
}

export const toasts = reactive<Toast[]>([])

let toastId = 1

export function toast(title: string, detail?: string, tone: Toast['tone'] = 'success', duration = 3600) {
  const id = toastId++
  toasts.push({ id, title, detail, tone })
  setTimeout(() => dismiss(id), duration)
}

export function dismiss(id: number) {
  const idx = toasts.findIndex((t) => t.id === id)
  if (idx >= 0) toasts.splice(idx, 1)
}
