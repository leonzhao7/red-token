<script setup lang="ts">
import { ref } from 'vue'
import { RouterView } from 'vue-router'
import AppSidebar from './components/AppSidebar.vue'
import AppHeader from './components/AppHeader.vue'
import ToastStack from './components/ToastStack.vue'

const collapsed = ref(false)
</script>

<template>
  <div class="aurora-layer">
    <span class="blob b1"></span>
    <span class="grain"></span>
  </div>

  <AppSidebar :collapsed="collapsed" @toggle="collapsed = !collapsed" />

  <div class="shell" :class="{ rail: collapsed }">
    <AppHeader @toggle-sidebar="collapsed = !collapsed" />
    <main class="main">
      <RouterView v-slot="{ Component }">
        <Transition name="page" mode="out-in">
          <component :is="Component" />
        </Transition>
      </RouterView>
    </main>
    <footer class="footer">
      <span class="mono" style="font-size: 11px; color: var(--text-faint)">
        NEXUS RELAY CONSOLE · AI GATEWAY v2.4.1
      </span>
      <span class="mono" style="font-size: 11px; color: var(--text-faint)">
        <span style="color: var(--success)">●</span> ALL SYSTEMS NOMINAL
      </span>
    </footer>
  </div>

  <ToastStack />
</template>

<style scoped>
.shell {
  min-height: 100vh;
  margin-left: var(--nav-w);
  display: flex;
  flex-direction: column;
  transition: margin-left 0.4s var(--ease-out);
}
.shell.rail { margin-left: var(--rail-w); }
.main {
  flex: 1;
  padding: 26px 28px 10px;
  max-width: 1500px;
  width: 100%;
  margin: 0 auto;
}
.footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 18px 28px 22px;
  border-top: 1px solid var(--border-soft);
  margin-top: 20px;
}

@media (max-width: 1024px) {
  .shell { margin-left: 0; }
  .main { padding: 18px 16px 8px; }
}
</style>
