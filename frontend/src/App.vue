<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import AppSidebar from './components/AppSidebar.vue'
import AppHeader from './components/AppHeader.vue'
import ToastStack from './components/ToastStack.vue'
import ConsoleLogPanel from './components/ConsoleLogPanel.vue'
import { useAuth, installUnauthorizedHandler } from './composables/useAuth'

const collapsed = ref(false)
const route = useRoute()
const router = useRouter()
const auth = useAuth()

// Install global 401 handler
installUnauthorizedHandler(() => {
  router.push({ name: 'login', query: { redirect: route.fullPath } })
})

onMounted(() => {
  auth.check()
})
</script>

<template>
  <div v-if="!route.meta.public" class="aurora-layer">
    <span class="blob b1"></span>
    <span class="grain"></span>
  </div>

  <AppSidebar v-if="!route.meta.public" :collapsed="collapsed" @toggle="collapsed = !collapsed" />

  <div v-if="!route.meta.public" class="shell" :class="{ rail: collapsed }">
    <AppHeader @toggle-sidebar="collapsed = !collapsed" />
    <main class="main">
      <RouterView />
    </main>
  </div>

  <!-- Login page without sidebar/header -->
  <div v-else>
    <RouterView />
  </div>

  <ToastStack />
  <ConsoleLogPanel />
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

@media (max-width: 1024px) {
  .shell { margin-left: 0; }
  .main { padding: 18px 16px 8px; }
}
</style>
