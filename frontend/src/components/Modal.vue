<script setup lang="ts">
import { computed } from 'vue'
import { X } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    subtitle?: string
    width?: string
    icon?: any
  }>(),
  { width: '560px' }
)

const emit = defineEmits<{ close: [] }>()

const style = computed(() => ({ maxWidth: 'min(92vw, ' + props.width + ')' }))

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="modal-mask" @click.self="emit('close')" @keydown="onKey">
        <div class="modal" :style="style" role="dialog" aria-modal="true">
          <div class="modal-glow"></div>
          <header class="modal-head">
            <div class="modal-title-wrap">
              <span v-if="icon" class="modal-icon"><component :is="icon" :size="18" /></span>
              <div>
                <h3 class="modal-title">{{ title }}</h3>
                <p v-if="subtitle" class="modal-sub">{{ subtitle }}</p>
              </div>
            </div>
            <button class="icon-btn modal-close" @click="emit('close')" aria-label="关闭">
              <X :size="16" />
            </button>
          </header>
          <div class="modal-body">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="modal-foot">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-mask {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(2, 2, 6, 0.62);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  padding: 24px;
}
.modal {
  position: relative;
  width: 100%;
  background: var(--glass-strong);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}
.modal-glow {
  position: absolute;
  top: -80px;
  left: 50%;
  transform: translateX(-50%);
  width: 320px;
  height: 160px;
  background: radial-gradient(ellipse, var(--aurora-2), transparent 70%);
  pointer-events: none;
}
.modal-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 22px 24px 0;
  gap: 16px;
  position: relative;
  z-index: 1;
}
.modal-title-wrap { display: flex; align-items: center; gap: 13px; }
.modal-icon {
  width: 38px;
  height: 38px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--grad-soft);
  color: var(--primary);
  border: 1px solid var(--border-strong);
  flex: none;
}
.modal-title { font-size: 17px; }
.modal-sub { font-size: 12px; color: var(--text-faint); margin-top: 2px; }
.modal-body { padding: 20px 24px; overflow-y: auto; }
.modal-foot {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 16px 24px 20px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}

.modal-close {
  color: var(--text);
}
.modal-close:hover {
  color: var(--text);
  background: var(--surface-3);
}

.modal-enter-active, .modal-leave-active { transition: opacity 0.25s ease; }
.modal-enter-active .modal, .modal-leave-active .modal { transition: transform 0.3s var(--ease-spring), opacity 0.25s ease; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal, .modal-leave-to .modal { transform: translateY(22px) scale(0.96); opacity: 0; }
</style>
