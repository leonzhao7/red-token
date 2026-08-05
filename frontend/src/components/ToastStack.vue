<script setup lang="ts">
import { CheckCircle2, AlertTriangle, XCircle, Info, X } from 'lucide-vue-next'
import { toasts, dismiss } from '../composables/toast'

const icons = {
  success: CheckCircle2,
  danger: XCircle,
  warning: AlertTriangle,
  info: Info
}
</script>

<template>
  <Teleport to="body">
    <div class="toast-stack" aria-live="polite">
      <TransitionGroup name="toast">
        <div v-for="t in toasts" :key="t.id" class="toast" :class="t.tone">
          <span class="toast-ico"><component :is="icons[t.tone]" :size="17" /></span>
          <div class="toast-txt">
            <strong>{{ t.title }}</strong>
            <p v-if="t.detail">{{ t.detail }}</p>
          </div>
          <button class="toast-x" @click="dismiss(t.id)" aria-label="关闭">
            <X :size="13" />
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-stack {
  position: fixed;
  top: 20px;
  right: 20px;
  z-index: 2000;
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: min(360px, calc(100vw - 32px));
  pointer-events: none;
}
.toast {
  pointer-events: auto;
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px 14px;
  background: var(--glass-strong);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-md);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
}
.toast-ico {
  width: 30px;
  height: 30px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}
.toast.success .toast-ico { background: var(--success-soft); color: var(--success); }
.toast.danger .toast-ico { background: var(--danger-soft); color: var(--danger); }
.toast.warning .toast-ico { background: var(--warning-soft); color: var(--warning); }
.toast.info .toast-ico { background: var(--info-soft); color: var(--info); }
.toast-txt { flex: 1; }
.toast-txt strong { font-size: 13px; font-weight: 600; }
.toast-txt p { font-size: 12px; color: var(--text-muted); margin-top: 1px; }
.toast-x {
  background: none;
  border: none;
  color: var(--text-faint);
  cursor: pointer;
  padding: 3px;
  border-radius: 6px;
}
.toast-x:hover { color: var(--text); background: var(--surface-3); }

.toast-enter-active, .toast-leave-active { transition: all 0.35s var(--ease-out); }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(30px); }
</style>
